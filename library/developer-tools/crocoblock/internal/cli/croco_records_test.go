package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCrocoRecordFieldsPrivacyAndPagination(t *testing.T) {
	c := crocoFormsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "100" {
			t.Error("unstable page size")
		}
		rows := []any{}
		if r.URL.Query().Get("page") == "1" {
			for i := 0; i < 100; i++ {
				name := fmt.Sprintf("field%d", i)
				if i == 0 {
					name = "contact"
				}
				rows = append(rows, map[string]any{"name": map[string]any{"value": map[string]any{"name": name, "label": "Field"}}, "value": map[string]any{"value": "personal@example.invalid"}, "field_type": map[string]any{"value": map[string]any{"title": "Text"}}})
			}
		} else {
			rows = append(rows, map[string]any{"name": map[string]any{"value": map[string]any{"name": "api_key"}}, "value": map[string]any{"value": "SECRET"}})
		}
		json.NewEncoder(w).Encode(map[string]any{"list": rows, "total": 101})
	})
	got, total, complete, e := crocoRecordFields(context.Background(), c, "5", 1000, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	if total != 101 || !complete || len(got) != 101 {
		t.Fatalf("pagination total=%d complete=%v len=%d", total, complete, len(got))
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "personal@") || strings.Contains(string(raw), "SECRET") {
		t.Fatal("default output disclosed submitted values")
	}
	got, _, complete, e = crocoRecordFields(context.Background(), c, "5", 1000, []string{"api_key"}, true)
	if e != nil || !complete {
		t.Fatal(e)
	}
	raw, _ = json.Marshal(got)
	if strings.Contains(string(raw), "SECRET") {
		t.Fatal("selected secret disclosed")
	}
	_, _, complete, e = crocoRecordFields(context.Background(), c, "5", 50, nil, false)
	if e != nil || complete {
		t.Fatalf("cap falsely complete: %v %v", complete, e)
	}
	_, _, _, e = crocoRecordFields(context.Background(), c, "5", 1000, []string{"absent"}, false)
	if e == nil {
		t.Fatal("unknown selected field accepted")
	}
}
func TestCrocoRecordsPageAssociation(t *testing.T) {
	c := crocoFormsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filters[form]") != "42" {
			t.Error("form filter missing")
		}
		fmt.Fprint(w, `{"list":[{"choose":{"value":8},"user":{"value":"private user"},"referrer":{"value":"https://secret.invalid"},"status":{"value":{"type":"success","text":"Success"}},"created_at":{"value":"2026-10-21"}}],"total":1}`)
	})
	rows, total, e := crocoRecordsPage(context.Background(), c, 1, 50, 42, "ASC")
	if e != nil || total != 1 {
		t.Fatal(e)
	}
	m := rows[0].(map[string]any)
	if m["form_id"] != 42 {
		t.Fatal("association lost")
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "secret.invalid") {
		t.Fatal("private columns leaked")
	}
}
func TestCrocoRecordEmptyAndIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		fail       bool
	}{{"empty", `{"list":[],"total":0}`, false}, {"missing_total", `{"list":[]}`, true}, {"missing_list", `{"total":0}`, true}, {"null_list", `{"list":null,"total":0}`, true}, {"partial", `{"list":[],"total":5}`, true}} {
		t.Run(tc.name, func(t *testing.T) {
			c := crocoFormsTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
			rows, _, complete, e := crocoRecordFields(context.Background(), c, "5", 1000, nil, false)
			if (e != nil) != tc.fail {
				t.Fatalf("err=%v", e)
			}
			if !tc.fail && (!complete || len(rows) != 0) {
				t.Fatal("verified empty collection lost")
			}
		})
	}
}
