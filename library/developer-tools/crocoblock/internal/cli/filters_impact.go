package cli

// pp:data-source local

import (
	"context"
	"fmt"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
	"reflect"
	"regexp"
	"strings"
	"time"
)

var crocoFieldNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func crocoEventFields(v any) []string {
	if a, ok := v.([]string); ok {
		return a
	}
	if a, ok := v.([]any); ok {
		out := []string{}
		for _, x := range a {
			out = append(out, crocoString(x))
		}
		return out
	}
	return []string{}
}
func crocoComputeFilterImpact(cap store.CrocoCapture, filters, events []map[string]any, since int64) ([]any, map[string]any, error) {
	var covered, last int64
	var receipt int64
	completedAttempts := map[int64]bool{}
	// Index once, before reconciling completions, so linkage does not rescan
	// the journal and remains independent of event ordering and filter count.
	attempts := make(map[int64]map[string]any)
	for _, event := range events {
		if event["kind"] == "post_fields_attempt" {
			if id, err := crocoInt(event["id"]); err == nil {
				attempts[id] = event
			}
		}
	}
	for _, event := range events {
		id, e := crocoInt(event["id"])
		if e != nil {
			return nil, nil, e
		}
		if id > last {
			last = id
		}
		if crocoString(event["site"]) != cap.Site {
			return nil, nil, configErr(fmt.Errorf("event and filter capture sites differ"))
		}
		if event["kind"] == "post_fields_write" {
			d, _ := event["detail"].(map[string]any)
			if d["verified"] == true && d["start_event_id"] != nil {
				start, err := crocoInt(d["start_event_id"])
				if err != nil || start <= 0 || start >= id {
					return nil, nil, configErr(fmt.Errorf("invalid field completion intent"))
				}
				// A completion may only discharge the same resource and fields.
				if intent, ok := attempts[start]; ok && intent["resource"] == event["resource"] && reflect.DeepEqual(crocoEventFields(intent["fields"]), crocoEventFields(event["fields"])) {
					completedAttempts[start] = true
				}
			}
		}
		if event["kind"] == "reindex" && event["resource"] == "filters" {
			d, _ := event["detail"].(map[string]any)
			if d["verified"] == true {
				n, e := crocoInt(d["reindex_event_id"])
				if e != nil || n >= id {
					return nil, nil, configErr(fmt.Errorf("invalid reindex receipt coverage"))
				}
				if n >= covered && id > receipt {
					covered = n
					receipt = id
				}
			}
		}
	}
	if since > last {
		return nil, nil, usageErr(fmt.Errorf("--since-event %d exceeds last recorded event %d", since, last))
	}
	result := []any{}
	counts := map[string]any{"candidate": 0, "covered_by_receipt": 0, "no_observed_dependency_change": 0, "unknown": 0, "inactive": 0}
	captured, _ := time.Parse(time.RFC3339Nano, cap.CapturedAt)
	for _, filter := range filters {
		id := crocoString(filter["ID"])
		if id == "" {
			return nil, nil, configErr(fmt.Errorf("filter snapshot has no ID"))
		}
		state := "no_observed_dependency_change"
		reason := "No matching changes in the selected local journal interval"
		matches := []any{}
		unknown := []any{}
		deps := []string{}
		source := crocoString(filter["_data_source"])
		for _, k := range []string{"_query_var", "_source_custom_field"} {
			v := crocoString(filter[k])
			if crocoFieldNameRE.MatchString(v) {
				deps = append(deps, v)
			}
		}
		known := cap.Versions["jet-smart-filters"] == "3.8.6.2" && source == "custom_fields" && len(deps) == 2
		if !known {
			state = "unknown"
			reason = "Provider or dependency schema is outside the verified custom-fields contract"
		}
		for _, event := range events {
			eid, _ := crocoInt(event["id"])
			if eid <= since || event["kind"] == "reindex" {
				continue
			}
			resource := crocoString(event["resource"])
			d, _ := event["detail"].(map[string]any)
			hit := false
			if resource == "filters" && crocoString(d["id"]) == id {
				at, e := time.Parse(time.RFC3339Nano, crocoString(event["created_at"]))
				if e != nil || at.After(captured) {
					unknown = append(unknown, eid)
					continue
				}
				hit = true
			}
			if strings.HasPrefix(resource, "fields:") && known {
				for _, field := range crocoEventFields(event["fields"]) {
					for _, dep := range deps {
						if field == dep {
							hit = true
						}
					}
				}
			}
			if strings.HasPrefix(resource, "engine:") || strings.HasPrefix(resource, "cct:") {
				unknown = append(unknown, eid)
			}
			if hit && event["kind"] == "post_fields_attempt" {
				if !completedAttempts[eid] {
					unknown = append(unknown, eid)
				}
				continue
			}
			if hit {
				matches = append(matches, map[string]any{"event_id": eid, "resource": resource, "covered_by_receipt": eid <= covered})
			}
		}
		uncovered := false
		for _, v := range matches {
			if v.(map[string]any)["covered_by_receipt"] != true {
				uncovered = true
			}
		}
		if known && len(matches) > 0 {
			if uncovered {
				state = "candidate"
				reason = "A recorded dependency change has no later covering reindex receipt"
			} else {
				state = "covered_by_receipt"
				reason = "Recorded dependency changes are covered by a verified local reindex receipt"
			}
		}
		if len(unknown) > 0 {
			state = "unknown"
			reason = "Unverified field attempts, structural/provider changes, or newer filter definitions require inspection"
		}
		if crocoString(filter["status"]) != "publish" {
			state = "inactive"
			reason = "Filter was not published in the captured status"
		}
		counts[state] = counts[state].(int) + 1
		result = append(result, map[string]any{"filter_id": id, "title": filter["title"], "status": state, "reason": reason, "dependencies": deps, "matching_events": matches, "unknown_events": unknown})
	}
	return result, map[string]any{"counts": counts, "since_event_exclusive": since, "last_event": last, "last_covering_receipt": receipt, "covered_through_event": covered, "scanned_events": len(events)}, nil
}
func newNovelFiltersImpactCmd(f *rootFlags) *cobra.Command {
	var dbPath string
	var since int64
	var limit int
	cmd := crocoLocalCommand(f, &cobra.Command{Use: "impact", Short: "Find filter index maintenance candidates from captured dependencies and recorded writes"}, func(ctx context.Context, cmd *cobra.Command, args []string) (any, error) {
		if len(args) > 0 || since < 0 || limit < 1 || limit > 10000 {
			return nil, usageErr(fmt.Errorf("filters impact takes no positional arguments; --since-event must be nonnegative and --limit 1..10000"))
		}
		cap, rows, e := crocoLoadCapture(ctx, dbPath, "filters")
		if e != nil {
			return nil, e
		}
		if e = crocoRequireComplete(cap); e != nil {
			return nil, e
		}
		if e = crocoRequireVersion(cap, "jet-smart-filters"); e != nil {
			return nil, e
		}
		path := dbPath
		if path == "" {
			path = crocoDBPath()
		}
		s, e := store.OpenReadOnlyContext(ctx, path)
		if e != nil {
			return nil, e
		}
		defer s.Close()
		events, e := s.CrocoEvents(ctx, 0)
		if e != nil {
			return nil, e
		}
		result, summary, e := crocoComputeFilterImpact(cap, rows, events, since)
		if e != nil {
			return nil, e
		}
		total := len(result)
		if total > limit {
			result = result[:limit]
		}
		return map[string]any{"filters": result, "total_filters": total, "truncated": total > limit, "capture": cap, "journal": summary, "note": "Only this CLI's recorded writes and receipts are observed. This report does not certify global index freshness or observe external changes."}, nil
	})
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite snapshot containing filter captures and this site's write journal")
	cmd.Flags().Int64Var(&since, "since-event", 0, "Consider write events strictly after this local journal sequence")
	cmd.Flags().IntVar(&limit, "limit", 500, "Maximum filter assessments returned after evaluating the captured set")
	cmd.Annotations["mcp:read-flags"] = "db"
	cmd.Annotations["pp:verified-by-proof"] = "crocoblock-local-analysis-lifecycle.md"
	cmd.Example = "  crocoblock-pp-cli filters impact --db candidate.db --since-event 1 --agent"
	return cmd
}
