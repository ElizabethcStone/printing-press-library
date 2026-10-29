// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
)

var liveCommandPaths = [][]string{
	{"address", "clean"}, {"address", "report"}, {"address", "dupes"}, {"address", "purge"},
	{"trip", "quote"}, {"coverage", "check"}, {"fleet", "plan"},
}

// TestHelpListsEveryDeclaredFlagAndTheLeafUsage fails when a flag is added
// without help text, a command loses its leaf path, or its Long text drops the
// exit-code and privacy notes the other commands carry.
func TestHelpListsEveryDeclaredFlagAndTheLeafUsage(t *testing.T) {
	for _, path := range liveCommandPaths {
		root := RootCmd()
		leaf, _, err := root.Find(path)
		if err != nil || leaf == nil || leaf.Name() != path[len(path)-1] {
			t.Fatalf("%v is not wired into the root command: %v", path, err)
		}
		var help bytes.Buffer
		root.SetOut(&help)
		root.SetErr(&help)
		root.SetArgs(append(append([]string{}, path...), "--help"))
		if err := root.Execute(); err != nil {
			t.Fatalf("%v --help: %v", path, err)
		}
		text := help.String()
		if want := "delhivery-maps-pp-cli " + strings.Join(path, " ") + " [flags]"; !strings.Contains(text, want) {
			t.Errorf("%v --help lacks the usage line %q", path, want)
		}
		leaf.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Name == "help" {
				return
			}
			if !strings.Contains(text, "--"+f.Name) {
				t.Errorf("%v --help does not list --%s", path, f.Name)
			}
			if strings.TrimSpace(f.Usage) == "" || strings.Contains(f.Usage, "TODO") {
				t.Errorf("%v --%s has no real description: %q", path, f.Name, f.Usage)
			}
		})
		if strings.Contains(text, "TODO") {
			t.Errorf("%v --help still contains a TODO", path)
		}
		batch := path[0] != "address" || path[1] == "clean"
		if batch && path[1] != "purge" && !strings.Contains(text, "Exit codes:") {
			t.Errorf("%v --help must document the exit-code policy", path)
		}
		if !strings.Contains(text, "address purge") {
			t.Errorf("%v --help must mention how stored addresses are purged", path)
		}
		if leaf.Annotations["pp:novel-scaffold"] != "" || leaf.Annotations["pp:data-source"] == "" {
			t.Errorf("%v annotations = %v", path, leaf.Annotations)
		}
	}
}

func TestDryRunPlansReportTheirInputsWithoutTouchingFilesOrNetwork(t *testing.T) {
	m := newDMMock(t)
	missing := filepath.Join(t.TempDir(), "absent.csv")
	cases := []struct {
		name string
		args []string
		keys map[string]any
	}{
		{"address clean", []string{"address", "clean", "--file", missing, "--out", "o.csv", "--steps", "geocode"},
			map[string]any{"action": "address clean", "calls_per_row": 1.0, "resume": false}},
		{"trip quote", []string{"trip", "quote", "--from", "Gurgaon", "--to", "Jaipur", "--via", "Alwar", "--via", "28.1,76.5", "--vehicle", "lcv"},
			map[string]any{"action": "trip quote", "vehicle": "lcv", "from": "Gurgaon", "to": "Jaipur", "via": []any{"Alwar", "28.1,76.5"}, "traffic_aware": false}},
		{"coverage check", []string{"coverage", "check", "--hub", "28.4,77.0", "--minutes", "45", "--file", missing, "--out", "o.csv"},
			map[string]any{"action": "coverage check", "hub": "28.4,77.0", "minutes": 45.0}},
		{"fleet plan", []string{"fleet", "plan", "--orders", missing, "--vehicles", "3", "--depot", "28.4,77.0", "--vehicle-mode", "truck"},
			map[string]any{"action": "fleet plan", "vehicles": 3.0, "vehicle_mode": "truck", "planned_calls": "ceil(orders / 50) (the input is not read in dry-run)"}},
		{"address report", []string{"address", "report", "--group-by", "city"}, map[string]any{"action": "address report"}},
		{"address dupes", []string{"address", "dupes"}, map[string]any{"action": "address dupes"}},
		{"address purge", []string{"address", "purge", "--all"}, map[string]any{"action": "address purge"}},
	}
	for _, c := range cases {
		stdout, _ := mustRun(t, append(c.args, "--dry-run", "--json", "--db", filepath.Join(t.TempDir(), "never.db"))...)
		plan := decodeObject(t, stdout)
		if plan["dry_run"] != true || plan["would"] == nil {
			t.Errorf("%s: plan = %v", c.name, plan)
		}
		for k, want := range c.keys {
			if !reflect.DeepEqual(plan[k], want) {
				t.Errorf("%s: plan[%s] = %#v, want %#v", c.name, k, plan[k], want)
			}
		}
	}
	if m.total() != 0 {
		t.Fatalf("dry-runs made %d API call(s)", m.total())
	}
}

// ---- budget, lockout and exit codes ----

func coordLanes(t *testing.T, dir string, lanes ...string) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("from,to\n")
	for _, l := range lanes {
		sb.WriteString(l + "\n")
	}
	return writeFile(t, dir, "lanes.csv", sb.String())
}

func TestTripQuoteDefersLanesThatDoNotFitButStillServesCachedOnes(t *testing.T) {
	m := newDMMock(t)
	mockRouteAndToll(m, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
	t.Setenv(dmBudgetEnv, "3")
	dir := t.TempDir()
	// A needs 2 calls (route + toll); B would need 2 but only 1 is left; A again is fully cached.
	lanes := coordLanes(t, dir, `"28.4,77.0","26.9,75.7"`, `"28.4,77.0","20.0,70.0"`, `"28.4,77.0","26.9,75.7"`)
	stdout, stderr, err := runCLI(t, "trip", "quote", "--file", lanes, "--db", filepath.Join(dir, "dm.db"), "--json")
	if err != nil {
		t.Fatalf("a deferred lane must not fail the run: %v", err)
	}
	recs := decodeArray(t, stdout)
	r := func(i int) map[string]any { return recs[i].(map[string]any) }
	if r(0)["error"] != nil || r(1)["deferred"] != true || !strings.Contains(r(1)["error"].(string), "deferred") || r(2)["error"] != nil || num(t, r(2)["api_calls"]) != 0 {
		t.Fatalf("records = %v", recs)
	}
	if m.count(epRoute) != 1 || m.count(epToll) != 1 || m.total() != 2 {
		t.Fatalf("calls: route=%d toll=%d total=%d, want 1/1/2", m.count(epRoute), m.count(epToll), m.total())
	}
	if !strings.Contains(stderr, "1 of 3 lane(s) deferred") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestTripQuoteSingleTripWithNoBudgetExitsSevenWithoutCalling(t *testing.T) {
	m := newDMMock(t)
	t.Setenv(dmBudgetEnv, "0")
	_, _, err := runCLI(t, "trip", "quote", "--from", "28.4,77.0", "--to", "26.9,75.7", "--db", tempDB(t), "--json")
	if err == nil || ExitCode(err) != 7 {
		t.Fatalf("want exit 7, got %v", err)
	}
	if m.total() != 0 {
		t.Fatalf("calls with no budget: %d", m.total())
	}
}

func TestA429RecordsALockoutSoTheNextRunPlansZeroCalls(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	orders := writeFile(t, dir, "orders.csv", ordersCSV(60, false))
	in := writeFile(t, dir, "in.csv", "address\nAddr one\nAddr two\n")

	t.Run("address clean", func(t *testing.T) {
		m := newDMMock(t)
		m.on(epStandardize, func(int, map[string]any) (int, any) {
			return http.StatusTooManyRequests, map[string]any{"error": "quota"}
		})
		out := filepath.Join(dir, "clean.csv")
		mustRun(t, "address", "clean", "--file", in, "--out", out, "--db", db, "--json")
		before := m.total()
		stdout, stderr := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--json")
		sum := decodeObject(t, stdout)
		if m.total() != before || sum["rate_limited"] != true || num(t, sum["deferred"]) != 2 || num(t, sum["planned_calls"]) != 0 {
			t.Fatalf("second run: new calls=%d summary=%v", m.total()-before, sum)
		}
		if !strings.Contains(stderr, "rate limited") {
			t.Fatalf("stderr = %q", stderr)
		}
	})
	t.Run("the lockout can be overridden explicitly", func(t *testing.T) {
		m := newDMMock(t)
		t.Setenv(dmIgnoreLockoutEnv, "1")
		out := filepath.Join(dir, "clean2.csv")
		mustRun(t, "address", "clean", "--file", in, "--out", out, "--db", db, "--json")
		if m.total() != 6 {
			t.Fatalf("calls = %d, want 6", m.total())
		}
	})
	t.Run("fleet plan", func(t *testing.T) {
		m := newDMMock(t)
		db := filepath.Join(dir, "fleet.db")
		m.on(epRoutePlan, func(int, map[string]any) (int, any) {
			return http.StatusTooManyRequests, map[string]any{"error": "quota"}
		})
		_, _, err := runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "2", "--depot", "28.4,77.0", "--db", db, "--json")
		if err == nil || ExitCode(err) != 7 || m.count(epRoutePlan) != 1 {
			t.Fatalf("first run: err=%v hits=%d", err, m.count(epRoutePlan))
		}
		_, _, err = runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "2", "--depot", "28.4,77.0", "--db", db, "--json")
		if err == nil || ExitCode(err) != 7 || m.count(epRoutePlan) != 1 {
			t.Fatalf("locked-out run must not call: err=%v hits=%d", err, m.count(epRoutePlan))
		}
	})
	t.Run("coverage check", func(t *testing.T) {
		m := newDMMock(t)
		db := filepath.Join(dir, "cov.db")
		m.on(epIsochrone, func(int, map[string]any) (int, any) {
			return http.StatusTooManyRequests, map[string]any{"error": "quota"}
		})
		pts := writeFile(t, dir, "pts.csv", "lat,lng\n28.5,77.1\n")
		_, _, err := runCLI(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", pts, "--out", filepath.Join(dir, "c.csv"), "--db", db, "--json")
		if err == nil || ExitCode(err) != 7 {
			t.Fatalf("429 on the isochrone must exit 7, got %v", err)
		}
		_, _, err = runCLI(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", pts, "--out", filepath.Join(dir, "c.csv"), "--db", db, "--json")
		if err == nil || ExitCode(err) != 7 || m.count(epIsochrone) != 1 {
			t.Fatalf("locked-out run: err=%v hits=%d", err, m.count(epIsochrone))
		}
	})
}

func TestCoverageCheckBudgetShortfallForTheIsochroneExitsSevenNotTwo(t *testing.T) {
	m := newDMMock(t)
	squareIsochrone(m)
	t.Setenv(dmBudgetEnv, "0")
	dir := t.TempDir()
	pts := writeFile(t, dir, "pts.csv", "lat,lng\n28.5,77.1\n")
	out := filepath.Join(dir, "o.csv")
	_, _, err := runCLI(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", pts, "--out", out, "--db", filepath.Join(dir, "dm.db"), "--json")
	if err == nil || ExitCode(err) != 7 {
		t.Fatalf("want exit 7, got %v", err)
	}
	if m.total() != 0 {
		t.Fatalf("calls = %d", m.total())
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Fatal("no output file should be written when the isochrone could not be fetched")
	}
}

func TestCoverageCheckKeysGeocodesLikeTheCache(t *testing.T) {
	m := newDMMock(t)
	squareIsochrone(m)
	m.on(epGeocode, func(int, map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"lat": 28.5, "lng": 77.1}
	})
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", "address\nSector 45  Gurgaon\nsector 45 gurgaon\nSECTOR 45 GURGAON\n")
	stdout, _ := mustRun(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", orders, "--out", filepath.Join(dir, "o.csv"), "--db", filepath.Join(dir, "dm.db"), "--json")
	sum := decodeObject(t, stdout)
	if num(t, sum["planned_calls"]) != 2 || m.count(epGeocode) != 1 || num(t, sum["inside"]) != 3 {
		t.Fatalf("one distinct address must cost one geocode: summary=%v geocode hits=%d", sum, m.count(epGeocode))
	}
}

func TestFleetPlanDefersChunksBeyondTheCallBudget(t *testing.T) {
	m := newDMMock(t)
	m.on(epRoutePlan, echoRoutePlan)
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", ordersCSV(120, false))
	out := filepath.Join(dir, "plan.csv")
	stdout, stderr, err := runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "2", "--depot", "28.4,77.0", "--out", out, "--max-calls", "2", "--db", filepath.Join(dir, "dm.db"), "--json")
	if err != nil {
		t.Fatalf("deferred chunks must exit 0: %v", err)
	}
	sum := decodeObject(t, stdout)
	if m.count(epRoutePlan) != 2 || num(t, sum["deferred_chunks"]) != 1 || num(t, sum["deferred_orders"]) != 20 || num(t, sum["assigned"]) != 100 ||
		num(t, sum["planned_calls"]) != 2 || num(t, sum["max_calls"]) != 2 || num(t, sum["fetch_failures"]) != 0 {
		t.Fatalf("calls=%d summary=%v", m.count(epRoutePlan), sum)
	}
	if !strings.Contains(stderr, "deferred") {
		t.Fatalf("stderr = %q", stderr)
	}

	// The human line subtracts deferred orders: 100 of 120 were planned.
	t.Setenv(dmIgnoreLockoutEnv, "1")
	human, _ := mustRun(t, "fleet", "plan", "--orders", orders, "--vehicles", "2", "--depot", "28.4,77.0", "--out", out, "--force", "--max-calls", "1", "--human-friendly", "--db", filepath.Join(dir, "dm2.db"))
	if !strings.Contains(human, "Planned 50 of 120 order(s)") {
		t.Fatalf("human summary = %q", human)
	}
	// The env budget caps --max-calls: asking for more than the budget allows is lowered.
	t.Setenv(dmPlanBudgetEnv, "1")
	stdout, _ = mustRun(t, "fleet", "plan", "--orders", orders, "--vehicles", "2", "--depot", "28.4,77.0", "--out", out, "--force", "--max-calls", "99", "--db", filepath.Join(dir, "dm3.db"), "--json")
	sum = decodeObject(t, stdout)
	if num(t, sum["max_calls"]) != 1 || num(t, sum["deferred_chunks"]) != 2 {
		t.Fatalf("--max-calls must never raise the budget: %v", sum)
	}
}

func TestRetriesStopWhenTheBudgetIsUsedUpAndNoticesFollowTheCommandStderr(t *testing.T) {
	m := newDMMock(t)
	m.on(epGeocode, func(int, map[string]any) (int, any) {
		return http.StatusServiceUnavailable, map[string]any{"error": "unavailable"}
	})
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	out := filepath.Join(dir, "o.csv")

	t.Setenv(dmBudgetEnv, "1")
	// Every row failing for a non-quota reason is exit 5, with output still written.
	stdout, _, err := runCLI(t, "address", "clean", "--file", in, "--out", out, "--steps", "geocode", "--db", filepath.Join(dir, "a.db"), "--json")
	if err == nil || ExitCode(err) != 5 {
		t.Fatalf("all rows failed: want exit 5, got %v", err)
	}
	sum := decodeObject(t, stdout)
	if m.count(epGeocode) != 1 || num(t, sum["errors"]) != 1 {
		t.Fatalf("a retry was sent with no budget left: hits=%d summary=%v", m.count(epGeocode), sum)
	}

	// With budget, the retry notice goes to the command's stderr and --quiet silences it.
	t.Setenv(dmBudgetEnv, "")
	var failNext = true
	m.on(epGeocode, func(_ int, body map[string]any) (int, any) {
		// Every first attempt per run fails; the retry succeeds.
		failNext = !failNext
		if !failNext {
			return http.StatusServiceUnavailable, map[string]any{"error": "unavailable"}
		}
		return http.StatusOK, defaultDMReply(epGeocode, body)
	})
	_, stderr := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--steps", "geocode", "--db", filepath.Join(dir, "b.db"), "--json")
	if !strings.Contains(stderr, "transient error, retrying") {
		t.Fatalf("stderr = %q, want the retry notice", stderr)
	}
	_, stderr = mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--steps", "geocode", "--db", filepath.Join(dir, "c.db"), "--json", "--quiet")
	if strings.Contains(stderr, "retrying") {
		t.Fatalf("--quiet must silence retry notices: %q", stderr)
	}
}

func TestFleetPlanPerCallTimeoutIsRaisedBeyondTheTimeoutFlag(t *testing.T) {
	m := newDMMock(t)
	slow := func(delay time.Duration, h func(int, map[string]any) (int, any)) func(int, map[string]any) (int, any) {
		return func(n int, body map[string]any) (int, any) { time.Sleep(delay); return h(n, body) }
	}
	m.on(epRoutePlan, slow(1300*time.Millisecond, echoRoutePlan))
	m.on(epGeocode, slow(1300*time.Millisecond, func(_ int, b map[string]any) (int, any) { return http.StatusOK, defaultDMReply(epGeocode, b) }))
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", ordersCSV(3, false))
	stdout, _, err := runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--timeout", "1s", "--db", filepath.Join(dir, "dm.db"), "--json")
	if err != nil {
		t.Fatalf("a 1.3s route-plan reply must survive --timeout 1s: %v", err)
	}
	if sum := decodeObject(t, stdout); num(t, sum["assigned"]) != 3 {
		t.Fatalf("summary = %v", sum)
	}
	if fleetMinCallTimeout < 90*time.Second {
		t.Fatalf("fleetMinCallTimeout = %v, want at least 90s", fleetMinCallTimeout)
	}
	// Control: the other commands keep the user's --timeout, so the same delay fails there.
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	stdout, _, err = runCLI(t, "address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv"), "--steps", "geocode", "--timeout", "1s", "--db", filepath.Join(dir, "dm2.db"), "--json")
	if err == nil || ExitCode(err) != 5 {
		t.Fatalf("a timed-out row is a per-row error (exit 5 when every row failed): %v", err)
	}
	if sum := decodeObject(t, stdout); num(t, sum["errors"]) != 1 {
		t.Fatalf("address clean should have timed out at 1s: %v", sum)
	}
}

// ---- output file safety, formula injection, limits ----

func TestOutputFileSafetyAppliesToEveryCommandThatWritesOne(t *testing.T) {
	m := newDMMock(t)
	squareIsochrone(m)
	mockRouteAndToll(m, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
	m.on(epRoutePlan, echoRoutePlan)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	addrIn := writeFile(t, dir, "addr.csv", "address\nAddr one\n")
	ptsIn := writeFile(t, dir, "pts.csv", "lat,lng\n28.5,77.1\n")
	laneIn := coordLanes(t, dir, `"28.4,77.0","26.9,75.7"`)
	ordIn := writeFile(t, dir, "ord.csv", ordersCSV(2, false))
	cmds := map[string]struct {
		in   string
		args func(out string) []string
	}{
		"address clean": {addrIn, func(o string) []string { return []string{"address", "clean", "--file", addrIn, "--out", o} }},
		"coverage check": {ptsIn, func(o string) []string {
			return []string{"coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", ptsIn, "--out", o}
		}},
		"trip quote": {laneIn, func(o string) []string { return []string{"trip", "quote", "--file", laneIn, "--out", o} }},
		"fleet plan": {ordIn, func(o string) []string {
			return []string{"fleet", "plan", "--orders", ordIn, "--vehicles", "1", "--depot", "28.4,77.0", "--out", o}
		}},
	}
	for name, c := range cmds {
		existing := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".csv")
		if err := os.WriteFile(existing, []byte("PRECIOUS"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := m.total()
		run := func(extra ...string) error {
			_, _, err := runCLI(t, append(append(c.args(existing), "--db", db, "--json"), extra...)...)
			return err
		}
		if err := run(); err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "--force") {
			t.Errorf("%s: overwriting without --force: err = %v, want exit 2 naming --force", name, err)
		}
		if b, _ := os.ReadFile(existing); string(b) != "PRECIOUS" || m.total() != before {
			t.Errorf("%s: the existing file or the API was touched before the refusal", name)
		}
		// Never the input file, even with --force and even through a symlink.
		link := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+"-link.csv")
		if err := os.Symlink(c.in, link); err != nil {
			t.Fatal(err)
		}
		for label, out := range map[string]string{"same path": c.in, "symlink to input": link} {
			_, _, err := runCLI(t, append(c.args(out), "--force", "--db", db, "--json")...)
			if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "input file") {
				t.Errorf("%s: --out as %s: err = %v, want exit 2 'input file'", name, label, err)
			}
		}
		_, _, err := runCLI(t, append(c.args(dir), "--force", "--db", db, "--json")...)
		if err == nil || ExitCode(err) != 2 {
			t.Errorf("%s: --out as a directory: err = %v", name, err)
		}
		if b, _ := os.ReadFile(c.in); len(b) == 0 {
			t.Errorf("%s: the input file was clobbered", name)
		}
		// --force overwrites.
		if err := run("--force"); err != nil {
			t.Errorf("%s: --force run failed: %v", name, err)
		}
		if b, _ := os.ReadFile(existing); string(b) == "PRECIOUS" {
			t.Errorf("%s: --force did not overwrite", name)
		}
	}
}

func TestCleanedCSVNeutralisesFormulaInjection(t *testing.T) {
	m := newDMMock(t)
	m.on(epStandardize, func(_ int, body map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"formatted_address": "=HYPERLINK(\"http://evil\",\"x\")", "address_components": map[string]any{"city": "@SUM(1)", "state": "-2+3"}}
	})
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "address\n\"=HYPERLINK(\"\"http://evil\"\",\"\"x\"\")\"\n+1 Main St\n@SUM(1)\n\"=cmd|' /C calc'!A0\"\n-12.5\n")
	out := filepath.Join(dir, "o.csv")
	mustRun(t, "address", "clean", "--file", in, "--out", out, "--steps", "standardize", "--db", filepath.Join(dir, "dm.db"), "--json")
	tb, err := addrkit.ReadTable(out)
	if err != nil {
		t.Fatal(err)
	}
	var raws, formatted, cities []string
	for _, r := range tb.Rows {
		raws = append(raws, r[tb.Col("raw")])
		formatted = append(formatted, r[tb.Col("formatted")])
		cities = append(cities, r[tb.Col("city")])
	}
	wantRaw := []string{`'=HYPERLINK("http://evil","x")`, "'+1 Main St", "'@SUM(1)", `'=cmd|' /C calc'!A0`, "-12.5"}
	if !reflect.DeepEqual(raws, wantRaw) {
		t.Fatalf("raw cells = %q, want %q", raws, wantRaw)
	}
	for _, f := range formatted {
		if !strings.HasPrefix(f, "'=") {
			t.Fatalf("a formula coming back from the API was not neutralised: %q", f)
		}
	}
	if cities[0] != "'@SUM(1)" {
		t.Fatalf("city = %q", cities[0])
	}
	if m.count(epStandardize) != 5 {
		t.Fatalf("standardize hits = %d", m.count(epStandardize))
	}
}

func TestInputLimitsAndFileTypes(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	out := filepath.Join(dir, "o.csv")

	// --max-rows
	three := writeFile(t, dir, "three.csv", "address\na\nb\nc\n")
	_, _, err := runCLI(t, "address", "clean", "--file", three, "--out", out, "--max-rows", "2", "--db", db)
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "limit is 2") {
		t.Fatalf("--max-rows: err = %v", err)
	}
	_, _, err = runCLI(t, "address", "clean", "--file", three, "--out", out, "--max-rows", "999999999", "--db", db)
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("an absurd --max-rows must be refused: %v", err)
	}
	// a device is not a regular file
	if runtime.GOOS != "windows" {
		_, _, err = runCLI(t, "address", "clean", "--file", "/dev/null", "--out", out, "--db", db)
		if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("/dev/null: err = %v", err)
		}
	}
	// a .json array of objects is accepted
	arr := writeFile(t, dir, "in.json", `[{"address":"Addr one"},{"address":"Addr two","note":1}]`)
	stdout, _ := mustRun(t, "address", "clean", "--file", arr, "--out", out, "--steps", "geocode", "--db", db, "--json")
	if sum := decodeObject(t, stdout); num(t, sum["rows"]) != 2 || num(t, sum["ok"]) != 2 {
		t.Fatalf("json array input: %v", sum)
	}
	if m.count(epGeocode) != 2 {
		t.Fatalf("geocode hits = %d", m.count(epGeocode))
	}
	// non-finite numbers in flags and files
	for name, args := range map[string][]string{
		"dupes radius NaN":     {"address", "dupes", "--radius-m", "NaN", "--db", db},
		"dupes radius Inf":     {"address", "dupes", "--radius-m", "Inf", "--db", db},
		"dupes radius huge":    {"address", "dupes", "--radius-m", "500000", "--db", db},
		"coverage minutes NaN": {"coverage", "check", "--hub", "28.5,77.1", "--minutes", "NaN", "--file", three, "--out", filepath.Join(dir, "x.csv"), "--db", db},
		"coverage km Inf":      {"coverage", "check", "--hub", "28.5,77.1", "--km", "Inf", "--file", three, "--out", filepath.Join(dir, "x.csv"), "--db", db},
		"fleet weight NaN":     {"fleet", "plan", "--orders", three, "--vehicles", "1", "--depot", "28.4,77.0", "--max-weight", "NaN", "--db", db},
	} {
		if _, _, err := runCLI(t, args...); err == nil || ExitCode(err) != 2 {
			t.Errorf("%s: want exit 2, got %v", name, err)
		}
	}
}

func TestBaseURLGuardRefusesToSendTheTokenToForeignHosts(t *testing.T) {
	m := newDMMock(t)
	t.Setenv("DELHIVERY_MAPS_BASE_URL", "https://maps.evil.example")
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	for name, args := range map[string][]string{
		"address clean":  {"address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv")},
		"trip quote":     {"trip", "quote", "--from", "28.4,77.0", "--to", "26.9,75.7"},
		"coverage check": {"coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", writeFile(t, dir, "p.csv", "lat,lng\n28.5,77.1\n"), "--out", filepath.Join(dir, "c.csv")},
		"fleet plan":     {"fleet", "plan", "--orders", writeFile(t, dir, "o2.csv", ordersCSV(1, false)), "--vehicles", "1", "--depot", "28.4,77.0"},
	} {
		_, _, err := runCLI(t, append(args, "--db", filepath.Join(dir, "dm.db"))...)
		if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "refusing to send the API token") {
			t.Errorf("%s: err = %v, want a usage error", name, err)
		}
	}
	if m.total() != 0 {
		t.Fatalf("requests reached the mock: %d", m.total())
	}
}

func TestCacheIsScopedToTheTokenUsed(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	out := filepath.Join(dir, "o.csv")
	mustRun(t, "address", "clean", "--file", in, "--out", out, "--steps", "geocode", "--db", db, "--json")
	mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--steps", "geocode", "--db", db, "--json")
	if m.count(epGeocode) != 1 {
		t.Fatalf("same token should share the cache: %d hits", m.count(epGeocode))
	}
	t.Setenv("DELHIVERY_MAPS_TOKEN", "a-different-token")
	mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--steps", "geocode", "--db", db, "--json")
	if m.count(epGeocode) != 2 {
		t.Fatalf("another token must not read the first token's cache: %d hits", m.count(epGeocode))
	}
	// The token itself is never written to the database.
	raw, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"a-different-token", "Bearer test"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("the database file contains the secret %q", secret)
		}
	}
}

func TestAddressCleanFailedPlanningLeavesAnExistingOutputUntouched(t *testing.T) {
	newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	out := writeFile(t, dir, "o.csv", "GOOD PRIOR OUTPUT")
	// Break the cache table so the planning probe fails.
	execDB(t, db, "ALTER TABLE dm_cache RENAME COLUMN fetched_at TO fetched_gone")
	_, _, err := runCLI(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--json")
	if err == nil {
		t.Fatal("expected a planning error")
	}
	if b, _ := os.ReadFile(out); string(b) != "GOOD PRIOR OUTPUT" {
		t.Fatalf("the earlier output was replaced: %q", b)
	}
}

func TestTripQuoteListFilesAndModes(t *testing.T) {
	m := newDMMock(t)
	mockRouteAndToll(m, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")

	empty := writeFile(t, dir, "empty.csv", "from,to\n")
	stdout, stderr := mustRun(t, "trip", "quote", "--file", empty, "--db", db, "--json")
	if strings.TrimSpace(stdout) != "[]" || !strings.Contains(stderr, "no lanes") {
		t.Fatalf("empty lanes file: stdout=%q stderr=%q, want []", stdout, stderr)
	}

	for _, args := range [][]string{
		{"trip", "quote", "--from", "28.4,77.0", "--to", "26.9,75.7", "--travel-mode", "hovercraft"},
		{"coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", writeFile(t, dir, "p.csv", "lat,lng\n28.5,77.1\n"), "--out", filepath.Join(dir, "c.csv"), "--travel-mode", "hovercraft"},
	} {
		_, _, err := runCLI(t, append(args, "--db", db)...)
		if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "pedestrian") {
			t.Errorf("%v: err = %v, want a usage error listing the modes", args, err)
		}
	}

	// A route is cached only without --traffic-aware and --departure-time.
	run := func(extra ...string) {
		mustRun(t, append([]string{"trip", "quote", "--from", "28.4,77.0", "--to", "26.9,75.7", "--db", db, "--json"}, extra...)...)
	}
	run()
	run()
	if m.count(epRoute) != 1 {
		t.Fatalf("plain route hits = %d, want 1", m.count(epRoute))
	}
	run("--departure-time", "2026-11-03T09:30:00+05:30")
	run("--departure-time", "2026-11-03T09:30:00+05:30")
	if m.count(epRoute) != 3 {
		t.Fatalf("--departure-time routes must not be cached: hits = %d, want 3", m.count(epRoute))
	}
	if got := m.bodies(epRoute)[2]["departure_time"]; got != "2026-11-03T09:30:00+05:30" {
		t.Fatalf("departure_time = %v", got)
	}
}

func TestAddressPurge(t *testing.T) {
	newDMMock(t)
	db := tempDB(t)
	seedAddresses(t, db, []seedAddr{
		{key: "old", raw: "old", granularity: "PREMISE", quality: "ok"},
		{key: "new", raw: "new", granularity: "PREMISE", quality: "ok"},
	})
	old := time.Now().Add(-45 * 24 * time.Hour).UTC().Format(time.RFC3339)
	fresh := time.Now().UTC().Format(time.RFC3339)
	execDB(t, db, `UPDATE dm_address SET updated_at = ? WHERE raw_key = 'old'`, old)
	execDB(t, db, `UPDATE dm_address SET updated_at = ? WHERE raw_key = 'new'`, fresh)
	execDB(t, db, `INSERT INTO dm_cache (endpoint, key, request, response, fetched_at) VALUES ('/geocode','k1','{}','{}',?), ('/geocode','k2','{}','{}',?)`, old, fresh)
	execDB(t, db, `INSERT INTO dm_calls (called_at, endpoint) VALUES (?, '/geocode')`, time.Now().Unix())

	stdout, _ := mustRun(t, "address", "purge", "--older-than", "30d", "--db", db, "--json")
	res := decodeObject(t, stdout)
	if num(t, res["deleted_cache"]) != 1 || num(t, res["deleted_address"]) != 1 {
		t.Fatalf("result = %v", res)
	}
	if countRows(t, db, "dm_address") != 1 || countRows(t, db, "dm_cache") != 1 {
		t.Fatal("purge removed the wrong rows")
	}
	// --all needs --yes.
	_, _, err := runCLI(t, "address", "purge", "--all", "--db", db)
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("--all without --yes: %v", err)
	}
	if countRows(t, db, "dm_address") != 1 {
		t.Fatal("rows were deleted without confirmation")
	}
	stdout, _ = mustRun(t, "address", "purge", "--all", "--yes", "--db", db, "--json")
	res = decodeObject(t, stdout)
	if num(t, res["deleted_cache"]) != 1 || num(t, res["deleted_address"]) != 1 || countRows(t, db, "dm_address") != 0 || countRows(t, db, "dm_cache") != 0 {
		t.Fatalf("--all result = %v", res)
	}
	if countRows(t, db, "dm_calls") != 1 {
		t.Fatal("the quota call log must survive a purge")
	}
	// Bad ages and a missing database.
	for _, age := range []string{"soon", "-3d", "xd"} {
		if _, _, err := runCLI(t, "address", "purge", "--older-than", age, "--db", db); err == nil || ExitCode(err) != 2 {
			t.Errorf("--older-than %q: %v", age, err)
		}
	}
	missing := tempDB(t)
	stdout, _ = mustRun(t, "address", "purge", "--older-than", "1d", "--db", missing, "--json")
	if res := decodeObject(t, stdout); num(t, res["deleted_cache"]) != 0 {
		t.Fatalf("missing db result = %v", res)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("purge must not create a database")
	}
	// Age parsing.
	for in, want := range map[string]time.Duration{"30d": 720 * time.Hour, "72h": 72 * time.Hour, "0d": 0, "1h30m": 90 * time.Minute} {
		if got, err := parsePurgeAge(in); err != nil || got != want {
			t.Errorf("parsePurgeAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

func TestAddressCleanPassesThroughTheStandardizePostalCodeField(t *testing.T) {
	newDMMock(t)
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "address\nFlat 4B Lotus Apt Sector 45 Gurgaon\n")
	out := filepath.Join(dir, "o.csv")
	mustRun(t, "address", "clean", "--file", in, "--out", out, "--steps", "standardize", "--db", filepath.Join(dir, "dm.db"), "--json")
	tb, _ := addrkit.ReadTable(out)
	if got := tb.Rows[0][tb.Col("pincode")]; got != "122003" {
		t.Fatalf("pincode = %q: the live /standarize reply carries postal_code", got)
	}
}
