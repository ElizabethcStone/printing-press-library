package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
)

func exportFixtureRows(resource string, start, count int) []any {
	rows := []any{}
	for i := start; i < start+count; i++ {
		key := "id"
		if resource == "bookings-api" {
			key = "booking_id"
		}
		if resource == "appointments-api" {
			key = "ID"
		}
		if resource == "records-api" {
			rows = append(rows, map[string]any{"choose": map[string]any{"value": i + 1}})
		} else {
			rows = append(rows, map[string]any{key: i + 1})
		}
	}
	return rows
}
func TestCrocoExportCollectionsPagination(t *testing.T) {
	for _, resource := range []string{"forms-api", "records-api", "bookings-api", "appointments-api"} {
		for _, total := range []int{201, 200} {
			t.Run(fmt.Sprintf("%s/%d", resource, total), func(t *testing.T) {
				calls := 0
				c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != "GET" || r.URL.Path != resourceReadPaths[resource] {
						t.Errorf("wrong request %s %s", r.Method, r.URL.Path)
					}
					q := r.URL.Query()
					cfg := resourceReadConfigFor(resource)
					if q.Get(cfg.limitParam) != "100" {
						t.Errorf("wrong size: %v", q)
					}
					start := 0
					if cfg.paginationType == "page" {
						page, _ := strconv.Atoi(q.Get("page"))
						start = (page - 1) * 100
					} else {
						start, _ = strconv.Atoi(q.Get("offset"))
						if q.Get("mode") != "all" {
							t.Error("mode missing")
						}
					}
					if resource == "records-api" && q.Get("sort") != "ASC" {
						t.Error("record sort missing")
					}
					if resource == "forms-api" && (q.Get("orderby") != "id" || q.Get("order") != "asc") {
						t.Error("forms stable sort missing")
					}
					if resource == "forms-api" && start >= total {
						w.WriteHeader(400)
						fmt.Fprint(w, `{"code":"rest_post_invalid_page_number"}`)
						return
					}
					n := min(100, total-start)
					rows := exportFixtureRows(resource, start, n)
					if resource == "forms-api" {
						json.NewEncoder(w).Encode(rows)
					} else {
						json.NewEncoder(w).Encode(map[string]any{cfg.responsePath: rows, "total": total, "success": true})
					}
				})
				got := []json.RawMessage{}
				count, err := crocoExportCollection(context.Background(), c, resource, 0, func(row json.RawMessage) error { got = append(got, row); return nil })
				wantCalls := 3
				if total == 200 && resource != "forms-api" {
					wantCalls = 2
				}
				if err != nil || count != total || len(got) != total || calls != wantCalls {
					t.Fatalf("count=%d rows=%d calls=%d error=%v", count, len(got), calls, err)
				}
			})
		}
	}
}
func TestCrocoExportCollectionsFailClosed(t *testing.T) {
	for _, tc := range []struct{ name, resource, body string }{
		{"forms envelope", "forms-api", `{"data":[{"id":1}]}`},
		{"forms null", "forms-api", `null`},
		{"forms scalar row", "forms-api", `[7]`},
		{"records missing list", "records-api", `{"total":0}`},
		{"records null list", "records-api", `{"list":null,"total":0}`},
		{"records missing total", "records-api", `{"list":[]}`},
		{"records malformed total", "records-api", `{"list":[],"total":1.5}`},
		{"records incomplete", "records-api", `{"list":[{"id":1}],"total":101}`},
		{"bookings success missing", "bookings-api", `{"data":[],"total":0}`},
		{"appointments missing data", "appointments-api", `{"success":true,"total":0}`},
		{"bookings empty incomplete", "bookings-api", `{"success":true,"data":[],"total":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
			_, err := crocoExportCollection(context.Background(), c, tc.resource, 0, func(json.RawMessage) error { return nil })
			if err == nil {
				t.Fatal("accepted incomplete/malformed backup")
			}
		})
	}
}
func TestCrocoExportOffsetShortPagesAndLimits(t *testing.T) {
	for _, limit := range []int{0, 3} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			calls := 0
			c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				start, _ := strconv.Atoi(r.URL.Query().Get("offset"))
				json.NewEncoder(w).Encode(map[string]any{"success": true, "total": 5, "data": exportFixtureRows("bookings-api", start, min(2, 5-start))})
			})
			count, err := crocoExportCollection(context.Background(), c, "bookings-api", limit, func(json.RawMessage) error { return nil })
			want, wantCalls := 5, 3
			if limit > 0 {
				want, wantCalls = 3, 2
			}
			if err != nil || count != want || calls != wantCalls {
				t.Fatal(count, calls, err)
			}
		})
	}
}
func TestCrocoExportRejectChangedTotalRepeatedRowsAndUnexpectedWPError(t *testing.T) {
	for _, scenario := range []string{"changed", "repeated", "wp-error", "wp-first-invalid"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			resource := "bookings-api"
			if scenario == "wp-error" || scenario == "wp-first-invalid" {
				resource = "forms-api"
			}
			c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if scenario == "wp-first-invalid" || (scenario == "wp-error" && calls == 2) {
					w.WriteHeader(400)
					code := "unrelated_error"
					if scenario == "wp-first-invalid" {
						code = "rest_post_invalid_page_number"
					}
					fmt.Fprintf(w, `{"code":%q}`, code)
					return
				}
				rows := exportFixtureRows(resource, 0, 100)
				if resource == "forms-api" {
					json.NewEncoder(w).Encode(rows)
					return
				}
				total := 200
				if scenario == "changed" && calls == 2 {
					total = 201
				}
				json.NewEncoder(w).Encode(map[string]any{"success": true, "total": total, "data": rows})
			})
			_, err := crocoExportCollection(context.Background(), c, resource, 0, func(json.RawMessage) error { return nil })
			if err == nil {
				t.Fatal("accepted inconsistent collection")
			}
		})
	}
}
func TestCrocoExportPageLimitKeepsStride(t *testing.T) {
	calls := 0
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("limit") != "100" || r.URL.Query().Get("page") != strconv.Itoa(calls) {
			t.Error(r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(map[string]any{"list": exportFixtureRows("records-api", (calls-1)*100, 100), "total": 300})
	})
	count, err := crocoExportCollection(context.Background(), c, "records-api", 101, func(json.RawMessage) error { return nil })
	if err != nil || count != 101 || calls != 2 {
		t.Fatal(count, calls, err)
	}
}
