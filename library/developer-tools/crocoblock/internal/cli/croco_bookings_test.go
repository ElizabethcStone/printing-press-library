package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/config"
	"github.com/spf13/cobra"
)

func crocoReservationTestClient(t *testing.T, h http.HandlerFunc) *client.Client {
	t.Helper()
	t.Setenv("CROCOBLOCK_DB", filepath.Join(t.TempDir(), "test.db"))
	server := httptest.NewTLSServer(h)
	t.Cleanup(server.Close)
	cfg := &config.Config{BaseURL: server.URL, CrocoblockUsername: "test", CrocoblockApplicationPassword: "test"}
	c := client.New(cfg, 3*time.Second, 10000)
	c.HTTPClient = server.Client()
	c.NoCache = true
	return c
}
func crocoBookingFixture(id int) map[string]any {
	return map[string]any{"booking_id": id, "apartment_id": 13, "apartment_unit": 0, "check_in_date_timestamp": 1794268801, "check_out_date_timestamp": 1794441600, "status": "pending"}
}
func TestCrocoBookingCreateVerifiedContract(t *testing.T) {
	calls := []string{}
	created := false
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") == "" {
			t.Error("missing auth")
		}
		switch r.URL.Path {
		case "/jet-booking/v2/bookings-list":
			if r.Method != "GET" {
				t.Error("wrong method")
			}
			if !strings.Contains(r.URL.Query().Get("filters"), "apartment_id") {
				t.Error("readback not resource scoped")
			}
			rows := []any{}
			if created {
				rows = append(rows, crocoBookingFixture(10))
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": rows, "total": len(rows)})
		case "/jet-booking/v2/add-booking":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["createRelatedOrder"] != false {
				t.Error("order creation enabled")
			}
			item, _ := body["item"].(map[string]any)
			if item["check_in_date"] != "2026-11-10" {
				t.Error("date altered")
			}
			created = true
			json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	row, e := crocoBookingWrite(context.Background(), c, "", map[string]any{"apartment_id": 13, "check_in_date": "2026-11-10", "check_out_date": "2026-11-12", "status": "pending"})
	if e != nil {
		t.Fatal(e)
	}
	if crocoString(row["booking_id"]) != "10" {
		t.Fatal(row)
	}
	if len(calls) != 3 || calls[1] != "POST /jet-booking/v2/add-booking" {
		t.Fatal(calls)
	}
}
func TestCrocoBookingUpdatePreservesRequiredFields(t *testing.T) {
	row := crocoBookingFixture(10)
	writes := 0
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			writes++
			if r.URL.Path != "/jet-booking/v2/update-booking/10" {
				t.Error(r.URL.Path)
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			item := body["item"].(map[string]any)
			if item["check_in_date"] != "2026-11-10" || item["check_out_date"] != "2026-11-12" || item["apartment_id"] != float64(13) {
				t.Error(item)
			}
			row["status"] = item["status"]
			json.NewEncoder(w).Encode(map[string]any{"success": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{row}, "total": 1})
	})
	got, e := crocoBookingWrite(context.Background(), c, "10", map[string]any{"status": "cancelled"})
	if e != nil {
		t.Fatal(e)
	}
	if got["status"] != "cancelled" || writes != 1 {
		t.Fatal(got, writes)
	}
}
func TestCrocoBookingRejectsInvalidBeforeHTTP(t *testing.T) {
	calls := 0
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; t.Error("unexpected HTTP") })
	for _, body := range []map[string]any{
		{"apartment_id": 13, "check_in_date": "2026-11-12", "check_out_date": "2026-11-10"},
		{"apartment_id": 13, "check_in_date": "2026-02-30", "check_out_date": "2026-03-01"},
		{"apartment_id": -1, "check_in_date": "2026-11-10", "check_out_date": "2026-11-12"},
		{"apartment_id": 13, "check_in_date": "2026-11-10", "check_out_date": "2026-11-12", "createRelatedOrder": true},
	} {
		if _, e := crocoBookingWrite(context.Background(), c, "", body); e == nil {
			t.Fatal("invalid body accepted", body)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}
func TestCrocoReservationEmptyAndFailureHonesty(t *testing.T) {
	for _, test := range []struct {
		name, reply string
		wantErr     bool
	}{
		{"empty", `{"success":true,"data":[],"total":0}`, false},
		{"denied", `{"success":false,"data":"denied"}`, true},
		{"missing_total", `{"success":true,"data":[]}`, true},
		{"incomplete", `{"success":true,"data":[],"total":5}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(test.reply)) })
			rows, e := crocoReservationAll(context.Background(), c, true, map[string]any{})
			if (e != nil) != test.wantErr {
				t.Fatalf("rows=%v err=%v", rows, e)
			}
			if !test.wantErr && len(rows) != 0 {
				t.Fatal(rows)
			}
		})
	}
}
func TestCrocoBookingSuccessNoOpRejected(t *testing.T) {
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.Write([]byte(`{"success":true}`))
			return
		}
		w.Write([]byte(`{"success":true,"data":[],"total":0}`))
	})
	_, e := crocoBookingWrite(context.Background(), c, "", map[string]any{"apartment_id": 13, "check_in_date": "2026-11-10", "check_out_date": "2026-11-12"})
	if e == nil || !strings.Contains(e.Error(), "0 new matching") {
		t.Fatal(e)
	}
}
func TestCrocoReservationsDryRunNeedsNoInputOrNetwork(t *testing.T) {
	f := &rootFlags{dryRun: true}
	cmds := []*cobra.Command{crocoBookingsList(f), crocoBookingsConfig(f), crocoBookingsWrite(f, false), crocoBookingsWrite(f, true), crocoBookingsDelete(f), crocoAppointmentsList(f), crocoAppointmentsGet(f), crocoAppointmentsWrite(f, false), crocoAppointmentsWrite(f, true), crocoAppointmentsDelete(f), crocoAppointmentsSlots(f), crocoAppointmentsProviders(f)}
	for _, cmd := range cmds {
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetArgs([]string{})
		if e := cmd.Execute(); e != nil {
			t.Fatalf("%s: %v", cmd.Name(), e)
		}
		if out.Len() == 0 {
			t.Fatal("missing dryrun output", cmd.Name())
		}
	}
}

func TestCrocoBookingDeleteUsesDELETEAndChecksAbsence(t *testing.T) {
	exists := true
	writes := 0
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/jet-booking/v2/delete-booking/10" {
			if r.Method != "DELETE" {
				t.Fatal("delete wrong method", r.Method)
			}
			exists = false
			writes++
			w.Write([]byte(`{"success":true}`))
			return
		}
		if r.Method != "GET" || r.URL.Path != "/jet-booking/v2/bookings-list" {
			t.Fatal("unexpected request", r.Method, r.URL.Path)
		}
		rows := []any{}
		if exists {
			rows = append(rows, crocoBookingFixture(10))
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": rows, "total": len(rows)})
	})
	if e := crocoBookingDeleteRecord(context.Background(), c, "10"); e != nil {
		t.Fatal(e)
	}
	if writes != 1 {
		t.Fatal(writes)
	}
}
func TestCrocoBookingDeleteNoOpRejected(t *testing.T) {
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.Write([]byte(`{"success":true}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{crocoBookingFixture(10)}, "total": 1})
	})
	if e := crocoBookingDeleteRecord(context.Background(), c, "10"); e == nil {
		t.Fatal("no-op delete accepted")
	}
}
func TestCrocoReservationReadbackRejectsChangingTotals(t *testing.T) {
	calls := 0
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		total := 2
		if calls > 1 {
			total = 3
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{crocoBookingFixture(calls)}, "total": total})
	})
	if _, e := crocoReservationAll(context.Background(), c, true, map[string]any{}); e == nil || !strings.Contains(e.Error(), "count changed") {
		t.Fatal(e)
	}
}

func TestCrocoBookingConfigReadUsesPOSTAndItem(t *testing.T) {
	calls := 0
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/jet-booking/v2/booking-config" {
			t.Fatal("wrong route/method", r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		item := body["item"].(map[string]any)
		if item["apartment_id"] != "13" {
			t.Fatal(body)
		}
		w.Write([]byte(`{"success":true,"units":[]}`))
	})
	if _, e := crocoBookingConfig(context.Background(), c, "13"); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
