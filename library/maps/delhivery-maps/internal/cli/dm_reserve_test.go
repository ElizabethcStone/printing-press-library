package cli

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// Separate store handles on one database file stand in for separate CLI
// processes. With a budget of one call, exactly one reservation may succeed.
func TestReserveCallSpendsTheLastCallOnlyOnce(t *testing.T) {
	ctx := context.Background()
	t.Setenv(dmBudgetEnv, "1")
	path := tempDB(t)
	const handles = 6
	errs := make(chan error, handles)
	var wg sync.WaitGroup
	for i := 0; i < handles; i++ {
		s, err := openDMStore(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- dmReserveCall(ctx, s.DB(), "scope", epGeocode)
		}()
	}
	wg.Wait()
	close(errs)

	granted := 0
	for err := range errs {
		switch {
		case err == nil:
			granted++
		case errors.Is(err, errDMRateLimited):
		default:
			t.Errorf("unexpected reservation error: %v", err)
		}
	}
	if granted != 1 {
		t.Fatalf("granted %d reservations on a budget of 1, want 1", granted)
	}
	if n := countRows(t, path, "dm_calls"); n != 1 {
		t.Errorf("dm_calls rows = %d, want 1 (a refused reservation must not log a call)", n)
	}
}

// --compact keeps a field only when most rows carry it. A single deferred
// row must still say it was deferred and why, not look like a zero result.
func TestCompactOutputKeepsTheOutcomeOfTheOneDeferredRow(t *testing.T) {
	rows := make([]map[string]any, 0, 10)
	for i := 0; i < 9; i++ {
		rows = append(rows, map[string]any{"from": "A", "to": "B", "distance_km": float64(10 + i), "toll": 40.0})
	}
	rows = append(rows, map[string]any{"from": "A", "to": "C", "distance_km": 0.0, "toll": 0.0, "deferred": true, "error": "rate limited"})

	var out bytes.Buffer
	if err := dmPrint(&out, rows, &rootFlags{asJSON: true, compact: true}); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`"deferred":\s*true`).MatchString(out.String()) {
		t.Errorf("compact output lost deferred:true from the deferred row:\n%s", out.String())
	}
	if !regexp.MustCompile(`"error":\s*"rate limited"`).MatchString(out.String()) {
		t.Errorf("compact output lost the error from the deferred row:\n%s", out.String())
	}
}

// Discovery must still find the batch workflows, which live in the curated
// index next to the promoted endpoint commands.
func TestWhichFindsTheBatchWorkflows(t *testing.T) {
	for query, want := range map[string]string{
		"duplicate doorstep":    "address dupes",
		"toll cost for a trip":  "trip quote",
		"service area coverage": "coverage check",
	} {
		got := rankWhich(whichIndex, query, 3)
		found := false
		for _, m := range got {
			if m.Entry.Command == want {
				found = true
			}
		}
		if !found {
			t.Errorf("which %q: %s not in the top matches", query, want)
		}
	}
}

// With the local-data switch on, the batch workflows that cache responses and
// write addresses are refused before any call or write.
func TestNoLocalDataRefusesTheBatchWorkflows(t *testing.T) {
	t.Setenv(noLocalDataEnvVar, "1")
	for _, path := range []string{"address clean", "trip quote", "coverage check", "fleet plan"} {
		_, _, err := runCLI(t, append(strings.Fields(path), "--stdin")...)
		if err == nil || !strings.Contains(err.Error(), "disabled while "+noLocalDataEnvVar+"=1") {
			t.Errorf("%s: err = %v, want the local-data refusal", path, err)
		}
	}
}
