// pp:data-source local
package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
)

func newNovelFormsFieldUsageCmd(f *rootFlags) *cobra.Command {
	var db string
	var limit int
	cmd := crocoLocalCommand(f, &cobra.Command{Use: "field-usage <id>", Short: "Compare declared fields with observed names in captured saved records"}, func(ctx context.Context, cmd *cobra.Command, args []string) (any, error) {
		id, e := crocoOneID(args)
		if e != nil {
			return nil, e
		}
		if limit < 1 || limit > 10000 {
			return nil, usageErr(fmt.Errorf("--limit must be between 1 and 10000"))
		}
		declarations, fields, e := crocoLoadCapture(ctx, db, "forms:fields:"+id)
		if e != nil {
			return nil, e
		}
		observations, records, e := crocoLoadCapture(ctx, db, "records:"+id)
		if e != nil {
			return nil, e
		}
		return crocoFieldUsage(id, declarations, fields, observations, records, limit)
	})
	cmd.Long = "Use this command for observed field-name coverage in selected saved submissions compared with the current form definition. Do NOT use this command for changes to the form definition between releases; use 'crocoblock-pp-cli schema diff' instead. This local analysis requires complete captures for both the form declaration and its saved records. It never infers submissions that were not captured or recommends deleting fields from an empty sample."
	cmd.Example = "  crocoblock-pp-cli sync --resources forms,records --record-form 42 --db candidate.db --full --strict --agent\n  crocoblock-pp-cli forms field-usage 42 --db candidate.db --limit 100 --agent"
	cmd.Flags().StringVar(&db, "db", "", "SQLite snapshot to inspect; defaults to CROCOBLOCK_DB or the CLI database")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum field rows to print (1–10000); totals still cover the full capture")
	cmd.Annotations["mcp:read-flags"] = "db"
	cmd.Annotations["pp:verified-by-proof"] = "crocoblock-local-analysis-lifecycle.md"
	return cmd
}

type crocoFieldUsageCount struct {
	Type           string
	Declared       bool
	Present, Empty int
}

// This calculation consumes names and boolean presence only. Submitted values,
// labels, HTML column renderings and unrelated record metadata never enter output.
func crocoFieldUsage(id string, decl store.CrocoCapture, fields []map[string]any, obs store.CrocoCapture, records []map[string]any, limit int) (map[string]any, error) {
	if _, e := crocoOneID([]string{id}); e != nil {
		return nil, e
	}
	if limit < 1 || limit > 10000 {
		return nil, usageErr(fmt.Errorf("--limit must be between 1 and 10000"))
	}
	if decl.Resource != "forms:fields:"+id || obs.Resource != "records:"+id {
		return nil, configErr(fmt.Errorf("coverage unknown: capture resources do not match selected form %s", id))
	}
	for _, cap := range []store.CrocoCapture{decl, obs} {
		if e := crocoRequireComplete(cap); e != nil {
			return nil, e
		}
		if e := crocoRequireVersion(cap, "jetformbuilder"); e != nil {
			return nil, e
		}
		if crocoString(cap.Scope["form_id"]) != id {
			return nil, configErr(fmt.Errorf("coverage unknown: %s capture scope does not match selected form %s", cap.Resource, id))
		}
		if _, e := time.Parse(time.RFC3339Nano, cap.CapturedAt); e != nil {
			return nil, configErr(fmt.Errorf("coverage unknown: invalid capture timestamp for %s", cap.Resource))
		}
	}
	if strings.TrimRight(decl.Site, "/") != strings.TrimRight(obs.Site, "/") {
		return nil, configErr(fmt.Errorf("coverage unknown: declarations and records belong to different sites"))
	}
	if decl.Versions["jetformbuilder"] != obs.Versions["jetformbuilder"] {
		return nil, configErr(fmt.Errorf("schema compatibility unknown: JetFormBuilder capture versions differ"))
	}
	if e := crocoFieldUsageCoverage(decl, []string{"name", "type"}); e != nil {
		return nil, e
	}
	if e := crocoFieldUsageCoverage(obs, []string{"record_id", "form_id", "fields.name", "fields.present"}); e != nil {
		return nil, e
	}
	if values, ok := obs.Scope["values"].(bool); !ok || values {
		return nil, configErr(fmt.Errorf("coverage unknown: records capture must explicitly exclude submitted values"))
	}
	counters := map[string]*crocoFieldUsageCount{}
	for _, field := range fields {
		name, ok := field["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, configErr(fmt.Errorf("coverage unknown: declared field name is missing"))
		}
		typ, ok := field["type"].(string)
		if !ok || typ == "" {
			return nil, configErr(fmt.Errorf("coverage unknown: declared field %s lacks its type", name))
		}
		if _, exists := counters[name]; exists {
			return nil, configErr(fmt.Errorf("coverage unknown: duplicate declared field %s", name))
		}
		counters[name] = &crocoFieldUsageCount{Type: typ, Declared: true}
	}
	recordIDs := map[string]bool{}
	for _, record := range records {
		recordID := crocoString(record["record_id"])
		if _, e := crocoOneID([]string{recordID}); e != nil {
			return nil, configErr(fmt.Errorf("coverage unknown: record lacks a valid identity"))
		}
		if recordIDs[recordID] {
			return nil, configErr(fmt.Errorf("coverage unknown: duplicate captured record ID %s", recordID))
		}
		recordIDs[recordID] = true
		if crocoString(record["form_id"]) != id {
			return nil, configErr(fmt.Errorf("coverage unknown: record %s belongs to another or unknown form", recordID))
		}
		observed, ok := record["fields"].([]any)
		if !ok {
			return nil, configErr(fmt.Errorf("coverage unknown: record %s is missing its field-presence capture", recordID))
		}
		seen := map[string]bool{}
		for _, v := range observed {
			field, ok := v.(map[string]any)
			if !ok {
				return nil, configErr(fmt.Errorf("coverage unknown: record %s has a malformed field", recordID))
			}
			name, ok := field["name"].(string)
			if !ok || strings.TrimSpace(name) == "" {
				return nil, configErr(fmt.Errorf("coverage unknown: record %s has an unnamed field", recordID))
			}
			present, ok := field["present"].(bool)
			if !ok {
				return nil, configErr(fmt.Errorf("coverage unknown: record %s field %s lacks explicit boolean presence", recordID, name))
			}
			if seen[name] {
				return nil, configErr(fmt.Errorf("coverage unknown: record %s has duplicate field %s", recordID, name))
			}
			seen[name] = true
			counter, exists := counters[name]
			if !exists {
				counter = &crocoFieldUsageCount{}
				counters[name] = counter
			}
			if present {
				counter.Present++
			} else {
				counter.Empty++
			}
		}
	}
	names := make([]string, 0, len(counters))
	for k := range counters {
		names = append(names, k)
	}
	sort.Strings(names)
	rows := []any{}
	declaredTotal, observedOnlyTotal, unusedTotal := 0, 0, 0
	for _, name := range names {
		counter := counters[name]
		if counter.Declared {
			declaredTotal++
		} else {
			observedOnlyTotal++
		}
		unused := len(records) > 0 && counter.Declared && counter.Present == 0
		if unused {
			unusedTotal++
		}
		classification := "declared"
		if !counter.Declared {
			classification = "observed_only"
		}
		var percent any
		if len(records) > 0 {
			percent = 100 * float64(counter.Present) / float64(len(records))
		}
		if len(rows) < limit {
			rows = append(rows, map[string]any{"name": name, "declared": counter.Declared, "type": counter.Type, "classification": classification, "records_seen": counter.Present + counter.Empty, "present": counter.Present, "empty": counter.Empty, "missing": len(records) - counter.Present - counter.Empty, "present_percent": percent, "percentage_denominator": len(records), "unused_in_sample": unused})
		}
	}
	notes := []any{"Counts describe only the complete captured set of saved records for this form; they do not measure retention or submissions that were never captured.", "present means the saved field existed with a nonempty value at capture time; empty means it existed with an empty value; missing means its name was absent from that saved record.", "observed_only fields were found in captured records but are absent from the captured declaration; they are review candidates, not automatic deletion recommendations.", "A declared field with no observed presence is not proof that the field should be removed."}
	if len(records) == 0 {
		notes = append(notes, "The captured sample is empty. All declared fields are shown with zero counts and an undefined percentage; usage remains unobserved.")
	}
	return map[string]any{"form_id": id, "coverage": "complete_for_captured_scope", "scope": "saved records captured for this form", "records_total": len(records), "empty_sample": len(records) == 0, "declared_fields_total": declaredTotal, "observed_only_fields_total": observedOnlyTotal, "unused_declared_fields_in_sample": unusedTotal, "fields_total": len(names), "fields_returned": len(rows), "limit": limit, "truncated": len(rows) < len(names), "fields": rows, "provenance": map[string]any{"site": decl.Site, "jetformbuilder_version": decl.Versions["jetformbuilder"], "declarations_captured_at": decl.CapturedAt, "records_captured_at": obs.CapturedAt, "declarations_resource": decl.Resource, "records_resource": obs.Resource}, "notes": notes}, nil
}
func crocoFieldUsageCoverage(cap store.CrocoCapture, required []string) error {
	found := map[string]bool{}
	for _, name := range cap.Fields {
		found[name] = true
	}
	for _, name := range required {
		if !found[name] {
			return configErr(fmt.Errorf("coverage unknown: %s capture does not include %s", cap.Resource, name))
		}
	}
	return nil
}
