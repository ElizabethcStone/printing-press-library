// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/client"
)

// ---- address purge ----

func TestAddressPurgeRefusesOverflowingAndZeroAgesWithoutYes(t *testing.T) {
	newDMMock(t)
	db := tempDB(t)
	seedAddresses(t, db, []seedAddr{{key: "a", raw: "a", granularity: "PREMISE", quality: "ok"}})
	for _, age := range []string{"999999999d", "106752d", "36501d", "9999999999999h"} {
		_, _, err := runCLI(t, "address", "purge", "--older-than", age, "--db", db, "--yes")
		if err == nil || ExitCode(err) != 2 {
			t.Errorf("--older-than %s must be a usage error, got %v", age, err)
		}
	}
	for _, age := range []string{"0d", "0s", "0"} {
		_, _, err := runCLI(t, "address", "purge", "--older-than", age, "--db", db)
		if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "--yes") {
			t.Errorf("--older-than %s without --yes: %v", age, err)
		}
	}
	if countRows(t, db, "dm_address") != 1 {
		t.Fatal("a refused purge deleted rows")
	}
	if _, err := parsePurgeAge("36500d"); err != nil {
		t.Errorf("the maximum age must be accepted: %v", err)
	}
	if d, err := parsePurgeAge("36500d"); err != nil || d != 36500*24*time.Hour {
		t.Errorf("36500d = %v, %v", d, err)
	}
}

func TestAddressPurgeReallyRemovesTheDataFromTheDatabaseFile(t *testing.T) {
	newDMMock(t)
	db := tempDB(t)
	const secret = "SECRET-ADDRESS-9f3a7c-Plot 12 Malviya Nagar"
	seedAddresses(t, db, []seedAddr{{key: "k1", raw: secret, formatted: secret, granularity: "PREMISE", quality: "ok"}})
	execDB(t, db, `INSERT INTO dm_cache (endpoint, key, request, response, fetched_at) VALUES ('/geocode', 'c1', ?, ?, ?)`,
		`{"address":"`+secret+`"}`, `{"formatted":"`+secret+`"}`, time.Now().UTC().Format(time.RFC3339))
	read := func() []byte {
		var all []byte
		for _, suffix := range []string{"", "-wal", "-journal"} {
			if b, err := os.ReadFile(db + suffix); err == nil {
				all = append(all, b...)
			}
		}
		return all
	}
	if !bytes.Contains(read(), []byte("SECRET-ADDRESS-9f3a7c")) {
		t.Fatal("test setup: the seeded address is not in the database files")
	}
	stdout, _ := mustRun(t, "address", "purge", "--all", "--yes", "--db", db, "--json")
	res := decodeObject(t, stdout)
	if num(t, res["deleted_address"]) != 1 || num(t, res["deleted_cache"]) != 1 {
		t.Fatalf("result = %v", res)
	}
	if bytes.Contains(read(), []byte("SECRET-ADDRESS-9f3a7c")) {
		t.Fatal("the purged address is still readable in the database file or its WAL")
	}
}

// ---- quota buckets, scopes, lockouts ----

func TestBudgetStateIsScopedPerTokenBucketAndIgnoresFutureLockouts(t *testing.T) {
	ctx := context.Background()
	s := newTestDB(t)
	db := s.DB()
	t.Setenv(dmBudgetEnv, "10")
	t.Setenv(dmPlanBudgetEnv, "5")
	now := time.Now().Unix()
	ins := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// 4 pool calls and 2 plan calls for scope A; 100 legacy rows with no scope.
	for i := 0; i < 4; i++ {
		ins(`INSERT INTO dm_calls (called_at, endpoint, scope) VALUES (?, '/geocode', 'A')`, now)
	}
	for i := 0; i < 2; i++ {
		ins(`INSERT INTO dm_calls (called_at, endpoint, scope) VALUES (?, '/route-plan', 'A')`, now)
	}
	for i := 0; i < 100; i++ {
		ins(`INSERT INTO dm_calls (called_at, endpoint) VALUES (?, '/geocode')`, now)
	}
	state := func(scope, bucket string) (int, bool) {
		t.Helper()
		r, l, err := dmBudgetState(ctx, db, scope, bucket)
		if err != nil {
			t.Fatal(err)
		}
		return r, l
	}
	if r, l := state("A", dmBucketPool); r != 6 || l {
		t.Errorf("A/pool = %d,%v want 6 (the plan calls are not in the pool)", r, l)
	}
	if r, _ := state("A", dmBucketPlan); r != 3 {
		t.Errorf("A/plan = %d want 3", r)
	}
	if r, _ := state("B", dmBucketPool); r != 10 {
		t.Errorf("another scope must start with a full budget, got %d", r)
	}

	// A lockout blocks only its own scope and bucket.
	ins(`INSERT INTO dm_lockout (locked_at, bucket, scope) VALUES (?, ?, 'A')`, now, dmBucketPlan)
	if _, l := state("A", dmBucketPlan); !l {
		t.Error("lockout not seen")
	}
	if r, l := state("A", dmBucketPool); l || r != 6 {
		t.Errorf("a route-plan lockout must not lock the pool: %d,%v", r, l)
	}
	if _, l := state("B", dmBucketPlan); l {
		t.Error("a lockout must not leak to another scope")
	}
	// A lockout in the future (clock skew) is ignored; an expired one too.
	ins(`INSERT INTO dm_lockout (locked_at, bucket, scope) VALUES (?, ?, 'C')`, now+3600, dmBucketPool)
	if _, l := state("C", dmBucketPool); l {
		t.Error("a future lockout time must be ignored")
	}
	ins(`INSERT INTO dm_lockout (locked_at, bucket, scope) VALUES (?, ?, 'D')`, now-int64(dmLockoutDuration/time.Second)-60, dmBucketPool)
	if _, l := state("D", dmBucketPool); l {
		t.Error("an expired lockout must be ignored")
	}
}

func TestRoutePlanHasItsOwnQuotaBucket(t *testing.T) {
	m := newDMMock(t)
	m.on(epRoutePlan, echoRoutePlan)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	orders := writeFile(t, dir, "orders.csv", ordersCSV(120, false)) // 3 chunks
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	fleet := func(extra ...string) map[string]any {
		t.Helper()
		stdout, _ := mustRun(t, append([]string{"fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--out", filepath.Join(dir, "p.csv"), "--force", "--db", db, "--json"}, extra...)...)
		return decodeObject(t, stdout)
	}

	// The shared pool being exhausted does not stop route-plan.
	t.Setenv(dmBudgetEnv, "0")
	sum := fleet()
	if num(t, sum["calls_made"]) != 3 || num(t, sum["max_calls"]) != float64(dmDefaultPlanBudget) {
		t.Fatalf("fleet with an empty pool: %v", sum)
	}
	// Those 3 calls used 3 of the 5 route-plan calls: only 2 chunks fit now.
	sum = fleet()
	if num(t, sum["max_calls"]) != 2 || num(t, sum["calls_made"]) != 2 || num(t, sum["deferred_chunks"]) != 1 || num(t, sum["deferred_orders"]) != 20 {
		t.Fatalf("second fleet run: %v", sum)
	}
	// And route-plan calls never ate into the pool.
	t.Setenv(dmBudgetEnv, "3")
	stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "c.csv"), "--db", db, "--json")
	if sum := decodeObject(t, stdout); num(t, sum["budget_remaining_before"]) != 3 || num(t, sum["ok"]) != 1 {
		t.Fatalf("clean after fleet runs: %v", sum)
	}
	// The plan budget is its own knob.
	t.Setenv(dmPlanBudgetEnv, "0")
	_, _, err := runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--db", db, "--json")
	if err == nil || ExitCode(err) != 7 {
		t.Fatalf("an empty route-plan budget must exit 7, got %v", err)
	}
}

func TestA429OnOneBucketDoesNotLockTheOther(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	orders := writeFile(t, dir, "orders.csv", ordersCSV(3, false))
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")

	// 429 on route-plan: geocode/validate/standardize keep working.
	m.on(epRoutePlan, func(int, map[string]any) (int, any) {
		return http.StatusTooManyRequests, map[string]any{"error": "plan quota"}
	})
	if _, _, err := runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--db", db, "--json"); err == nil || ExitCode(err) != 7 {
		t.Fatalf("fleet: %v", err)
	}
	stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "a.csv"), "--db", db, "--json")
	if sum := decodeObject(t, stdout); num(t, sum["ok"]) != 1 || sum["rate_limited"] != false {
		t.Fatalf("a route-plan 429 locked the shared pool: %v", sum)
	}
	// 429 on the pool: route-plan keeps working.
	m.on(epGeocode, func(int, map[string]any) (int, any) {
		return http.StatusTooManyRequests, map[string]any{"error": "pool quota"}
	})
	in2 := writeFile(t, dir, "in2.csv", "address\nAddr two\n")
	stdout, _ = mustRun(t, "address", "clean", "--file", in2, "--out", filepath.Join(dir, "b.csv"), "--steps", "geocode", "--db", db, "--json")
	if decodeObject(t, stdout)["rate_limited"] != true {
		t.Fatal("expected the pool 429 to be reported")
	}
	m.on(epRoutePlan, echoRoutePlan)
	// The pool lockout is in db: route-plan there still works.
	execDB(t, db, `DELETE FROM dm_lockout WHERE bucket = 'plan'`)
	stdout, _ = mustRun(t, "fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--db", db, "--json")
	if sum := decodeObject(t, stdout); num(t, sum["assigned"]) != 3 {
		t.Fatalf("a pool 429 locked route-plan: %v", sum)
	}
}

func TestQuotaAndLockoutDoNotLeakAcrossTokens(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "address\nAddr one\nAddr two\n")
	t.Setenv(dmBudgetEnv, "3")
	mustRun(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--db", db, "--json")
	if m.total() != 3 {
		t.Fatalf("calls = %d", m.total())
	}
	// The first token's budget is spent.
	stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--force", "--no-cache", "--db", db, "--json")
	if sum := decodeObject(t, stdout); num(t, sum["budget_remaining_before"]) != 0 || num(t, sum["planned_calls"]) != 0 {
		t.Fatalf("the first token's spent budget was not seen: %v", sum)
	}
	if m.total() != 3 {
		t.Fatalf("an exhausted budget still made calls: %d", m.total())
	}
	// A different token has its own budget and its own lockout.
	t.Setenv("DELHIVERY_MAPS_TOKEN", "another-token")
	stdout, _ = mustRun(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--force", "--db", db, "--json")
	if sum := decodeObject(t, stdout); num(t, sum["budget_remaining_before"]) != 3 || m.total() != 6 {
		t.Fatalf("second token: calls=%d summary=%v", m.total(), sum)
	}
	m.on(epStandardize, func(int, map[string]any) (int, any) { return http.StatusTooManyRequests, map[string]any{"error": "q"} })
	in3 := writeFile(t, dir, "in3.csv", "address\nAddr three\n")
	mustRun(t, "address", "clean", "--file", in3, "--out", filepath.Join(dir, "o3.csv"), "--db", db, "--json")
	m.on(epStandardize, func(_ int, body map[string]any) (int, any) { return http.StatusOK, defaultDMReply(epStandardize, body) })
	t.Setenv("DELHIVERY_MAPS_TOKEN", "a-third-token")
	stdout, _ = mustRun(t, "address", "clean", "--file", in3, "--out", filepath.Join(dir, "o3.csv"), "--force", "--db", db, "--json")
	if sum := decodeObject(t, stdout); sum["rate_limited"] == true || num(t, sum["ok"]) != 1 {
		t.Fatalf("another token inherited the lockout: %v", sum)
	}
}

// ---- zero-call work is still served after a stop ----

func TestCachedRowsStillServedAfterA429(t *testing.T) {
	t.Run("address clean", func(t *testing.T) {
		m := newDMMock(t)
		dir := t.TempDir()
		db := filepath.Join(dir, "dm.db")
		out := filepath.Join(dir, "o.csv")
		warm := writeFile(t, dir, "warm.csv", "address\nCached addr\n")
		mustRun(t, "address", "clean", "--file", warm, "--out", out, "--db", db, "--json")
		m.on(epStandardize, func(_ int, body map[string]any) (int, any) {
			if body["address"] == "Uncached addr" {
				return http.StatusTooManyRequests, map[string]any{"error": "quota"}
			}
			return http.StatusOK, defaultDMReply(epStandardize, body)
		})
		in := writeFile(t, dir, "in.csv", "address\nUncached addr\nCached addr\n")
		stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--json")
		sum := decodeObject(t, stdout)
		if sum["rate_limited"] != true || num(t, sum["deferred"]) != 1 || num(t, sum["cached"]) != 1 {
			t.Fatalf("summary = %v", sum)
		}
		tb, _ := addrkit.ReadTable(out)
		if tb.Rows[0][tb.Col("status")] != "deferred" || tb.Rows[1][tb.Col("status")] != "cached" || tb.Rows[1][tb.Col("quality")] != "ok" {
			t.Fatalf("rows = %v", tb.Rows)
		}
	})
	t.Run("trip quote lanes", func(t *testing.T) {
		m := newDMMock(t)
		dir := t.TempDir()
		db := filepath.Join(dir, "dm.db")
		mockRouteAndToll(m, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
		warm := coordLanes(t, dir, `"28.4,77.0","26.9,75.7"`)
		mustRun(t, "trip", "quote", "--file", warm, "--db", db, "--json")
		m.on(epRoute, func(_ int, body map[string]any) (int, any) {
			if coords := body["geo_coords"].([]any); coords[1].([]any)[0] == 20.0 {
				return http.StatusTooManyRequests, map[string]any{"error": "quota"}
			}
			return http.StatusOK, realRoute(270.54, 17250, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
		})
		lanes := writeFile(t, dir, "lanes2.csv", "from,to\n\"28.4,77.0\",\"20.0,70.0\"\n\"28.4,77.0\",\"26.9,75.7\"\n")
		stdout, _ := mustRun(t, "trip", "quote", "--file", lanes, "--db", db, "--json")
		recs := decodeArray(t, stdout)
		first, second := recs[0].(map[string]any), recs[1].(map[string]any)
		if first["deferred"] != true || second["error"] != nil || num(t, second["api_calls"]) != 0 || num(t, second["cache_hits"]) == 0 {
			t.Fatalf("records = %v", recs)
		}
	})
	t.Run("coverage check", func(t *testing.T) {
		m := newDMMock(t)
		squareIsochrone(m)
		dir := t.TempDir()
		db := filepath.Join(dir, "dm.db")
		out := filepath.Join(dir, "o.csv")
		warm := writeFile(t, dir, "warm.csv", "address\nCached addr\n")
		m.on(epGeocode, func(_ int, body map[string]any) (int, any) {
			if body["address"] == "Uncached addr" {
				return http.StatusTooManyRequests, map[string]any{"error": "quota"}
			}
			return http.StatusOK, map[string]any{"lat": 28.5, "lng": 77.1}
		})
		mustRun(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", warm, "--out", out, "--db", db, "--json")
		in := writeFile(t, dir, "in.csv", "address\nUncached addr\nCached addr\n")
		stdout, _ := mustRun(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", in, "--out", out, "--force", "--db", db, "--json")
		sum := decodeObject(t, stdout)
		tb, _ := addrkit.ReadTable(out)
		if sum["rate_limited"] != true || tb.Rows[0][tb.Col("geocode_status")] != "deferred" || tb.Rows[1][tb.Col("geocode_status")] != "cached" || tb.Rows[1][tb.Col("inside")] != "true" {
			t.Fatalf("summary=%v rows=%v", sum, tb.Rows)
		}
	})
}

// ---- fleet plan reconciliation ----

func TestMergeFleetChunkReconcilesOrdersAndFailsAnUnplannedChunk(t *testing.T) {
	orders := []fleetOrder{{id: "A"}, {id: "B"}, {id: "C"}, {id: "D"}}
	t.Run("an order missing from both lists becomes unassigned", func(t *testing.T) {
		raw := `{"plan_status":true,"routes_info":[{"vehicle_id":"vehicle_1","orders":["A","B"],"route":[
			{"type":"order","order_id":"A","sequence":1,"lat":1,"lng":1},{"type":"order","order_id":"B","sequence":2,"lat":1,"lng":1}],"total_distance":1,"total_duration":1}],
			"unassigned_orders":["C"]}`
		got, err := mergeFleetChunk(1, orders, json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		if got.assigned != 2 || !reflect.DeepEqual(got.unassigned, []string{"C", "D"}) {
			t.Fatalf("assigned=%d unassigned=%v, want 2 and [C D]", got.assigned, got.unassigned)
		}
	})
	t.Run("plan_status false with nothing assigned fails the chunk", func(t *testing.T) {
		for _, raw := range []string{
			`{"plan_status":false,"routes_info":[],"unassigned_orders":["A","B","C","D"]}`,
			`{"plan_status":"false","routes_info":[],"unassigned_orders":[]}`,
		} {
			if _, err := mergeFleetChunk(1, orders, json.RawMessage(raw)); !errors.Is(err, errDMUnusableResponse) {
				t.Errorf("%s: err = %v", raw, err)
			}
		}
		// plan_status false but some orders routed is still merged.
		raw := `{"plan_status":false,"routes_info":[{"vehicle_id":"v","orders":["A"],"route":[{"type":"order","order_id":"A","sequence":1}]}]}`
		if got, err := mergeFleetChunk(1, orders, json.RawMessage(raw)); err != nil || got.assigned != 1 || len(got.unassigned) != 3 {
			t.Errorf("partial plan: %+v %v", got, err)
		}
	})
}

func TestFleetPlanCountsDroppedOrdersAndFailedPlansHonestly(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", ordersCSV(4, false))
	m.on(epRoutePlan, func(n int, body map[string]any) (int, any) {
		status, resp := echoRoutePlan(n, body)
		r := resp.(map[string]any)
		route := r["routes_info"].([]any)[0].(map[string]any)
		stops := route["route"].([]any)
		// Drop ORD004 from the solver's answer without listing it as unassigned.
		var kept []any
		for _, s := range stops {
			if s.(map[string]any)["order_id"] != "ORD004" {
				kept = append(kept, s)
			}
		}
		route["route"] = kept
		return status, r
	})
	stdout, _ := mustRun(t, "fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--db", filepath.Join(dir, "dm.db"), "--json")
	sum := decodeObject(t, stdout)
	if num(t, sum["assigned"]) != 3 || num(t, sum["unassigned"]) != 1 || !reflect.DeepEqual(sum["unassigned_orders"], []any{"ORD004"}) {
		t.Fatalf("summary = %v", sum)
	}
	human, _ := mustRun(t, "fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--db", filepath.Join(dir, "dm2.db"), "--human-friendly")
	if !strings.Contains(human, "Planned 3 of 4 order(s)") {
		t.Fatalf("human summary overstates the plan: %q", human)
	}

	// plan_status false and nothing assigned: the chunk fails and is not counted as planned.
	m.on(epRoutePlan, func(int, map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"plan_status": false, "routes_info": []any{}, "unassigned_orders": []any{"ORD001", "ORD002", "ORD003", "ORD004"}}
	})
	stdout, _, err := runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--db", filepath.Join(dir, "dm3.db"), "--json")
	if err == nil || ExitCode(err) != 5 {
		t.Fatalf("every chunk failing for a non-quota reason must exit 5, got %v (stdout %q)", err, stdout)
	}
}

// ---- --max-calls 0, empty locations ----

func TestZeroCapMeansNoCalls(t *testing.T) {
	m := newDMMock(t)
	s := newTestDB(t)
	cmd := &cobra.Command{Use: "x"}
	api, err := newDMAPI(cmd, &rootFlags{timeout: 5 * time.Second}, s.DB(), 0)
	if err != nil {
		t.Fatal(err)
	}
	api.setCap(0)
	if _, err := api.post(context.Background(), epGeocode, dmAddressBody("x")); !errors.Is(err, errDMRateLimited) {
		t.Fatalf("a cap of 0 must refuse the call, got %v", err)
	}
	if m.total() != 0 || api.calls.Load() != 0 {
		t.Fatalf("calls with a zero cap: server=%d logged=%d", m.total(), api.calls.Load())
	}
	// Without setCap only the budget limits.
	api2, _ := newDMAPI(cmd, &rootFlags{timeout: 5 * time.Second}, s.DB(), 0)
	if _, err := api2.post(context.Background(), epGeocode, dmAddressBody("x")); err != nil {
		t.Fatalf("no cap set: %v", err)
	}

	// Command level: --max-calls 0 defers everything that needs a call.
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--max-calls", "0", "--db", filepath.Join(dir, "dm.db"), "--json")
	if sum := decodeObject(t, stdout); num(t, sum["deferred"]) != 1 || num(t, sum["max_calls"]) != 0 || num(t, sum["calls_made"]) != 0 {
		t.Fatalf("summary = %v", sum)
	}
	if m.total() != 1 { // only the direct api2 call above
		t.Fatalf("server calls = %d", m.total())
	}
}

func TestTripLaneCostChargesGeocodesBeforeAnEmptyLocation(t *testing.T) {
	newDMMock(t)
	s := newTestDB(t)
	api, err := newDMAPI(&cobra.Command{Use: "x"}, &rootFlags{timeout: 5 * time.Second}, s.DB(), 0)
	if err != nil {
		t.Fatal(err)
	}
	opts := tripOptions{travelMode: "auto"}
	tests := []struct {
		name     string
		from, to string
		want     int
	}{
		{"empty from costs nothing", "", "Jaipur", 0},
		{"geocode from, then an empty to", "Gurgaon", "", 1},
		{"coordinates then an empty to", "28.4,77.0", "", 0},
		{"both addresses uncached", "Gurgaon", "Jaipur", 4},
		{"coordinates only", "28.4,77.0", "26.9,75.7", 2},
	}
	for _, tt := range tests {
		got, err := tripLaneCost(context.Background(), api, &tripQuote{From: tt.from, To: tt.to, Vehicle: "lcv"}, opts)
		if err != nil || got != tt.want {
			t.Errorf("%s: cost = %d, %v; want %d", tt.name, got, err, tt.want)
		}
	}
	// With via stops: runTripQuote geocodes from, then to, then the vias, so an
	// empty `to` is never charged for them.
	got, _ := tripLaneCost(context.Background(), api, &tripQuote{From: "Gurgaon", To: "", Vehicle: "lcv"}, tripOptions{travelMode: "auto", via: []string{"Alwar"}})
	if got != 1 {
		t.Errorf("empty to with an uncached via: cost = %d, want 1", got)
	}

	// Command level: with no budget the geocode before the empty location is not sent.
	m := newDMMock(t)
	t.Setenv(dmBudgetEnv, "0")
	dir := t.TempDir()
	lanes := writeFile(t, dir, "lanes.csv", "from,to\nGurgaon,\n")
	stdout, _ := mustRun(t, "trip", "quote", "--file", lanes, "--db", filepath.Join(dir, "dm.db"), "--json")
	if rec := decodeArray(t, stdout)[0].(map[string]any); rec["deferred"] != true || m.total() != 0 {
		t.Fatalf("record = %v, server calls = %d", rec, m.total())
	}
}

// ---- coverage limits ----

func TestCoverageCheckRejectsAreasOverTheAPILimitsBeforeAnyCall(t *testing.T) {
	m := newDMMock(t)
	squareIsochrone(m)
	dir := t.TempDir()
	pts := writeFile(t, dir, "pts.csv", "lat,lng\n28.5,77.1\n")
	base := []string{"coverage", "check", "--hub", "28.5,77.1", "--file", pts, "--out", filepath.Join(dir, "o.csv"), "--db", filepath.Join(dir, "dm.db"), "--json"}
	for _, extra := range [][]string{{"--minutes", "121"}, {"--minutes", "100000"}, {"--km", "100.5"}, {"--km", "5000"}, {"--minutes", "1e308"}, {"--km", "NaN"}} {
		if _, _, err := runCLI(t, append(append([]string{}, base...), extra...)...); err == nil || ExitCode(err) != 2 {
			t.Errorf("%v: want exit 2, got %v", extra, err)
		}
	}
	if m.total() != 0 {
		t.Fatalf("limit violations made %d call(s)", m.total())
	}
	for _, extra := range [][]string{{"--minutes", "120"}, {"--km", "100"}} {
		mustRun(t, append(append([]string{}, base...), append(extra, "--force")...)...)
	}
	if got := m.bodies(epIsochrone); num(t, got[0]["cost_value"]) != 7200 || num(t, got[1]["cost_value"]) != 100000 {
		t.Fatalf("limits are inclusive: %v", got)
	}
}

// ---- csv / plain output neutralisation ----

func TestCSVAndPlainOutputNeutraliseStringCells(t *testing.T) {
	newDMMock(t)
	db := tempDB(t)
	evil := `=HYPERLINK("http://evil","x")`
	seedAddresses(t, db, []seedAddr{
		{key: "a", raw: evil, formatted: "Plot 9 Malviya Nagar", granularity: "PREMISE", quality: "ok", city: evil, lat: 26.85, lng: 75.8},
		{key: "b", raw: "+1 Main St", formatted: "plot 9 malviya nagar", granularity: "PREMISE", quality: "ok", city: "@SUM(1)", lat: 26.85, lng: 75.8},
	})
	for _, mode := range []string{"--csv", "--plain"} {
		for _, args := range [][]string{
			{"address", "report", "--group-by", "city"},
			{"address", "dupes"},
		} {
			stdout, _ := mustRun(t, append(append([]string{}, args...), "--db", db, mode)...)
			for _, line := range strings.FieldsFunc(stdout, func(r rune) bool { return r == '\n' }) {
				for _, cell := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == '\t' }) {
					cell = strings.Trim(cell, `"`)
					if strings.HasPrefix(cell, "=") || strings.HasPrefix(cell, "+1 ") || strings.HasPrefix(cell, "@SUM") {
						t.Errorf("%v %s: an unquoted formula cell %q reached stdout:\n%s", args, mode, cell, stdout)
					}
				}
			}
			if !strings.Contains(stdout, "'=HYPERLINK") && !strings.Contains(stdout, "'@SUM") && !strings.Contains(stdout, "'+1 Main St") {
				t.Errorf("%v %s: no neutralised cell found in output:\n%s", args, mode, stdout)
			}
		}
	}
	// JSON output is data, not a spreadsheet: it is left exactly as stored.
	stdout, _ := mustRun(t, "address", "report", "--group-by", "city", "--db", db, "--json")
	if !strings.Contains(stdout, `=HYPERLINK`) || strings.Contains(stdout, `'=HYPERLINK`) {
		t.Errorf("--json must not be altered: %s", stdout)
	}
}

func TestNeutralizeValueKeepsNumbersAndNesting(t *testing.T) {
	in := map[string]any{
		"n": 100000, "f": 12.5, "neg": -3.25, "s": "=1+1", "ok": "plain",
		"list": []any{"+x", 7, map[string]any{"deep": "@a", "num": 1e21}},
	}
	got, err := neutralizeValue(in)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	list := back["list"].([]any)
	if back["s"] != "'=1+1" || back["ok"] != "plain" || list[0] != "'+x" || list[2].(map[string]any)["deep"] != "'@a" {
		t.Fatalf("neutralised = %s", raw)
	}
	if !strings.Contains(string(raw), `"n":100000`) || !strings.Contains(string(raw), `"f":12.5`) || !strings.Contains(string(raw), `"neg":-3.25`) {
		t.Fatalf("numbers must keep their exact text: %s", raw)
	}
}

// ---- exit 5 when every item failed ----

func TestEveryItemFailingForANonQuotaReasonExitsFive(t *testing.T) {
	m := newDMMock(t)
	squareIsochrone(m)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	bad := func(int, map[string]any) (int, any) {
		return http.StatusBadRequest, map[string]any{"error": "invalid input"}
	}

	t.Run("trip quote --file", func(t *testing.T) {
		m.on(epRoute, bad)
		lanes := coordLanes(t, dir, `"28.4,77.0","26.9,75.7"`, `"28.4,77.0","27.0,75.0"`)
		out := filepath.Join(dir, "q.csv")
		stdout, stderr, err := runCLI(t, "trip", "quote", "--file", lanes, "--out", out, "--db", db, "--json")
		if err == nil || ExitCode(err) != 5 {
			t.Fatalf("want exit 5, got %v", err)
		}
		if recs := decodeArray(t, stdout); len(recs) != 2 || recs[0].(map[string]any)["error"] == nil {
			t.Fatalf("partial output missing: %s", stdout)
		}
		if _, statErr := os.Stat(out); statErr != nil || !strings.Contains(stderr, "2 of 2 lane(s) failed") {
			t.Fatalf("csv written? %v; stderr %q", statErr, stderr)
		}
		// One lane succeeding is exit 0.
		m.on(epRoute, func(n int, b map[string]any) (int, any) {
			if n == 3 {
				return http.StatusBadRequest, map[string]any{"error": "x"}
			}
			return http.StatusOK, realRoute(10, 600, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
		})
		m.on(epToll, func(int, map[string]any) (int, any) { return http.StatusOK, realToll(1, realPlaza("P", "id", 1)) })
		if _, _, err := runCLI(t, "trip", "quote", "--file", lanes, "--db", filepath.Join(dir, "other.db"), "--json"); err != nil {
			t.Fatalf("a partial failure must exit 0: %v", err)
		}
	})
	t.Run("coverage check", func(t *testing.T) {
		m.on(epGeocode, bad)
		in := writeFile(t, dir, "addr.csv", "address\nAddr one\nAddr two\n")
		out := filepath.Join(dir, "cov.csv")
		stdout, _, err := runCLI(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", in, "--out", out, "--db", filepath.Join(dir, "cov.db"), "--json")
		if err == nil || ExitCode(err) != 5 {
			t.Fatalf("want exit 5, got %v", err)
		}
		if sum := decodeObject(t, stdout); num(t, sum["failed"]) != 2 {
			t.Fatalf("summary = %v", sum)
		}
		if _, statErr := os.Stat(out); statErr != nil {
			t.Fatal("the flagged CSV must still be written")
		}
	})
	t.Run("fleet plan", func(t *testing.T) {
		m.on(epRoutePlan, bad)
		orders := writeFile(t, dir, "orders.csv", ordersCSV(3, false))
		if _, _, err := runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--db", filepath.Join(dir, "fleet.db"), "--json"); err == nil || ExitCode(err) != 5 {
			t.Fatalf("want exit 5, got %v", err)
		}
	})
	t.Run("a mix of failures and quota stops stays exit 0", func(t *testing.T) {
		m.on(epStandardize, func(n int, body map[string]any) (int, any) {
			if n == 1 {
				return http.StatusBadRequest, map[string]any{"error": "bad"}
			}
			return http.StatusTooManyRequests, map[string]any{"error": "quota"}
		})
		in := writeFile(t, dir, "mix.csv", "address\nAddr one\nAddr two\n")
		if _, _, err := runCLI(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "mix-out.csv"), "--steps", "standardize", "--db", filepath.Join(dir, "mix.db"), "--json"); err != nil {
			t.Fatalf("an error plus a quota deferral must exit 0: %v", err)
		}
	})
}

// ---- output file TOCTOU and parent checks ----

func TestOutputIsNotReplacedByAFileThatAppearsDuringTheRun(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	out := filepath.Join(dir, "o.csv")
	// The pre-check sees no file; one appears while the API call is in flight.
	m.on(epGeocode, func(_ int, body map[string]any) (int, any) {
		_ = os.WriteFile(out, []byte("CREATED BY SOMEONE ELSE"), 0o600)
		return http.StatusOK, defaultDMReply(epGeocode, body)
	})
	_, _, err := runCLI(t, "address", "clean", "--file", in, "--out", out, "--steps", "geocode", "--db", filepath.Join(dir, "dm.db"), "--json")
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("want a usage error naming --force, got %v", err)
	}
	if b, _ := os.ReadFile(out); string(b) != "CREATED BY SOMEONE ELSE" {
		t.Fatalf("the file that appeared was overwritten: %q", b)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".csv-") || strings.HasPrefix(e.Name(), ".pp-writecheck-") {
			t.Errorf("temp file %s left behind", e.Name())
		}
	}
	// --force still overwrites.
	mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--steps", "geocode", "--db", filepath.Join(dir, "dm.db"), "--json")
	if b, _ := os.ReadFile(out); string(b) == "CREATED BY SOMEONE ELSE" {
		t.Fatal("--force did not overwrite")
	}
}

func TestOutputParentDirectoryIsCheckedBeforeAnyCall(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	_, _, err := runCLI(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "no-such-dir", "o.csv"), "--db", filepath.Join(dir, "dm.db"))
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing parent: %v", err)
	}
	if os.Geteuid() != 0 {
		ro := filepath.Join(dir, "ro")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(ro, 0o700)
		_, _, err = runCLI(t, "address", "clean", "--file", in, "--out", filepath.Join(ro, "o.csv"), "--db", filepath.Join(dir, "dm.db"))
		if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "not writable") {
			t.Fatalf("read-only parent: %v", err)
		}
	}
	if m.total() != 0 {
		t.Fatalf("calls before the output check: %d", m.total())
	}
}

// ---- terminal safety ----

func TestSanitizeDropsFormatCharactersAndServerTextIsCleanedWhereItIsPrinted(t *testing.T) {
	for in, want := range map[string]string{
		"safe\u202etext":        "safetext", // right-to-left override
		"zero\u200bwidth\ufeff": "zerowidth",
		"a\u2066b\u2069":        "ab",
		"नमस्ते":                "नमस्ते",
		"line\nbreak":           "line break",
	} {
		if got := dmStripControl(in); got != want {
			t.Errorf("dmStripControl(%q) = %q, want %q", in, got, want)
		}
	}
	if got := dmSanitize("a\nb\x1b[31m", true); got != "a\nb" {
		t.Errorf("keepNewlines: %q", got)
	}

	m := newDMMock(t)
	hostile := "bad\x1b]0;pwned\x07 request \u202egnp.exe"
	for _, p := range []string{epRoute, epGeocode, epRoutePlan} {
		m.on(p, func(int, map[string]any) (int, any) { return http.StatusBadRequest, map[string]any{"error": hostile} })
	}
	dir := t.TempDir()
	clean := func(s string) bool { return !strings.ContainsAny(s, "\x1b\x07\u202e") }

	// trip quote: single-trip error, lane record error, human view.
	_, stderr, err := runCLI(t, "trip", "quote", "--from", "28.4,77.0", "--to", "26.9,75.7", "--db", filepath.Join(dir, "a.db"), "--json")
	if err == nil || !clean(err.Error()) || !clean(stderr) {
		t.Fatalf("trip single error not sanitised: %q / %q", err, stderr)
	}
	lanes := coordLanes(t, dir, `"28.4,77.0","26.9,75.7"`)
	stdout, stderr, _ := runCLI(t, "trip", "quote", "--file", lanes, "--db", filepath.Join(dir, "b.db"), "--human-friendly")
	if !clean(stdout) || !clean(stderr) || !strings.Contains(stdout, "FAILED") {
		t.Fatalf("trip human view: %q %q", stdout, stderr)
	}
	// coverage check: the per-address geocode failure goes to stderr.
	squareIsochrone(m)
	cov := writeFile(t, dir, "cov.csv", "address\nAddr one\n")
	_, stderr, _ = runCLI(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", cov, "--out", filepath.Join(dir, "c.csv"), "--db", filepath.Join(dir, "c.db"), "--json")
	if !clean(stderr) || !strings.Contains(stderr, "could not geocode") {
		t.Fatalf("coverage stderr: %q", stderr)
	}
	// fleet plan: failed_chunks[].error and the returned error.
	orders := writeFile(t, dir, "o.csv", ordersCSV(2, false))
	_, _, err = runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--db", filepath.Join(dir, "f.db"), "--json")
	if err == nil || !clean(err.Error()) {
		t.Fatalf("fleet error not sanitised: %q", err)
	}
	// address clean: the error column.
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	out := filepath.Join(dir, "o.csv")
	runCLI(t, "address", "clean", "--file", in, "--out", out, "--steps", "geocode", "--db", filepath.Join(dir, "d.db"), "--json")
	if b, _ := os.ReadFile(out); !clean(string(b)) {
		t.Fatalf("error column not sanitised: %q", b)
	}
	// An API error classified for exit is sanitised too, newlines in hints kept.
	e := dmFinishRunErr(&client.APIError{Method: "POST", Path: "/x", StatusCode: 404, Body: hostile})
	if !clean(e.Error()) {
		t.Fatalf("classified error: %q", e.Error())
	}
}

func TestRetryNoticeIsOnlyPrintedForAttemptsThatWillBeSent(t *testing.T) {
	m := newDMMock(t)
	m.on(epGeocode, func(int, map[string]any) (int, any) {
		return http.StatusServiceUnavailable, map[string]any{"error": "down"}
	})
	t.Setenv(dmBudgetEnv, "2") // enough for the first try and one retry only
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	_, stderr, _ := runCLI(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--steps", "geocode", "--db", filepath.Join(dir, "dm.db"), "--json")
	if m.count(epGeocode) != 2 {
		t.Fatalf("hits = %d, want 2", m.count(epGeocode))
	}
	if !strings.Contains(stderr, "attempt 2/3") || strings.Contains(stderr, "attempt 3/3") {
		t.Fatalf("stderr = %q: attempt 3/3 was announced but never sent", stderr)
	}
}

// ---- toll plaza labels ----

func TestPlazaLabelAndCSVAgreeWithTheTollPlazaCount(t *testing.T) {
	tests := []struct {
		name string
		p    map[string]any
		want string
	}{
		{"flat plaza", map[string]any{"name": "Ghamroj"}, "Ghamroj"},
		{"close loop", map[string]any{"toll_type": "close_loop", "entry_toll": map[string]any{"name": "Sawai Madhopur"}, "exit_toll": map[string]any{"name": "Alwar Interchange Toll Plaza"}}, "Sawai Madhopur -> Alwar Interchange Toll Plaza"},
		{"only an exit", map[string]any{"exit_toll": map[string]any{"name": "Alwar"}}, " -> Alwar"},
		{"nothing", map[string]any{"toll_type": "x"}, ""},
	}
	for _, tt := range tests {
		if got := plazaLabel(tt.p); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
	q := tripQuote{From: "a", To: "b", Vehicle: "lcv", TollPlazaCount: 2, Plazas: []map[string]any{
		{"name": "Ghamroj"},
		{"entry_toll": map[string]any{"name": "Sawai Madhopur"}, "exit_toll": map[string]any{"name": "Alwar"}, "toll_type": "close_loop"},
	}}
	rows := tripCSVRows([]tripQuote{q})
	if got := rows[0][13]; got != "Ghamroj; Sawai Madhopur -> Alwar" {
		t.Fatalf("plazas column = %q: it must list one entry per counted plaza", got)
	}
}

// ---- help text ----

func TestCoverageCheckHelpDocumentsFormulaQuotingAndLimits(t *testing.T) {
	root := RootCmd()
	var help bytes.Buffer
	root.SetOut(&help)
	root.SetErr(&help)
	root.SetArgs([]string{"coverage", "check", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(help.String()), " ")
	for _, want := range []string{"= + - @", "leading apostrophe", "formula injection", "exits 7"} {
		if !strings.Contains(text, want) {
			t.Errorf("coverage check --help lacks %q", want)
		}
	}
	for _, path := range [][]string{{"address", "clean"}, {"trip", "quote"}, {"coverage", "check"}, {"fleet", "plan"}} {
		root := RootCmd()
		var h bytes.Buffer
		root.SetOut(&h)
		root.SetErr(&h)
		root.SetArgs(append(append([]string{}, path...), "--help"))
		_ = root.Execute()
		if !strings.Contains(strings.Join(strings.Fields(h.String()), " "), "5 when every item of a batch failed") {
			t.Errorf("%v --help does not document exit 5", path)
		}
	}
}
