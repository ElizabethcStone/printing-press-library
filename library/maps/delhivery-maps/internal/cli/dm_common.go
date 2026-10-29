// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

// dm_common.go is the shared plumbing for the hand-written address, trip,
// coverage and fleet commands: the request cache, the local quota estimate,
// and a thin wrapper that sends every call through the generated client.
//
// The generated client does not expose response headers, so the
// x-ratelimit-remaining value the API sends is not readable from here (the
// client's own limiter does consume it for pacing). The quota guard below is
// therefore a local estimate: it counts the calls this CLI made in the last
// 24 hours against a dmDefaultDailyBudget-call budget (override with
// DELHIVERY_MAPS_DAILY_BUDGET) and cannot see calls made by other tools
// sharing the same token. A real 429 also records a lockout, so the next run
// in the same window plans zero calls.

package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/client"
	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/platform"
	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/store"
)

// API paths, shared by the planners (cache probes) and the request methods so
// the two can never drift.
const (
	epGeocode     = "/geocode"
	epStandardize = "/standarize"
	epValidate    = "/validate"
	epRoute       = "/route"
	epToll        = "/toll_charges_by_route"
	epIsochrone   = "/isochrone"
	epRoutePlan   = "/route-plan"
)

const (
	dmBudgetEnv          = "DELHIVERY_MAPS_DAILY_BUDGET"
	dmPlanBudgetEnv      = "DELHIVERY_MAPS_PLAN_BUDGET"
	dmIgnoreLockoutEnv   = "DELHIVERY_MAPS_IGNORE_LOCKOUT"
	dmDefaultDailyBudget = 50
	// dmDefaultPlanBudget is the separate daily limit of /route-plan, which has
	// its own rate-limit bucket (x-ratelimit-limit: 5) on the live API.
	dmDefaultPlanBudget = 5
	// dmDefaultAPIBase is the gateway the bearer token may be sent to.
	dmDefaultAPIBase = "https://gateway-maps-pub-int.delhivery.com"
	dmBudgetWindow   = 24 * time.Hour
	// dmLockoutDuration is how long a 429 blocks further calls: the quota is
	// per 24 hours and the reset time is not visible to the client.
	dmLockoutDuration = dmBudgetWindow
	// dmMaxAttempts bounds retries of 502/503/504: one try plus two retries.
	dmMaxAttempts = 3

	// dmCallsPerTrip is the most calls one trip quote costs: two geocodes,
	// the route and the toll lookup.
	dmCallsPerTrip = 4
	// dmRoutePolylinePrecision is the decimal precision of encoded route geometry.
	dmRoutePolylinePrecision = 6
	// dmDefaultMaxRows is the default --max-rows for commands that read a file.
	dmDefaultMaxRows = 100000

	dmAddressCacheTTL = 30 * 24 * time.Hour
	dmRouteCacheTTL   = 24 * time.Hour
)

// Quota buckets: every endpoint except /route-plan shares one pool; /route-plan
// has its own.
const (
	dmBucketPool = "pool"
	dmBucketPlan = "plan"
)

// dmCacheTTLs is how long a cached response stays valid, per endpoint.
// /route-plan is deliberately absent: plans are never cached.
var dmCacheTTLs = map[string]time.Duration{
	epGeocode:     dmAddressCacheTTL,
	epStandardize: dmAddressCacheTTL,
	epValidate:    dmAddressCacheTTL,
	epRoute:       dmRouteCacheTTL,
	epToll:        dmRouteCacheTTL,
	epIsochrone:   dmRouteCacheTTL,
}

// dmTravelModes is the shared list of travel modes for trip quote and
// coverage check.
var dmTravelModes = []string{"auto", "motorcycle", "truck", "pedestrian"}

// dmHumanDuration renders a quota or cache duration for help text, so the text
// is derived from the same constants the code uses: 24h is "24 hours" and
// 720h is "30 days".
func dmHumanDuration(d time.Duration) string {
	if d >= 48*time.Hour && d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
	}
	return fmt.Sprintf("%d hours", int(d/time.Hour))
}

// Help-text renderings of the quota window and cache lifetimes.
var (
	dmWindowText     = dmHumanDuration(dmBudgetWindow)
	dmAddressTTLText = dmHumanDuration(dmAddressCacheTTL)
	dmRouteTTLText   = dmHumanDuration(dmRouteCacheTTL)
)

// dmMaxCallsUsage is the --max-calls flag description. what is the noun for the
// calls ("live API calls"), deferred the items that wait when the limit is hit.
func dmMaxCallsUsage(what, deferred string) string {
	return fmt.Sprintf("Most %s this run may make, never more than the estimated budget left in the current %s quota window. Omit it to use the whole remaining budget; an explicit 0 means make no calls. %s beyond the limit are deferred.", what, dmWindowText, deferred)
}

const dmRetentionNote = `Privacy: addresses, coordinates and raw API responses are stored in the local SQLite database
(tables dm_cache and dm_address) and kept until you delete them; run 'address purge' to remove
them by age.`

var dmExitCodesNote = fmt.Sprintf(`Exit codes: 0 on success, including when a 429 or the local call budget (%d per %s;
%d for route-plan) stops a batch (the JSON then has rate_limited and the deferred items);
2 for bad input; 4 when the token is rejected (stops at once, partial output is still written);
5 when every item of a batch failed for a reason other than quota (partial output is still
written); 7 when quota exhaustion prevents the request this command cannot do without.`, dmDefaultDailyBudget, dmHumanDuration(dmBudgetWindow), dmDefaultPlanBudget)

// dmBackoff is the wait before retry attempt n (0-based). Tests replace it.
var dmBackoff = cliutil.Backoff

// dmWriteMu serializes writes to the dm_* tables from concurrent workers.
var dmWriteMu sync.Mutex

// errDMRateLimited marks quota exhaustion: a 429, a lockout after one, or the
// local call budget. Commands stop and defer the remaining work instead of
// treating it as a per-item failure.
var errDMRateLimited = errors.New("API call quota exhausted")

// errDMUnusableResponse marks a 2xx reply that carried no usable data.
var errDMUnusableResponse = errors.New("unusable API response")

// openDMStore opens the local store at dbPath (the default data database
// when empty) and makes sure the dm_* tables exist.
func openDMStore(ctx context.Context, dbPath string) (*store.Store, error) {
	if dbPath == "" {
		dbPath = defaultDBPath("delhivery-maps-pp-cli")
	}
	s, err := store.OpenWithContext(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening local store: %w", err)
	}
	if err := store.EnsureDelhiveryMapsTables(ctx, s.DB()); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func dmBucketFor(endpoint string) string {
	if endpoint == epRoutePlan {
		return dmBucketPlan
	}
	return dmBucketPool
}

// dmBucketBudget is the assumed quota of a bucket for one 24-hour window.
func dmBucketBudget(bucket string) int {
	env, def := dmBudgetEnv, dmDefaultDailyBudget
	if bucket == dmBucketPlan {
		env, def = dmPlanBudgetEnv, dmDefaultPlanBudget
	}
	if v := strings.TrimSpace(cliutil.EnvOverride(env)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

// dmDailyBudget is the assumed per-token quota of the shared pool for one
// 24-hour window.
func dmDailyBudget() int { return dmBucketBudget(dmBucketPool) }

func dmBudgetEnvName(bucket string) string {
	if bucket == dmBucketPlan {
		return dmPlanBudgetEnv
	}
	return dmBudgetEnv
}

// dmQuerier is the query surface shared by *sql.DB and a *sql.Conn that has
// begun a transaction, so the budget can be read inside a reservation.
type dmQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// dmBudgetError turns a bucket's budget state into the error a call must
// return, or nil when one more call may be sent.
func dmBudgetError(bucket string, remaining int, locked bool) error {
	switch {
	case locked:
		return fmt.Errorf("%w: locked out after an earlier HTTP 429 (set %s=1 to override)", errDMRateLimited, dmIgnoreLockoutEnv)
	case remaining <= 0:
		return fmt.Errorf("%w: the local budget of %d %s calls per %s is used up (see %s)", errDMRateLimited, dmBucketBudget(bucket), bucket, dmWindowText, dmBudgetEnvName(bucket))
	}
	return nil
}

// dmBudgetState estimates the calls left in a bucket's rolling window for one
// scope (base URL and token fingerprint). remaining is 0 while a 429 lockout
// for that scope and bucket is active; lockedOut says why. A lockout time in
// the future (clock skew) is ignored.
func dmBudgetState(ctx context.Context, db dmQuerier, scope, bucket string) (remaining int, lockedOut bool, err error) {
	var used int
	now := time.Now()
	cond := "endpoint != ?"
	if bucket == dmBucketPlan {
		cond = "endpoint = ?"
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dm_calls WHERE scope = ? AND called_at > ? AND `+cond,
		scope, now.Add(-dmBudgetWindow).Unix(), epRoutePlan).Scan(&used); err != nil {
		return 0, false, fmt.Errorf("reading call log: %w", err)
	}
	if os.Getenv(dmIgnoreLockoutEnv) != "1" {
		var last sql.NullInt64
		if err := db.QueryRowContext(ctx, `SELECT MAX(locked_at) FROM dm_lockout WHERE scope = ? AND bucket = ? AND locked_at <= ?`,
			scope, bucket, now.Unix()).Scan(&last); err != nil {
			return 0, false, fmt.Errorf("reading lockout: %w", err)
		}
		if last.Valid && now.Sub(time.Unix(last.Int64, 0)) < dmLockoutDuration {
			return 0, true, nil
		}
	}
	if left := dmBucketBudget(bucket) - used; left > 0 {
		return left, false, nil
	}
	return 0, false, nil
}

func dmRecordCall(ctx context.Context, db *sql.DB, scope, endpoint string) error {
	dmWriteMu.Lock()
	defer dmWriteMu.Unlock()
	if _, err := db.ExecContext(ctx, `INSERT INTO dm_calls (called_at, endpoint, scope) VALUES (?, ?, ?)`, time.Now().Unix(), endpoint, scope); err != nil {
		return fmt.Errorf("logging API call: %w", err)
	}
	return nil
}

// dmReserveCall checks the bucket budget and, when a call is allowed, logs it
// in one write-locked transaction. BEGIN IMMEDIATE takes the database write
// lock before the count is read, so a second CLI process sharing the database
// waits and then sees this call. Two processes cannot both spend the last call.
func dmReserveCall(ctx context.Context, db *sql.DB, scope, endpoint string) error {
	dmWriteMu.Lock()
	defer dmWriteMu.Unlock()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserving API call: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("reserving API call: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	bucket := dmBucketFor(endpoint)
	remaining, locked, err := dmBudgetState(ctx, conn, scope, bucket)
	if err != nil {
		return err
	}
	if err := dmBudgetError(bucket, remaining, locked); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO dm_calls (called_at, endpoint, scope) VALUES (?, ?, ?)`, time.Now().Unix(), endpoint, scope); err != nil {
		return fmt.Errorf("logging API call: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("logging API call: %w", err)
	}
	committed = true
	return nil
}

func dmRecordLockout(ctx context.Context, db *sql.DB, scope, bucket string) error {
	dmWriteMu.Lock()
	defer dmWriteMu.Unlock()
	if _, err := db.ExecContext(ctx, `INSERT INTO dm_lockout (locked_at, bucket, scope) VALUES (?, ?, ?)`, time.Now().Unix(), bucket, scope); err != nil {
		return fmt.Errorf("recording lockout: %w", err)
	}
	return nil
}

// dmBudgetPlan is the outcome of dmPlanBudget.
type dmBudgetPlan struct {
	Max       int  // most live calls this run may plan
	Remaining int  // estimated calls left in the window
	LockedOut bool // a 429 lockout is active
}

// dmPlanBudget is the one budget helper every live command uses: Max is the
// estimated remaining budget of the bucket, lowered (never raised) by an
// explicit --max-calls.
func dmPlanBudget(ctx context.Context, a *dmAPI, cmd *cobra.Command, flagMax int, bucket string) (dmBudgetPlan, error) {
	changed := cmd.Flags().Changed("max-calls")
	if changed && flagMax < 0 {
		return dmBudgetPlan{}, dmUsage(cmd, "--max-calls must be 0 or greater")
	}
	remaining, locked, err := dmBudgetState(ctx, a.db, a.scope, bucket)
	if err != nil {
		return dmBudgetPlan{}, err
	}
	plan := dmBudgetPlan{Max: remaining, Remaining: remaining, LockedOut: locked}
	if changed && flagMax < plan.Max {
		plan.Max = flagMax
	}
	a.setCap(plan.Max)
	return plan, nil
}

// dmCacheKey returns the cache key and the request text for a request body.
// scope ties the key to one base URL and token; marshalling a map sorts its
// keys and HashKey lowercases and collapses whitespace, so equivalent
// requests share one entry.
func dmCacheKey(scope string, requestBody any) (key, reqJSON string, err error) {
	raw, err := json.Marshal(requestBody)
	if err != nil {
		return "", "", fmt.Errorf("encoding request for cache key: %w", err)
	}
	return addrkit.HashKey(scope + "\x00" + string(raw)), string(raw), nil
}

func dmCacheGet(ctx context.Context, db *sql.DB, endpoint, key string, ttl time.Duration) (json.RawMessage, bool, error) {
	var resp, fetched sql.NullString
	err := db.QueryRowContext(ctx, `SELECT response, fetched_at FROM dm_cache WHERE endpoint = ? AND key = ?`, endpoint, key).Scan(&resp, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading cache: %w", err)
	}
	if ttl > 0 {
		at, perr := time.Parse(time.RFC3339, fetched.String)
		if perr != nil || time.Since(at) > ttl {
			return nil, false, nil
		}
	}
	return json.RawMessage(resp.String), true, nil
}

func dmCachePut(ctx context.Context, db *sql.DB, endpoint, key, reqJSON string, resp json.RawMessage) error {
	dmWriteMu.Lock()
	defer dmWriteMu.Unlock()
	_, err := db.ExecContext(ctx,
		`INSERT OR REPLACE INTO dm_cache (endpoint, key, request, response, fetched_at) VALUES (?, ?, ?, ?, ?)`,
		endpoint, key, reqJSON, string(resp), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("writing cache: %w", err)
	}
	return nil
}

// dmCacheOpts tunes cachedCallOpt.
type dmCacheOpts struct {
	Scope    string                      // base URL and token fingerprint
	TTL      time.Duration               // 0 means entries never expire
	SkipRead bool                        // --no-cache: still stores the fresh response
	Validate func(json.RawMessage) error // rejects a response before it is cached
}

// cachedCallOpt returns the cached response for (endpoint, requestBody) when
// there is a fresh one, counting zero API calls; otherwise it runs fetchFn,
// validates the reply and stores it. hit reports whether the cache answered.
// A failed fetch or a reply Validate rejects is returned as an error and is
// never cached.
func cachedCallOpt(ctx context.Context, db *sql.DB, endpoint string, requestBody any, opts dmCacheOpts, fetchFn func(context.Context) (json.RawMessage, error)) (json.RawMessage, bool, error) {
	key, reqJSON, err := dmCacheKey(opts.Scope, requestBody)
	if err != nil {
		return nil, false, err
	}
	if !opts.SkipRead {
		cached, ok, err := dmCacheGet(ctx, db, endpoint, key, opts.TTL)
		if err != nil {
			return nil, false, err
		}
		if ok {
			return cached, true, nil
		}
	}
	fresh, err := fetchFn(ctx)
	if err != nil {
		return nil, false, err
	}
	if opts.Validate != nil {
		if err := opts.Validate(fresh); err != nil {
			return nil, false, err
		}
	}
	if err := dmCachePut(ctx, db, endpoint, key, reqJSON, fresh); err != nil {
		return nil, false, err
	}
	return fresh, false, nil
}

// dmAPI sends requests through the generated client and tracks quota use.
type dmAPI struct {
	c         *client.Client
	db        *sql.DB
	callFlags rootFlags // per-call copy of the root flags; timeout may be raised
	noCache   bool
	scope     string // base URL and token fingerprint for cache keys
	errOut    io.Writer
	quiet     bool
	cap       int  // most calls this run may make, valid when hasCap
	hasCap    bool // false until setCap: only the budget limits
	spendMu   sync.Mutex
	calls     atomic.Int64
	hits      atomic.Int64
}

// newDMAPI builds a dmAPI. minTimeout raises the per-call timeout for slow
// endpoints (route-plan takes ~30s); 0 keeps the --timeout value. It refuses
// to hand out a client whose base URL could receive the bearer token outside
// Delhivery (or a loopback test server).
func newDMAPI(cmd *cobra.Command, flags *rootFlags, db *sql.DB, minTimeout time.Duration) (*dmAPI, error) {
	f := *flags
	if minTimeout > 0 && f.timeout < minTimeout {
		f.timeout = minTimeout
		f.timeoutExplicit = true
	}
	c, err := f.newClient()
	if err != nil {
		return nil, err
	}
	if err := dmCheckBaseURL(c.RequestBaseURL()); err != nil {
		return nil, usageErr(err)
	}
	return &dmAPI{
		c: c, db: db, callFlags: f, noCache: flags.noCache, scope: dmCacheScope(c),
		errOut: cmd.ErrOrStderr(), quiet: flags.quiet,
	}, nil
}

// dmCheckBaseURL allows only the exact gateway host in dmDefaultAPIBase over
// https, or a loopback host (so httptest servers keep working). Any other
// host, including other delhivery.com subdomains, would receive the bearer
// token, so it is refused.
func dmCheckBaseURL(base string) error {
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("refusing to send the API token: base URL %q is not a valid URL", base)
	}
	gateway, _ := url.Parse(dmDefaultAPIBase)
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "localhost" || host == "127.0.0.1" || host == "::1":
		if u.Scheme == "http" || u.Scheme == "https" {
			return nil
		}
	case u.Scheme == "https" && host == strings.ToLower(gateway.Hostname()):
		return nil
	}
	return fmt.Errorf("refusing to send the API token to %q: the base URL must be %s (or a loopback address)", dmStripControl(base), dmDefaultAPIBase)
}

// dmCacheScope fingerprints the base URL and token for cache keys. Only the
// first 12 hex characters of each hash are used; the token itself is never
// stored.
func dmCacheScope(c *client.Client) string {
	fp := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])[:12]
	}
	auth := ""
	if c.Config != nil {
		auth = c.Config.AuthHeader()
	}
	return fp(c.RequestBaseURL()) + ":" + fp(auth)
}

func (a *dmAPI) notef(format string, args ...any) {
	if !a.quiet {
		fmt.Fprintf(a.errOut, format, args...)
	}
}

// setCap limits the calls this run may make; a cap of 0 means no calls.
func (a *dmAPI) setCap(n int) {
	a.cap, a.hasCap = n, true
}

// spendCheck says whether one more call to path is allowed: it returns an
// errDMRateLimited error when the budget, the run cap or a lockout says no.
func (a *dmAPI) spendCheck(ctx context.Context, path string) error {
	bucket := dmBucketFor(path)
	remaining, locked, err := dmBudgetState(ctx, a.db, a.scope, bucket)
	if err != nil {
		return err
	}
	if err := dmBudgetError(bucket, remaining, locked); err != nil {
		return err
	}
	return a.capError()
}

// capError reports when this run's own call cap is used up. It is per run
// (per process), so it is checked here rather than in the shared budget.
func (a *dmAPI) capError() error {
	if a.hasCap && int(a.calls.Load()) >= a.cap {
		return fmt.Errorf("%w: this run's call cap of %d is reached", errDMRateLimited, a.cap)
	}
	return nil
}

// spend reserves one call against the shared budget and logs it before it is
// sent. The budget check and the log entry are one locked transaction.
func (a *dmAPI) spend(ctx context.Context, path string) error {
	a.spendMu.Lock()
	defer a.spendMu.Unlock()
	if err := a.capError(); err != nil {
		return err
	}
	if err := dmReserveCall(ctx, a.db, a.scope, path); err != nil {
		return err
	}
	a.calls.Add(1)
	return nil
}

// post sends one POST, retrying 502/503/504 up to dmMaxAttempts attempts in
// total while budget remains. The client itself never replays a POST, so this
// is the only retry layer. Every attempt that reaches the network is logged
// against the quota; a 429 also records a lockout for the endpoint's bucket.
func (a *dmAPI) post(ctx context.Context, path string, body any) (json.RawMessage, error) {
	var lastErr error
	for attempt := 0; attempt < dmMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := a.spend(ctx, path); err != nil {
			if errors.Is(err, errDMRateLimited) && attempt > 0 {
				break // no budget left to retry: report the transient error
			}
			return nil, err
		}
		callCtx, cancel := boundCtx(ctx, &a.callFlags)
		data, _, err := a.c.Post(callCtx, path, body)
		cancel()
		if err == nil {
			if strings.Contains(string(data), "__pp_verify_synthetic__") {
				return nil, fmt.Errorf("%s: %w: request was short-circuited by verify mode and returned no data", path, errDMUnusableResponse)
			}
			return data, nil
		}
		if dmIsRateLimited(err) {
			if lerr := dmRecordLockout(ctx, a.db, a.scope, dmBucketFor(path)); lerr != nil {
				return nil, lerr
			}
			return nil, fmt.Errorf("%w: HTTP 429 from %s", errDMRateLimited, path)
		}
		lastErr = err
		if !dmIsTransient(err) || attempt == dmMaxAttempts-1 {
			break
		}
		// Announce a retry only if it can actually be sent.
		if cerr := a.spendCheck(ctx, path); cerr != nil {
			if errors.Is(cerr, errDMRateLimited) {
				break
			}
			return nil, cerr
		}
		wait := dmBackoff(attempt)
		a.notef("%s returned a transient error, retrying in %s (attempt %d/%d)\n", dmStripControl(path), wait, attempt+2, dmMaxAttempts)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

// dmFetch is post behind the request cache: the reply is decoded (and so
// validated) before it is stored, and a cached copy is served only while it
// is within the endpoint's TTL.
func dmFetch[T any](ctx context.Context, a *dmAPI, path string, body any, decode func(json.RawMessage) (T, error)) (T, bool, error) {
	var zero, decoded T
	haveDecoded := false
	raw, hit, err := cachedCallOpt(ctx, a.db, path, body, dmCacheOpts{
		Scope: a.scope, TTL: dmCacheTTLs[path], SkipRead: a.noCache,
		Validate: func(r json.RawMessage) error {
			v, err := decode(r)
			decoded, haveDecoded = v, err == nil
			return err
		},
	}, func(ctx context.Context) (json.RawMessage, error) { return a.post(ctx, path, body) })
	if err != nil {
		return zero, false, err
	}
	if hit {
		a.hits.Add(1)
	}
	if haveDecoded && !hit {
		return decoded, false, nil // a fresh reply was already decoded by the validator
	}
	out, err := decode(raw)
	return out, hit, err
}

// dmFetchLive is dmFetch without the cache (traffic-aware routes, plans).
func dmFetchLive[T any](ctx context.Context, a *dmAPI, path string, body any, decode func(json.RawMessage) (T, error)) (T, error) {
	var zero T
	raw, err := a.post(ctx, path, body)
	if err != nil {
		return zero, err
	}
	return decode(raw)
}

// cacheHas reports whether a fresh cached response exists for the request,
// without fetching. It is false under --no-cache.
func (a *dmAPI) cacheHas(ctx context.Context, path string, body any) (bool, error) {
	if a.noCache {
		return false, nil
	}
	key, _, err := dmCacheKey(a.scope, body)
	if err != nil {
		return false, err
	}
	_, ok, err := dmCacheGet(ctx, a.db, path, key, dmCacheTTLs[path])
	return ok, err
}

func dmAPIStatus(err error) int {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

func dmIsRateLimited(err error) bool {
	var limited *platform.RateLimitedError
	var legacy *cliutil.RateLimitError
	return errors.As(err, &limited) || errors.As(err, &legacy)
}

func dmIsTransient(err error) bool {
	switch dmAPIStatus(err) {
	case 502, 503, 504:
		return true
	}
	return false
}

// dmIsFatal reports errors that make continuing pointless: the token is
// rejected, so every further call would spend quota for nothing.
func dmIsFatal(err error) bool {
	switch dmAPIStatus(err) {
	case 401, 403:
		return true
	}
	return errors.Is(err, client.ErrPlaceholderCredential)
}

// dmStr decodes a JSON string or number into a string (pincodes arrive as
// either).
type dmStr string

func (s *dmStr) UnmarshalJSON(b []byte) error {
	t := strings.TrimSpace(string(b))
	if t == "null" || t == "" {
		*s = ""
		return nil
	}
	var str string
	if json.Unmarshal(b, &str) == nil {
		*s = dmStr(strings.TrimSpace(str))
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*s = dmStr(n.String())
	return nil
}

func dmAddressBody(addr string) map[string]any { return map[string]any{"address": addr} }

type dmGeocodeResp struct {
	Lat         *float64 `json:"lat"`
	Lng         *float64 `json:"lng"`
	ErrorRadius *float64 `json:"error_radius"`
	Metadata    struct {
		Pincode dmStr `json:"pincode"`
	} `json:"metadata"`
}

type dmStandardizeResp struct {
	AddressComponents struct {
		City       dmStr `json:"city"`
		State      dmStr `json:"state"`
		PostalCode dmStr `json:"postal_code"`
		Pincode    dmStr `json:"pincode"` // fallback only: the live API uses postal_code
	} `json:"address_components"`
	FormattedAddress dmStr `json:"formatted_address"`
}

// postalCode prefers the live field name and falls back to the documented one.
func (r dmStandardizeResp) postalCode() string {
	if r.AddressComponents.PostalCode != "" {
		return string(r.AddressComponents.PostalCode)
	}
	return string(r.AddressComponents.Pincode)
}

type dmValidateResp struct {
	FormattedAddress dmStr `json:"formatted_address"`
	Granularity      dmStr `json:"granularity_level"`
	Quality          dmStr `json:"quality"`
}

func decodeGeocode(raw json.RawMessage) (dmGeocodeResp, error) {
	var out dmGeocodeResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("%w: decoding %s: %v", errDMUnusableResponse, epGeocode, err)
	}
	if out.Lat == nil || out.Lng == nil {
		return out, fmt.Errorf("%w: %s returned no coordinates", errDMUnusableResponse, epGeocode)
	}
	return out, nil
}

func decodeStandardize(raw json.RawMessage) (dmStandardizeResp, error) {
	var out dmStandardizeResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("%w: decoding %s: %v", errDMUnusableResponse, epStandardize, err)
	}
	if out.FormattedAddress == "" {
		return out, fmt.Errorf("%w: %s returned no formatted_address", errDMUnusableResponse, epStandardize)
	}
	return out, nil
}

func decodeValidate(raw json.RawMessage) (dmValidateResp, error) {
	var out dmValidateResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("%w: decoding %s: %v", errDMUnusableResponse, epValidate, err)
	}
	if out.Quality == "" {
		return out, fmt.Errorf("%w: %s returned no quality", errDMUnusableResponse, epValidate)
	}
	return out, nil
}

func (a *dmAPI) geocode(ctx context.Context, address string) (dmGeocodeResp, bool, error) {
	out, hit, err := dmFetch(ctx, a, epGeocode, dmAddressBody(address), decodeGeocode)
	if err != nil {
		return out, hit, fmt.Errorf("geocoding %q: %w", address, err)
	}
	return out, hit, nil
}

func (a *dmAPI) standardize(ctx context.Context, address string) (dmStandardizeResp, bool, error) {
	return dmFetch(ctx, a, epStandardize, dmAddressBody(address), decodeStandardize)
}

func (a *dmAPI) validate(ctx context.Context, address string) (dmValidateResp, bool, error) {
	return dmFetch(ctx, a, epValidate, dmAddressBody(address), decodeValidate)
}

// dmWriteDryRunPlan ends a --dry-run: it reports what the command would do
// without having read files, opened the database or touched the network.
func dmWriteDryRunPlan(cmd *cobra.Command, flags *rootFlags, action, would string, plan map[string]any) error {
	out := map[string]any{"dry_run": true, "action": action, "would": would}
	for k, v := range plan {
		out[k] = v
	}
	if wantsHumanTable(cmd.OutOrStdout(), flags) {
		return writeDMPlanHuman(cmd.OutOrStdout(), would, plan)
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func writeDMPlanHuman(w io.Writer, would string, plan map[string]any) error {
	if _, err := fmt.Fprintf(w, "dry-run: would %s\n", would); err != nil {
		return err
	}
	keys := make([]string, 0, len(plan))
	for k := range plan {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := fmt.Fprintf(w, "  %s: %v\n", k, plan[k]); err != nil {
			return err
		}
	}
	return nil
}

// dmUsage prints usage and returns the exit-2 usage error, the shape every
// hand-written command uses for a missing or invalid input.
func dmUsage(cmd *cobra.Command, format string, args ...any) error {
	_ = cmd.Usage()
	return usageErr(fmt.Errorf(format, args...))
}

// dmFinishRunErr maps an API error to the structured exit code without
// writing an envelope (hand-written commands own their output).
func dmFinishRunErr(err error) error {
	var classified error
	if errors.Is(err, errDMRateLimited) {
		classified = rateLimitErr(err)
	} else {
		classified = classifyAPIErrorOnly(err)
	}
	// Server-controlled text (error bodies) reaches the terminal through the
	// error message, so strip control and format characters from it.
	var ce *cliError
	if errors.As(classified, &ce) {
		return &cliError{code: ce.code, err: errors.New(dmSanitize(ce.Error(), true))}
	}
	return classified
}

// dmErrText is an error message made safe to print or store: server-supplied
// text may carry terminal escapes.
func dmErrText(err error) string { return dmSanitize(err.Error(), false) }

// dmInputPath resolves the --file/--orders value together with --stdin: it
// returns addrkit.StdinPath when the rows come from standard input. Giving both
// a real path and --stdin is a usage error.
func dmInputPath(cmd *cobra.Command, flagName, path string, stdin bool) (string, error) {
	if stdin {
		if path != "" && path != addrkit.StdinPath {
			return "", dmUsage(cmd, "use either --stdin or --%s, not both", flagName)
		}
		return addrkit.StdinPath, nil
	}
	return path, nil
}

// dmReadTable reads an input file (or standard input when path is "-") under
// the row, size and file-type limits; limit violations and empty standard
// input are usage errors.
func dmReadTable(cmd *cobra.Command, path string, maxRows int) (*addrkit.Table, error) {
	if path == addrkit.StdinPath {
		t, err := addrkit.ReadTableReader(cmd.InOrStdin(), maxRows)
		if errors.Is(err, addrkit.ErrLimit) || errors.Is(err, addrkit.ErrNoInput) {
			return nil, dmUsage(cmd, "standard input: %v (pipe CSV, JSON Lines or a JSON array of objects)", err)
		}
		if err != nil {
			return nil, fmt.Errorf("reading standard input: %w", err)
		}
		return t, nil
	}
	t, err := addrkit.ReadTableMax(path, maxRows)
	if errors.Is(err, addrkit.ErrLimit) {
		return nil, dmUsage(cmd, "%v", err)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return t, nil
}

// dmCheckOut validates an --out path: it must not be the input file (even
// through a symlink), must not be a directory, and must not overwrite an
// existing file unless force is set.
func dmCheckOut(cmd *cobra.Command, in, out string, force bool) error {
	if out == "" {
		return nil
	}
	// The parent directory must exist and accept a new file, checked now so a
	// bad path fails before any API call spends quota.
	dir := filepath.Dir(out)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return dmUsage(cmd, "--out directory %s does not exist", dir)
	}
	probe, err := os.CreateTemp(dir, ".pp-writecheck-*")
	if err != nil {
		return dmUsage(cmd, "--out directory %s is not writable: %v", dir, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	outInfo, outErr := os.Stat(out)
	if outErr == nil {
		if outInfo.IsDir() {
			return dmUsage(cmd, "--out %s is a directory", out)
		}
		if in != "" && in != addrkit.StdinPath {
			if inInfo, err := os.Stat(in); err == nil && os.SameFile(inInfo, outInfo) {
				return dmUsage(cmd, "--out %s is the input file; refusing to overwrite it", out)
			}
		}
		if !force {
			return dmUsage(cmd, "--out %s already exists; pass --force to overwrite it", out)
		}
		return nil
	}
	return nil
}

// dmWriteCSV writes the output file. Without force the file is published
// with a hard link, so a file that appeared after the dmCheckOut pre-check is
// never replaced.
func dmWriteCSV(cmd *cobra.Command, path string, header []string, rows [][]string, force bool) error {
	err := addrkit.WriteCSVMode(path, header, rows, force)
	if errors.Is(err, addrkit.ErrExists) {
		return dmUsage(cmd, "--out %s appeared while the command ran; pass --force to overwrite it", path)
	}
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// dmWriteRowsCSV prints rows as CSV on stdout (the --csv output of commands run
// without --out), neutralizing formula-like cells.
func dmWriteRowsCSV(cmd *cobra.Command, header []string, rows [][]string) error {
	return addrkit.WriteCSVStream(cmd.OutOrStdout(), header, rows)
}

// dmStdinHelp is the flag description of --stdin, shared by the file-reading commands.
func dmStdinHelp(flagName string) string {
	return fmt.Sprintf("Read the rows from standard input instead of --%s (same as --%s -); CSV, JSON Lines or a JSON array of objects is detected automatically.", flagName, flagName)
}

// dmFiniteFlag rejects NaN, Inf and (when nonNegative) negative flag values.
func dmFiniteFlag(cmd *cobra.Command, name string, v float64, nonNegative bool) error {
	if math.IsNaN(v) || math.IsInf(v, 0) || (nonNegative && v < 0) {
		return dmUsage(cmd, "--%s must be a finite number%s (got %v)", name, map[bool]string{true: " that is not negative", false: ""}[nonNegative], v)
	}
	return nil
}

// dmRound rounds f to the given number of decimal places.
func dmRound(f float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(f*p) / p
}

func dmTrimmed(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

var dmEscapeSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b.`)

// dmSanitize removes ANSI/OSC escape sequences, control characters and Unicode
// format characters (such as the bidi override U+202E) from text before it is
// printed to a terminal. Line breaks become spaces unless keepNewlines.
func dmSanitize(s string, keepNewlines bool) string {
	s = dmEscapeSeq.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' && keepNewlines:
			return r
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
}

// dmStripControl is dmSanitize on a single line of text.
func dmStripControl(s string) string { return dmSanitize(s, false) }

// dmOutcomeKeys are the row fields that say why a row has no result: a quota
// deferral, a failed call, or a reason. --compact keeps a field only when most
// rows carry it, so a single deferred or failed row would lose these and look
// like a successful one with zero values.
var dmOutcomeKeys = []string{"deferred", "error", "reason", "status", "rate_limited"}

// dmPrint prints v like printJSONFiltered, but for the csv, plain and quiet
// output paths (which the shared printers do not neutralise) every string
// value is first made safe against spreadsheet formula injection. The
// outcome keys survive --compact on every row.
func dmPrint(w io.Writer, v any, flags *rootFlags) error {
	if flags.csv || flags.plain || flags.quiet {
		neutral, err := neutralizeValue(v)
		if err != nil {
			return err
		}
		v = neutral
	}
	return printJSONFilteredKeep(w, v, flags, dmOutcomeKeys...)
}

// neutralizeValue round-trips v through JSON and quotes every string that
// could run as a spreadsheet formula. Numbers keep their exact text.
func neutralizeValue(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	var walk func(any) any
	walk = func(x any) any {
		switch t := x.(type) {
		case string:
			return addrkit.NeutralizeCell(t)
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
		case map[string]any:
			for k := range t {
				t[k] = walk(t[k])
			}
		}
		return x
	}
	return walk(generic), nil
}

// dmValidTravelMode reports whether mode is one of dmTravelModes.
func dmValidTravelMode(mode string) bool { return slices.Contains(dmTravelModes, mode) }
