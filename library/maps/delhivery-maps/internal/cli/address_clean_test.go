// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Tests for `address clean`: help wiring plus behaviour against an httptest mock.

package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAddressCleanWritesColumnsAndSecondRunIsFullyCached(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "id,address\n1,\"Flat 4B, Lotus Apt, Sector 45, Gurgaon\"\n2,Plot 9 Malviya Nagar Jaipur\n3,\"flat 4b,  lotus apt, sector 45, gurgaon\"\n")
	out := filepath.Join(dir, "cleaned.csv")

	stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--json")
	sum := decodeObject(t, stdout)

	tb, err := addrkit.ReadTable(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tb.Header, cleanColumns) {
		t.Fatalf("output columns = %v, want %v", tb.Header, cleanColumns)
	}
	if len(tb.Rows) != 3 {
		t.Fatalf("output rows = %d, want 3", len(tb.Rows))
	}
	first := map[string]string{}
	for i, h := range tb.Header {
		first[h] = tb.Rows[0][i]
	}
	if first["raw"] != "Flat 4B, Lotus Apt, Sector 45, Gurgaon" || first["formatted"] != "STD flat 4b, lotus apt, sector 45, gurgaon" ||
		first["quality"] != "ok" || first["granularity"] != "PREMISE" || first["pincode"] != "122003" ||
		first["city"] != "Gurgaon" || first["state"] != "Haryana" || first["error_radius"] != "25" || first["status"] != "ok" || first["error"] != "" {
		t.Fatalf("first row = %v", first)
	}
	if first["lat"] == "" || first["lng"] != "77" {
		t.Fatalf("first row coordinates = %q,%q", first["lat"], first["lng"])
	}
	// The third row is the first with different case and spacing: same cache key.
	if st := tb.Rows[2][tb.Col("status")]; st != "cached" {
		t.Fatalf("duplicate address status = %q, want cached", st)
	}
	// Two distinct addresses x three steps.
	for _, p := range []string{epStandardize, epValidate, epGeocode} {
		if got := m.count(p); got != 2 {
			t.Errorf("%s hits = %v, want 2", p, got)
		}
	}
	if num(t, sum["planned_calls"]) != 6 || num(t, sum["calls_made"]) != 6 || num(t, sum["ok"]) != 2 || num(t, sum["cached"]) != 1 || num(t, sum["deferred"]) != 0 {
		t.Fatalf("first-run summary = %v", sum)
	}
	if n := countRows(t, db, "dm_address"); n != 2 {
		t.Fatalf("dm_address rows = %d, want 2", n)
	}

	before := m.total()
	stdout, _ = mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--json")
	sum = decodeObject(t, stdout)
	if m.total() != before {
		t.Fatalf("second run made %d new API call(s), want 0", m.total()-before)
	}
	if num(t, sum["planned_calls"]) != 0 || num(t, sum["calls_made"]) != 0 || num(t, sum["cache_hits"]) != 6 || num(t, sum["cached"]) != 3 || num(t, sum["ok"]) != 0 {
		t.Fatalf("second-run summary = %v", sum)
	}
}

func TestAddressCleanBudgetGuardDefersRowsThatDoNotFit(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "address\nAddr one Gurgaon\nAddr two Gurgaon\nAddr three Gurgaon\nAddr four Gurgaon\n")
	out := filepath.Join(dir, "cleaned.csv")

	stdout, stderr := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--max-calls", "7", "--json")
	sum := decodeObject(t, stdout)
	// 7 calls fit two whole rows (3 each); the other two are deferred whole.
	if m.total() != 6 {
		t.Fatalf("API calls = %d, want 6 (two whole rows)", m.total())
	}
	if num(t, sum["planned_calls"]) != 6 || num(t, sum["deferred"]) != 2 || num(t, sum["ok"]) != 2 || num(t, sum["max_calls"]) != 7 {
		t.Fatalf("summary = %v", sum)
	}
	if !strings.Contains(stderr, "deferred 2 row") {
		t.Fatalf("stderr does not announce the deferral: %q", stderr)
	}
	tb, err := addrkit.ReadTable(out)
	if err != nil {
		t.Fatal(err)
	}
	var statuses []string
	for _, r := range tb.Rows {
		statuses = append(statuses, r[tb.Col("status")])
	}
	if !reflect.DeepEqual(statuses, []string{"ok", "ok", "deferred", "deferred"}) {
		t.Fatalf("statuses = %v", statuses)
	}
	if got := tb.Rows[2][tb.Col("raw")]; got != "Addr three Gurgaon" {
		t.Fatalf("deferred row lost its raw address: %q", got)
	}

	// A later run with room finishes the deferred rows; finished rows stay free.
	stdout, _ = mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--max-calls", "50", "--json")
	sum = decodeObject(t, stdout)
	if num(t, sum["calls_made"]) != 6 || num(t, sum["deferred"]) != 0 || num(t, sum["cached"]) != 2 || num(t, sum["ok"]) != 2 {
		t.Fatalf("follow-up summary = %v", sum)
	}
}

func TestAddressCleanBudgetDefaultsToLocalCallLog(t *testing.T) {
	m := newDMMock(t)
	t.Setenv("DELHIVERY_MAPS_DAILY_BUDGET", "4")
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "address\nAddr one\nAddr two\n")
	out := filepath.Join(dir, "cleaned.csv")

	stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--json")
	sum := decodeObject(t, stdout)
	if m.total() != 3 || num(t, sum["max_calls"]) != 4 || num(t, sum["deferred"]) != 1 {
		t.Fatalf("calls=%d summary=%v", m.total(), sum)
	}
	// The three calls are logged: only one call of the 4-call budget is left.
	stdout, _ = mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--json")
	sum = decodeObject(t, stdout)
	if num(t, sum["budget_remaining_before"]) != 1 || num(t, sum["max_calls"]) != 1 || num(t, sum["deferred"]) != 1 || m.total() != 3 {
		t.Fatalf("second run: calls=%d summary=%v", m.total(), sum)
	}
}

func TestAddressCleanStopsCleanlyOnRateLimit(t *testing.T) {
	m := newDMMock(t)
	m.on(epValidate, func(n int, body map[string]any) (int, any) {
		if n >= 2 {
			return http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"}
		}
		return http.StatusOK, defaultDMReply(epValidate, body)
	})
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "address\nAddr one\nAddr two\nAddr three\n")
	out := filepath.Join(dir, "cleaned.csv")

	stdout, stderr, err := runCLI(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--json")
	if err != nil {
		t.Fatalf("a 429 must not fail the command: %v\nstderr: %s", err, stderr)
	}
	sum := decodeObject(t, stdout)
	if sum["rate_limited"] != true || num(t, sum["ok"]) != 1 || num(t, sum["deferred"]) != 2 || num(t, sum["errors"]) != 0 {
		t.Fatalf("summary = %v", sum)
	}
	// Row 1 = 3 calls; row 2 = standardize + the rejected validate; nothing after.
	if m.count(epValidate) != 2 || m.count(epStandardize) != 2 || m.count(epGeocode) != 1 {
		t.Fatalf("hits standarize=%d validate=%d geocode=%d", m.count(epStandardize), m.count(epValidate), m.count(epGeocode))
	}
	if !strings.Contains(stderr, "429") {
		t.Fatalf("stderr should explain the 429: %q", stderr)
	}
	tb, err := addrkit.ReadTable(out)
	if err != nil {
		t.Fatal(err)
	}
	var statuses []string
	for _, r := range tb.Rows {
		statuses = append(statuses, r[tb.Col("status")])
	}
	if !reflect.DeepEqual(statuses, []string{"ok", "deferred", "deferred"}) {
		t.Fatalf("statuses = %v", statuses)
	}
	// The standardize result for the interrupted row was kept in the cache.
	if got := tb.Rows[1][tb.Col("city")]; got != "Gurgaon" {
		t.Fatalf("partial data for the interrupted row was lost: city=%q", got)
	}
}

func TestAddressCleanRetriesTransientErrorsThenSucceeds(t *testing.T) {
	m := newDMMock(t)
	m.on(epGeocode, func(n int, body map[string]any) (int, any) {
		if n <= 2 {
			return http.StatusServiceUnavailable, map[string]any{"error": "upstream unavailable"}
		}
		return http.StatusOK, defaultDMReply(epGeocode, body)
	})
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "address\nAddr one\n")
	out := filepath.Join(dir, "cleaned.csv")
	stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", filepath.Join(dir, "dm.db"), "--steps", "geocode", "--json")
	sum := decodeObject(t, stdout)
	if m.count(epGeocode) != 3 || num(t, sum["ok"]) != 1 || num(t, sum["errors"]) != 0 {
		t.Fatalf("geocode hits=%d summary=%v", m.count(epGeocode), sum)
	}

	// A permanently failing endpoint gives up after three attempts, marks the
	// row as an error and keeps going.
	m2 := newDMMock(t)
	m2.on(epGeocode, func(int, map[string]any) (int, any) {
		return http.StatusBadGateway, map[string]any{"error": "bad gateway"}
	})
	in2 := writeFile(t, dir, "in2.csv", "address\nAddr one\nAddr two\n")
	stdout, stderr, err := runCLI(t, "address", "clean", "--file", in2, "--out", out, "--force", "--db", filepath.Join(dir, "dm2.db"), "--steps", "geocode", "--json")
	if err == nil || ExitCode(err) != 5 {
		t.Fatalf("every row failing for a non-quota reason must exit 5, got %v", err)
	}
	sum = decodeObject(t, stdout)
	if m2.count(epGeocode) != 6 || num(t, sum["errors"]) != 2 {
		t.Fatalf("hits=%d (want 3 attempts x 2 rows) summary=%v", m2.count(epGeocode), sum)
	}
	if !strings.Contains(stderr, "2 row(s) failed") {
		t.Fatalf("stderr = %q", stderr)
	}
	tb, _ := addrkit.ReadTable(out)
	if st := tb.Rows[0][tb.Col("status")]; st != "error" || !strings.Contains(tb.Rows[0][tb.Col("error")], "502") {
		t.Fatalf("failed row = %v", tb.Rows[0])
	}
}

func TestAddressCleanRejectedTokenStopsTheRun(t *testing.T) {
	m := newDMMock(t)
	m.on(epStandardize, func(int, map[string]any) (int, any) {
		return http.StatusUnauthorized, map[string]any{"error": "bad token"}
	})
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "address\nAddr one\nAddr two\nAddr three\n")
	out := filepath.Join(dir, "cleaned.csv")
	_, _, err := runCLI(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", filepath.Join(dir, "dm.db"), "--json")
	if err == nil || ExitCode(err) != 4 {
		t.Fatalf("expected an auth error (exit 4), got %v", err)
	}
	if m.total() != 1 {
		t.Fatalf("a rejected token must not burn more quota: %d calls", m.total())
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Fatalf("the partial CSV should still be written: %v", statErr)
	}
}

func TestAddressCleanResumeSkipsCompletedRows(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	in := writeFile(t, dir, "in.csv", "address\nAddr one\nAddr two\n")
	out := filepath.Join(dir, "cleaned.csv")
	mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--json")
	if m.total() != 6 {
		t.Fatalf("first run calls = %d", m.total())
	}
	// Wipe the response cache: only dm_address remembers the rows now.
	execDB(t, db, "DELETE FROM dm_cache")
	stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--resume", "--json")
	sum := decodeObject(t, stdout)
	if m.total() != 6 || num(t, sum["calls_made"]) != 0 || num(t, sum["cached"]) != 2 {
		t.Fatalf("resume re-fetched: calls=%d summary=%v", m.total(), sum)
	}
	tb, _ := addrkit.ReadTable(out)
	if tb.Rows[0][tb.Col("formatted")] != "STD addr one" || tb.Rows[0][tb.Col("lat")] == "" {
		t.Fatalf("resumed row lost its data: %v", tb.Rows[0])
	}
	// Without --resume the wiped cache means real calls again.
	execDB(t, db, "DELETE FROM dm_cache")
	mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", db, "--json")
	if m.total() != 12 {
		t.Fatalf("without --resume calls = %d, want 12", m.total())
	}
}

func TestAddressCleanConcurrentWorkersKeepInputOrder(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	var sb strings.Builder
	sb.WriteString("address\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&sb, "Address number %d Gurgaon\n", i)
	}
	in := writeFile(t, dir, "in.csv", sb.String())
	out := filepath.Join(dir, "cleaned.csv")
	stdout, _ := mustRun(t, "address", "clean", "--file", in, "--out", out, "--force", "--db", filepath.Join(dir, "dm.db"), "--concurrency", "9", "--json")
	sum := decodeObject(t, stdout)
	if m.total() != 24 || num(t, sum["ok"]) != 8 {
		t.Fatalf("calls=%d summary=%v", m.total(), sum)
	}
	tb, _ := addrkit.ReadTable(out)
	for i, r := range tb.Rows {
		if want := fmt.Sprintf("Address number %d Gurgaon", i); r[0] != want {
			t.Fatalf("row %d raw = %q, want %q (order must follow the input)", i, r[0], want)
		}
	}
}

func TestAddressCleanDryRunPlansWithoutReadingOrCalling(t *testing.T) {
	m := newDMMock(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.csv")
	out := filepath.Join(t.TempDir(), "cleaned.csv")
	stdout, _ := mustRun(t, "address", "clean", "--file", missing, "--out", out, "--force", "--steps", "validate,geocode", "--dry-run", "--json")
	plan := decodeObject(t, stdout)
	if plan["dry_run"] != true || plan["action"] != "address clean" || num(t, plan["calls_per_row"]) != 2 {
		t.Fatalf("plan = %v", plan)
	}
	if !reflect.DeepEqual(plan["steps"], []any{"validate", "geocode"}) {
		t.Fatalf("steps = %v", plan["steps"])
	}
	if m.total() != 0 {
		t.Fatalf("dry-run touched the network: %d call(s)", m.total())
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("dry-run wrote the output file")
	}
}

func TestAddressCleanUsageErrorsExitTwo(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	in := writeFile(t, dir, "in.csv", "name\nx\n")
	cases := map[string][]string{
		"missing file":   {"address", "clean", "--out", "o.csv"},
		"missing out":    {"address", "clean", "--file", in},
		"bad step":       {"address", "clean", "--file", in, "--out", "o.csv", "--steps", "bogus"},
		"missing column": {"address", "clean", "--file", in, "--out", filepath.Join(dir, "o.csv")},
	}
	for name, args := range cases {
		_, _, err := runCLI(t, append(args, "--db", filepath.Join(dir, "dm.db"))...)
		if err == nil || ExitCode(err) != 2 {
			t.Errorf("%s: want exit 2, got %v", name, err)
		}
	}
	if m.total() != 0 {
		t.Fatalf("usage errors must not call the API: %d", m.total())
	}
}
