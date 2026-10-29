// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Tests for `address dupes`: help wiring plus grouping against a seeded store.

package cli

import (
	"strings"
	"testing"
)

func TestAddressDupesGroupsByTextAndByDistance(t *testing.T) {
	db := tempDB(t)
	seedAddresses(t, db, []seedAddr{
		// Same text, written differently, in two different places.
		{key: "t1", raw: "Flat 4B Lotus Apt", formatted: "Flat 4B, Lotus Apt, Sector 45, Gurgaon 122003", granularity: "PREMISE", lat: 28.4595, lng: 77.0266},
		{key: "t2", raw: "flat 4b lotus apt sec 45", formatted: "flat 4b lotus apt sector 45 gurgaon 122003", granularity: "PREMISE", lat: 26.9124, lng: 75.7873},
		// Different text, about 20 m apart.
		{key: "g1", raw: "Plot 9 Malviya Nagar", formatted: "Plot 9, Malviya Nagar, Jaipur", granularity: "STREET_LANDMARK", lat: 26.85, lng: 75.8},
		{key: "g2", raw: "Near plot 9 Malviya Nagar", formatted: "Opposite Plot 9, Malviya Nagar, Jaipur", granularity: "PREMISE", radius: 40, lat: 26.85018, lng: 75.8},
		// Alone.
		{key: "s1", raw: "Somewhere else", formatted: "Somewhere else, Pune", granularity: "PREMISE", lat: 18.5204, lng: 73.8567},
		// No coordinates and no formatted text: cannot be matched.
		{key: "n1", raw: "unresolved", formatted: ""},
	})
	stdout, _ := mustRun(t, "address", "dupes", "--db", db, "--json")
	groups := decodeArray(t, stdout)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2: %s", len(groups), stdout)
	}
	reasons := map[string][]string{}
	for _, g := range groups {
		m := g.(map[string]any)
		var raws []string
		for _, mem := range m["members"].([]any) {
			raws = append(raws, mem.(map[string]any)["raw"].(string))
		}
		reasons[m["reason"].(string)] = raws
		if num(t, m["group_id"]) < 1 {
			t.Fatalf("group_id missing: %v", m)
		}
	}
	if got := strings.Join(reasons["text"], "|"); got != "Flat 4B Lotus Apt|flat 4b lotus apt sec 45" {
		t.Fatalf("text group = %q", got)
	}
	if got := strings.Join(reasons["geo"], "|"); got != "Plot 9 Malviya Nagar|Near plot 9 Malviya Nagar" {
		t.Fatalf("geo group = %q", got)
	}
}

func TestAddressDupesDoesNotGroupAddressesFiveKmApart(t *testing.T) {
	db := tempDB(t)
	seedAddresses(t, db, []seedAddr{
		{key: "a", raw: "Plot 9 Malviya Nagar", formatted: "Plot 9, Malviya Nagar, Jaipur", granularity: "STREET_LANDMARK", lat: 26.85, lng: 75.8},
		// 0.045 degrees of latitude is about 5 km.
		{key: "b", raw: "Plot 9 Vaishali Nagar", formatted: "Plot 9, Vaishali Nagar, Jaipur", granularity: "PREMISE", lat: 26.895, lng: 75.8},
	})
	for _, radius := range []string{"30", "100", "1000"} {
		stdout, _ := mustRun(t, "address", "dupes", "--radius-m", radius, "--db", db, "--json")
		if got := decodeArray(t, stdout); len(got) != 0 {
			t.Fatalf("radius %s grouped addresses 5 km apart: %s", radius, stdout)
		}
	}
	// A radius that does span the gap groups them, proving the radius is honoured.
	stdout, _ := mustRun(t, "address", "dupes", "--radius-m", "6000", "--db", db, "--json")
	if got := decodeArray(t, stdout); len(got) != 1 {
		t.Fatalf("radius 6000 should group them: %s", stdout)
	}
}

func TestAddressDupesEmptyStoreReturnsEmptyList(t *testing.T) {
	stdout, stderr, err := runCLI(t, "address", "dupes", "--json", "--db", tempDB(t))
	if err != nil || strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("missing db: err=%v stdout=%q", err, stdout)
	}
	if !strings.Contains(stderr, "address clean") {
		t.Fatalf("stderr hint = %q", stderr)
	}
	empty := tempDB(t)
	seedAddresses(t, empty, nil)
	stdout, _, err = runCLI(t, "address", "dupes", "--json", "--db", empty)
	if err != nil || strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("empty table: err=%v stdout=%q", err, stdout)
	}
}

func TestAddressDupesRejectsNonPositiveRadius(t *testing.T) {
	_, _, err := runCLI(t, "address", "dupes", "--radius-m", "0", "--db", tempDB(t))
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("want a usage error (exit 2), got %v", err)
	}
}

func TestAddressDupesIgnoresCoarseAndUnvalidatedRows(t *testing.T) {
	db := tempDB(t)
	seedAddresses(t, db, []seedAddr{
		// Same pincode centroid and same text, but coarse granularity: never a duplicate.
		{key: "c1", raw: "Gurgaon 122003", formatted: "Gurgaon, Haryana 122003", granularity: "LOCALITY", lat: 28.45, lng: 77.03},
		{key: "c2", raw: "gurgaon  122003", formatted: "gurgaon haryana 122003", granularity: "LOCALITY", lat: 28.45, lng: 77.03},
		{key: "v1", raw: "Village X", formatted: "Village X, Rajasthan", granularity: "SUBLOCALITY_VILLAGE", lat: 26.1, lng: 75.1},
		{key: "v2", raw: "Village X again", formatted: "Village X again, Rajasthan", granularity: "SUBLOCALITY_VILLAGE", lat: 26.1, lng: 75.1},
		// Never validated: no granularity, so not comparable.
		{key: "u1", raw: "No validate A", formatted: "Same text", lat: 22.1, lng: 73.1},
		{key: "u2", raw: "No validate B", formatted: "same text", lat: 22.1, lng: 73.1},
		// Precise rows, but the geocode itself is too uncertain (error radius 500 m).
		{key: "e1", raw: "Fuzzy A", formatted: "Fuzzy A, Pune", granularity: "PREMISE", radius: 500, lat: 18.5, lng: 73.8},
		{key: "e2", raw: "Fuzzy B", formatted: "Fuzzy B, Pune", granularity: "PREMISE", radius: 500, lat: 18.5, lng: 73.8},
	})
	stdout, _ := mustRun(t, "address", "dupes", "--db", db, "--json")
	if got := decodeArray(t, stdout); len(got) != 0 {
		t.Fatalf("coarse, unvalidated or imprecise rows were grouped: %s", stdout)
	}
}

func TestAddressDupesStripsTerminalEscapesFromStoredText(t *testing.T) {
	db := tempDB(t)
	evil := "Plot 9\x1b]0;pwned\x07 Malviya \x1b[31mNagar\x1b[0m\x01"
	seedAddresses(t, db, []seedAddr{
		{key: "a", raw: evil, formatted: "Plot 9, Malviya Nagar, Jaipur", granularity: "PREMISE", lat: 26.85, lng: 75.8},
		{key: "b", raw: "Plot 9 Malviya Nagar", formatted: "plot 9 malviya nagar jaipur", granularity: "PREMISE", lat: 26.9, lng: 75.9},
	})
	for _, args := range [][]string{{"--json"}, {"--human-friendly"}} {
		stdout, _ := mustRun(t, append([]string{"address", "dupes", "--db", db}, args...)...)
		if strings.ContainsAny(stdout, "\x1b\x07\x01") {
			t.Fatalf("control characters reached the output with %v: %q", args, stdout)
		}
		if !strings.Contains(stdout, "Plot 9 Malviya Nagar") {
			t.Fatalf("sanitized text missing with %v: %q", args, stdout)
		}
	}
}
