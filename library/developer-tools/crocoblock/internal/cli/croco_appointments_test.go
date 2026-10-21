package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func crocoAppointmentFixture(item map[string]any) map[string]any {
	r := map[string]any{"ID": 22}
	for k, v := range item {
		r[k] = v
	}
	return r
}
func TestCrocoAppointmentCreateArrayAndPairedTimes(t *testing.T) {
	var item map[string]any
	calls := []string{}
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == "POST" {
			if r.URL.Path != "/jet-engine/v2/appointment-add-appointment" {
				t.Error(r.URL.Path)
			}
			var body []map[string]any
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Error("must send top-level array", e)
			}
			if len(body) != 1 {
				t.Fatal(body)
			}
			item = body[0]
			for _, k := range []string{"date", "slot", "slot_end", "date_timestamp", "slot_timestamp", "slot_end_timestamp"} {
				if item[k] == nil {
					t.Error("missing", k)
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": "localized success text"})
			return
		}
		if r.URL.Path != "/jet-engine/v2/appointments-list" {
			t.Error(r.URL.Path)
		}
		if !strings.Contains(r.URL.Query().Get("filter"), "service") {
			t.Error("not scoped")
		}
		rows := []any{}
		if item != nil {
			rows = append(rows, crocoAppointmentFixture(item))
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": rows, "total": len(rows)})
	})
	got, e := crocoAppointmentWrite(context.Background(), c, "", map[string]any{"service": 13, "date": "2026-11-10", "slot": "10:00", "slot_end": "11:00", "user_email": "fixture@example.invalid"}, "UTC")
	if e != nil {
		t.Fatal(e)
	}
	if crocoString(got["ID"]) != "22" || len(calls) != 3 {
		t.Fatal(got, calls)
	}
	if item["slot_timestamp"] != float64(1794304800) {
		t.Fatal(item)
	}
	safe := crocoSafe(got).(map[string]any)
	if safe["user_email"] != "[redacted]" {
		t.Fatal("email leaked")
	}
}
func TestCrocoAppointmentNoOpNotSuccess(t *testing.T) {
	writes := 0
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			writes++
			w.Write([]byte(`{"success":true,"data":"Appointment time already taken"}`))
			return
		}
		w.Write([]byte(`{"success":true,"data":[],"total":0}`))
	})
	_, e := crocoAppointmentWrite(context.Background(), c, "", map[string]any{"service": 13, "date": "2026-11-10", "slot": "10:00", "slot_end": "11:00", "user_email": "fixture@example.invalid"}, "UTC")
	if e == nil || !strings.Contains(e.Error(), "0 new matching") || writes != 1 {
		t.Fatal(e, writes)
	}
}
func TestCrocoAppointmentStatusUpdatePreservesTimes(t *testing.T) {
	row := map[string]any{"ID": "22", "service": "13", "provider": "0", "status": "pending", "user_email": "fixture@example.invalid", "date": "10/11/26", "slot": "10:00 am", "slot_end": "11:00 am", "date_timestamp": "1794268800", "slot_timestamp": "1794304800", "slot_end_timestamp": "1794308400"}
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if r.URL.Path != "/jet-engine/v2/update-appointment/22" {
				t.Error(r.URL.Path)
			}
			var body map[string]any
			decoder := json.NewDecoder(r.Body)
			decoder.UseNumber()
			decoder.Decode(&body)
			item := body["item"].(map[string]any)
			for _, k := range []string{"date_timestamp", "slot_timestamp", "slot_end_timestamp"} {
				if crocoString(item[k]) != crocoString(row[k]) {
					t.Error(k, item[k], row[k])
				}
			}
			row["status"] = item["status"]
			w.Write([]byte(`{"success":true}`))
			return
		}
		if r.URL.Path != "/jet-engine/v2/get-appointment/22" {
			t.Error(r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "item": row})
	})
	got, e := crocoAppointmentWrite(context.Background(), c, "22", map[string]any{"status": "cancelled"}, "")
	if e != nil {
		t.Fatal(e)
	}
	if got["status"] != "cancelled" {
		t.Fatal(got)
	}
}
func TestCrocoAppointmentDateValidationNoHTTP(t *testing.T) {
	calls := 0
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; t.Error("unexpected HTTP") })
	for _, tc := range []struct{ day, start, end, zone string }{
		{"2026-11-10", "10:00", "11:00", ""},
		{"2026-11-10", "10:00", "11:00", "Local"},
		{"2026-11-10", "12:00", "11:00", "UTC"},
		{"2026-02-30", "10:00", "11:00", "UTC"},
		{"2026-03-08", "02:30", "03:30", "America/New_York"},
		{"2026-11-01", "01:30", "02:30", "America/New_York"},
	} {
		_, e := crocoAppointmentWrite(context.Background(), c, "", map[string]any{"service": 13, "date": tc.day, "slot": tc.start, "slot_end": tc.end, "user_email": "fixture@example.invalid"}, tc.zone)
		if e == nil {
			t.Fatal("invalid time accepted", tc)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}
func TestCrocoAppointmentConversionUsesDeclaredZone(t *testing.T) {
	got, e := crocoAppointmentInput(map[string]any{"service": 13, "date": "2026-11-10", "slot": "10:00", "slot_end": "11:00", "user_email": "fixture@example.invalid"}, nil, "America/Monterrey")
	if e != nil {
		t.Fatal(e)
	}
	if got["slot_timestamp"] != int64(1794326400) {
		t.Fatal(got)
	}
}

func TestCrocoAppointmentDeleteNeverExpandsGroup(t *testing.T) {
	exists := true
	writes := 0
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jet-engine/v2/get-appointment/22":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "item": map[string]any{"ID": 22, "group_ID": 7}})
		case "/jet-engine/v2/delete-appointment":
			if r.Method != "POST" {
				t.Error(r.Method)
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if len(body) != 1 || body["group_ID"] != nil {
				t.Fatal("implicit group expansion", body)
			}
			items := body["items"].([]any)
			if len(items) != 1 || items[0] != "22" {
				t.Fatal(items)
			}
			exists = false
			writes++
			w.Write([]byte(`{"success":true,"appointment_IDs":[22]}`))
		case "/jet-engine/v2/appointments-list":
			if r.Method != "GET" {
				t.Error(r.Method)
			}
			rows := []any{}
			if exists {
				rows = append(rows, map[string]any{"ID": 22})
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": rows, "total": len(rows)})
		default:
			t.Fatal("unexpected route", r.URL.Path)
		}
	})
	if e := crocoAppointmentDeleteRecords(context.Background(), c, []string{"22"}); e != nil {
		t.Fatal(e)
	}
	if writes != 1 {
		t.Fatal(writes)
	}
}
func TestCrocoAppointmentUnknownFieldsRejectedBeforeHTTP(t *testing.T) {
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected HTTP") })
	if _, e := crocoAppointmentWrite(context.Background(), c, "22", map[string]any{"group_ID": 7}, ""); e == nil {
		t.Fatal("group rewrite allowed")
	}
	if _, e := crocoAppointmentWrite(context.Background(), c, "22", map[string]any{"date": "2026-02-30"}, "UTC"); e == nil {
		t.Fatal("invalid update date allowed")
	}
}

func TestCrocoAppointmentSlotsPossibleWriteDoesNotReplay(t *testing.T) {
	calls := 0
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/jet-engine/v2/appointment-date-slots" {
			t.Fatal(r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["service"] != "13" || body["timezone"] != "UTC" {
			t.Fatal(body)
		}
		w.WriteHeader(503)
		w.Write([]byte(`{"message":"temporary failure"}`))
	})
	if _, e := crocoAppointmentSlotView(context.Background(), c, "13", 0, 1796860800, "UTC"); e == nil {
		t.Fatal("expected uncertain outcome error")
	}
	if calls != 1 {
		t.Fatal("possible write replayed", calls)
	}
	cmd := crocoAppointmentsSlots(&rootFlags{dryRun: true})
	if cmd.Annotations["mcp:read-only"] != "false" {
		t.Fatal("slot view incorrectly read-only")
	}
	if !strings.Contains(cmd.Long, "persist an excluded calendar day") {
		t.Fatal("provider side effect missing from help")
	}
	if e := cmd.RunE(cmd, nil); e != nil {
		t.Fatal("dryrun requires input or network", e)
	}
}

func TestCrocoAppointmentSlotsJournalAcceptsOperationalMetadata(t *testing.T) {
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("journal must not call upstream") })
	if e := crocoAppointmentSlotsJournal(&rootFlags{}, context.Background(), c, "13"); e != nil {
		t.Fatal(e)
	}
}
