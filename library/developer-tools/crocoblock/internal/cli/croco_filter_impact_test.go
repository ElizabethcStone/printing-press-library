package cli

import (
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"testing"
)

func crocoImpactFixture() (store.CrocoCapture, []map[string]any) {
	return store.CrocoCapture{Site: "https://site.invalid/wp-json", CapturedAt: "2026-10-20T12:00:00Z", Versions: map[string]string{"jet-smart-filters": "3.8.6.2"}}, []map[string]any{{"ID": "12", "status": "publish", "_data_source": "custom_fields", "_query_var": "price", "_source_custom_field": "price"}}
}
func crocoImpactEvent(id int64, kind, resource string, fields []string, detail map[string]any) map[string]any {
	return map[string]any{"id": id, "site": "https://site.invalid/wp-json", "kind": kind, "resource": resource, "fields": fields, "detail": detail, "created_at": "2026-10-20T10:00:00Z"}
}
func TestCrocoFilterImpactCoverageAndNegativeCases(t *testing.T) {
	change := crocoImpactEvent(1, "post_fields_write", "fields:13", []string{"price"}, map[string]any{"id": "13"})
	receipt := crocoImpactEvent(2, "reindex", "filters", nil, map[string]any{"verified": true, "reindex_event_id": 1})
	cases := []struct {
		name   string
		events []map[string]any
		since  int64
		want   string
	}{
		{"no_history", nil, 0, "no_observed_dependency_change"},
		{"matching_field", []map[string]any{change}, 0, "candidate"},
		{"different_field", []map[string]any{crocoImpactEvent(1, "post_fields_write", "fields:13", []string{"unrelated"}, nil)}, 0, "no_observed_dependency_change"},
		{"covered", []map[string]any{change, receipt}, 0, "covered_by_receipt"},
		{"since_exclusive", []map[string]any{change}, 1, "no_observed_dependency_change"},
		{"cct_provider_unknown", []map[string]any{crocoImpactEvent(1, "update", "cct:rooms", []string{"price"}, nil)}, 0, "unknown"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cap, filters := crocoImpactFixture()
			rows, _, e := crocoComputeFilterImpact(cap, filters, tt.events, tt.since)
			if e != nil || len(rows) != 1 {
				t.Fatal(rows, e)
			}
			if rows[0].(map[string]any)["status"] != tt.want {
				t.Fatal(rows)
			}
		})
	}
}
func TestCrocoFilterImpactDoesNotCoverConcurrentWrite(t *testing.T) {
	cap, filters := crocoImpactFixture()
	events := []map[string]any{crocoImpactEvent(1, "post_fields_write", "fields:13", []string{"price"}, nil), crocoImpactEvent(2, "post_fields_write", "fields:13", []string{"price"}, nil), crocoImpactEvent(3, "reindex", "filters", nil, map[string]any{"verified": true, "reindex_event_id": 1})}
	rows, _, e := crocoComputeFilterImpact(cap, filters, events, 0)
	if e != nil || rows[0].(map[string]any)["status"] != "candidate" {
		t.Fatal(rows, e)
	}
}
func TestCrocoFilterImpactUnknownAndInvalidProvenance(t *testing.T) {
	cap, filters := crocoImpactFixture()
	filters[0]["_data_source"] = "taxonomy"
	rows, _, e := crocoComputeFilterImpact(cap, filters, nil, 0)
	if e != nil || rows[0].(map[string]any)["status"] != "unknown" {
		t.Fatal(rows, e)
	}
	event := crocoImpactEvent(1, "update", "fields:13", []string{"price"}, nil)
	event["site"] = "https://other.invalid/wp-json"
	if _, _, e = crocoComputeFilterImpact(cap, filters, []map[string]any{event}, 0); e == nil {
		t.Fatal("mixed sites accepted")
	}
	if _, _, e = crocoComputeFilterImpact(cap, filters, nil, 1); e == nil {
		t.Fatal("future since accepted")
	}
	rows, _, e = crocoComputeFilterImpact(cap, []map[string]any{}, nil, 0)
	if e != nil || rows == nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}
