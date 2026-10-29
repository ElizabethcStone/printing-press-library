// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/store"
)

// dmMock is an httptest stand-in for the Delhivery Maps gateway. It records
// every request body per path so tests can assert on what the CLI sent.
type dmMock struct {
	t    *testing.T
	srv  *httptest.Server
	mu   sync.Mutex
	hits map[string]int
	reqs map[string][]map[string]any
	// fn overrides the reply for a path: it gets the 1-based hit number and the
	// decoded body and returns an HTTP status plus a JSON-encodable payload.
	fn map[string]func(n int, body map[string]any) (int, any)
}

// newDMMock starts the mock and points the CLI at it. No test may reach a
// real host: the base URL and a fake token come from the environment.
func newDMMock(t *testing.T) *dmMock {
	t.Helper()
	m := &dmMock{t: t, hits: map[string]int{}, reqs: map[string][]map[string]any{}, fn: map[string]func(int, map[string]any) (int, any){}}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	t.Setenv("DELHIVERY_MAPS_BASE_URL", m.srv.URL)
	t.Setenv("DELHIVERY_MAPS_TOKEN", "test")
	t.Setenv("DELHIVERY_MAPS_DAILY_BUDGET", "")
	prev := dmBackoff
	dmBackoff = func(int) time.Duration { return 0 }
	t.Cleanup(func() { dmBackoff = prev })
	return m
}

func (m *dmMock) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	m.mu.Lock()
	m.hits[r.URL.Path]++
	n := m.hits[r.URL.Path]
	m.reqs[r.URL.Path] = append(m.reqs[r.URL.Path], body)
	fn := m.fn[r.URL.Path]
	m.mu.Unlock()
	if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
		m.t.Errorf("Authorization = %q, want a bearer token", got)
	}
	status, payload := http.StatusOK, any(nil)
	if fn != nil {
		status, payload = fn(n, body)
	} else {
		payload = defaultDMReply(r.URL.Path, body)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-ratelimit-limit", "50")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// defaultDMReply answers the three address endpoints deterministically from
// the address text, in the shapes captured from the live API.
func defaultDMReply(path string, body map[string]any) any {
	addr, _ := body["address"].(string)
	switch path {
	case epStandardize:
		return map[string]any{
			"address_components": map[string]any{
				"block_number": nil, "building_name": "Lotus Apartment", "building_type": "Apartment",
				"city": "Gurgaon", "country": nil, "district": nil, "is_rooftop_level": true, "landmark": nil,
				"locality": "Sector 45", "plot_no": nil, "postal_code": "122003", "property_type": "Residential",
				"state": "Haryana", "street_address": nil, "sub_locality": nil, "sub_unit": "Flat 4B",
			},
			"corrections":       []string{"Added state: Haryana"},
			"formatted_address": "STD " + strings.ToLower(addr),
			"req_id":            "t6", "request_id": "7ad232da-f80e-498d-9ce9-03f6914311ef",
		}
	case epValidate:
		return map[string]any{
			"corrections": "H-36 <1st floor|1st Floor>, Residency Greens", "formatted_address": "VAL " + addr,
			"granularity_level": "PREMISE", "quality": "ok", "reason": "valid",
			"req_id": "t3", "request_id": "cf67360f-8bb3-44c4-a665-ae471080bb3b",
		}
	case epGeocode:
		return map[string]any{
			"lat": 28.4 + float64(len(addr))*0.001, "lng": 77.0, "error_radius": 25,
			"metadata": map[string]any{"pincode": "122003"},
			"req_id":   "t1", "request_id": "3187fe91-24f5-4e8a-a0b6-ad39ea50d3e0",
		}
	}
	return map[string]any{}
}

// realRoute builds a /route reply shaped like the live API: the route sits
// under recommended_route, next to alternates, request_id and status.
func realRoute(distanceKM, durationSec float64, geometry any) map[string]any {
	leg := map[string]any{"summary": map[string]any{"has_ferry": false, "has_highway": true, "has_toll": true, "length": distanceKM, "time": durationSec}}
	return map[string]any{
		"alternates": []any{map[string]any{"distance": distanceKM + 5, "duration": durationSec + 600, "geometry": geometry, "legs": []any{leg}}},
		"recommended_route": map[string]any{
			"distance": distanceKM, "duration": durationSec, "geometry": geometry, "legs": []any{leg},
			"waypoints": []any{map[string]any{"lat": 28.5954, "lng": 77.0319, "snap_distance_m": 1.25}},
		},
		"request_id": "e2203f64-06a2-4f7e-95a2-04a17bd59933", "status": true,
	}
}

// realToll builds a /toll_charges_by_route reply shaped like the live API.
func realToll(total float64, plazas ...map[string]any) map[string]any {
	list := make([]any, 0, len(plazas))
	for _, p := range plazas {
		list = append(list, p)
	}
	return map[string]any{
		"data": map[string]any{
			"request_params": map[string]any{"vehicle_type": "bus_truck"},
			"routes": []any{map[string]any{
				"origin_state":   map[string]any{"state_code": "HR", "state_name": "Haryana"},
				"route_name":     "route",
				"states_entered": []any{map[string]any{"sequence_no": 1, "state_code": "RJ", "state_name": "Rajasthan"}},
				"summary":        map[string]any{"toll_plaza_count": len(plazas), "total_cost_inr": total, "total_toll_inr": total},
				"toll_plazas":    list,
			}},
		},
		"req_id": "829d1f2c-a8b6-49c6-82c3-58919c100a0d", "status": 200,
	}
}

func realPlaza(name, id string, rate float64) map[string]any {
	return map[string]any{"address": "", "city": "", "lat": 28.33, "lon": 77.06, "name": name, "pincode": "", "state": "", "toll_id": id, "toll_rate_inr": rate, "toll_type": "flat"}
}

func (m *dmMock) count(path string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hits[path]
}

func (m *dmMock) total() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.hits {
		n += c
	}
	return n
}

func (m *dmMock) bodies(path string) []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]any(nil), m.reqs[path]...)
}

func (m *dmMock) on(path string, fn func(n int, body map[string]any) (int, any)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fn[path] = fn
}

// runCLI executes the root command with pacing disabled and learning off,
// returning stdout, stderr and the command error.
func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := RootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append(args, "--rate-limit", "0", "--no-learn"))
	err := cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func mustRun(t *testing.T, args ...string) (string, string) {
	t.Helper()
	out, errOut, err := runCLI(t, args...)
	if err != nil {
		t.Fatalf("%s failed: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, out, errOut)
	}
	return out, errOut
}

func decodeObject(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s", err, s)
	}
	return v
}

func decodeArray(t *testing.T, s string) []any {
	t.Helper()
	var v []any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, s)
	}
	return v
}

func num(t *testing.T, v any) float64 {
	t.Helper()
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("expected a JSON number, got %T (%v)", v, v)
	}
	return f
}

func tempDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "dm.db")
}

// execDB runs a statement against the database a command used.
func execDB(t *testing.T, dbPath, query string, args ...any) {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := store.EnsureDelhiveryMapsTables(context.Background(), s.DB()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func countRows(t *testing.T, dbPath, table string) int {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	if err := s.DB().QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// runCLIStdin is runCLI with standard input.
func runCLIStdin(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := RootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(append(args, "--rate-limit", "0", "--no-learn"))
	err := cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}
