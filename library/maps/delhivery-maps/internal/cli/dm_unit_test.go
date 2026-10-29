// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/store"
)

func TestParseCleanSteps(t *testing.T) {
	tests := []struct {
		in      string
		want    []string
		wantErr string
	}{
		{"standardize,validate,geocode", []string{"standardize", "validate", "geocode"}, ""},
		{"geocode,standardize", []string{"standardize", "geocode"}, ""}, // execution order is fixed
		{" Validate , validate ", []string{"validate"}, ""},
		{"standardise", []string{"standardize"}, ""},
		{"standarize,geocode", []string{"standardize", "geocode"}, ""},
		{"", nil, "no steps"},
		{",,", nil, "no steps"},
		{"standardize,bogus", nil, `unknown step "bogus"`},
	}
	for _, tt := range tests {
		got, err := parseCleanSteps(tt.in)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("parseCleanSteps(%q) err = %v, want %q", tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseCleanSteps(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
}

func TestValidDepartureTime(t *testing.T) {
	tests := map[string]bool{
		"2026-11-03T09:30:00+05:30": true,
		"2026-11-03T09:30:00Z":      true,
		"2026-11-03T09:30:00":       true,
		"2026-11-03T09:30":          true,
		"2026-11-03 09:30:00":       true,
		"2026-11-03":                true,
		"":                          false,
		"tomorrow":                  false,
		"2026-13-45":                false,
		"09:30":                     false,
		"1762137000":                false,
	}
	for in, want := range tests {
		if got := validDepartureTime(in); got != want {
			t.Errorf("validDepartureTime(%q) = %v, want %v", in, got, want)
		}
	}
}

// realGeometry is the encoded recommended_route geometry captured from the
// live API (precision 6, 26 points).
const realGeometry = "}kipu@c|s|qCzNrL|NWrDUxFWb@u@l_@un@be@aw@`Uk_@jDwKn@wMm@_JuCkIc{@st@yBgBei@oc@sEzAkF@{FuByD{Ee@mBq@oC@}Gj@sDsLwJuHiG"

func TestParseRouteGeometry(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantLen   int
		wantFirst [2]float64
		wantErr   bool
	}{
		{"encoded polyline from the live API", `"` + realGeometry + `"`, 26, [2]float64{28.595407, 77.03189}, false},
		{"plain pairs", `[[28.5,77.0],[28.6,77.1],[28.7,77.2]]`, 3, [2]float64{28.5, 77.0}, false},
		{"triples keep lat and lng", `[[28.5,77.0,210],[28.6,77.1,211]]`, 2, [2]float64{28.5, 77.0}, false},
		{"null", `null`, 0, [2]float64{}, true},
		{"missing", ``, 0, [2]float64{}, true},
		{"point with one value", `[[28.5]]`, 0, [2]float64{}, true},
		{"latitude out of range", `[[128.5,77.0]]`, 0, [2]float64{}, true},
		{"not geometry", `{"a":1}`, 0, [2]float64{}, true},
	}
	for _, tt := range tests {
		got, err := parseRouteGeometry(json.RawMessage(tt.raw))
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tt.name, err, tt.wantErr)
			continue
		}
		if tt.wantErr {
			continue
		}
		if len(got) != tt.wantLen || got[0] != tt.wantFirst {
			t.Errorf("%s: got %d points starting %v, want %d starting %v", tt.name, len(got), got[0], tt.wantLen, tt.wantFirst)
		}
	}
}

func TestDecodeRoute(t *testing.T) {
	nested := `{"alternates":[{"distance":9,"duration":9,"geometry":[[1,1],[2,2]]}],"recommended_route":{"distance":0.825,"duration":99.04,"geometry":"` + realGeometry + `","legs":[]},"request_id":"x","status":true}`
	flat := `{"distance":12.5,"duration":600,"geometry":[[28.5,77.0],[28.6,77.1]]}`
	tests := []struct {
		name    string
		raw     string
		wantKM  float64
		wantSec float64
		wantLen int
		wantErr bool
	}{
		{"live nested form uses recommended_route, not alternates", nested, 0.825, 99.04, 26, false},
		{"flat form is a fallback", flat, 12.5, 600, 2, false},
		{"only alternates", `{"alternates":[{"distance":9,"duration":9,"geometry":[[1,1],[2,2]]}],"status":true}`, 0, 0, 0, true},
		{"single point", `{"recommended_route":{"distance":1,"duration":1,"geometry":[[1,1]]}}`, 0, 0, 0, true},
		{"not json", `<html>`, 0, 0, 0, true},
	}
	for _, tt := range tests {
		got, err := decodeRoute(json.RawMessage(tt.raw))
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tt.name, err, tt.wantErr)
			continue
		}
		if tt.wantErr {
			if !errors.Is(err, errDMUnusableResponse) {
				t.Errorf("%s: error should wrap errDMUnusableResponse: %v", tt.name, err)
			}
			continue
		}
		if got.DistanceKM != tt.wantKM || got.DurationSec != tt.wantSec || len(got.Geometry) != tt.wantLen {
			t.Errorf("%s: got %v km %v s %d points", tt.name, got.DistanceKM, got.DurationSec, len(got.Geometry))
		}
	}
}

func TestDecodeToll(t *testing.T) {
	live := `{"data":{"request_params":{"vehicle_type":"bus_truck"},"routes":[{"summary":{"toll_plaza_count":2,"total_cost_inr":755.0,"total_toll_inr":755.0},"toll_plazas":[{"name":"Ghamroj","toll_id":"dlv_00652","toll_rate_inr":440.0},{"name":"Hilalpur","toll_id":"dlv_02397","toll_rate_inr":315.0}]}]},"req_id":"x","status":200}`
	got, err := decodeToll(json.RawMessage(live))
	if err != nil || got.Count != 2 || got.Total != 755 || len(got.Plazas) != 2 || got.Plazas[0]["name"] != "Ghamroj" {
		t.Fatalf("live toll = %+v, %v", got, err)
	}
	costOnly := `{"data":{"routes":[{"summary":{"toll_plaza_count":1,"total_cost_inr":120.5}}]}}`
	got, err = decodeToll(json.RawMessage(costOnly))
	if err != nil || got.Total != 120.5 || got.Plazas == nil || len(got.Plazas) != 0 {
		t.Fatalf("cost-only toll = %+v, %v (plazas must be an empty list, not null)", got, err)
	}
	for _, bad := range []string{`{"data":{"routes":[]}}`, `{}`, `nope`} {
		if _, err := decodeToll(json.RawMessage(bad)); !errors.Is(err, errDMUnusableResponse) {
			t.Errorf("decodeToll(%q) err = %v, want unusable", bad, err)
		}
	}
}

func TestParseIsochrone(t *testing.T) {
	square := `[[[77.0,28.4],[77.2,28.4],[77.2,28.6],[77.0,28.6],[77.0,28.4]]]`
	tests := []struct {
		name     string
		raw      string
		inLngLat [2]float64
		inside   bool
		wantErr  bool
	}{
		{"polygon", `{"features":[{"geometry":{"type":"Polygon","coordinates":` + square + `}}]}`, [2]float64{77.1, 28.5}, true, false},
		{"polygon outside", `{"features":[{"geometry":{"type":"Polygon","coordinates":` + square + `}}]}`, [2]float64{77.5, 28.5}, false, false},
		{"polygon with a hole", `{"features":[{"geometry":{"type":"Polygon","coordinates":[[[77.0,28.4],[77.2,28.4],[77.2,28.6],[77.0,28.6],[77.0,28.4]],[[77.08,28.48],[77.12,28.48],[77.12,28.52],[77.08,28.52],[77.08,28.48]]]}}]}`, [2]float64{77.1, 28.5}, false, false},
		{"multipolygon second part", `{"features":[{"geometry":{"type":"MultiPolygon","coordinates":[` + square + `,[[[80,10],[81,10],[81,11],[80,11],[80,10]]]]}}]}`, [2]float64{80.5, 10.5}, true, false},
		{"two features", `{"features":[{"geometry":{"type":"Polygon","coordinates":` + square + `}},{"geometry":{"type":"Polygon","coordinates":[[[80,10],[81,10],[81,11],[80,11],[80,10]]]}}]}`, [2]float64{80.5, 10.5}, true, false},
		{"no polygon", `{"features":[{"geometry":{"type":"Point","coordinates":[77,28]}}]}`, [2]float64{}, false, true},
		{"no features", `{"features":[]}`, [2]float64{}, false, true},
		{"bad coordinates", `{"features":[{"geometry":{"type":"Polygon","coordinates":"x"}}]}`, [2]float64{}, false, true},
		{"not json", `x`, [2]float64{}, false, true},
	}
	for _, tt := range tests {
		area, err := parseIsochrone(json.RawMessage(tt.raw))
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tt.name, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && area.Contains(tt.inLngLat[0], tt.inLngLat[1]) != tt.inside {
			t.Errorf("%s: Contains(%v) = %v, want %v", tt.name, tt.inLngLat, !tt.inside, tt.inside)
		}
	}
}

func TestParseFleetOrders(t *testing.T) {
	tests := []struct {
		name    string
		csv     string
		wantErr string
		check   func(t *testing.T, got []fleetOrder)
	}{
		{"coordinates and numbers", "order_id,lat,lng,weight,volume,service_duration\nA,28.4,77.0,2.5,0.1,120\n", "", func(t *testing.T, got []fleetOrder) {
			b := got[0].body
			if b["order_lat"] != 28.4 || b["order_lng"] != 77.0 || b["order_weight"] != 2.5 || b["order_volume"] != 0.1 || b["order_service_duration"] != 120.0 || b["order_address"] != nil {
				t.Errorf("body = %v", b)
			}
		}},
		{"address fallback when coordinates are blank", "order_id,address,lat,lng\nA,\"House 1, Gurgaon\",,\nB,House 2,28.5,77.1\n", "", func(t *testing.T, got []fleetOrder) {
			if got[0].body["order_address"] != "House 1, Gurgaon" || got[0].body["order_lat"] != nil {
				t.Errorf("first = %v", got[0].body)
			}
			if got[1].body["order_lat"] != 28.5 || got[1].body["order_address"] != nil {
				t.Errorf("coordinates must win over the address: %v", got[1].body)
			}
		}},
		{"duplicate id", "order_id,lat,lng\nA,28.4,77.0\nA,28.5,77.1\n", `repeats order_id "A"`, nil},
		{"empty id", "order_id,lat,lng\n,28.4,77.0\n", "empty order_id", nil},
		{"no order_id column", "id,lat,lng\nA,28.4,77.0\n", "needs an order_id column", nil},
		{"missing coordinates and address", "order_id,lat,lng\nA,,\n", "neither valid lat/lng nor an address", nil},
		{"out of range coordinates and no address", "order_id,lat,lng\nA,95,77\n", "neither valid lat/lng nor an address", nil},
		{"no location columns at all", "order_id,weight\nA,1\n", "needs lat and lng columns or an address column", nil},
		{"negative weight", "order_id,lat,lng,weight\nA,28.4,77.0,-1\n", "order_weight", nil},
		{"NaN weight", "order_id,lat,lng,weight\nA,28.4,77.0,NaN\n", "order_weight", nil},
		{"Inf volume", "order_id,lat,lng,volume\nA,28.4,77.0,Inf\n", "order_volume", nil},
		{"hex service duration", "order_id,lat,lng,service_duration\nA,28.4,77.0,0x10\n", "order_service_duration", nil},
		{"NaN latitude falls through to an error", "order_id,lat,lng\nA,NaN,77.0\n", "neither valid lat/lng nor an address", nil},
	}
	for _, tt := range tests {
		tb, err := addrkit.ParseCSV(strings.NewReader(tt.csv))
		if err != nil {
			t.Fatalf("%s: fixture: %v", tt.name, err)
		}
		got, err := parseFleetOrders(tb)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: err = %v, want %q", tt.name, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		tt.check(t, got)
	}
}

func TestMergeFleetChunk(t *testing.T) {
	orders := []fleetOrder{{id: "A", lat: ptr(28.4), lng: ptr(77.0)}, {id: "B", lat: ptr(28.5), lng: ptr(77.1)}, {id: "C"}}
	live := `{"plan_status":true,"plan_summary":{"assigned_orders":3,"total_distance":23.854,"total_duration":3689,"total_orders":3,"unassigned_orders":0,"vehicles_used":1},
	"routes_info":[{"num_stops":3,"orders":["A","C","B"],"route":[{"lat":28.46,"lng":77.06,"type":"depot"},
	{"lat":28.48,"lng":77.09,"order_id":"A","sequence":1,"type":"order"},{"lat":28.49,"lng":77.09,"order_id":"C","sequence":2,"type":"order"},{"lat":28.5,"lng":77.1,"order_id":"B","sequence":3,"type":"order"},{"lat":28.46,"lng":77.06,"type":"depot"}],
	"total_distance":23.854,"total_duration":3689,"vehicle_id":"vehicle_1"}],"unassigned_orders":[]}`
	t.Run("live shape with a boolean plan_status", func(t *testing.T) {
		got, err := mergeFleetChunk(2, orders, json.RawMessage(live))
		if err != nil {
			t.Fatal(err)
		}
		if got.planStatus != "true" || got.assigned != 3 || got.vehiclesUsed != 1 || got.distance != 23.854 || got.duration != 3689 || len(got.unassigned) != 0 {
			t.Errorf("result = %+v", got)
		}
		ids := []string{}
		for _, s := range got.stops {
			if s.Chunk != 2 || s.VehicleID != "c2_vehicle_1" {
				t.Errorf("stop = %+v", s)
			}
			ids = append(ids, s.OrderID+"@"+s.Sequence)
		}
		if !reflect.DeepEqual(ids, []string{"A@1", "C@2", "B@3"}) {
			t.Errorf("stops = %v (depot stops must be excluded)", ids)
		}
	})
	t.Run("string plan_status is still accepted", func(t *testing.T) {
		got, err := mergeFleetChunk(1, orders, json.RawMessage(strings.Replace(live, `"plan_status":true`, `"plan_status":"optimal"`, 1)))
		if err != nil || got.planStatus != "optimal" {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("no per-stop route falls back to the order list", func(t *testing.T) {
		raw := `{"plan_status":true,"routes_info":[{"vehicle_id":7,"orders":["B","A"],"total_distance":5,"total_duration":60}]}`
		got, err := mergeFleetChunk(1, orders, json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		if len(got.stops) != 2 || got.stops[0].OrderID != "B" || got.stops[0].Sequence != "1" || got.stops[0].VehicleID != "c1_7" || got.stops[0].Lat == nil || *got.stops[0].Lat != 28.5 {
			t.Errorf("stops = %+v", got.stops)
		}
	})
	t.Run("unassigned orders as strings, numbers and objects", func(t *testing.T) {
		raw := `{"plan_status":true,"routes_info":[],"unassigned_orders":["A",12,{"order_id":"C","reason":"too heavy"}],"plan_summary":{"total_distance":0,"total_duration":0}}`
		got, err := mergeFleetChunk(1, orders, json.RawMessage(raw))
		if err != nil || !reflect.DeepEqual(got.unassigned, []string{"A", "12", "C", "B"}) || got.assigned != 0 || got.vehiclesUsed != 0 {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("a reply with neither routes nor unassigned orders is an error", func(t *testing.T) {
		if _, err := mergeFleetChunk(1, orders, json.RawMessage(`{"plan_status":false}`)); !errors.Is(err, errDMUnusableResponse) {
			t.Errorf("err = %v", err)
		}
		if _, err := mergeFleetChunk(1, orders, json.RawMessage(`not json`)); !errors.Is(err, errDMUnusableResponse) {
			t.Errorf("err = %v", err)
		}
	})
}

func ptr(f float64) *float64 { return &f }

func TestDMStrUnmarshal(t *testing.T) {
	tests := []struct {
		in   string
		want string
		err  bool
	}{
		{`"122003"`, "122003", false},
		{`" 122003 "`, "122003", false},
		{`122003`, "122003", false},
		{`12.5`, "12.5", false},
		{`null`, "", false},
		{`""`, "", false},
		{`true`, "", true},
		{`{"a":1}`, "", true},
	}
	for _, tt := range tests {
		var got dmStr
		err := json.Unmarshal([]byte(tt.in), &got)
		if (err != nil) != tt.err || string(got) != tt.want {
			t.Errorf("Unmarshal(%s) = %q, %v; want %q (err %v)", tt.in, got, err, tt.want, tt.err)
		}
	}
	var wrapper struct {
		P dmStr `json:"p"`
	}
	if err := json.Unmarshal([]byte(`{}`), &wrapper); err != nil || wrapper.P != "" {
		t.Errorf("absent field = %q, %v", wrapper.P, err)
	}
}

func TestDMDailyBudget(t *testing.T) {
	tests := []struct {
		env  string
		want int
	}{
		{"", dmDefaultDailyBudget},
		{"7", 7},
		{" 12 ", 12},
		{"0", 0},
		{"-1", dmDefaultDailyBudget},
		{"abc", dmDefaultDailyBudget},
		{"3.5", dmDefaultDailyBudget},
		{"${user_config.budget}", dmDefaultDailyBudget}, // an unresolved placeholder is not a value
	}
	for _, tt := range tests {
		t.Setenv(dmBudgetEnv, tt.env)
		if got := dmDailyBudget(); got != tt.want {
			t.Errorf("budget env %q = %d, want %d", tt.env, got, tt.want)
		}
	}
}

func TestDMCheckBaseURL(t *testing.T) {
	tests := []struct {
		url string
		ok  bool
	}{
		{"https://gateway-maps-pub-int.delhivery.com", true},
		{"https://gateway-maps-pub-int.delhivery.com/", true},
		{"HTTPS://Gateway-Maps-Pub-Int.Delhivery.COM", true},
		// Other delhivery.com hosts are not the maps gateway and get no token.
		{"https://delhivery.com", false},
		{"https://api.staging.delhivery.com/v1", false},
		{"https://track.delhivery.com", false},
		{"http://127.0.0.1:8080", true},
		{"http://localhost:9000", true},
		{"http://[::1]:9000", true},
		{"http://gateway-maps-pub-int.delhivery.com", false}, // plain http to a real host
		{"https://delhivery.com.evil.example", false},
		{"https://evildelhivery.com", false},
		{"https://delhivery.com@evil.example", false},
		{"https://evil.example/delhivery.com", false},
		{"https://10.0.0.5", false},
		{"ftp://127.0.0.1", false},
		{"not a url", false},
		{"", false},
	}
	for _, tt := range tests {
		err := dmCheckBaseURL(tt.url)
		if (err == nil) != tt.ok {
			t.Errorf("dmCheckBaseURL(%q) = %v, want ok=%v", tt.url, err, tt.ok)
		}
	}
}

func TestDMStripControl(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"line one\nline two\ttabbed", "line one line two tabbed"},
		{"a\x1b[31mred\x1b[0mb", "aredb"},
		{"a\x1b]0;window title\x07b", "ab"},
		{"a\x1b]8;;http://evil\x1b\\link\x1b]8;;\x1b\\b", "alinkb"},
		{"bell\x07 and nul\x00 gone", "bell and nul gone"},
		{"नमस्ते Gurgaon", "नमस्ते Gurgaon"},
	}
	for _, tt := range tests {
		if got := dmStripControl(tt.in); got != tt.want {
			t.Errorf("dmStripControl(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestStandardizePostalCodeFieldNames(t *testing.T) {
	// The live API names the field postal_code; the spec's "pincode" is only a fallback.
	tests := []struct{ name, body, want string }{
		{"live postal_code", `{"address_components":{"postal_code":"122003"},"formatted_address":"x"}`, "122003"},
		{"postal_code wins over pincode", `{"address_components":{"postal_code":"122003","pincode":"999999"},"formatted_address":"x"}`, "122003"},
		{"pincode fallback", `{"address_components":{"pincode":"302001"},"formatted_address":"x"}`, "302001"},
		{"numeric postal_code", `{"address_components":{"postal_code":122003},"formatted_address":"x"}`, "122003"},
		{"neither", `{"address_components":{},"formatted_address":"x"}`, ""},
	}
	for _, tt := range tests {
		got, err := decodeStandardize(json.RawMessage(tt.body))
		if err != nil || got.postalCode() != tt.want {
			t.Errorf("%s: postalCode = %q, %v; want %q", tt.name, got.postalCode(), err, tt.want)
		}
	}
	if _, err := decodeStandardize(json.RawMessage(`{"address_components":{}}`)); !errors.Is(err, errDMUnusableResponse) {
		t.Errorf("a reply without formatted_address must be rejected, got %v", err)
	}
}

func TestDecodeGeocodeAndValidateRejectUnusableReplies(t *testing.T) {
	if _, err := decodeGeocode(json.RawMessage(`{"error":"not found"}`)); !errors.Is(err, errDMUnusableResponse) {
		t.Errorf("geocode without coordinates: %v", err)
	}
	if g, err := decodeGeocode(json.RawMessage(`{"lat":28.443114,"lng":77.06365650000001,"error_radius":368,"metadata":{"pincode":"122003"}}`)); err != nil || string(g.Metadata.Pincode) != "122003" {
		t.Errorf("live geocode: %+v %v", g, err)
	}
	if _, err := decodeValidate(json.RawMessage(`{"formatted_address":"x"}`)); !errors.Is(err, errDMUnusableResponse) {
		t.Errorf("validate without quality: %v", err)
	}
}

func newTestDB(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := store.EnsureDelhiveryMapsTables(context.Background(), s.DB()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCachedCallOptValidatesBeforeCachingAndHonoursTTLAndScope(t *testing.T) {
	ctx := context.Background()
	s := newTestDB(t)
	db := s.DB()
	calls := 0
	fetch := func(reply string) func(context.Context) (json.RawMessage, error) {
		return func(context.Context) (json.RawMessage, error) { calls++; return json.RawMessage(reply), nil }
	}
	reject := func(json.RawMessage) error { return errors.New("rejected") }
	body := map[string]any{"address": "Flat 4B"}

	// A reply the validator rejects is returned as an error and not stored.
	if _, _, err := cachedCallOpt(ctx, db, epGeocode, body, dmCacheOpts{Scope: "s1", Validate: reject}, fetch(`{"x":1}`)); err == nil {
		t.Fatal("expected the validation error")
	}
	if n := countTable(t, db, "dm_cache"); n != 0 {
		t.Fatalf("rejected reply was cached (%d rows)", n)
	}

	// A good reply is stored, then served from the cache.
	if _, hit, err := cachedCallOpt(ctx, db, epGeocode, body, dmCacheOpts{Scope: "s1", TTL: time.Hour}, fetch(`{"ok":1}`)); err != nil || hit {
		t.Fatalf("first call hit=%v err=%v", hit, err)
	}
	if _, hit, _ := cachedCallOpt(ctx, db, epGeocode, body, dmCacheOpts{Scope: "s1", TTL: time.Hour}, fetch(`{"ok":2}`)); !hit || calls != 2 {
		t.Fatalf("second call should hit the cache: hit=%v calls=%d", hit, calls)
	}
	// Case and spacing of the address do not change the key.
	if _, hit, _ := cachedCallOpt(ctx, db, epGeocode, map[string]any{"address": "flat   4b"}, dmCacheOpts{Scope: "s1", TTL: time.Hour}, fetch(`{"ok":3}`)); !hit {
		t.Fatal("normalized request should share the entry")
	}
	// A different base URL / token fingerprint is a different entry.
	if _, hit, _ := cachedCallOpt(ctx, db, epGeocode, body, dmCacheOpts{Scope: "s2", TTL: time.Hour}, fetch(`{"ok":4}`)); hit {
		t.Fatal("a different scope must not share cache entries")
	}
	// SkipRead (--no-cache) refetches.
	if _, hit, _ := cachedCallOpt(ctx, db, epGeocode, body, dmCacheOpts{Scope: "s1", SkipRead: true}, fetch(`{"ok":5}`)); hit {
		t.Fatal("SkipRead must refetch")
	}

	// Entries older than the TTL are ignored; with no TTL they never expire.
	if _, err := db.Exec(`UPDATE dm_cache SET fetched_at = ?`, time.Now().Add(-25*time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := cachedCallOpt(ctx, db, epRoute, body, dmCacheOpts{Scope: "s1", TTL: dmRouteCacheTTL}, fetch(`{"ok":6}`)); hit {
		t.Fatal("a different endpoint must not hit")
	}
	if _, hit, _ := cachedCallOpt(ctx, db, epGeocode, body, dmCacheOpts{Scope: "s1", TTL: dmRouteCacheTTL}, fetch(`{"ok":7}`)); hit {
		t.Fatal("a 25h-old entry must be expired for a 24h TTL")
	}
	if _, err := db.Exec(`UPDATE dm_cache SET fetched_at = ?`, time.Now().Add(-25*time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := cachedCallOpt(ctx, db, epGeocode, body, dmCacheOpts{Scope: "s1", TTL: dmAddressCacheTTL}, fetch(`{"ok":8}`)); !hit {
		t.Fatal("a 25h-old entry is fresh for a 30-day TTL")
	}
	if dmCacheTTLs[epGeocode] != 30*24*time.Hour || dmCacheTTLs[epToll] != 24*time.Hour || dmCacheTTLs[epRoute] != 24*time.Hour || dmCacheTTLs[epIsochrone] != 24*time.Hour {
		t.Fatalf("TTL table = %v", dmCacheTTLs)
	}
	if _, ok := dmCacheTTLs[epRoutePlan]; ok {
		t.Fatal("route-plan must not be cacheable")
	}
}

func countTable(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
