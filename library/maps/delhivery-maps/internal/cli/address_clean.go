// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
)

// cleanColumns is the fixed output layout of `address clean`.
var cleanColumns = []string{"raw", "formatted", "quality", "granularity", "lat", "lng", "error_radius", "pincode", "city", "state", "status", "error"}

// cleanStepEndpoints maps each step to the API path it calls.
var cleanStepEndpoints = map[string]string{
	"standardize": epStandardize,
	"validate":    epValidate,
	"geocode":     epGeocode,
}

// cleanAllSteps is the default step list, in execution order.
var cleanAllSteps = []string{"standardize", "validate", "geocode"}

const cleanMaxConcurrency = 4

// cleanResult is the merged outcome for one distinct address.
type cleanResult struct {
	Raw          string
	Standardized string
	Formatted    string
	Quality      string
	Granularity  string
	Pincode      string
	City         string
	State        string
	Lat, Lng     *float64
	ErrorRadius  *int64
	Status       string // ok | cached | error | deferred
	Err          string
	live         int // live API calls this address needed
}

type cleanSignal int

const (
	cleanSigNone cleanSignal = iota
	cleanSigRateLimited
	cleanSigFatal
)

type cleanOutcome struct {
	key string
	res cleanResult
	sig cleanSignal
	err error
}

type cleanJob struct {
	key    string
	raw    string
	needed int          // live calls this address still needs
	steps  []string     // steps left to run (with --resume, only those not yet stored)
	base   *cleanResult // data already stored for the address, merged into the result
}

// cleanSummary is the JSON envelope printed after a run.
type cleanSummary struct {
	File                  string   `json:"file"`
	Out                   string   `json:"out"`
	Steps                 []string `json:"steps"`
	Rows                  int      `json:"rows"`
	OK                    int      `json:"ok"`
	Cached                int      `json:"cached"`
	Errors                int      `json:"errors"`
	Deferred              int      `json:"deferred"`
	PlannedCalls          int      `json:"planned_calls"`
	CallsMade             int64    `json:"calls_made"`
	CacheHits             int64    `json:"cache_hits"`
	MaxCalls              int      `json:"max_calls"`
	BudgetRemainingBefore int      `json:"budget_remaining_before"`
	RateLimited           bool     `json:"rate_limited"`
	Note                  string   `json:"note,omitempty"`
	// Cleaned holds the merged rows when no --out file is written.
	Cleaned []cleanRowJSON `json:"cleaned,omitempty"`
}

// cleanRowJSON is one cleaned address as a JSON object (the same fields as the
// output CSV columns).
type cleanRowJSON struct {
	Raw         string   `json:"raw"`
	Formatted   string   `json:"formatted"`
	Quality     string   `json:"quality"`
	Granularity string   `json:"granularity"`
	Lat         *float64 `json:"lat"`
	Lng         *float64 `json:"lng"`
	ErrorRadius *int64   `json:"error_radius"`
	Pincode     string   `json:"pincode"`
	City        string   `json:"city"`
	State       string   `json:"state"`
	Status      string   `json:"status"`
	Error       string   `json:"error,omitempty"`
}

func (c cleanResult) json() cleanRowJSON {
	return cleanRowJSON{Raw: c.Raw, Formatted: c.Formatted, Quality: c.Quality, Granularity: c.Granularity, Lat: c.Lat, Lng: c.Lng,
		ErrorRadius: c.ErrorRadius, Pincode: c.Pincode, City: c.City, State: c.State, Status: c.Status, Error: c.Err}
}

func newNovelAddressCleanCmd(flags *rootFlags) *cobra.Command {
	var flagFile string
	var flagOut string
	var flagAddressColumn string
	var flagSteps string
	var flagConcurrency int
	var flagMaxCalls int
	var flagResume, flagForce, flagStdin bool
	var flagMaxRows int
	var flagDB string

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean thousands of raw addresses from a CSV into standardised, validated, geocoded rows without losing progress to rate limits or expiry.",
		Long: strings.Trim(fmt.Sprintf(`
Reads raw addresses from a CSV, JSON Lines or JSON-array file (or from standard
input with --stdin, or --file -) and runs each one through the standardize,
validate and geocode endpoints, then writes one merged row per input address:
raw, formatted, quality, granularity, lat, lng, error_radius, pincode, city,
state, status, error. With --out the rows go to that CSV file; without it no
file is written and the rows are returned in the JSON output (cleaned) or, with
--csv, printed as CSV.

Every API response is cached locally by normalized request (geocode, standardize
and validate for %[5]s), so re-running the command costs zero calls for
addresses already seen, and every finished address is stored in the local
dm_address table (used by 'address report' and 'address dupes'). The token
allows about %[1]d requests per %[4]s, so before any call the command works
out how many it needs. If that exceeds the budget (the locally estimated
remaining calls, lowered by --max-calls) it processes only as many whole
addresses as fit, marks the rest status=deferred and says so. A 429 stops the
run at once, remaining addresses are deferred, and further runs in the same
%[4]s plan zero calls. Use --resume to skip addresses that already completed
every selected step. An --out file is never overwritten without --force and
may not be the input file; cells that look like spreadsheet formulas are
prefixed with a single quote.

The budget estimate counts calls made by this CLI only; set
DELHIVERY_MAPS_DAILY_BUDGET to change the assumed quota.

%[2]s

%[3]s`, dmDefaultDailyBudget, dmExitCodesNote, dmRetentionNote, dmWindowText, dmAddressTTLText), "\n"),
		Example: strings.Trim(`
  delhivery-maps-pp-cli address clean --stdin
  delhivery-maps-pp-cli address clean --file orders.csv --out cleaned.csv --concurrency 4 --resume
  delhivery-maps-pp-cli address clean --file leads.jsonl --address-column full_address --out cleaned.csv --steps standardize,geocode --max-calls 20 --force
  delhivery-maps-pp-cli address clean --file orders.csv --out cleaned.csv --dry-run`, "\n"),
		// The live-dogfood runner feeds pp:happy-stdin (valid JSON: one address) on
		// standard input and appends --stdin; --steps=geocode keeps the live matrix to 1 API call (then cache).
		Annotations: map[string]string{
			"pp:data-source": "live",
			// Reads and computes; it writes a file only when --out is given, and MCP hosts
			// are barred from that destination flag.
			"mcp:read-only":              "true",
			"mcp:read-only-unless-flags": "out",
			"mcp:write-flags":            "out",
			// --stdin reads the host's standard input, which an MCP call cannot supply, so
			// the flag is withheld from the MCP tool. MCP callers pass a server-local file.
			"mcp:read-flags": "stdin",
			"pp:happy-args":  "--steps=geocode",
			"pp:happy-stdin": `[{"address":"flat 4b, lotus apt, sec 45, gurgaon, haryana 122003"}]`,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			steps, stepsErr := parseCleanSteps(flagSteps)
			if dryRunOK(flags) {
				maxCalls := "auto (estimated remaining budget)"
				if cmd.Flags().Changed("max-calls") {
					maxCalls = strconv.Itoa(flagMaxCalls)
				}
				return dmWriteDryRunPlan(cmd, flags, "address clean",
					"read the addresses (file or standard input), call "+strings.Join(steps, "+")+" per uncached address and write the cleaned CSV (or return the rows); no request sent",
					map[string]any{
						"file":          flagFile,
						"stdin":         flagStdin,
						"out":           flagOut,
						"steps":         steps,
						"calls_per_row": len(steps),
						"planned_calls": "uncached addresses x calls_per_row (the input is not read in dry-run)",
						"max_calls":     maxCalls,
						"resume":        flagResume,
					})
			}
			inPath, err := dmInputPath(cmd, "file", flagFile, flagStdin)
			if err != nil {
				return err
			}
			if inPath == "" {
				return dmUsage(cmd, "--file is required (or --stdin to read standard input)")
			}
			flagFile = inPath
			if stepsErr != nil {
				return dmUsage(cmd, "%v", stepsErr)
			}
			if flagConcurrency < 1 {
				flagConcurrency = 1
			}
			if flagConcurrency > cleanMaxConcurrency {
				fmt.Fprintf(cmd.ErrOrStderr(), "address clean: --concurrency capped at %d to respect the API quota\n", cleanMaxConcurrency)
				flagConcurrency = cleanMaxConcurrency
			}
			if err := dmCheckOut(cmd, flagFile, flagOut, flagForce); err != nil {
				return err
			}

			table, err := dmReadTable(cmd, flagFile, flagMaxRows)
			if err != nil {
				return err
			}
			col := table.Col(flagAddressColumn)
			if col < 0 {
				return dmUsage(cmd, "column %q not found in %s (columns: %s); pass --address-column", flagAddressColumn, flagFile, strings.Join(table.Header, ", "))
			}

			ctx := cmd.Context()
			s, err := openDMStore(ctx, flagDB)
			if err != nil {
				return err
			}
			defer s.Close()
			db := s.DB()
			api, err := newDMAPI(cmd, flags, db, 0)
			if err != nil {
				return err
			}
			budget, err := dmPlanBudget(ctx, api, cmd, flagMaxCalls, dmBucketPool)
			if err != nil {
				return err
			}

			run := &cleanRun{api: api, db: db, steps: steps, resume: flagResume}
			summary, results, runErr := run.execute(ctx, table, col, flagConcurrency, budget.Max)
			if results == nil && runErr != nil {
				// Planning failed: leave any earlier output file untouched.
				return dmFinishRunErr(runErr)
			}
			summary.File, summary.Out, summary.Steps = flagFile, flagOut, steps
			summary.BudgetRemainingBefore = budget.Remaining
			if budget.LockedOut && summary.Deferred > 0 {
				summary.RateLimited = true
			}

			rows := make([][]string, 0, len(results))
			for _, res := range results {
				rows = append(rows, res.csvRow())
			}
			where := "the error field of each row"
			if flagOut != "" {
				if err := dmWriteCSV(cmd, flagOut, cleanColumns, rows, flagForce); err != nil {
					return err
				}
				where = "the error column in " + flagOut
			} else {
				summary.Cleaned = make([]cleanRowJSON, 0, len(results))
				for _, res := range results {
					summary.Cleaned = append(summary.Cleaned, res.json())
				}
			}
			if summary.Deferred > 0 && !summary.RateLimited {
				fmt.Fprintf(cmd.ErrOrStderr(), "address clean: deferred %d row(s): planned %d call(s) within the call budget of %d. Re-run with --resume once the quota resets (or with a larger --max-calls, up to the remaining budget).\n", summary.Deferred, summary.PlannedCalls, summary.MaxCalls)
			}
			if summary.RateLimited {
				summary.Note = "the API quota is exhausted (HTTP 429 or an earlier lockout); remaining rows were deferred. Re-run with --resume after the quota window resets (" + dmWindowText + ")."
				fmt.Fprintf(cmd.ErrOrStderr(), "address clean: rate limited (HTTP 429). Stopped; %d row(s) deferred. %s\n", summary.Deferred, summary.Note)
			}
			if summary.Errors > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "address clean: %d row(s) failed; see %s\n", summary.Errors, where)
			}
			if err := printCleanSummary(cmd, flags, summary, rows); err != nil {
				return err
			}
			if runErr != nil {
				return dmFinishRunErr(runErr)
			}
			if summary.Errors > 0 && summary.Errors == summary.Rows {
				return apiErr(fmt.Errorf("all %d row(s) failed (see %s)", summary.Rows, where))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagFile, "file", "", "CSV, JSON Lines or JSON-array file of raw addresses to clean; - reads standard input.")
	cmd.Flags().BoolVar(&flagStdin, "stdin", false, dmStdinHelp("file"))
	cmd.Flags().StringVar(&flagOut, "out", "", "Optional path of the cleaned CSV to write (raw, formatted, quality, granularity, lat, lng, error_radius, pincode, city, state, status, error); without it no file is written and the rows are returned in the JSON output.")
	cmd.Flags().StringVar(&flagAddressColumn, "address-column", "address", "Name of the input column that holds the raw address (matched case-insensitively).")
	cmd.Flags().StringVar(&flagSteps, "steps", strings.Join(cleanAllSteps, ","), "Comma-separated steps to run per address: standardize, validate, geocode.")
	cmd.Flags().IntVar(&flagConcurrency, "concurrency", 1, fmt.Sprintf("Addresses processed in parallel (1 to %d); keep at 1 unless the quota is generous.", cleanMaxConcurrency))
	cmd.Flags().IntVar(&flagMaxCalls, "max-calls", 0, dmMaxCallsUsage("live API calls", "Rows"))
	cmd.Flags().IntVar(&flagMaxRows, "max-rows", dmDefaultMaxRows, fmt.Sprintf("Refuse input files with more rows than this (at most %d).", addrkit.HardMaxRows))
	cmd.Flags().BoolVar(&flagForce, "force", false, "Overwrite --out if it already exists.")
	cmd.Flags().BoolVar(&flagResume, "resume", false, "Skip addresses already completed for every selected step in the local address table.")
	cmd.Flags().StringVar(&flagDB, "db", "", "SQLite database for the cache and address table (default: the CLI's data database).")
	return cmd
}

// parseCleanSteps validates --steps and returns them in execution order.
func parseCleanSteps(spec string) ([]string, error) {
	want := map[string]bool{}
	for _, part := range strings.Split(spec, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		switch p {
		case "":
		case "standardize", "standardise", "standarize":
			want["standardize"] = true
		case "validate", "geocode":
			want[p] = true
		default:
			return cleanAllSteps, fmt.Errorf("unknown step %q in --steps (use standardize, validate, geocode)", p)
		}
	}
	steps := make([]string, 0, 3)
	for _, s := range cleanAllSteps {
		if want[s] {
			steps = append(steps, s)
		}
	}
	if len(steps) == 0 {
		return cleanAllSteps, errors.New("--steps selected no steps")
	}
	return steps, nil
}

type cleanRun struct {
	api    *dmAPI
	db     *sql.DB
	steps  []string
	resume bool
}

// execute plans, runs and merges the whole file. It returns the summary, the
// output rows in input order, and a fatal error (rejected token) if one ended
// the run early; per-row failures and 429s are reported in the summary instead.
func (r *cleanRun) execute(ctx context.Context, table *addrkit.Table, col, concurrency, maxCalls int) (cleanSummary, []cleanResult, error) {
	type inRow struct {
		raw, key string
	}
	rows := make([]inRow, 0, len(table.Rows))
	var order []string
	seen := map[string]bool{}
	rawByKey := map[string]string{}
	for _, tr := range table.Rows {
		raw := strings.TrimSpace(addrkit.Cell(tr, col))
		key := ""
		if raw != "" {
			key = addrkit.HashKey(raw)
			if !seen[key] {
				seen[key] = true
				order = append(order, key)
				rawByKey[key] = raw
			}
		}
		rows = append(rows, inRow{raw: raw, key: key})
	}

	summary := cleanSummary{Rows: len(rows), MaxCalls: maxCalls}
	results := make(map[string]cleanResult, len(order))
	var jobs []cleanJob

	// Plan: resumed rows are free; otherwise count the uncached steps.
	planned := 0
	blocked := false
	for _, key := range order {
		raw := rawByKey[key]
		steps := r.steps
		var base *cleanResult
		if r.resume {
			prev, ok, err := dmLoadAddress(ctx, r.db, key)
			if err != nil {
				return summary, nil, err
			}
			if ok {
				res := prev.toResult(raw)
				if prev.completeFor(r.steps) {
					res.Status = "cached"
					results[key] = res
					continue
				}
				// Partially stored: run only the selected steps still missing.
				steps = prev.missingSteps(r.steps)
				base = &res
			}
		}
		needed := 0
		for _, step := range steps {
			has, err := r.api.cacheHas(ctx, cleanStepEndpoints[step], dmAddressBody(raw))
			if err != nil {
				return summary, nil, err
			}
			if !has {
				needed++
			}
		}
		if needed > 0 && (blocked || planned+needed > maxCalls) {
			blocked = true
			results[key] = cleanResult{Raw: raw, Status: "deferred", Err: fmt.Sprintf("deferred: needs %d call(s) but only %d fit within the call budget of %d", needed, max(maxCalls-planned, 0), maxCalls)}
			continue
		}
		planned += needed
		jobs = append(jobs, cleanJob{key: key, raw: raw, needed: needed, steps: steps, base: base})
	}
	summary.PlannedCalls = planned

	// Run: workers carry each item's result and error through a channel; the
	// collector is the only goroutine that writes dm_address.
	var stop atomic.Bool
	jobCh := make(chan cleanJob)
	outCh := make(chan cleanOutcome)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				// After a 429, a rejected token or the call cap, only rows that
				// need no call (fully cached) can still be served.
				if stop.Load() && job.needed > 0 {
					outCh <- cleanOutcome{key: job.key, res: cleanResult{Raw: job.raw, Status: "deferred", Err: "deferred: run stopped early (rate limit or rejected token)"}}
					continue
				}
				res, sig, err := r.process(ctx, job)
				if sig != cleanSigNone {
					stop.Store(true)
				}
				outCh <- cleanOutcome{key: job.key, res: res, sig: sig, err: err}
			}
		}()
	}
	go func() {
		for _, j := range jobs {
			jobCh <- j
		}
		close(jobCh)
		wg.Wait()
		close(outCh)
	}()

	var fatal error
	var persistErr error
	for out := range outCh {
		results[out.key] = out.res
		switch out.sig {
		case cleanSigRateLimited:
			summary.RateLimited = true
		case cleanSigFatal:
			if fatal == nil {
				fatal = out.err
			}
		}
		if out.res.hasData() {
			if err := dmUpsertAddress(ctx, r.db, out.key, out.res); err != nil && persistErr == nil {
				persistErr = err
			}
		}
	}
	summary.CallsMade = r.api.calls.Load()
	summary.CacheHits = r.api.hits.Load()

	// Merge back into input order.
	outRows := make([]cleanResult, 0, len(rows))
	firstSeen := map[string]bool{}
	for _, row := range rows {
		var res cleanResult
		if row.key == "" {
			res = cleanResult{Raw: row.raw, Status: "error", Err: "empty address"}
		} else {
			res = results[row.key]
			if firstSeen[row.key] && res.Status == "ok" {
				res.Status = "cached"
			}
			firstSeen[row.key] = true
		}
		switch res.Status {
		case "ok":
			summary.OK++
		case "cached":
			summary.Cached++
		case "deferred":
			summary.Deferred++
		default:
			summary.Errors++
		}
		outRows = append(outRows, res)
	}
	if persistErr != nil {
		return summary, outRows, persistErr
	}
	return summary, outRows, fatal
}

// process runs every selected step for one address through the cache.
func (r *cleanRun) process(ctx context.Context, job cleanJob) (cleanResult, cleanSignal, error) {
	raw := job.raw
	res := cleanResult{Raw: raw}
	if job.base != nil {
		res = *job.base
	}
	fail := func(err error) (cleanResult, cleanSignal, error) {
		res.Err = dmErrText(err)
		switch {
		case errors.Is(err, errDMRateLimited):
			res.Status = "deferred"
			return res, cleanSigRateLimited, err
		case dmIsFatal(err):
			res.Status = "error"
			return res, cleanSigFatal, err
		default:
			res.Status = "error"
			return res, cleanSigNone, err
		}
	}
	for _, step := range job.steps {
		switch step {
		case "standardize":
			v, hit, err := r.api.standardize(ctx, raw)
			if err != nil {
				return fail(err)
			}
			if !hit {
				res.live++
			}
			res.Standardized = string(v.FormattedAddress)
			if res.Formatted == "" {
				res.Formatted = string(v.FormattedAddress)
			}
			res.City = string(v.AddressComponents.City)
			res.State = string(v.AddressComponents.State)
			if res.Pincode == "" {
				res.Pincode = v.postalCode()
			}
		case "validate":
			v, hit, err := r.api.validate(ctx, raw)
			if err != nil {
				return fail(err)
			}
			if !hit {
				res.live++
			}
			res.Quality = string(v.Quality)
			res.Granularity = string(v.Granularity)
			if res.Formatted == "" {
				res.Formatted = string(v.FormattedAddress)
			}
		case "geocode":
			v, hit, err := r.api.geocode(ctx, raw)
			if err != nil {
				return fail(err)
			}
			if !hit {
				res.live++
			}
			res.Lat, res.Lng = v.Lat, v.Lng
			if v.ErrorRadius != nil {
				er := int64(math.Round(*v.ErrorRadius))
				res.ErrorRadius = &er
			}
			if res.Pincode == "" {
				res.Pincode = string(v.Metadata.Pincode)
			}
		}
	}
	res.Status = "cached"
	if res.live > 0 {
		res.Status = "ok"
	}
	return res, cleanSigNone, nil
}

func (c cleanResult) hasData() bool {
	return c.Standardized != "" || c.Formatted != "" || c.Quality != "" || c.Granularity != "" || c.Lat != nil || c.Pincode != ""
}

func (c cleanResult) csvRow() []string {
	lat, lng, er := "", "", ""
	if c.Lat != nil {
		lat = addrkit.FormatFloat(*c.Lat)
	}
	if c.Lng != nil {
		lng = addrkit.FormatFloat(*c.Lng)
	}
	if c.ErrorRadius != nil {
		er = strconv.FormatInt(*c.ErrorRadius, 10)
	}
	return []string{c.Raw, c.Formatted, c.Quality, c.Granularity, lat, lng, er, c.Pincode, c.City, c.State, c.Status, c.Err}
}

// dmAddressRow mirrors a dm_address row with nullable columns.
type dmAddressRow struct {
	Standardized, Quality, Granularity, Formatted, Pincode, City, State sql.NullString
	Lat, Lng                                                            sql.NullFloat64
	ErrorRadius                                                         sql.NullInt64
}

// completeStep reports whether the stored row already holds a step's result.
func (a dmAddressRow) completeStep(step string) bool {
	switch step {
	case "standardize":
		return a.Standardized.Valid
	case "validate":
		return a.Quality.Valid
	case "geocode":
		return a.Lat.Valid && a.Lng.Valid
	}
	return true
}

func (a dmAddressRow) completeFor(steps []string) bool {
	return len(a.missingSteps(steps)) == 0
}

// missingSteps returns, in order, the steps the stored row has no result for.
func (a dmAddressRow) missingSteps(steps []string) []string {
	var missing []string
	for _, s := range steps {
		if !a.completeStep(s) {
			missing = append(missing, s)
		}
	}
	return missing
}

func (a dmAddressRow) toResult(raw string) cleanResult {
	res := cleanResult{
		Raw: raw, Standardized: a.Standardized.String, Formatted: a.Formatted.String,
		Quality: a.Quality.String, Granularity: a.Granularity.String,
		Pincode: a.Pincode.String, City: a.City.String, State: a.State.String,
	}
	if a.Lat.Valid && a.Lng.Valid {
		lat, lng := a.Lat.Float64, a.Lng.Float64
		res.Lat, res.Lng = &lat, &lng
	}
	if a.ErrorRadius.Valid {
		er := a.ErrorRadius.Int64
		res.ErrorRadius = &er
	}
	return res
}

func dmLoadAddress(ctx context.Context, db *sql.DB, key string) (dmAddressRow, bool, error) {
	var a dmAddressRow
	err := db.QueryRowContext(ctx, `SELECT standardized, validated_quality, granularity, formatted, lat, lng, error_radius, pincode, city, state
		FROM dm_address WHERE raw_key = ?`, key).
		Scan(&a.Standardized, &a.Quality, &a.Granularity, &a.Formatted, &a.Lat, &a.Lng, &a.ErrorRadius, &a.Pincode, &a.City, &a.State)
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	if err != nil {
		return a, false, fmt.Errorf("reading address table: %w", err)
	}
	return a, true, nil
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

// dmUpsertAddress merges res into dm_address. Empty fields never overwrite a
// value stored by an earlier run.
func dmUpsertAddress(ctx context.Context, db *sql.DB, key string, res cleanResult) error {
	var lat, lng sql.NullFloat64
	if res.Lat != nil && res.Lng != nil {
		lat = sql.NullFloat64{Float64: *res.Lat, Valid: true}
		lng = sql.NullFloat64{Float64: *res.Lng, Valid: true}
	}
	var er sql.NullInt64
	if res.ErrorRadius != nil {
		er = sql.NullInt64{Int64: *res.ErrorRadius, Valid: true}
	}
	dmWriteMu.Lock()
	defer dmWriteMu.Unlock()
	_, err := db.ExecContext(ctx, `INSERT INTO dm_address
		(raw_key, raw, standardized, validated_quality, granularity, formatted, lat, lng, error_radius, pincode, city, state, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(raw_key) DO UPDATE SET
			raw = excluded.raw,
			standardized = COALESCE(excluded.standardized, dm_address.standardized),
			validated_quality = COALESCE(excluded.validated_quality, dm_address.validated_quality),
			granularity = COALESCE(excluded.granularity, dm_address.granularity),
			formatted = COALESCE(excluded.formatted, dm_address.formatted),
			lat = COALESCE(excluded.lat, dm_address.lat),
			lng = COALESCE(excluded.lng, dm_address.lng),
			error_radius = COALESCE(excluded.error_radius, dm_address.error_radius),
			pincode = COALESCE(excluded.pincode, dm_address.pincode),
			city = COALESCE(excluded.city, dm_address.city),
			state = COALESCE(excluded.state, dm_address.state),
			updated_at = excluded.updated_at`,
		key, res.Raw, nullStr(res.Standardized), nullStr(res.Quality), nullStr(res.Granularity), nullStr(res.Formatted),
		lat, lng, er, nullStr(res.Pincode), nullStr(res.City), nullStr(res.State), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("saving address: %w", err)
	}
	return nil
}

func printCleanSummary(cmd *cobra.Command, flags *rootFlags, s cleanSummary, rows [][]string) error {
	if flags.csv && s.Out == "" {
		return dmWriteRowsCSV(cmd, cleanColumns, rows)
	}
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return dmPrint(cmd.OutOrStdout(), s, flags)
	}
	w := cmd.OutOrStdout()
	dest := s.Out
	if dest == "" {
		dest = "the table below"
	}
	fmt.Fprintf(w, "Cleaned %d row(s) from %s into %s\n", s.Rows, s.File, dest)
	fmt.Fprintf(w, "  ok %d, cached %d, errors %d, deferred %d\n", s.OK, s.Cached, s.Errors, s.Deferred)
	fmt.Fprintf(w, "  planned calls %d, calls made %d, cache hits %d (max calls %d)\n", s.PlannedCalls, s.CallsMade, s.CacheHits, s.MaxCalls)
	if s.Out == "" && len(rows) > 0 {
		table := make([][]string, 0, len(rows))
		for _, r := range rows {
			clean := make([]string, len(r))
			for i, c := range r {
				clean[i] = dmStripControl(c)
			}
			table = append(table, clean)
		}
		return flags.printTable(cmd, strings.Fields(strings.ToUpper(strings.Join(cleanColumns, " "))), table)
	}
	return nil
}
