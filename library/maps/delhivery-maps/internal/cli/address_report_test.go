// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Tests for `address report`: help wiring plus aggregation against a seeded store.

package cli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/store"
)

type seedAddr struct {
	key, raw, formatted, quality, granularity, pincode, city, state string
	lat, lng                                                        any
	radius                                                          any
}

func seedAddresses(t *testing.T, dbPath string, rows []seedAddr) {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := store.EnsureDelhiveryMapsTables(context.Background(), s.DB()); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		_, err := s.DB().Exec(`INSERT INTO dm_address (raw_key, raw, formatted, validated_quality, granularity, pincode, city, state, lat, lng, error_radius, updated_at)
			VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, '2026-10-27T00:00:00Z')`,
			r.key, r.raw, r.formatted, r.quality, r.granularity, r.pincode, r.city, r.state, r.lat, r.lng, r.radius)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestAddressReportGroupsByPincode(t *testing.T) {
	db := tempDB(t)
	seedAddresses(t, db, []seedAddr{
		{key: "a", raw: "a", quality: "ok", granularity: "PREMISE", pincode: "122003", city: "Gurgaon", radius: 10},
		{key: "b", raw: "b", quality: "ok", granularity: "STREET_LANDMARK", pincode: "122003", city: "Gurgaon", radius: 30},
		{key: "c", raw: "c", quality: "bad", granularity: "PREMISE", pincode: "122003", city: "Gurgaon", radius: 20},
		{key: "d", raw: "d", quality: "ok", granularity: "LOCALITY", pincode: "302001", city: "Jaipur"},
		{key: "e", raw: "e", quality: "", granularity: "", pincode: "", city: "Jaipur"},
	})
	stdout, _ := mustRun(t, "address", "report", "--group-by", "pincode", "--db", db, "--json")
	rows := decodeArray(t, stdout)
	if len(rows) != 3 {
		t.Fatalf("groups = %d, want 3: %s", len(rows), stdout)
	}
	byGroup := map[string]map[string]any{}
	for _, r := range rows {
		m := r.(map[string]any)
		byGroup[m["group"].(string)] = m
	}
	g := byGroup["122003"]
	if num(t, g["count"]) != 3 || num(t, g["pct_premise"]) != 66.7 || num(t, g["pct_ok"]) != 66.7 || num(t, g["avg_error_radius"]) != 20 || num(t, g["without_validate"]) != 0 {
		t.Fatalf("122003 = %v", g)
	}
	j := byGroup["302001"]
	if num(t, j["count"]) != 1 || num(t, j["pct_premise"]) != 0 || num(t, j["pct_ok"]) != 100 || j["avg_error_radius"] != nil {
		t.Fatalf("302001 = %v (no radius data must be null, not 0)", j)
	}
	u := byGroup["(unknown)"]
	if u == nil || num(t, u["count"]) != 1 {
		t.Fatalf("a missing pincode must form an (unknown) group: %v", byGroup)
	}
	// The one row here was never validated: it counts as without_validate and
	// must not drag the percentages to 0; with no denominator they are null.
	if num(t, u["without_validate"]) != 1 || u["pct_ok"] != nil || u["pct_premise"] != nil {
		t.Fatalf("(unknown) group = %v", u)
	}
	if first := rows[0].(map[string]any)["group"]; first != "122003" {
		t.Fatalf("groups are not ordered by count: first = %v", first)
	}

	stdout, _ = mustRun(t, "address", "report", "--group-by", "city", "--db", db, "--json")
	cities := decodeArray(t, stdout)
	if len(cities) != 2 || cities[0].(map[string]any)["group"] != "Gurgaon" {
		t.Fatalf("city groups = %v", cities)
	}
	stdout, _ = mustRun(t, "address", "report", "--group-by", "quality", "--db", db, "--json")
	if got := decodeArray(t, stdout); len(got) != 3 {
		t.Fatalf("quality groups = %v", got)
	}
}

func TestAddressReportEmptyOrMissingDBReturnsEmptyList(t *testing.T) {
	missing := tempDB(t)
	stdout, stderr, err := runCLI(t, "address", "report", "--json", "--db", missing)
	if err != nil {
		t.Fatalf("missing db must exit 0: %v", err)
	}
	if got := decodeArray(t, stdout); len(got) != 0 || !strings.Contains(stdout, "[]") {
		t.Fatalf("stdout = %q, want []", stdout)
	}
	if !strings.Contains(stderr, "address clean") {
		t.Fatalf("stderr hint = %q, want a pointer to 'address clean'", stderr)
	}
	if _, statErr := os.Stat(missing); statErr == nil {
		t.Fatal("a read-only report must not create the database file")
	}

	// A database that exists but never saw `address clean`.
	other := tempDB(t)
	s, err := store.Open(other)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	stdout, _, err = runCLI(t, "address", "report", "--json", "--db", other)
	if err != nil || strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("no-table db: err=%v stdout=%q", err, stdout)
	}

	// Created but empty.
	empty := tempDB(t)
	seedAddresses(t, empty, nil)
	stdout, _, err = runCLI(t, "address", "report", "--group-by", "state", "--json", "--db", empty)
	if err != nil || strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("empty table: err=%v stdout=%q", err, stdout)
	}
}

func TestAddressReportRejectsUnknownGroupBy(t *testing.T) {
	_, _, err := runCLI(t, "address", "report", "--group-by", "colour", "--db", tempDB(t))
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("want a usage error (exit 2), got %v", err)
	}
}
