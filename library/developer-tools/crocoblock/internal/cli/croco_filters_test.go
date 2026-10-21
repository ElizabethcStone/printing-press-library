package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCrocoFilterReadbackComparesEverySentField(t *testing.T) {
	for _, tc := range []struct {
		name      string
		want, got map[string]any
		fail      bool
	}{
		{"title", map[string]any{"title": "Wanted"}, map[string]any{"title": "Wanted"}, false},
		{"no-op-title", map[string]any{"title": "Wanted"}, map[string]any{"title": "Old"}, true},
		{"missing-setting", map[string]any{"_query_var": "pp_keep"}, map[string]any{}, true},
		{"scalar-number", map[string]any{"_source_step": json.Number("1.0")}, map[string]any{"_source_step": "1"}, false},
		{"scalar-true", map[string]any{"_show_empty_terms": true}, map[string]any{"_show_empty_terms": "1"}, false},
		{"scalar-false", map[string]any{"_show_empty_terms": false}, map[string]any{"_show_empty_terms": ""}, false},
		{"not-loose-string", map[string]any{"_query_var": "001"}, map[string]any{"_query_var": "1"}, true},
		{"serialized-array", map[string]any{"_source_manual_input": []any{map[string]any{"value": json.Number("9007199254740993"), "label": "A"}}}, map[string]any{"_source_manual_input": []any{map[string]any{"label": "A", "value": json.Number("9007199254740993")}}}, false},
		{"serialized-precision", map[string]any{"_source_manual_input": []any{json.Number("9007199254740993")}}, map[string]any{"_source_manual_input": []any{json.Number("9007199254740992")}}, true},
		{"serialized-number-normalization", map[string]any{"_source_manual_input": []any{json.Number("1.0")}}, map[string]any{"_source_manual_input": []any{json.Number("1")}}, false},
		{"serialized-empty-object", map[string]any{"_data_exclude_include": map[string]any{}}, map[string]any{"_data_exclude_include": []any{}}, false},
		{"title-raw-not-entity-decoded", map[string]any{"title": "A & B"}, map[string]any{"title": "A &amp; B"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := crocoVerifyFilterValues(tc.got, tc.want)
			if (e != nil) != tc.fail {
				t.Fatal(e)
			}
			if e != nil && (strings.Contains(e.Error(), "Wanted") || strings.Contains(e.Error(), "Old")) {
				t.Fatal("payload leaked into error", e)
			}
		})
	}
}
func TestCrocoFilterMutationIDMustMatchOperation(t *testing.T) {
	for _, tc := range []struct {
		op, id string
		reply  any
		fail   bool
	}{
		{"update", "12", json.Number("12"), false}, {"update", "12", json.Number("13"), true}, {"duplicate", "12", json.Number("12"), true}, {"duplicate", "12", json.Number("13"), false}, {"create", "new", json.Number("13"), false}, {"update", "12", false, true},
	} {
		_, e := crocoFilterMutationID(tc.op, tc.id, tc.reply)
		if (e != nil) != tc.fail {
			t.Fatalf("%+v: %v", tc, e)
		}
	}
}
func TestCrocoFilterReadbackRejectsHTTP200NoOp(t *testing.T) {
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/jet-smart-filters-api/v1/filter" {
			t.Fatal(r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["id"] != "12" || len(body) != 1 {
			t.Fatal(body)
		}
		w.Write([]byte(`{"ID":"12","title":"Old","_query_var":"pp_keep"}`))
	})
	if _, e := crocoVerifyFilterReadback(context.Background(), c, "12", map[string]any{"title": "Requested"}); e == nil {
		t.Fatal("same ID was accepted despite unchanged title")
	}
	if _, e := crocoVerifyFilterReadback(context.Background(), c, "12", map[string]any{"_query_var": "pp_keep"}); e != nil {
		t.Fatal(e)
	}
}
func TestCrocoFilterRejectsUnverifiableValueShapes(t *testing.T) {
	for _, body := range []map[string]any{{"title": 42}, {"_query_var": nil}, {"_query_var": []any{"field"}}, {"_source_manual_input": "opaque-serialized-value"}} {
		if e := crocoFilterData(body); e == nil {
			t.Fatal("unverifiable input accepted", body)
		}
	}
	for _, body := range []map[string]any{{"_query_var": "pp_keep"}, {"_source_step": json.Number("2")}, {"_source_manual_input": []any{}}, {"_data_exclude_include": map[string]any{}}} {
		if e := crocoFilterData(body); e != nil {
			t.Fatal(e)
		}
	}
}
