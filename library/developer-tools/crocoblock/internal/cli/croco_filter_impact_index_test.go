package cli

import (
	"fmt"
	"testing"
)

func TestCrocoFilterImpactIndexManyPairs(t *testing.T) {
	const pairs = 10000
	// Large int64 identities must retain precision in the lookup index.
	const base int64 = 1 << 54
	events := make([]map[string]any, 0, pairs*2)
	for i := 0; i < pairs; i++ {
		start := base + int64(i)*2 + 1
		resource := fmt.Sprintf("fields:%d", i+1)
		// Put completions before their intents in the input slice. Journal IDs,
		// rather than slice order, define linkage chronology.
		events = append(events, crocoImpactEvent(start+1, "post_fields_write", resource, []string{"price"}, map[string]any{"verified": true, "start_event_id": start}))
		events = append(events, crocoImpactEvent(start, "post_fields_attempt", resource, []string{"price"}, nil))
	}
	cap, filters := crocoImpactFixture()
	rows, summary, err := crocoComputeFilterImpact(cap, filters, events, 0)
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0].(map[string]any)
	if row["status"] != "candidate" || len(row["unknown_events"].([]any)) != 0 || len(row["matching_events"].([]any)) != pairs {
		t.Fatalf("incorrect reconciled status or event counts: %s", row["status"])
	}
	if summary["scanned_events"] != pairs*2 || summary["last_event"] != base+pairs*2 {
		t.Fatal(summary)
	}
}

func TestCrocoFilterImpactIndexNonmatchingLinksRemainUnknown(t *testing.T) {
	for _, scenario := range []string{"missing_intent", "wrong_kind", "wrong_resource", "wrong_fields", "field_order", "unverified"} {
		t.Run(scenario, func(t *testing.T) {
			intent := crocoImpactEvent(1, "post_fields_attempt", "fields:13", []string{"price", "category"}, nil)
			detail := map[string]any{"verified": true, "start_event_id": int64(1)}
			completion := crocoImpactEvent(3, "post_fields_write", "fields:13", []string{"price", "category"}, detail)
			events := []map[string]any{intent, completion}
			switch scenario {
			case "missing_intent":
				detail["start_event_id"] = int64(2)
			case "wrong_kind":
				events = append(events, crocoImpactEvent(2, "update", "fields:13", []string{"price", "category"}, nil))
				detail["start_event_id"] = int64(2)
			case "wrong_resource":
				completion["resource"] = "fields:14"
			case "wrong_fields":
				completion["fields"] = []string{"price"}
			case "field_order":
				completion["fields"] = []any{"category", "price"}
			case "unverified":
				detail["verified"] = false
			}
			cap, filters := crocoImpactFixture()
			rows, _, err := crocoComputeFilterImpact(cap, filters, events, 0)
			if err != nil {
				t.Fatal(err)
			}
			row := rows[0].(map[string]any)
			unknown := row["unknown_events"].([]any)
			if row["status"] != "unknown" || len(unknown) != 1 || unknown[0] != int64(1) {
				t.Fatal(row)
			}
		})
	}
}

func TestCrocoFilterImpactIndexRejectsMalformedLinkage(t *testing.T) {
	for _, start := range []any{int64(0), int64(-1), int64(3), int64(4), "bad", 1.5} {
		t.Run(fmt.Sprint(start), func(t *testing.T) {
			cap, filters := crocoImpactFixture()
			events := []map[string]any{
				crocoImpactEvent(1, "post_fields_attempt", "fields:13", []string{"price"}, nil),
				crocoImpactEvent(3, "post_fields_write", "fields:13", []string{"price"}, map[string]any{"verified": true, "start_event_id": start}),
			}
			if _, _, err := crocoComputeFilterImpact(cap, filters, events, 0); err == nil {
				t.Fatal("accepted malformed completion linkage")
			}
		})
	}
}
