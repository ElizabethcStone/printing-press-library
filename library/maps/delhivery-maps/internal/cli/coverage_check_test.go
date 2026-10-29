// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Tests for `coverage check`: help wiring plus behaviour against an httptest mock.

package cli

import (
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
)

// squareIsochrone answers /isochrone with a square covering longitude
// 77.0..77.2 and latitude 28.4..28.6. GeoJSON order is [lng, lat], so a
// lat/lng mix-up in the CLI flips the inside/outside verdicts below.
func squareIsochrone(m *dmMock) {
	m.on(epIsochrone, func(int, map[string]any) (int, any) {
		return http.StatusOK, map[string]any{
			"type": "FeatureCollection", "request_id": "6c40792b-6443-4870-b773-2a970e210f8a", "status": true,
			"features": []any{map[string]any{
				"type":       "Feature",
				"properties": map[string]any{"area_sq_km": 8.64, "cost_type": "time", "cost_value": 600, "travel_mode": "auto"},
				"geometry": map[string]any{
					"type":        "Polygon",
					"coordinates": [][][]float64{{{77.0, 28.4}, {77.2, 28.4}, {77.2, 28.6}, {77.0, 28.6}, {77.0, 28.4}}},
				},
			}},
		}
	})
}

func TestCoverageCheckFlagsOutsidePointsWithCorrectLngLatOrder(t *testing.T) {
	m := newDMMock(t)
	squareIsochrone(m)
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", strings.Join([]string{
		"id,lat,lng",
		"inside,28.5,77.1",
		"east-of-box,28.5,77.5",
		// lat 77.1 / lng 28.5 would be inside the box if the axes were swapped.
		"axis-trap,77.1,28.5",
		"",
	}, "\n"))
	out := filepath.Join(dir, "flagged.csv")

	stdout, _ := mustRun(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "45", "--file", orders, "--out", out, "--db", filepath.Join(dir, "dm.db"), "--json")
	sum := decodeObject(t, stdout)
	if num(t, sum["total"]) != 3 || num(t, sum["inside"]) != 1 || num(t, sum["outside"]) != 2 || num(t, sum["ungeocoded"]) != 0 || num(t, sum["planned_calls"]) != 1 {
		t.Fatalf("summary = %v", sum)
	}
	tb, err := addrkit.ReadTable(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tb.Header, []string{"id", "lat", "lng", "inside", "geocode_status"}) {
		t.Fatalf("header = %v", tb.Header)
	}
	var verdicts []string
	for _, r := range tb.Rows {
		verdicts = append(verdicts, r[0]+"="+r[tb.Col("inside")]+"/"+r[tb.Col("geocode_status")])
	}
	if !reflect.DeepEqual(verdicts, []string{"inside=true/given", "east-of-box=false/given", "axis-trap=false/given"}) {
		t.Fatalf("verdicts = %v", verdicts)
	}
	iso := m.bodies(epIsochrone)
	if len(iso) != 1 {
		t.Fatalf("isochrone calls = %d, want exactly 1", len(iso))
	}
	body := iso[0]
	if !reflect.DeepEqual(body["origin"], []any{28.5, 77.1}) || body["cost_type"] != "time" || num(t, body["cost_value"]) != 2700 ||
		body["resolution"] != "medium" || body["encode_geojson"] != false {
		t.Fatalf("isochrone request = %v", body)
	}
}

func TestCoverageCheckDistanceModeAndCachedIsochrone(t *testing.T) {
	m := newDMMock(t)
	squareIsochrone(m)
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	orders := writeFile(t, dir, "orders.csv", "latitude,longitude\n28.5,77.1\n")
	out := filepath.Join(dir, "flagged.csv")
	mustRun(t, "coverage", "check", "--hub", "28.5,77.1", "--km", "25", "--file", orders, "--out", out, "--db", db, "--json")
	body := m.bodies(epIsochrone)[0]
	if body["cost_type"] != "distance" || num(t, body["cost_value"]) != 25000 {
		t.Fatalf("distance request = %v", body)
	}
	stdout, _ := mustRun(t, "coverage", "check", "--hub", "28.5,77.1", "--km", "25", "--file", orders, "--out", out, "--db", db, "--force", "--json")
	sum := decodeObject(t, stdout)
	if m.count(epIsochrone) != 1 || num(t, sum["planned_calls"]) != 0 || num(t, sum["calls_made"]) != 0 || num(t, sum["inside"]) != 1 {
		t.Fatalf("isochrone must be cached on a repeat: hits=%d summary=%v", m.count(epIsochrone), sum)
	}
}

func TestCoverageCheckGeocodesAddressOnlyOrders(t *testing.T) {
	m := newDMMock(t)
	squareIsochrone(m)
	m.on(epGeocode, func(_ int, body map[string]any) (int, any) {
		switch body["address"] {
		case "Sector 45 Gurgaon":
			return http.StatusOK, map[string]any{"lat": 28.5, "lng": 77.1, "error_radius": 20}
		case "Far away":
			return http.StatusOK, map[string]any{"lat": 19.0, "lng": 72.8, "error_radius": 20}
		}
		return http.StatusOK, map[string]any{"error": "not found"}
	})
	dir := t.TempDir()
	db := filepath.Join(dir, "dm.db")
	orders := writeFile(t, dir, "orders.csv", strings.Join([]string{
		"order_id,address,lat,lng",
		"1,Sector 45 Gurgaon,,",
		"2,Far away,,",
		"3,Nowhere land,,",
		"4,,28.45,77.05",
		"5,,,",
		"",
	}, "\n"))
	out := filepath.Join(dir, "flagged.csv")
	stdout, _, err := runCLI(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", orders, "--out", out, "--db", db, "--json")
	if err != nil {
		t.Fatalf("an ungeocodable order must not fail the command: %v", err)
	}
	sum := decodeObject(t, stdout)
	if num(t, sum["total"]) != 5 || num(t, sum["inside"]) != 2 || num(t, sum["outside"]) != 1 || num(t, sum["ungeocoded"]) != 2 || num(t, sum["missing"]) != 1 || num(t, sum["failed"]) != 1 || num(t, sum["planned_calls"]) != 4 {
		t.Fatalf("summary = %v", sum)
	}
	tb, _ := addrkit.ReadTable(out)
	var got []string
	for _, r := range tb.Rows {
		got = append(got, r[tb.Col("inside")]+"/"+r[tb.Col("geocode_status")])
	}
	if !reflect.DeepEqual(got, []string{"true/geocoded", "false/geocoded", "/failed", "true/given", "/missing"}) {
		t.Fatalf("rows = %v", got)
	}
	// Repeat: the isochrone and the good geocodes come from the cache; only the
	// address the API could not locate is asked again, because an unusable
	// reply is never cached.
	before := m.total()
	geoBefore := m.count(epGeocode)
	stdout, _ = mustRun(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", orders, "--out", out, "--db", db, "--force", "--json")
	if m.total()-before != 1 || m.count(epGeocode)-geoBefore != 1 || m.count(epIsochrone) != 1 {
		t.Fatalf("repeat made %d API call(s), want exactly the 1 retry of the unlocatable address", m.total()-before)
	}
	tb, _ = addrkit.ReadTable(out)
	if st := tb.Rows[0][tb.Col("geocode_status")]; st != "cached" {
		t.Fatalf("repeat geocode_status = %q, want cached", st)
	}
}

func TestCoverageCheckStopsGeocodingOnRateLimitAndHonoursMaxCalls(t *testing.T) {
	m := newDMMock(t)
	squareIsochrone(m)
	m.on(epGeocode, func(n int, body map[string]any) (int, any) {
		if n >= 2 {
			return http.StatusTooManyRequests, map[string]any{"error": "quota"}
		}
		return http.StatusOK, map[string]any{"lat": 28.5, "lng": 77.1}
	})
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", "address\nOne\nTwo\nThree\n")
	out := filepath.Join(dir, "flagged.csv")
	stdout, stderr, err := runCLI(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", orders, "--out", out, "--db", filepath.Join(dir, "dm.db"), "--json")
	if err != nil {
		t.Fatalf("429 must exit 0: %v", err)
	}
	sum := decodeObject(t, stdout)
	if sum["rate_limited"] != true || num(t, sum["inside"]) != 1 || num(t, sum["ungeocoded"]) != 2 || m.count(epGeocode) != 2 {
		t.Fatalf("summary=%v geocode hits=%d", sum, m.count(epGeocode))
	}
	if !strings.Contains(stderr, "429") {
		t.Fatalf("stderr = %q", stderr)
	}
	tb, _ := addrkit.ReadTable(out)
	if st := tb.Rows[2][tb.Col("geocode_status")]; st != "deferred" {
		t.Fatalf("third order status = %q, want deferred", st)
	}

	// Fresh database, --max-calls 3: the isochrone takes one call, so only two of
	// the three addresses can be geocoded and the third is deferred.
	m2 := newDMMock(t)
	squareIsochrone(m2)
	stdout, stderr = mustRun(t, "coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", orders, "--out", out, "--db", filepath.Join(dir, "dm2.db"), "--max-calls", "3", "--force", "--json")
	sum = decodeObject(t, stdout)
	if m2.count(epGeocode) != 2 || m2.count(epIsochrone) != 1 || num(t, sum["planned_calls"]) != 3 || num(t, sum["ungeocoded"]) != 1 {
		t.Fatalf("max-calls: geocodes=%d iso=%d summary=%v", m2.count(epGeocode), m2.count(epIsochrone), sum)
	}
	if !strings.Contains(stderr, "deferred") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestCoverageCheckUsageErrors(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", "lat,lng\n28.5,77.1\n")
	out := filepath.Join(dir, "o.csv")
	cases := map[string][]string{
		"both minutes and km": {"coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--km", "5", "--file", orders, "--out", out},
		"neither":             {"coverage", "check", "--hub", "28.5,77.1", "--file", orders, "--out", out},
		"bad hub":             {"coverage", "check", "--hub", "Gurgaon", "--minutes", "30", "--file", orders, "--out", out},
		"no coordinates":      {"coverage", "check", "--hub", "28.5,77.1", "--minutes", "30", "--file", writeFile(t, dir, "bad.csv", "name\nx\n"), "--out", out},
	}
	for name, args := range cases {
		_, _, err := runCLI(t, append(args, "--db", filepath.Join(dir, "dm.db"))...)
		if err == nil || ExitCode(err) != 2 {
			t.Errorf("%s: want exit 2, got %v", name, err)
		}
	}
	if m.total() != 0 {
		t.Fatalf("usage errors made %d API call(s)", m.total())
	}
}
