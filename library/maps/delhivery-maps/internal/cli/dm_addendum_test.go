// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/store"
)

// openTestAPI builds a dmAPI against the current mock (env) and a fresh db.
func openTestAPI(t *testing.T, flags *rootFlags) (*dmAPI, *sql.DB) {
	t.Helper()
	s := newTestDB(t)
	if flags == nil {
		flags = &rootFlags{timeout: 5 * time.Second}
	}
	api, err := newDMAPI(&cobra.Command{Use: "x"}, flags, s.DB(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return api, s.DB()
}

func statusHandler(code int) func(int, map[string]any) (int, any) {
	return func(int, map[string]any) (int, any) { return code, map[string]any{"error": "forced"} }
}

// ---- A: budget, cap and lockout guards ----

func TestLiveVerifiedDefaultLimits(t *testing.T) {
	// These are the limits read from the real x-ratelimit-limit headers; a change
	// here must be a deliberate, evidence-backed one.
	if dmDefaultDailyBudget != 50 || dmDefaultPlanBudget != 5 || dmBudgetWindow != 24*time.Hour || fleetChunkSize != 50 {
		t.Fatalf("limits changed: pool=%d plan=%d window=%v chunk=%d", dmDefaultDailyBudget, dmDefaultPlanBudget, dmBudgetWindow, fleetChunkSize)
	}
	t.Setenv(dmBudgetEnv, "")
	t.Setenv(dmPlanBudgetEnv, "")
	if dmBucketBudget(dmBucketPool) != 50 || dmBucketBudget(dmBucketPlan) != 5 || dmDailyBudget() != 50 {
		t.Fatal("default bucket budgets are not 50 / 5")
	}
	if dmDefaultAPIBase != "https://gateway-maps-pub-int.delhivery.com" {
		t.Fatalf("the pinned gateway changed: %s", dmDefaultAPIBase)
	}
}

func TestSpendGuardsRefuseBeforeTheNetwork(t *testing.T) {
	ctx := context.Background()
	body := dmAddressBody("x")

	t.Run("a used-up budget refuses, the last call of a budget is allowed", func(t *testing.T) {
		m := newDMMock(t)
		api, db := openTestAPI(t, nil)
		t.Setenv(dmBudgetEnv, "3")
		for i := 0; i < 2; i++ {
			if err := dmRecordCall(ctx, db, api.scope, epGeocode); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := api.post(ctx, epGeocode, body); err != nil { // the 3rd of 3
			t.Fatalf("the last call within the budget must be sent: %v", err)
		}
		if _, err := api.post(ctx, epGeocode, body); !errorsIsRateLimited(err) { // the 4th
			t.Fatalf("an exhausted budget must refuse: %v", err)
		}
		if m.total() != 1 {
			t.Fatalf("server calls = %d, want 1", m.total())
		}
	})
	t.Run("the run cap is inclusive of its own limit and a cap of 1 allows exactly one call", func(t *testing.T) {
		m := newDMMock(t)
		api, _ := openTestAPI(t, nil)
		api.setCap(1)
		if _, err := api.post(ctx, epGeocode, body); err != nil {
			t.Fatal(err)
		}
		if _, err := api.post(ctx, epGeocode, body); !errorsIsRateLimited(err) {
			t.Fatalf("the second call must hit the cap: %v", err)
		}
		if m.total() != 1 {
			t.Fatalf("server calls = %d, want 1", m.total())
		}
	})
	t.Run("a lockout for the scope and bucket refuses", func(t *testing.T) {
		m := newDMMock(t)
		api, db := openTestAPI(t, nil)
		if err := dmRecordLockout(ctx, db, api.scope, dmBucketPool); err != nil {
			t.Fatal(err)
		}
		if _, err := api.post(ctx, epGeocode, body); !errorsIsRateLimited(err) || !strings.Contains(err.Error(), "locked out") {
			t.Fatalf("a locked-out scope must refuse: %v", err)
		}
		// route-plan is another bucket and is not locked.
		m.on(epRoutePlan, func(int, map[string]any) (int, any) { return 200, map[string]any{"ok": true} })
		if _, err := api.post(ctx, epRoutePlan, map[string]any{}); err != nil {
			t.Fatalf("a pool lockout must not block route-plan: %v", err)
		}
		if m.count(epGeocode) != 0 {
			t.Fatal("the locked-out call reached the server")
		}
	})
}

func errorsIsRateLimited(err error) bool {
	return err != nil && strings.Contains(err.Error(), errDMRateLimited.Error())
}

func TestExplicitMaxCallsCapsRetriesToo(t *testing.T) {
	m := newDMMock(t)
	m.on(epGeocode, statusHandler(http.StatusServiceUnavailable))
	t.Setenv(dmBudgetEnv, "50")
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	_, _, err := runCLI(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--steps", "geocode", "--max-calls", "1", "--db", filepath.Join(dir, "dm.db"), "--json")
	if err == nil || ExitCode(err) != 5 {
		t.Fatalf("the only row failed: want exit 5, got %v", err)
	}
	if m.count(epGeocode) != 1 {
		t.Fatalf("hits = %d: a retry exceeded the explicit --max-calls 1", m.count(epGeocode))
	}
}

func TestNegativeMaxCallsIsAUsageErrorEverywhere(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	addr := writeFile(t, dir, "a.csv", "address\nAddr one\n")
	pts := writeFile(t, dir, "p.csv", "lat,lng\n28.5,77.1\n")
	lanes := coordLanes(t, dir, `"28.4,77.0","26.9,75.7"`)
	ord := writeFile(t, dir, "o.csv", ordersCSV(2, false))
	for name, args := range map[string][]string{
		"address clean":  {"address", "clean", "--file", addr, "--out", filepath.Join(dir, "1.csv")},
		"coverage check": {"coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", pts, "--out", filepath.Join(dir, "2.csv")},
		"trip quote":     {"trip", "quote", "--file", lanes},
		"fleet plan":     {"fleet", "plan", "--orders", ord, "--vehicles", "1", "--depot", "28.4,77.0"},
	} {
		_, _, err := runCLI(t, append(args, "--max-calls", "-1", "--db", filepath.Join(dir, "dm.db"), "--json")...)
		if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "--max-calls") {
			t.Errorf("%s: err = %v, want a usage error naming --max-calls", name, err)
		}
	}
	if m.total() != 0 {
		t.Fatalf("calls = %d", m.total())
	}
}

func TestMaxCallsHelpExplainsZeroVersusOmitted(t *testing.T) {
	for _, path := range [][]string{{"address", "clean"}, {"trip", "quote"}, {"coverage", "check"}, {"fleet", "plan"}} {
		text := helpText(t, path...)
		if !strings.Contains(text, "Omit it to use the whole remaining budget") || !strings.Contains(text, "an explicit 0 means make no calls") {
			t.Errorf("%v --help does not explain --max-calls 0 versus omitting it", path)
		}
	}
}

func helpText(t *testing.T, path ...string) string {
	t.Helper()
	root := RootCmd()
	var buf strings.Builder
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append(append([]string{}, path...), "--help"))
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

// ---- B: rate_limited only after a real 429 or a lockout ----

func TestPureBudgetDeferralIsNotReportedAsRateLimited(t *testing.T) {
	t.Run("address clean", func(t *testing.T) {
		newDMMock(t)
		dir := t.TempDir()
		in := writeFile(t, dir, "in.csv", "address\nAddr one\nAddr two\n")
		stdout, stderr := mustRun(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--max-calls", "3", "--db", filepath.Join(dir, "dm.db"), "--json")
		sum := decodeObject(t, stdout)
		if sum["rate_limited"] != false || num(t, sum["deferred"]) != 1 || sum["note"] != nil || strings.Contains(stderr, "429") {
			t.Fatalf("summary=%v stderr=%q", sum, stderr)
		}
	})
	t.Run("coverage check", func(t *testing.T) {
		m := newDMMock(t)
		squareIsochrone(m)
		dir := t.TempDir()
		in := writeFile(t, dir, "in.csv", "address\nAddr one\nAddr two\n")
		stdout, _ := mustRun(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--max-calls", "2", "--db", filepath.Join(dir, "dm.db"), "--json")
		if sum := decodeObject(t, stdout); sum["rate_limited"] != false || num(t, sum["deferred"]) != 1 {
			t.Fatalf("summary = %v", sum)
		}
	})
	t.Run("fleet plan", func(t *testing.T) {
		m := newDMMock(t)
		m.on(epRoutePlan, echoRoutePlan)
		dir := t.TempDir()
		ord := writeFile(t, dir, "o.csv", ordersCSV(120, false))
		stdout, _ := mustRun(t, "fleet", "plan", "--orders", ord, "--vehicles", "1", "--depot", "28.4,77.0", "--max-calls", "1", "--db", filepath.Join(dir, "dm.db"), "--json")
		if sum := decodeObject(t, stdout); sum["rate_limited"] != false || num(t, sum["deferred_chunks"]) != 2 || num(t, sum["calls_made"]) != 1 {
			t.Fatalf("summary = %v", sum)
		}
	})
	t.Run("trip quote", func(t *testing.T) {
		m := newDMMock(t)
		mockRouteAndToll(m, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
		dir := t.TempDir()
		lanes := coordLanes(t, dir, `"28.4,77.0","26.9,75.7"`, `"28.4,77.0","20.0,70.0"`)
		stdout, _ := mustRun(t, "trip", "quote", "--file", lanes, "--max-calls", "2", "--db", filepath.Join(dir, "dm.db"), "--json")
		second := decodeArray(t, stdout)[1].(map[string]any)
		msg, _ := second["error"].(string)
		if second["deferred"] != true || !strings.Contains(msg, "call budget") || strings.Contains(msg, "stopped early") || strings.Contains(msg, "429") {
			t.Fatalf("a budget deferral must say so, not blame a rate limit: %q", msg)
		}
	})
	t.Run("only a real 429 lockout sets it", func(t *testing.T) {
		m := newDMMock(t)
		m.on(epStandardize, statusHandler(http.StatusTooManyRequests))
		dir := t.TempDir()
		in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
		stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--db", filepath.Join(dir, "dm.db"), "--json")
		if decodeObject(t, stdout)["rate_limited"] != true {
			t.Fatal("a 429 must set rate_limited")
		}
	})
}

// ---- C: exit-code table ----

func TestExitCodesForRejectedTokensAndTotalFailure(t *testing.T) {
	type tc struct {
		name      string
		prepare   func(t *testing.T, m *dmMock, dir string, code int)
		args      func(dir string) []string
		outFile   string // expected partial output ("" = none is written)
		endpoint  string // the endpoint that is forced to fail
		calls401  int    // calls to that endpoint before a 401/403 stops the run
		calls400  int    // calls to that endpoint when every item fails with 400
		fatalOnly bool
	}
	cases := []tc{
		{name: "address clean", endpoint: epStandardize, calls401: 1, calls400: 3, outFile: "out.csv",
			prepare: func(t *testing.T, m *dmMock, dir string, code int) { m.on(epStandardize, statusHandler(code)) },
			args: func(dir string) []string {
				in := writeFile(t, dir, "in.csv", "address\nAddr one\nAddr two\nAddr three\n")
				return []string{"address", "clean", "--file", in, "--out", filepath.Join(dir, "out.csv")}
			}},
		{name: "trip single", endpoint: epRoute, calls401: 1, calls400: 1,
			prepare: func(t *testing.T, m *dmMock, dir string, code int) { m.on(epRoute, statusHandler(code)) },
			args: func(dir string) []string {
				return []string{"trip", "quote", "--from", "28.4,77.0", "--to", "26.9,75.7"}
			}},
		{name: "trip --file", endpoint: epRoute, calls401: 1, calls400: 3, outFile: "out.csv",
			prepare: func(t *testing.T, m *dmMock, dir string, code int) { m.on(epRoute, statusHandler(code)) },
			args: func(dir string) []string {
				lanes := coordLanes(t, dir, `"28.4,77.0","26.9,75.7"`, `"28.4,77.0","27.0,75.0"`, `"28.4,77.0","27.5,75.5"`)
				return []string{"trip", "quote", "--file", lanes, "--out", filepath.Join(dir, "out.csv")}
			}},
		{name: "coverage check", endpoint: epGeocode, calls401: 1, calls400: 3, outFile: "out.csv",
			prepare: func(t *testing.T, m *dmMock, dir string, code int) {
				squareIsochrone(m)
				m.on(epGeocode, statusHandler(code))
			},
			args: func(dir string) []string {
				in := writeFile(t, dir, "in.csv", "address\nAddr one\nAddr two\nAddr three\n")
				return []string{"coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", in, "--out", filepath.Join(dir, "out.csv")}
			}},
		{name: "fleet plan", endpoint: epRoutePlan, calls401: 1, calls400: 2,
			prepare: func(t *testing.T, m *dmMock, dir string, code int) { m.on(epRoutePlan, statusHandler(code)) },
			args: func(dir string) []string {
				ord := writeFile(t, dir, "o.csv", ordersCSV(60, false)) // two chunks
				return []string{"fleet", "plan", "--orders", ord, "--vehicles", "1", "--depot", "28.4,77.0", "--out", filepath.Join(dir, "out.csv")}
			}},
	}
	for _, c := range cases {
		for _, code := range []int{401, 403, 400} {
			t.Run(fmt.Sprintf("%s/%d", c.name, code), func(t *testing.T) {
				m := newDMMock(t)
				mockRouteAndToll(m, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
				dir := t.TempDir()
				c.prepare(t, m, dir, code)
				_, _, err := runCLI(t, append(c.args(dir), "--db", filepath.Join(dir, "dm.db"), "--json")...)
				wantExit, wantCalls := 5, c.calls400
				if code != 400 {
					wantExit, wantCalls = 4, c.calls401
				}
				if err == nil || ExitCode(err) != wantExit {
					t.Fatalf("exit = %v, want %d", err, wantExit)
				}
				if got := m.count(c.endpoint); got != wantCalls {
					t.Fatalf("%s calls = %d, want %d (a rejected token must stop the run at once)", c.endpoint, got, wantCalls)
				}
				// The partial output is still written for the commands that write one per item.
				_, statErr := os.Stat(filepath.Join(dir, "out.csv"))
				if c.outFile != "" && statErr != nil {
					t.Fatalf("partial output not written: %v", statErr)
				}
				if c.outFile == "" && c.name == "fleet plan" && statErr == nil {
					t.Fatal("fleet plan has no plan to write when no chunk succeeded")
				}
			})
		}
	}
}

// ---- D: dm_address is merged, never wiped ----

type addrState struct {
	standardized, quality, granularity, formatted, pincode, city, state sql.NullString
	lat, lng                                                            sql.NullFloat64
	radius                                                              sql.NullInt64
}

func readAddr(t *testing.T, dbPath, raw string) addrState {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var a addrState
	err = s.DB().QueryRow(`SELECT standardized, validated_quality, granularity, formatted, lat, lng, error_radius, pincode, city, state FROM dm_address WHERE raw_key = ?`, addrkit.HashKey(raw)).
		Scan(&a.standardized, &a.quality, &a.granularity, &a.formatted, &a.lat, &a.lng, &a.radius, &a.pincode, &a.city, &a.state)
	if err != nil {
		t.Fatalf("reading dm_address: %v", err)
	}
	return a
}

func seedFullAddress(t *testing.T, db, raw string) {
	t.Helper()
	execDB(t, db, `INSERT INTO dm_address (raw_key, raw, standardized, validated_quality, granularity, formatted, lat, lng, error_radius, pincode, city, state, updated_at)
		VALUES (?, ?, 'STD seeded', 'ok', 'PREMISE', 'SEEDED FORMATTED', 26.85, 75.8, 12, '302017', 'Jaipur', 'Rajasthan', ?)`,
		addrkit.HashKey(raw), raw, time.Now().UTC().Format(time.RFC3339))
}

func TestAddressUpsertNeverWipesStoredFields(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	const raw = "Plot 9 Malviya Nagar Jaipur"
	in := writeFile(t, dir, "in.csv", "address\n"+raw+"\n")
	seedFullAddress(t, db, raw)
	m.on(epValidate, func(int, map[string]any) (int, any) {
		return 200, map[string]any{"quality": "bad", "granularity_level": "LOCALITY", "formatted_address": ""}
	})
	mustRun(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--steps", "validate", "--db", db, "--json")
	a := readAddr(t, db, raw)
	if a.quality.String != "bad" || a.granularity.String != "LOCALITY" {
		t.Fatalf("the validated fields must be updated: %+v", a)
	}
	if a.lat.Float64 != 26.85 || a.lng.Float64 != 75.8 || a.radius.Int64 != 12 || a.pincode.String != "302017" || a.city.String != "Jaipur" || a.state.String != "Rajasthan" || a.standardized.String != "STD seeded" || a.formatted.String != "SEEDED FORMATTED" {
		t.Fatalf("a validate-only re-run lost stored fields: %+v", a)
	}

	// A geocode-only re-run: new coordinates, error radius rounded 45.6 -> 46,
	// and an empty pincode in the reply does not erase the stored one.
	m.on(epGeocode, func(int, map[string]any) (int, any) {
		return 200, map[string]any{"lat": 27.1, "lng": 76.2, "error_radius": 45.6, "metadata": map[string]any{"pincode": ""}}
	})
	mustRun(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--force", "--steps", "geocode", "--db", db, "--json")
	a = readAddr(t, db, raw)
	if a.lat.Float64 != 27.1 || a.lng.Float64 != 76.2 || a.radius.Int64 != 46 {
		t.Fatalf("geocode fields: %+v (error_radius 45.6 must round to 46)", a)
	}
	if a.quality.String != "bad" || a.pincode.String != "302017" || a.city.String != "Jaipur" || a.standardized.String != "STD seeded" {
		t.Fatalf("a geocode-only re-run lost stored fields: %+v", a)
	}
	tb, _ := addrkit.ReadTable(filepath.Join(dir, "o.csv"))
	if tb.Rows[0][tb.Col("error_radius")] != "46" {
		t.Fatalf("csv error_radius = %q", tb.Rows[0][tb.Col("error_radius")])
	}
}

func TestResumeRunsOnlyTheStepsStillMissing(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	out := filepath.Join(dir, "o.csv")
	const stdOnly, valOnly = "Standardized only addr", "Validated only addr"
	now := time.Now().UTC().Format(time.RFC3339)
	execDB(t, db, `INSERT INTO dm_address (raw_key, raw, standardized, formatted, updated_at) VALUES (?, ?, 'STD x', 'STD x', ?)`, addrkit.HashKey(stdOnly), stdOnly, now)
	execDB(t, db, `INSERT INTO dm_address (raw_key, raw, validated_quality, granularity, formatted, updated_at) VALUES (?, ?, 'ok', 'PREMISE', 'VAL x', ?)`, addrkit.HashKey(valOnly), valOnly, now)
	run := func(addr string, steps string) map[string]any {
		t.Helper()
		in := writeFile(t, dir, "in.csv", "address\n"+addr+"\n")
		stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--resume", "--steps", steps, "--db", db, "--json")
		return decodeObject(t, stdout)
	}
	reset := func() {
		for _, p := range []string{epStandardize, epValidate, epGeocode} {
			m.mu.Lock()
			m.hits[p] = 0
			m.mu.Unlock()
		}
	}
	// Complete for the selected step: nothing is called.
	if sum := run(stdOnly, "standardize"); num(t, sum["calls_made"]) != 0 || num(t, sum["cached"]) != 1 {
		t.Fatalf("standardize-only row, --steps standardize: %v", sum)
	}
	if sum := run(valOnly, "validate"); num(t, sum["calls_made"]) != 0 {
		t.Fatalf("validate-only row, --steps validate: %v", sum)
	}
	if m.total() != 0 {
		t.Fatalf("calls = %d", m.total())
	}
	// Missing a selected step: only that step is called.
	run(stdOnly, "standardize,validate,geocode")
	if m.count(epStandardize) != 0 || m.count(epValidate) != 1 || m.count(epGeocode) != 1 {
		t.Fatalf("standardize-only row, all steps: standardize=%d validate=%d geocode=%d, want 0/1/1", m.count(epStandardize), m.count(epValidate), m.count(epGeocode))
	}
	reset()
	run(valOnly, "standardize,validate,geocode")
	if m.count(epStandardize) != 1 || m.count(epValidate) != 0 || m.count(epGeocode) != 1 {
		t.Fatalf("validate-only row, all steps: standardize=%d validate=%d geocode=%d, want 1/0/1", m.count(epStandardize), m.count(epValidate), m.count(epGeocode))
	}
	a := readAddr(t, db, valOnly)
	if a.quality.String != "ok" || !a.lat.Valid || a.standardized.String == "" {
		t.Fatalf("merged row = %+v", a)
	}
}

func TestPlanningStaysBlockedAfterTheFirstRowThatDoesNotFit(t *testing.T) {
	newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	out := filepath.Join(dir, "o.csv")
	// Row C has standardize+validate cached, so it needs only 1 call.
	warm := writeFile(t, dir, "warm.csv", "address\nRow C\n")
	mustRun(t, "address", "clean", "--file", warm, "--out", out, "--steps", "standardize,validate", "--db", db, "--json")
	in := writeFile(t, dir, "in.csv", "address\nRow A\nRow B\nRow C\n")
	stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--max-calls", "4", "--db", db, "--json")
	sum := decodeObject(t, stdout)
	tb, _ := addrkit.ReadTable(out)
	var st []string
	for _, r := range tb.Rows {
		st = append(st, r[tb.Col("status")])
	}
	// A needs 3 (fits), B needs 3 (3+3 > 4: blocked), C needs 1 (would fit, but the plan is blocked).
	if !reflect.DeepEqual(st, []string{"ok", "deferred", "deferred"}) || num(t, sum["planned_calls"]) != 3 {
		t.Fatalf("statuses = %v summary = %v", st, sum)
	}
}

func TestAddressWriteFailurePropagates(t *testing.T) {
	newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	mustRun(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "first.csv"), "--db", db, "--json")
	execDB(t, db, `ALTER TABLE dm_address RENAME COLUMN city TO city_gone`)
	in2 := writeFile(t, dir, "in2.csv", "address\nAddr two\n")
	_, _, err := runCLI(t, "address", "clean", "--file", in2, "--out", filepath.Join(dir, "second.csv"), "--db", db, "--json")
	if err == nil || !strings.Contains(err.Error(), "saving address") {
		t.Fatalf("a dm_address write failure must fail the command, got %v", err)
	}
}

// ---- E: cache lifetime, scope and --no-cache ----

func backdate(t *testing.T, db, endpoint string, age time.Duration) {
	t.Helper()
	execDB(t, db, `UPDATE dm_cache SET fetched_at = ? WHERE endpoint = ?`, time.Now().Add(-age).UTC().Format(time.RFC3339), endpoint)
}

func TestExpiredCacheRowsAreRefetchedEndToEnd(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	out := filepath.Join(dir, "o.csv")
	clean := func() {
		mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--steps", "geocode", "--db", db, "--json")
	}
	clean()
	backdate(t, db, epGeocode, 25*time.Hour) // far inside a 30-day TTL
	clean()
	if m.count(epGeocode) != 1 {
		t.Fatalf("a 25h-old geocode must still be cached: hits = %d", m.count(epGeocode))
	}
	backdate(t, db, epGeocode, dmAddressCacheTTL+time.Hour)
	clean()
	if m.count(epGeocode) != 2 {
		t.Fatalf("a geocode older than the TTL must be refetched: hits = %d", m.count(epGeocode))
	}

	// Routes expire after 24 hours; the toll lookup is cached independently.
	mockRouteAndToll(m, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
	quote := func() {
		mustRun(t, "trip", "quote", "--from", "28.4,77.0", "--to", "26.9,75.7", "--db", db, "--json")
	}
	quote()
	backdate(t, db, epRoute, dmRouteCacheTTL-time.Hour)
	quote()
	if m.count(epRoute) != 1 {
		t.Fatalf("a 23h-old route must still be cached: hits = %d", m.count(epRoute))
	}
	backdate(t, db, epRoute, dmRouteCacheTTL+time.Hour)
	quote()
	if m.count(epRoute) != 2 || m.count(epToll) != 1 {
		t.Fatalf("an expired route must be refetched (route=%d) while the fresh toll is not (toll=%d)", m.count(epRoute), m.count(epToll))
	}
}

func TestCacheScopeChangesWithTheBaseURL(t *testing.T) {
	m1 := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	out := filepath.Join(dir, "o.csv")
	run := func() {
		mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--steps", "geocode", "--db", db, "--json")
	}
	run()
	run()
	if m1.total() != 1 {
		t.Fatalf("same base URL must share the cache: %d", m1.total())
	}
	m2 := newDMMock(t) // a second server: a different base URL (and port)
	run()
	if m2.total() != 1 {
		t.Fatalf("a different base URL must not read the first one's cache: %d", m2.total())
	}
}

func TestNoCachePlannerCountsEveryCallAsUncached(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	out := filepath.Join(dir, "o.csv")
	mustRun(t, "address", "clean", "--file", in, "--out", out, "--db", db, "--json")
	stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--no-cache", "--db", db, "--json")
	if sum := decodeObject(t, stdout); num(t, sum["planned_calls"]) != 3 || num(t, sum["calls_made"]) != 3 {
		t.Fatalf("--no-cache must plan and make all 3 calls: %v", sum)
	}
	before := m.total()
	stdout, _ = mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--no-cache", "--max-calls", "2", "--db", db, "--json")
	if sum := decodeObject(t, stdout); num(t, sum["deferred"]) != 1 || m.total() != before {
		t.Fatalf("--no-cache with a cap of 2: %v, new calls %d", sum, m.total()-before)
	}
}

// ---- F: trip ----

func TestTripLaneCostAgainstASeededCache(t *testing.T) {
	m := newDMMock(t)
	mockRouteAndToll(m, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	mustRun(t, "trip", "quote", "--from", "Sector 45 Gurgaon", "--to", "Jaipur", "--db", db, "--json")
	if m.total() != 4 {
		t.Fatalf("seed run calls = %d, want 4", m.total())
	}
	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	newAPI := func(noCache bool) *dmAPI {
		api, err := newDMAPI(&cobra.Command{Use: "x"}, &rootFlags{timeout: 5 * time.Second, noCache: noCache}, s.DB(), 0)
		if err != nil {
			t.Fatal(err)
		}
		return api
	}
	cost := func(api *dmAPI, from, to string, opts tripOptions) int {
		t.Helper()
		n, err := tripLaneCost(context.Background(), api, &tripQuote{From: from, To: to, Vehicle: "car_jeep_van"}, opts)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	auto := tripOptions{travelMode: "auto"}
	api := newAPI(false)
	if got := cost(api, "Sector 45 Gurgaon", "Jaipur", auto); got != 0 {
		t.Errorf("everything cached: cost = %d, want 0", got)
	}
	if got := cost(api, "sector 45  gurgaon", "JAIPUR", auto); got != 0 {
		t.Errorf("the address cache key ignores case and spacing: cost = %d, want 0", got)
	}
	if got := cost(api, "Sector 45 Gurgaon", "Pune", auto); got != 3 {
		t.Errorf("one uncached address: cost = %d, want 1 geocode + route + toll = 3", got)
	}
	if got := cost(api, "Pune", "Nagpur", auto); got != 4 {
		t.Errorf("two uncached addresses: cost = %d, want 4", got)
	}
	if got := cost(api, "Sector 45 Gurgaon", "Jaipur", tripOptions{travelMode: "auto", traffic: true}); got != 2 {
		t.Errorf("a traffic-aware route is never cached: cost = %d, want 2", got)
	}
	if got := cost(api, "Sector 45 Gurgaon", "Jaipur", tripOptions{travelMode: "auto", departure: "2026-11-03T09:30:00+05:30"}); got != 2 {
		t.Errorf("a --departure-time route is never cached: cost = %d, want 2", got)
	}
	if got := cost(newAPI(true), "Sector 45 Gurgaon", "Jaipur", auto); got != 4 {
		t.Errorf("--no-cache: cost = %d, want 4", got)
	}
	// Drop the toll row: only the toll call is left.
	execDB(t, db, `DELETE FROM dm_cache WHERE endpoint = ?`, epToll)
	if got := cost(api, "Sector 45 Gurgaon", "Jaipur", auto); got != 1 {
		t.Errorf("route cached, toll not: cost = %d, want 1", got)
	}
	// Drop the route row too: route + toll.
	execDB(t, db, `DELETE FROM dm_cache WHERE endpoint = ?`, epRoute)
	if got := cost(api, "Sector 45 Gurgaon", "Jaipur", auto); got != 2 {
		t.Errorf("geocodes cached only: cost = %d, want 2", got)
	}
}

func TestTripTollRequestCarriesNoAddressTextAndRoundsTheDistance(t *testing.T) {
	m := newDMMock(t)
	m.on(epRoute, func(int, map[string]any) (int, any) {
		return 200, realRoute(12.3456, 750, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
	})
	m.on(epToll, func(int, map[string]any) (int, any) { return 200, realToll(10, realPlaza("P", "id", 10)) })
	stdout, _ := mustRun(t, "trip", "quote", "--from", "Sector 45 Gurgaon", "--to", "Jaipur Railway Station", "--db", tempDB(t), "--json")
	q := decodeObject(t, stdout)
	if num(t, q["distance_km"]) != 12.35 || num(t, q["duration_min"]) != 12.5 {
		t.Fatalf("distance_km=%v duration_min=%v, want 12.35 and 12.5", q["distance_km"], q["duration_min"])
	}
	raw, _ := json.Marshal(m.bodies(epToll)[0])
	for _, leak := range []string{"gurgaon", "jaipur", "station", "sector"} {
		if strings.Contains(strings.ToLower(string(raw)), leak) {
			t.Fatalf("the toll request leaks %q: %s", leak, raw)
		}
	}
	if !strings.Contains(string(raw), `"route_name":"route"`) {
		t.Fatalf("toll body = %s", raw)
	}
}

func TestTripFileAndFromToAreMutuallyExclusive(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	lanes := coordLanes(t, dir, `"28.4,77.0","26.9,75.7"`)
	_, _, err := runCLI(t, "trip", "quote", "--file", lanes, "--from", "28.4,77.0", "--db", tempDB(t))
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "--file or --from/--to") {
		t.Fatalf("err = %v", err)
	}
	if m.total() != 0 {
		t.Fatal("calls made")
	}
}

// ---- G: dupes, report, purge ----

func dupeSizes(t *testing.T, db string) ([]int, []string) {
	t.Helper()
	stdout, _ := mustRun(t, "address", "dupes", "--db", db, "--json")
	var sizes []int
	var reasons []string
	for _, g := range decodeArray(t, stdout) {
		m := g.(map[string]any)
		sizes = append(sizes, len(m["members"].([]any)))
		reasons = append(reasons, m["reason"].(string))
	}
	return sizes, reasons
}

func TestAddressDupesOrderingEligibilityAndNoDoubleReporting(t *testing.T) {
	newDMMock(t)
	t.Run("largest group first", func(t *testing.T) {
		db := tempDB(t)
		seedAddresses(t, db, []seedAddr{
			{key: "t1", raw: "t1", formatted: "Same text one", granularity: "PREMISE", lat: 10.0, lng: 70.0},
			{key: "t2", raw: "t2", formatted: "same text one", granularity: "PREMISE", lat: 11.0, lng: 71.0},
			{key: "g1", raw: "g1", formatted: "geo a", granularity: "PREMISE", lat: 26.85, lng: 75.8},
			{key: "g2", raw: "g2", formatted: "geo b", granularity: "PREMISE", lat: 26.85005, lng: 75.8},
			{key: "g3", raw: "g3", formatted: "geo c", granularity: "PREMISE", lat: 26.8501, lng: 75.8},
		})
		sizes, reasons := dupeSizes(t, db)
		if !reflect.DeepEqual(sizes, []int{3, 2}) || !reflect.DeepEqual(reasons, []string{"geo", "text"}) {
			t.Fatalf("sizes=%v reasons=%v, want the 3-member group first", sizes, reasons)
		}
	})
	t.Run("error radius boundary", func(t *testing.T) {
		db := tempDB(t)
		seedAddresses(t, db, []seedAddr{
			{key: "a1", raw: "a1", formatted: "A one", granularity: "PREMISE", radius: 100, lat: 26.85, lng: 75.8},
			{key: "a2", raw: "a2", formatted: "A two", granularity: "PREMISE", radius: 100, lat: 26.85, lng: 75.8},
			{key: "b1", raw: "b1", formatted: "B one", granularity: "PREMISE", radius: 101, lat: 22.0, lng: 72.0},
			{key: "b2", raw: "b2", formatted: "B two", granularity: "PREMISE", radius: 101, lat: 22.0, lng: 72.0},
			{key: "c1", raw: "c1", formatted: "C one", granularity: "PREMISE", lat: 18.0, lng: 73.0}, // unknown radius is eligible
			{key: "c2", raw: "c2", formatted: "C two", granularity: "PREMISE", lat: 18.0, lng: 73.0},
		})
		stdout, _ := mustRun(t, "address", "dupes", "--db", db, "--json")
		var raws []string
		for _, g := range decodeArray(t, stdout) {
			for _, mem := range g.(map[string]any)["members"].([]any) {
				raws = append(raws, mem.(map[string]any)["raw"].(string))
			}
		}
		if !reflect.DeepEqual(raws, []string{"a1", "a2", "c1", "c2"}) && !reflect.DeepEqual(raws, []string{"c1", "c2", "a1", "a2"}) {
			t.Fatalf("grouped rows = %v: radius 100 and unknown are eligible, 101 is not", raws)
		}
	})
	t.Run("granularity is case-insensitive", func(t *testing.T) {
		db := tempDB(t)
		seedAddresses(t, db, []seedAddr{
			{key: "a", raw: "a", formatted: "One", granularity: "premise", lat: 26.85, lng: 75.8},
			{key: "b", raw: "b", formatted: "Two", granularity: "Street_Landmark", lat: 26.85, lng: 75.8},
		})
		if sizes, _ := dupeSizes(t, db); !reflect.DeepEqual(sizes, []int{2}) {
			t.Fatalf("mixed-case granularity values must group: %v", sizes)
		}
	})
	t.Run("a geo cluster sharing one text key is reported once", func(t *testing.T) {
		db := tempDB(t)
		seedAddresses(t, db, []seedAddr{
			{key: "a", raw: "a", formatted: "Plot 9, Malviya Nagar", granularity: "PREMISE", lat: 26.85, lng: 75.8},
			{key: "b", raw: "b", formatted: "plot 9 malviya nagar", granularity: "PREMISE", lat: 26.85, lng: 75.8},
		})
		if sizes, reasons := dupeSizes(t, db); !reflect.DeepEqual(sizes, []int{2}) || reasons[0] != "text" {
			t.Fatalf("sizes=%v reasons=%v: the pair must appear once, as text", sizes, reasons)
		}
	})
}

func TestAddressReportQualityIsCaseInsensitive(t *testing.T) {
	newDMMock(t)
	db := tempDB(t)
	seedAddresses(t, db, []seedAddr{
		{key: "a", raw: "a", quality: "OK", granularity: "premise", pincode: "122003"},
		{key: "b", raw: "b", quality: "Ok", granularity: "PREMISE", pincode: "122003"},
		{key: "c", raw: "c", quality: "bad", granularity: "LOCALITY", pincode: "122003"},
	})
	stdout, _ := mustRun(t, "address", "report", "--group-by", "pincode", "--db", db, "--json")
	g := decodeArray(t, stdout)[0].(map[string]any)
	if num(t, g["pct_ok"]) != 66.7 || num(t, g["pct_premise"]) != 66.7 {
		t.Fatalf("group = %v", g)
	}
}

func TestPurgeRemovesRowsWithUnknownAge(t *testing.T) {
	newDMMock(t)
	db := tempDB(t)
	seedAddresses(t, db, []seedAddr{{key: "a", raw: "a", granularity: "PREMISE", quality: "ok"}})
	execDB(t, db, `UPDATE dm_address SET updated_at = NULL`)
	execDB(t, db, `INSERT INTO dm_cache (endpoint, key, request, response, fetched_at) VALUES ('/geocode', 'k', '{}', '{}', NULL)`)
	stdout, _ := mustRun(t, "address", "purge", "--older-than", "30d", "--db", db, "--json")
	if res := decodeObject(t, stdout); num(t, res["deleted_address"]) != 1 || num(t, res["deleted_cache"]) != 1 {
		t.Fatalf("rows with a NULL timestamp must be purged: %v", res)
	}
}

// ---- H: help text derives from the constants ----

func TestHelpTextIsDerivedFromTheConstants(t *testing.T) {
	if dmHumanDuration(24*time.Hour) != "24 hours" || dmHumanDuration(30*24*time.Hour) != "30 days" || dmHumanDuration(48*time.Hour) != "2 days" {
		t.Fatalf("dmHumanDuration: %q %q %q", dmHumanDuration(24*time.Hour), dmHumanDuration(720*time.Hour), dmHumanDuration(48*time.Hour))
	}
	want := map[string][]string{
		"address clean":  {dmAddressTTLText, dmWindowText, fmt.Sprint(dmDefaultDailyBudget)},
		"trip quote":     {dmAddressTTLText, dmRouteTTLText, dmWindowText, fmt.Sprint(tripMaxGeometryPoints), fmt.Sprint(dmCallsPerTrip), fmt.Sprint(dmDefaultDailyBudget)},
		"coverage check": {dmAddressTTLText, dmRouteTTLText, dmWindowText, fmt.Sprint(dmDefaultDailyBudget)},
		"fleet plan":     {dmWindowText, fmt.Sprint(fleetChunkSize), fmt.Sprint(dmDefaultPlanBudget), fleetMinCallTimeout.String()},
	}
	for cmd, tokens := range want {
		text := helpText(t, strings.Fields(cmd)...)
		for _, tok := range tokens {
			if !strings.Contains(text, tok) {
				t.Errorf("%s --help does not mention %q", cmd, tok)
			}
		}
	}
	// The help for the documented behaviours matches what the code does.
	trip := helpText(t, "trip", "quote")
	if !strings.Contains(trip, "a 429 or a rejected token (401/403) does") {
		t.Error("trip quote --help must say a 429 or 401/403 stops the lanes")
	}
	if !strings.Contains(helpText(t, "fleet", "plan"), "no summary is printed") {
		t.Error("fleet plan --help must say all-chunks-failed prints no summary")
	}
	// No hard-coded copy of a constant in the help strings of the commands.
	literal := regexp.MustCompile(`30 days|24 hours|24-hour|per 24|chunks of 50|up to 50 orders|about 50 `)
	for _, f := range []string{"address_clean.go", "trip_quote.go", "coverage_check.go", "fleet_plan.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if literal.MatchString(line) {
				t.Errorf("%s:%d hard-codes a value that has a constant: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

func TestOnlyTheClientPathMapsA429(t *testing.T) {
	m := newDMMock(t)
	m.on(epGeocode, statusHandler(http.StatusTooManyRequests))
	api, _ := openTestAPI(t, nil)
	_, _, err := api.c.Post(context.Background(), epGeocode, dmAddressBody("x"))
	if err == nil || !dmIsRateLimited(err) {
		t.Fatalf("the generated client must surface a 429 as a rate-limit error on its own, got %v", err)
	}
	if dmIsRateLimited(fmt.Errorf("some other failure")) {
		t.Fatal("an unrelated error must not look rate limited")
	}
}
