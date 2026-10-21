package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
)

func crocoOverlapTestPolicy() crocoOverlapPolicy {
	return crocoOverlapPolicy{SchemaVersion: 1, Timezone: "America/Monterrey", Bookings: crocoOverlapModelPolicy{IncludedStatuses: []string{"pending"}, Boundary: "half-open", Capacity: 1, GroupBy: []string{"apartment_id", "apartment_unit"}, DateEncoding: "utc_calendar_date", CheckInAdjustment: "subtract_one_second"}, Appointments: crocoOverlapModelPolicy{IncludedStatuses: []string{"pending"}, Boundary: "half-open", Capacity: 1, GroupBy: []string{"service", "provider"}}}
}
func crocoOverlapTestCapture(resource string) store.CrocoCapture {
	return store.CrocoCapture{Site: "https://fixture.invalid/wp-json", Resource: resource, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano), Complete: true, Fields: []string{"id", "resource", "status", "interval"}, Versions: map[string]string{"jet-booking": "4.1.3.1", "jet-appointments-booking": "2.5.3"}, Scope: map[string]any{"all_statuses": true}, Note: "Synthetic acceptance fixture; not a real reservation"}
}
func crocoOverlapTestAppointment(id, start, end int64) map[string]any {
	return map[string]any{"ID": id, "status": "pending", "service": 13, "provider": 0, "type": "slot", "slot_timestamp": start, "slot_end_timestamp": end}
}
func crocoOverlapTestBooking(id int64, start, end string) map[string]any {
	a, _ := time.Parse("2006-01-02", start)
	b, _ := time.Parse("2006-01-02", end)
	return map[string]any{"booking_id": id, "status": "pending", "apartment_id": 13, "apartment_unit": 0, "check_in_date_timestamp": a.Unix() + 1, "check_out_date_timestamp": b.Unix()}
}
func TestCrocoOverlapSweepPolicies(t *testing.T) {
	for _, tc := range []struct {
		name, boundary string
		capacity       int
		spans          [][2]int64
		count          int
		point          bool
	}{
		{"touch-half-open", "half-open", 1, [][2]int64{{10, 20}, {20, 30}}, 0, false},
		{"touch-closed", "closed", 1, [][2]int64{{10, 20}, {20, 30}}, 1, true},
		{"overlap", "half-open", 1, [][2]int64{{10, 25}, {20, 30}}, 1, false},
		{"nested", "half-open", 1, [][2]int64{{10, 40}, {20, 30}}, 1, false},
		{"equal", "half-open", 1, [][2]int64{{10, 30}, {10, 30}}, 1, false},
		{"capacity-two-allowed", "half-open", 2, [][2]int64{{10, 40}, {20, 30}}, 0, false},
		{"capacity-two-exceeded", "half-open", 2, [][2]int64{{10, 40}, {20, 30}, {25, 35}}, 1, false},
		{"closed-triple-point", "closed", 2, [][2]int64{{10, 40}, {10, 20}, {20, 30}}, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := crocoOverlapTestPolicy().Appointments
			p.Boundary = tc.boundary
			p.Capacity = tc.capacity
			intervals := []crocoOverlapInterval{}
			for i, s := range tc.spans {
				intervals = append(intervals, crocoOverlapInterval{ID: string(rune('a' + i)), Start: s[0], End: s[1], Group: "service=13/provider=0", Resource: map[string]string{"service": "13", "provider": "0"}})
			}
			w, count, e := crocoFindCapacityWindows(context.Background(), "appointments", intervals, p, time.UTC, 100)
			if e != nil {
				t.Fatal(e)
			}
			if count != tc.count {
				t.Fatalf("count=%d want=%d windows=%v", count, tc.count, w)
			}
			if tc.point && w[0].(map[string]any)["point_overlap"] != true {
				t.Fatal(w)
			}
		})
	}
}
func TestCrocoOverlapCalendarDatesAndDST(t *testing.T) {
	for _, tc := range []struct {
		zone, start, end string
		hours            int64
	}{
		{"America/Monterrey", "2026-11-10", "2026-11-11", 24},
		{"America/New_York", "2026-03-08", "2026-03-09", 23},
		{"America/New_York", "2026-11-01", "2026-11-02", 25},
	} {
		t.Run(tc.zone+tc.start, func(t *testing.T) {
			loc, _ := time.LoadLocation(tc.zone)
			row := crocoOverlapTestBooking(1, tc.start, tc.end)
			start, e := crocoBookingCalendarTime(row["check_in_date_timestamp"], true, loc)
			if e != nil {
				t.Fatal(e)
			}
			end, e := crocoBookingCalendarTime(row["check_out_date_timestamp"], false, loc)
			if e != nil {
				t.Fatal(e)
			}
			if (end-start)/3600 != tc.hours {
				t.Fatal(end - start)
			}
			if time.Unix(start, 0).In(loc).Format("2006-01-02") != tc.start {
				t.Fatal("booking moved to previous calendar day")
			}
		})
	}
	loc, _ := time.LoadLocation("UTC")
	if _, e := crocoBookingCalendarTime(int64(1794268800), true, loc); e == nil {
		t.Fatal("missing +1s accepted")
	}
}
func TestCrocoOverlapAbsoluteAppointmentTime(t *testing.T) {
	p := crocoOverlapTestPolicy()
	p.Timezone = "America/New_York"
	loc, _ := time.LoadLocation(p.Timezone)
	row := crocoOverlapTestAppointment(1, 1793511000, 1793514600)
	intervals, _, e := crocoOverlapIntervals(context.Background(), "appointments", []map[string]any{row}, p.Appointments, loc)
	if e != nil {
		t.Fatal(e)
	}
	if intervals[0].End-intervals[0].Start != 3600 {
		t.Fatal("absolute appointment timestamp shifted around DST")
	}
}
func TestCrocoOverlapStatusAndResourceIsolation(t *testing.T) {
	p := crocoOverlapTestPolicy()
	rows := []map[string]any{crocoOverlapTestAppointment(1, 10, 30), crocoOverlapTestAppointment(2, 15, 25), crocoOverlapTestAppointment(3, 15, 25)}
	rows[1]["provider"] = 2
	rows[2]["status"] = "cancelled"
	result, e := crocoAnalyzeOverlaps(context.Background(), crocoOverlapTestCapture("bookings"), nil, crocoOverlapTestCapture("appointments"), rows, p, 10)
	if e != nil {
		t.Fatal(e)
	}
	if result["count"] != 0 || result["excluded_by_status"].(map[string]any)["appointments"] != 1 {
		t.Fatal(result)
	}
	p.Appointments.GroupBy = []string{"service"}
	result, e = crocoAnalyzeOverlaps(context.Background(), crocoOverlapTestCapture("bookings"), nil, crocoOverlapTestCapture("appointments"), rows, p, 10)
	if e != nil {
		t.Fatal(e)
	}
	if result["count"] != 1 {
		t.Fatal(result)
	}
	p.Appointments.IncludedStatuses = append(p.Appointments.IncludedStatuses, "cancelled")
	result, e = crocoAnalyzeOverlaps(context.Background(), crocoOverlapTestCapture("bookings"), nil, crocoOverlapTestCapture("appointments"), rows, p, 10)
	if e != nil {
		t.Fatal(e)
	}
	if result["over_capacity_windows"].([]any)[0].(map[string]any)["occupancy"] != 3 {
		t.Fatal(result)
	}
}
func TestCrocoOverlapUnknownIsError(t *testing.T) {
	for _, tc := range []string{"incomplete", "wrong-version", "missing-version", "missing-status-coverage", "missing-fields", "different-sites", "invalid-interval", "missing-resource", "duplicate-id", "missing-status", "recurring-unknown"} {
		t.Run(tc, func(t *testing.T) {
			b := crocoOverlapTestCapture("bookings")
			a := crocoOverlapTestCapture("appointments")
			rows := []map[string]any{crocoOverlapTestAppointment(1, 10, 20)}
			switch tc {
			case "incomplete":
				b.Complete = false
			case "wrong-version":
				b.Versions["jet-booking"] = "99.0"
			case "missing-version":
				delete(a.Versions, "jet-appointments-booking")
			case "missing-status-coverage":
				a.Scope = map[string]any{}
			case "missing-fields":
				a.Fields = nil
			case "different-sites":
				a.Site = "https://other.invalid/wp-json"
			case "invalid-interval":
				rows[0]["slot_end_timestamp"] = 9
			case "missing-resource":
				delete(rows[0], "provider")
			case "duplicate-id":
				rows = append(rows, rows[0])
			case "missing-status":
				delete(rows[0], "status")
			case "recurring-unknown":
				rows[0]["type"] = "recurring"
			}
			if _, e := crocoAnalyzeOverlaps(context.Background(), b, nil, a, rows, crocoOverlapTestPolicy(), 10); e == nil {
				t.Fatal("unknown reported clean", tc)
			}
		})
	}
}
func TestCrocoOverlapCompleteEmptyHonesty(t *testing.T) {
	r, e := crocoAnalyzeOverlaps(context.Background(), crocoOverlapTestCapture("bookings"), nil, crocoOverlapTestCapture("appointments"), nil, crocoOverlapTestPolicy(), 10)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(r)
	if r["count"] != 0 || r["truncated"] != false || !bytes.Contains(b, []byte(`"over_capacity_windows":[]`)) || !strings.Contains(r["explanation"].(string), "not a live availability") {
		t.Fatal(string(b))
	}
}
func TestCrocoOverlapTruncationStillCountsAll(t *testing.T) {
	rows := []map[string]any{crocoOverlapTestAppointment(1, 10, 30), crocoOverlapTestAppointment(2, 15, 25), crocoOverlapTestAppointment(3, 40, 60), crocoOverlapTestAppointment(4, 45, 55)}
	r, e := crocoAnalyzeOverlaps(context.Background(), crocoOverlapTestCapture("bookings"), nil, crocoOverlapTestCapture("appointments"), rows, crocoOverlapTestPolicy(), 1)
	if e != nil {
		t.Fatal(e)
	}
	if r["count"] != 2 || r["reported"] != 1 || r["truncated"] != true || r["scanned"].(map[string]any)["appointments"] != 4 {
		t.Fatal(r)
	}
}
func TestCrocoOverlapPolicyFailClosed(t *testing.T) {
	for _, tc := range []string{"missing-timezone", "missing-status", "missing-boundary", "zero-capacity", "invalid-grouping", "missing-encoding", "unknown-json"} {
		t.Run(tc, func(t *testing.T) {
			p := crocoOverlapTestPolicy()
			switch tc {
			case "missing-timezone":
				p.Timezone = ""
			case "missing-status":
				p.Bookings.IncludedStatuses = nil
			case "missing-boundary":
				p.Appointments.Boundary = ""
			case "zero-capacity":
				p.Appointments.Capacity = 0
			case "invalid-grouping":
				p.Bookings.GroupBy = []string{"service"}
			case "missing-encoding":
				p.Bookings.DateEncoding = ""
			}
			b, _ := json.Marshal(p)
			if tc == "unknown-json" {
				b = append(b[:len(b)-1], []byte(`,"secret":"not-accepted"}`)...)
			}
			path := filepath.Join(t.TempDir(), "policy.json")
			os.WriteFile(path, b, 0600)
			if _, e := crocoReadOverlapPolicy(path); e == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}
func TestCrocoOverlapDryRunAndLiveRejectBeforeFiles(t *testing.T) {
	f := &rootFlags{dryRun: true, dataSource: "live"}
	root := &cobra.Command{Use: "crocoblock-pp-cli"}
	for _, hook := range novelCommandHooks {
		hook(root, f)
	}
	cmd, _, e := root.Find([]string{"reservations", "overlaps"})
	if e != nil {
		t.Fatal(e)
	}
	cmd.Flags().Set("policy", "/path/does/not/exist")
	cmd.Flags().Set("db", "/path/does/not/exist")
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	if e = cmd.RunE(cmd, nil); e != nil {
		t.Fatal("dryrun touched files", e)
	}
	f.dryRun = false
	if e = cmd.RunE(cmd, nil); e == nil || !strings.Contains(e.Error(), "local snapshots") {
		t.Fatal("live not rejected before files", e)
	}
}
func TestCrocoOverlapSQLiteSyntheticFixture(t *testing.T) {
	dir := os.Getenv("CROCOBLOCK_OVERLAP_PROOF_DIR")
	if dir == "" {
		dir = t.TempDir()
	} else {
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	path := filepath.Join(dir, "synthetic-overlaps.db")
	s, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	bookings := []map[string]any{crocoOverlapTestBooking(1, "2026-11-10", "2026-11-12"), crocoOverlapTestBooking(2, "2026-11-11", "2026-11-13")}
	appointments := []map[string]any{crocoOverlapTestAppointment(1, 1794304800, 1794308400), crocoOverlapTestAppointment(2, 1794306600, 1794310200)}
	for resource, rows := range map[string][]map[string]any{"bookings": bookings, "appointments": appointments} {
		stored := []store.CrocoRow{}
		for i, row := range rows {
			raw, _ := json.Marshal(row)
			stored = append(stored, store.CrocoRow{ID: string(rune('1' + i)), Data: raw})
		}
		if e = s.CrocoReplace(context.Background(), crocoOverlapTestCapture(resource), stored); e != nil {
			t.Fatal(e)
		}
	}
	s.Close()
	b, br, e := crocoLoadCapture(context.Background(), path, "bookings")
	if e != nil {
		t.Fatal(e)
	}
	a, ar, e := crocoLoadCapture(context.Background(), path, "appointments")
	if e != nil {
		t.Fatal(e)
	}
	r, e := crocoAnalyzeOverlaps(context.Background(), b, br, a, ar, crocoOverlapTestPolicy(), 10)
	if e != nil {
		t.Fatal(e)
	}
	if r["count"] != 2 {
		t.Fatal(r)
	}
	if dir == os.Getenv("CROCOBLOCK_OVERLAP_PROOF_DIR") {
		raw, _ := json.MarshalIndent(map[string]any{"fixture_kind": "synthetic", "result": r}, "", "  ")
		if e = os.WriteFile(filepath.Join(dir, "synthetic-overlaps-result.json"), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
