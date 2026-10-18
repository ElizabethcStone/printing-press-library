// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Novel command scaffold tests. Keep the wiring smoke test and add behavior cases as needed.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/payments/investorpaisa/internal/cliutil/testenv"
	"github.com/mvanhorn/printing-press-library/library/payments/investorpaisa/internal/store"
)

// TestNovelChangesHelpWires smoke-tests that the changes command
// resolves at runtime and renders useful --help output. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Keep this smoke test when adding behavior-specific cases.
func TestNovelChangesHelpWires(t *testing.T) {
	testenv.Isolate(t)
	cmd := RootCmd()
	cmd.SetArgs([]string{"changes", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("changes --help error = %v (novel command not wired correctly?)", err)
	}
	help := out.String()
	for _, want := range []string{"Usage:", "changes"} {
		if !strings.Contains(help, want) {
			t.Fatalf("changes --help missing %q in output:\n%s", want, help)
		}
	}
}

func TestChangesBaselineFindsRequestedDateBeyondLatest60(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "snapshots.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ipEnsureSnapshotTable(ctx, db); err != nil {
		t.Fatal(err)
	}

	current := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	for daysAgo := 365; daysAgo >= 0; daysAgo-- {
		asof := current.AddDate(0, 0, -daysAgo).Format("2006-01-02")
		body, _ := json.Marshal([]ipRow{{"symbol": "A", "close": float64(daysAgo)}})
		if err := ipSaveSnapshot(ctx, db, ipKindScreener, asof, body); err != nil {
			t.Fatal(err)
		}
	}

	target := current.AddDate(0, 0, -365).Format("2006-01-02")
	got, err := ipSnapshotBaseline(ctx, db, ipKindScreener, current.Format("2006-01-02"), target)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.AsOf != target {
		t.Fatalf("baseline = %#v, want requested date %s despite 60-snapshot window", got, target)
	}
}

func TestChangesBaselineFallsBackToOldestEarlierSnapshot(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "snapshots.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ipEnsureSnapshotTable(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, asof := range []string{"2026-10-10", "2026-10-12", "2026-10-15"} {
		if err := ipSaveSnapshot(ctx, db, ipKindScreener, asof, json.RawMessage(`[]`)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ipSnapshotBaseline(ctx, db, ipKindScreener, "2026-10-17", "2026-09-17")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.AsOf != "2026-10-10" {
		t.Fatalf("baseline = %#v, want oldest earlier snapshot 2026-10-10", got)
	}
	wantNote := "no snapshot as old as --since 30d; compared against the oldest stored one (2026-10-10)"
	if note := changesFallbackNote("30d", got.AsOf); note != wantNote {
		t.Fatalf("fallback note = %q, want %q", note, wantNote)
	}
}
