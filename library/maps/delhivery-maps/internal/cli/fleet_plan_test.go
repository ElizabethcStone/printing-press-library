// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Tests for `fleet plan`: help wiring plus behaviour against an httptest mock.

package cli

import (
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
)

// echoRoutePlan answers /route-plan by putting every order on vehicle_1 in
// request order, with a depot stop at each end like the real solver.
func echoRoutePlan(_ int, body map[string]any) (int, any) {
	orders := body["orders"].([]any)
	route := []any{map[string]any{"lat": body["depot_lat"], "lng": body["depot_lng"], "type": "depot"}}
	ids := make([]any, 0, len(orders))
	for i, o := range orders {
		om := o.(map[string]any)
		ids = append(ids, om["order_id"])
		stop := map[string]any{"type": "order", "order_id": om["order_id"], "sequence": i + 1}
		if om["order_lat"] != nil {
			stop["lat"], stop["lng"] = om["order_lat"], om["order_lng"]
		} else {
			stop["lat"], stop["lng"] = 28.5, 77.0 // the API geocoded the address
		}
		route = append(route, stop)
	}
	route = append(route, map[string]any{"lat": body["depot_lat"], "lng": body["depot_lng"], "type": "depot"})
	// plan_status is a JSON boolean on the live API.
	return http.StatusOK, map[string]any{
		"plan_status": true,
		"plan_summary": map[string]any{
			"assigned_orders": len(orders), "total_distance": 1000, "total_duration": 600,
			"total_orders": len(orders), "unassigned_orders": 0, "vehicles_used": 1,
		},
		"req_id": "plan-1", "request_id": "209da29c-2eae-4f70-b04a-22e22b6d1d94",
		"routes_info": []any{map[string]any{
			"vehicle_id": "vehicle_1", "orders": ids, "route": route, "num_stops": len(orders),
			"total_distance": 1000, "total_duration": 600, "total_volume": 14000.0, "total_weight": 30.0,
		}},
		"unassigned_orders": []any{},
	}
}

func ordersCSV(n int, withAddress bool) string {
	var sb strings.Builder
	if withAddress {
		sb.WriteString("order_id,address,weight\n")
	} else {
		sb.WriteString("order_id,lat,lng,weight,volume,service_duration\n")
	}
	for i := 1; i <= n; i++ {
		if withAddress {
			fmt.Fprintf(&sb, "ORD%03d,\"House %d, Sector 45, Gurgaon\",2.5\n", i, i)
		} else {
			fmt.Fprintf(&sb, "ORD%03d,%.4f,%.4f,2.5,0.1,120\n", i, 28.40+float64(i)*0.001, 77.00+float64(i)*0.001)
		}
	}
	return sb.String()
}

func TestFleetPlanSplits120OrdersIntoThreeCallsAndMerges(t *testing.T) {
	m := newDMMock(t)
	m.on(epRoutePlan, echoRoutePlan)
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", ordersCSV(120, false))
	out := filepath.Join(dir, "plan.csv")

	stdout, _ := mustRun(t, "fleet", "plan", "--orders", orders, "--vehicles", "4", "--depot", "28.4599,77.0679", "--return-to-depot", "--vehicle-mode", "truck", "--max-weight", "1500", "--out", out, "--db", filepath.Join(dir, "dm.db"), "--json")
	sum := decodeObject(t, stdout)

	reqs := m.bodies(epRoutePlan)
	if len(reqs) != 3 {
		t.Fatalf("route-plan calls = %d, want 3", len(reqs))
	}
	var sizes []int
	seen := map[string]bool{}
	for _, r := range reqs {
		ords := r["orders"].([]any)
		sizes = append(sizes, len(ords))
		for _, o := range ords {
			id := o.(map[string]any)["order_id"].(string)
			if seen[id] {
				t.Fatalf("order %s sent twice", id)
			}
			seen[id] = true
		}
		if num(t, r["num_vehicles"]) != 4 || r["return_to_depot"] != true || r["vehicle_mode"] != "truck" ||
			num(t, r["depot_lat"]) != 28.4599 || num(t, r["depot_lng"]) != 77.0679 || num(t, r["vehicle_max_weight"]) != 1500 {
			t.Fatalf("route-plan body = %v", r)
		}
		first := ords[0].(map[string]any)
		if num(t, first["order_weight"]) != 2.5 || num(t, first["order_volume"]) != 0.1 || num(t, first["order_service_duration"]) != 120 || first["order_lat"] == nil {
			t.Fatalf("order body = %v", first)
		}
	}
	if !reflect.DeepEqual(sizes, []int{50, 50, 20}) || len(seen) != 120 {
		t.Fatalf("chunk sizes = %v distinct orders = %d", sizes, len(seen))
	}

	if num(t, sum["orders"]) != 120 || num(t, sum["assigned"]) != 120 || num(t, sum["unassigned"]) != 0 || num(t, sum["vehicles_used"]) != 3 ||
		num(t, sum["total_distance"]) != 3000 || num(t, sum["deferred_orders"]) != 0 || num(t, sum["planned_calls"]) != 3 || num(t, sum["calls_made"]) != 3 || num(t, sum["fetch_failures"]) != 0 || num(t, sum["chunks"]) != 3 {
		t.Fatalf("summary = %v", sum)
	}
	tb, err := addrkit.ReadTable(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tb.Header, []string{"chunk", "vehicle_id", "sequence", "order_id", "lat", "lng"}) {
		t.Fatalf("header = %v", tb.Header)
	}
	if len(tb.Rows) != 120 {
		t.Fatalf("stop rows = %d, want 120 (depot stops excluded)", len(tb.Rows))
	}
	if !reflect.DeepEqual(tb.Rows[0], []string{"1", "c1_vehicle_1", "1", "ORD001", "28.401", "77.001"}) {
		t.Fatalf("first stop = %v", tb.Rows[0])
	}
	if got := tb.Rows[119]; got[0] != "3" || got[1] != "c3_vehicle_1" || got[3] != "ORD120" || got[2] != "20" {
		t.Fatalf("last stop = %v", got)
	}
}

func TestFleetPlanPassesAddressesThroughToTheAPI(t *testing.T) {
	m := newDMMock(t)
	m.on(epRoutePlan, echoRoutePlan)
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", ordersCSV(3, true))
	stdout, _ := mustRun(t, "fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--db", filepath.Join(dir, "dm.db"), "--json")
	o := m.bodies(epRoutePlan)[0]["orders"].([]any)[0].(map[string]any)
	if o["order_address"] != "House 1, Sector 45, Gurgaon" || o["order_lat"] != nil {
		t.Fatalf("address order body = %v", o)
	}
	if m.count(epGeocode) != 0 {
		t.Fatal("fleet plan must leave geocoding to the route-plan API")
	}
	// Without --out the stops ride along in the JSON.
	sum := decodeObject(t, stdout)
	if stops, _ := sum["stops"].([]any); len(stops) != 3 {
		t.Fatalf("stops = %v", sum["stops"])
	}
}

func TestFleetPlanKeepsSuccessfulChunksWhenOneFails(t *testing.T) {
	m := newDMMock(t)
	m.on(epRoutePlan, func(n int, body map[string]any) (int, any) {
		if n == 2 {
			return http.StatusBadRequest, map[string]any{"error": "order outside service area"}
		}
		return echoRoutePlan(n, body)
	})
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", ordersCSV(120, false))
	out := filepath.Join(dir, "plan.csv")
	stdout, stderr, err := runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "2", "--depot", "28.4,77.0", "--out", out, "--db", filepath.Join(dir, "dm.db"), "--json")
	if err != nil {
		t.Fatalf("a partial failure must still exit 0: %v", err)
	}
	sum := decodeObject(t, stdout)
	if num(t, sum["fetch_failures"]) != 1 || num(t, sum["assigned"]) != 70 || num(t, sum["failed_orders"]) != 50 || num(t, sum["orders"]) != 120 || num(t, sum["total_distance"]) != 2000 || num(t, sum["vehicles_used"]) != 2 {
		t.Fatalf("summary = %v", sum)
	}
	failed := sum["failed_chunks"].([]any)
	if len(failed) != 1 || num(t, failed[0].(map[string]any)["chunk"]) != 2 || !strings.Contains(failed[0].(map[string]any)["error"].(string), "400") {
		t.Fatalf("failed_chunks = %v", failed)
	}
	if !strings.Contains(stderr, "1 of 3 chunk(s) failed") {
		t.Fatalf("stderr = %q", stderr)
	}
	tb, _ := addrkit.ReadTable(out)
	if len(tb.Rows) != 70 {
		t.Fatalf("stop rows = %d, want 70 (failed chunk excluded)", len(tb.Rows))
	}
	chunks := map[string]bool{}
	for _, r := range tb.Rows {
		chunks[r[0]] = true
	}
	if !reflect.DeepEqual(chunks, map[string]bool{"1": true, "3": true}) {
		t.Fatalf("chunks present = %v", chunks)
	}
}

func TestFleetPlanRetriesTransientErrorsAndStopsOnRateLimit(t *testing.T) {
	m := newDMMock(t)
	m.on(epRoutePlan, func(n int, body map[string]any) (int, any) {
		switch n {
		case 1:
			return http.StatusGatewayTimeout, map[string]any{"error": "timeout"}
		case 3:
			return http.StatusTooManyRequests, map[string]any{"error": "quota"}
		}
		return echoRoutePlan(n, body)
	})
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", ordersCSV(120, false))
	stdout, stderr, err := runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "2", "--depot", "28.4,77.0", "--out", filepath.Join(dir, "p.csv"), "--db", filepath.Join(dir, "dm.db"), "--json")
	if err != nil {
		t.Fatalf("429 after a successful chunk must exit 0: %v", err)
	}
	sum := decodeObject(t, stdout)
	// Call 1: 504 then retry succeeds (call 2) = chunk 1. Call 3: 429 on chunk 2.
	if m.count(epRoutePlan) != 3 || num(t, sum["assigned"]) != 50 || sum["rate_limited"] != true || num(t, sum["deferred_chunks"]) != 2 || num(t, sum["fetch_failures"]) != 0 {
		t.Fatalf("hits=%d summary=%v", m.count(epRoutePlan), sum)
	}
	if !strings.Contains(stderr, "429") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestFleetPlanAllChunksFailingIsAnError(t *testing.T) {
	m := newDMMock(t)
	m.on(epRoutePlan, func(int, map[string]any) (int, any) {
		return http.StatusTooManyRequests, map[string]any{"error": "quota"}
	})
	dir := t.TempDir()
	orders := writeFile(t, dir, "orders.csv", ordersCSV(5, false))
	_, _, err := runCLI(t, "fleet", "plan", "--orders", orders, "--vehicles", "1", "--depot", "28.4,77.0", "--db", filepath.Join(dir, "dm.db"), "--json")
	if err == nil || ExitCode(err) != 7 {
		t.Fatalf("want exit 7 when nothing could be planned, got %v", err)
	}
}

func TestFleetPlanUsageErrors(t *testing.T) {
	m := newDMMock(t)
	dir := t.TempDir()
	good := writeFile(t, dir, "good.csv", ordersCSV(2, false))
	noID := writeFile(t, dir, "noid.csv", "lat,lng\n28.4,77.0\n")
	dupe := writeFile(t, dir, "dupe.csv", "order_id,lat,lng\nA,28.4,77.0\nA,28.5,77.1\n")
	noLoc := writeFile(t, dir, "noloc.csv", "order_id,lat,lng\nA,,\n")
	cases := map[string][]string{
		"no orders":    {"fleet", "plan", "--vehicles", "2", "--depot", "28.4,77.0"},
		"no vehicles":  {"fleet", "plan", "--orders", good, "--depot", "28.4,77.0"},
		"bad depot":    {"fleet", "plan", "--orders", good, "--vehicles", "2", "--depot", "Gurgaon"},
		"bad mode":     {"fleet", "plan", "--orders", good, "--vehicles", "2", "--depot", "28.4,77.0", "--vehicle-mode", "boat"},
		"no order_id":  {"fleet", "plan", "--orders", noID, "--vehicles", "2", "--depot", "28.4,77.0"},
		"dupe id":      {"fleet", "plan", "--orders", dupe, "--vehicles", "2", "--depot", "28.4,77.0"},
		"no location":  {"fleet", "plan", "--orders", noLoc, "--vehicles", "2", "--depot", "28.4,77.0"},
		"negative cap": {"fleet", "plan", "--orders", good, "--vehicles", "2", "--depot", "28.4,77.0", "--max-weight", "-1"},
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
