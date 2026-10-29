// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Tests for `trip quote`: help wiring plus behaviour against an httptest mock.

package cli

import (
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
)

// mockRouteAndToll wires /route to answer in the live (recommended_route)
// shape with the given geometry and /toll to answer with two plazas.
func mockRouteAndToll(m *dmMock, geometry [][]float64) {
	m.on(epRoute, func(int, map[string]any) (int, any) {
		return http.StatusOK, realRoute(270.54, 17250, geometry)
	})
	m.on(epToll, func(int, map[string]any) (int, any) {
		return http.StatusOK, realToll(185.5, realPlaza("Kherki Daula", "dlv_00652", 105), realPlaza("Shahjahanpur", "dlv_02397", 80.5))
	})
}

func TestTripQuoteFeedsRouteGeometryIntoTollRequest(t *testing.T) {
	m := newDMMock(t)
	geometry := [][]float64{{28.4599, 77.0679}, {27.8, 76.6}, {27.1, 76.0}, {26.9124, 75.7873}}
	mockRouteAndToll(m, geometry)
	db := tempDB(t)

	stdout, _ := mustRun(t, "trip", "quote", "--from", "28.4599,77.0679", "--to", "26.9124,75.7873", "--vehicle", "bus_truck", "--db", db, "--json")
	q := decodeObject(t, stdout)

	routeReq := m.bodies(epRoute)
	if len(routeReq) != 1 {
		t.Fatalf("route requests = %d", len(routeReq))
	}
	if !reflect.DeepEqual(routeReq[0]["geo_coords"], []any{[]any{28.4599, 77.0679}, []any{26.9124, 75.7873}}) {
		t.Fatalf("route geo_coords = %v", routeReq[0]["geo_coords"])
	}
	if of, _ := routeReq[0]["output_format"].(map[string]any); of["encode_polyline"] != false {
		t.Fatalf("route must ask for plain geometry: output_format = %v", routeReq[0]["output_format"])
	}
	tollReq := m.bodies(epToll)
	if len(tollReq) != 1 {
		t.Fatalf("toll requests = %d", len(tollReq))
	}
	if tollReq[0]["vehicle_type"] != "bus_truck" {
		t.Fatalf("toll vehicle_type = %v", tollReq[0]["vehicle_type"])
	}
	routes := tollReq[0]["routes"].([]any)
	if name := routes[0].(map[string]any)["route_name"]; name != tripTollRouteName {
		t.Fatalf("toll route_name = %v, want the constant %q (raw addresses must not be sent)", name, tripTollRouteName)
	}
	sent := routes[0].(map[string]any)["geometry"]
	want := make([]any, 0, len(geometry))
	for _, p := range geometry {
		want = append(want, []any{p[0], p[1]})
	}
	if !reflect.DeepEqual(sent, want) {
		t.Fatalf("toll geometry = %v, want the route response geometry %v", sent, want)
	}
	if m.count(epGeocode) != 0 {
		t.Fatal("coordinate endpoints must not be geocoded")
	}

	if num(t, q["distance_km"]) != 270.54 || num(t, q["duration_min"]) != 287.5 || num(t, q["toll_plaza_count"]) != 2 || num(t, q["total_toll_inr"]) != 185.5 {
		t.Fatalf("quote = %v", q)
	}
	if q["traffic_aware"] != false || q["vehicle"] != "bus_truck" {
		t.Fatalf("quote flags = %v", q)
	}
	plazas := q["plazas"].([]any)
	if len(plazas) != 2 || plazas[0].(map[string]any)["name"] != "Kherki Daula" || plazas[0].(map[string]any)["toll_id"] != "dlv_00652" {
		t.Fatalf("plazas = %v", plazas)
	}
	from := q["from_resolved"].(map[string]any)
	if num(t, from["lat"]) != 28.4599 || from["source"] != "coordinates" {
		t.Fatalf("from_resolved = %v", from)
	}
	if num(t, q["api_calls"]) != 2 {
		t.Fatalf("api_calls = %v, want route + toll", q["api_calls"])
	}
}

func TestTripQuoteGeocodesAddressesDownsamplesAndCaches(t *testing.T) {
	m := newDMMock(t)
	big := make([][]float64, 0, 5000)
	for i := 0; i < 5000; i++ {
		big = append(big, []float64{28.0 - float64(i)*0.0002, 77.0 - float64(i)*0.0001})
	}
	mockRouteAndToll(m, big)
	db := tempDB(t)

	stdout, _ := mustRun(t, "trip", "quote", "--from", "Sector 45 Gurgaon", "--to", "Jaipur", "--via", "26.5,76.5", "--db", db, "--json")
	q := decodeObject(t, stdout)
	if m.count(epGeocode) != 2 {
		t.Fatalf("geocode calls = %d, want 2", m.count(epGeocode))
	}
	toll := m.bodies(epToll)[0]
	geo := toll["routes"].([]any)[0].(map[string]any)["geometry"].([]any)
	if len(geo) != 2000 {
		t.Fatalf("toll geometry points = %d, want 2000", len(geo))
	}
	if !reflect.DeepEqual(geo[0], []any{big[0][0], big[0][1]}) || !reflect.DeepEqual(geo[len(geo)-1], []any{big[4999][0], big[4999][1]}) {
		t.Fatal("downsampling must keep the first and last point")
	}
	if num(t, q["route_points"]) != 2000 {
		t.Fatalf("route_points = %v", q["route_points"])
	}
	coords := m.bodies(epRoute)[0]["geo_coords"].([]any)
	if len(coords) != 3 {
		t.Fatalf("route geo_coords = %v, want from, via, to", coords)
	}
	if v := q["via"].([]any); len(v) != 1 {
		t.Fatalf("via = %v", q["via"])
	}

	// Same quote again: geocodes, the (non-traffic) route and the toll are cached.
	before := m.total()
	stdout, _ = mustRun(t, "trip", "quote", "--from", "sector 45  gurgaon", "--to", "jaipur", "--via", "26.5,76.5", "--db", db, "--json")
	q = decodeObject(t, stdout)
	if m.total() != before {
		t.Fatalf("second quote made %d API call(s), want 0", m.total()-before)
	}
	if num(t, q["api_calls"]) != 0 || num(t, q["cache_hits"]) != 4 {
		t.Fatalf("second quote api_calls=%v cache_hits=%v", q["api_calls"], q["cache_hits"])
	}

	// A traffic-aware route is never served from the cache.
	mustRun(t, "trip", "quote", "--from", "Sector 45 Gurgaon", "--to", "Jaipur", "--via", "26.5,76.5", "--traffic-aware", "--db", db, "--json")
	mustRun(t, "trip", "quote", "--from", "Sector 45 Gurgaon", "--to", "Jaipur", "--via", "26.5,76.5", "--traffic-aware", "--db", db, "--json")
	if m.count(epRoute) != 3 {
		t.Fatalf("route hits = %d, want 3 (1 cached run + 2 traffic-aware runs)", m.count(epRoute))
	}
	if tr := m.bodies(epRoute)[2]["traffic_aware"]; tr != true {
		t.Fatalf("traffic_aware not forwarded: %v", tr)
	}
}

func TestTripQuoteRejectsUnknownVehicleBeforeAnyCall(t *testing.T) {
	m := newDMMock(t)
	_, _, err := runCLI(t, "trip", "quote", "--from", "28.4,77.0", "--to", "26.9,75.7", "--vehicle", "hovercraft", "--db", tempDB(t))
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "bus_truck") {
		t.Fatalf("want a usage error naming the valid vehicles, got %v", err)
	}
	_, _, err = runCLI(t, "trip", "quote", "--from", "28.4,77.0", "--db", tempDB(t))
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("missing --to: want exit 2, got %v", err)
	}
	_, _, err = runCLI(t, "trip", "quote", "--from", "28.4,77.0", "--to", "26.9,75.7", "--departure-time", "tomorrow", "--db", tempDB(t))
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("bad departure time: want exit 2, got %v", err)
	}
	if m.total() != 0 {
		t.Fatalf("validation errors made %d API call(s)", m.total())
	}
}

func TestTripQuoteLanesFileRunsEachLaneAndWritesCSV(t *testing.T) {
	m := newDMMock(t)
	mockRouteAndToll(m, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
	dir := t.TempDir()
	lanes := writeFile(t, dir, "lanes.csv", "from,to,vehicle\n\"28.4,77.0\",\"26.9,75.7\",\n\"28.4,77.0\",\"27.1,78.0\",lcv\n")
	out := filepath.Join(dir, "quotes.csv")
	stdout, _ := mustRun(t, "trip", "quote", "--file", lanes, "--out", out, "--db", filepath.Join(dir, "dm.db"), "--json")
	recs := decodeArray(t, stdout)
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2", len(recs))
	}
	tolls := m.bodies(epToll)
	if len(tolls) != 2 || tolls[0]["vehicle_type"] != "car_jeep_van" || tolls[1]["vehicle_type"] != "lcv" {
		t.Fatalf("toll vehicle types = %v", tolls)
	}
	tb, err := addrkit.ReadTable(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.Rows) != 2 || tb.Rows[1][tb.Col("vehicle")] != "lcv" || tb.Rows[0][tb.Col("total_toll_inr")] != "185.5" || tb.Rows[0][tb.Col("plazas")] != "Kherki Daula; Shahjahanpur" {
		t.Fatalf("csv = %v %v", tb.Header, tb.Rows)
	}
}

func TestTripQuoteLanesKeepGoingAfterLaneFailureAndStopOnRateLimit(t *testing.T) {
	m := newDMMock(t)
	mockRouteAndToll(m, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
	m.on(epRoute, func(n int, body map[string]any) (int, any) {
		switch n {
		case 2:
			return http.StatusBadRequest, map[string]any{"error": "no route found"}
		case 3:
			return http.StatusTooManyRequests, map[string]any{"error": "quota"}
		}
		return http.StatusOK, realRoute(10, 600, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
	})
	dir := t.TempDir()
	lanes := writeFile(t, dir, "lanes.csv", "from,to\n\"28.4,77.0\",\"26.9,75.7\"\n\"28.4,77.0\",\"20.0,70.0\"\n\"28.4,77.0\",\"27.0,75.0\"\n\"28.4,77.0\",\"27.5,75.5\"\n")
	stdout, stderr, err := runCLI(t, "trip", "quote", "--file", lanes, "--db", filepath.Join(dir, "dm.db"), "--json")
	if err != nil {
		t.Fatalf("lane failures and 429 must not fail the run: %v", err)
	}
	recs := decodeArray(t, stdout)
	errs := make([]string, 0, 4)
	for _, r := range recs {
		e, _ := r.(map[string]any)["error"].(string)
		errs = append(errs, e)
	}
	if errs[0] != "" || !strings.Contains(errs[1], "400") || !strings.Contains(errs[2], "429") || !strings.Contains(errs[3], "deferred") {
		t.Fatalf("lane errors = %q", errs)
	}
	if m.count(epRoute) != 3 {
		t.Fatalf("route calls = %d: the lane after a 429 must not be attempted", m.count(epRoute))
	}
	if !strings.Contains(stderr, "1 of 4 lane(s) failed") || !strings.Contains(stderr, "2 of 4 lane(s) deferred") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestTripQuoteSingleTripRateLimitExitsSeven(t *testing.T) {
	m := newDMMock(t)
	m.on(epRoute, func(int, map[string]any) (int, any) {
		return http.StatusTooManyRequests, map[string]any{"error": "quota"}
	})
	_, _, err := runCLI(t, "trip", "quote", "--from", "28.4,77.0", "--to", "26.9,75.7", "--db", tempDB(t), "--json")
	if err == nil || ExitCode(err) != 7 {
		t.Fatalf("want exit 7 for a rate limit, got %v", err)
	}
	if m.count(epRoute) != 1 {
		t.Fatalf("route hits = %d: a 429 must not be retried", m.count(epRoute))
	}
}
