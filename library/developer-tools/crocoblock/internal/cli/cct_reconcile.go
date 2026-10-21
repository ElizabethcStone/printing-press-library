package cli

// pp:data-source local

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
)

type crocoReconcileSnapshot struct {
	Capture       store.CrocoCapture
	SchemaCapture store.CrocoCapture
	Rows          []map[string]any
	Definitions   []map[string]any
}

func newNovelCctReconcileCmd(f *rootFlags) *cobra.Command { return newCrocoReconcileCommand(f) }
func newCrocoReconcileCommand(f *rootFlags) *cobra.Command {
	var before, after, key string
	var fields []string
	limit := 100
	cmd := crocoLocalCommand(f, &cobra.Command{Use: "reconcile <type>", Short: "Compare complete CCT snapshots by an explicit unique key"}, func(ctx context.Context, cmd *cobra.Command, args []string) (any, error) {
		if len(args) != 1 {
			return nil, usageErr(fmt.Errorf("cct reconcile requires one CCT slug and --before, --after, --key and --fields"))
		}
		slug, e := crocoSlug(args[0])
		if e != nil {
			return nil, e
		}
		if before == "" || after == "" {
			return nil, usageErr(fmt.Errorf("--before and --after must identify the two SQLite snapshots"))
		}
		if e = crocoReconcileInput(key, fields, limit); e != nil {
			return nil, e
		}
		load := func(path string) (crocoReconcileSnapshot, error) {
			var s crocoReconcileSnapshot
			var e error
			s.Capture, s.Rows, e = crocoLoadCapture(ctx, path, "cct:"+slug)
			if e != nil {
				return s, e
			}
			s.SchemaCapture, s.Definitions, e = crocoLoadCapture(ctx, path, "engine:cct")
			return s, e
		}
		b, e := load(before)
		if e != nil {
			return nil, e
		}
		a, e := load(after)
		if e != nil {
			return nil, e
		}
		return crocoComputeReconcile(b, a, slug, key, fields, limit)
	})
	cmd.Long = "Use this command for record-level differences between two complete snapshots of one CCT. Do NOT use this command for changed field definitions; use 'crocoblock-pp-cli schema diff' instead. Both snapshots need schema and the selected CCT fields. Matching requires identical JetEngine versions and compatible selected field definitions. Output includes row IDs and changed field names; field values and matching-key values are never printed. Missing or duplicate keys are reported as issues and never paired arbitrarily. --limit bounds displayed differences and issues; scanned counts cover the whole captures."
	cmd.Flags().StringVar(&before, "before", "", "SQLite snapshot containing the earlier complete CCT collection")
	cmd.Flags().StringVar(&after, "after", "", "SQLite snapshot containing the later complete CCT collection")
	cmd.Flags().StringVar(&key, "key", "", "Explicit unique business-key field present in both captures")
	cmd.Flags().StringSliceVar(&fields, "fields", nil, "Explicit captured field names whose values should be compared")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum differences and issues displayed, from 1 to 500")
	cmd.Annotations["mcp:read-flags"] = "before,after"
	cmd.Annotations["pp:verified-by-proof"] = "crocoblock-local-analysis-lifecycle.md"
	cmd.Example = "  crocoblock-pp-cli cct reconcile rooms --before before.db --after after.db --key external_id --fields title,price --agent"
	return cmd
}

var crocoReconcileFieldRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func crocoReconcileInput(key string, fields []string, limit int) error {
	if e := crocoPageBounds(limit, 0); e != nil {
		return e
	}
	if key == "" || len(fields) == 0 {
		return usageErr(fmt.Errorf("--key and --fields are required; choose explicitly captured fields"))
	}
	seen := map[string]bool{}
	for _, name := range append([]string{key}, fields...) {
		if !crocoReconcileFieldRE.MatchString(name) {
			return usageErr(fmt.Errorf("invalid --key/--fields name %q", name))
		}
		if crocoSensitive(name) {
			return usageErr(fmt.Errorf("sensitive field %q cannot be analyzed", name))
		}
	}
	for _, name := range fields {
		if seen[name] {
			return usageErr(fmt.Errorf("duplicate --fields name %q", name))
		}
		seen[name] = true
	}
	return nil
}
func crocoReconcileSchema(s crocoReconcileSnapshot, slug string, selected []string) (map[string]any, error) {
	for _, cap := range []store.CrocoCapture{s.Capture, s.SchemaCapture} {
		if e := crocoRequireComplete(cap); e != nil {
			return nil, e
		}
		if e := crocoRequireVersion(cap, "jet-engine"); e != nil {
			return nil, e
		}
	}
	if s.Capture.Resource != "cct:"+slug || s.SchemaCapture.Resource != "engine:cct" {
		return nil, configErr(fmt.Errorf("snapshot resources do not match the selected CCT and engine:cct schema"))
	}
	if s.Capture.Site != s.SchemaCapture.Site || s.Capture.Versions["jet-engine"] != s.SchemaCapture.Versions["jet-engine"] {
		return nil, configErr(fmt.Errorf("CCT capture and schema provenance differ; sync both from the same site and JetEngine version"))
	}
	if !reflect.DeepEqual(s.Capture.Scope, map[string]any{"type": slug}) {
		return nil, configErr(fmt.Errorf("CCT scope must be the complete unfiltered type %s; restricted or unknown scope cannot be reconciled", slug))
	}
	available := map[string]bool{}
	for _, k := range s.Capture.Fields {
		available[k] = true
	}
	for _, k := range append([]string{"_ID"}, selected...) {
		if !available[k] {
			return nil, configErr(fmt.Errorf("field %q was not captured for %s; sync it explicitly", k, s.Capture.Resource))
		}
	}
	var definition map[string]any
	for _, d := range s.Definitions {
		args, _ := d["args"].(map[string]any)
		name := crocoString(d["slug"])
		if name == "" {
			name = crocoString(args["slug"])
		}
		if name == slug {
			if definition != nil {
				return nil, configErr(fmt.Errorf("duplicate CCT schema for %s", slug))
			}
			definition = d
		}
	}
	if definition == nil {
		return nil, configErr(fmt.Errorf("CCT %s is missing from engine:cct; sync schema first", slug))
	}
	defs := map[string]any{"_ID": map[string]any{"type": "integer"}, "cct_status": map[string]any{"type": "string"}}
	raw, ok := definition["meta_fields"].([]any)
	if !ok {
		return nil, configErr(fmt.Errorf("CCT %s field schema is missing or malformed", slug))
	}
	for _, v := range raw {
		d, ok := v.(map[string]any)
		if !ok {
			return nil, configErr(fmt.Errorf("CCT field definition is malformed"))
		}
		name := crocoString(d["name"])
		if name == "" {
			continue
		}
		if _, exists := defs[name]; exists {
			return nil, configErr(fmt.Errorf("CCT field schema repeats %q", name))
		}
		clean := map[string]any{}
		for k, v := range d {
			if k != "title" && k != "description" {
				clean[k] = v
			}
		}
		defs[name] = clean
	}
	out := map[string]any{}
	for _, name := range selected {
		d, ok := defs[name]
		if !ok {
			return nil, configErr(fmt.Errorf("selected field %q is absent from the CCT schema", name))
		}
		if m, ok := d.(map[string]any); !ok || crocoString(m["type"]) == "" {
			return nil, configErr(fmt.Errorf("selected field %q has no declared type", name))
		}
		out[name] = d
	}
	return out, nil
}
func crocoReconcileIdentity(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "", false
		}
	case json.Number:
		if _, e := v.Float64(); e != nil {
			return "", false
		}
	default:
		return "", false
	}
	b, e := json.Marshal(value)
	return string(b), e == nil
}
func crocoComputeReconcile(before, after crocoReconcileSnapshot, slug, key string, fields []string, limit int) (map[string]any, error) {
	if _, e := crocoSlug(slug); e != nil {
		return nil, e
	}
	if e := crocoReconcileInput(key, fields, limit); e != nil {
		return nil, e
	}
	selected := append([]string{key}, fields...)
	bSchema, e := crocoReconcileSchema(before, slug, selected)
	if e != nil {
		return nil, e
	}
	aSchema, e := crocoReconcileSchema(after, slug, selected)
	if e != nil {
		return nil, e
	}
	if before.Capture.Versions["jet-engine"] != after.Capture.Versions["jet-engine"] {
		return nil, configErr(fmt.Errorf("incompatible JetEngine versions; this comparison requires the same exact version"))
	}
	if !reflect.DeepEqual(bSchema, aSchema) {
		return nil, configErr(fmt.Errorf("selected CCT field schemas are incompatible; use schema diff first"))
	}
	if before.Capture.Site != after.Capture.Site && key == "_ID" {
		return nil, usageErr(fmt.Errorf("cross-site reconciliation requires a business key, not the site-local _ID"))
	}
	issues := []any{}
	differences := []any{}
	unknownIdentity := false
	index := func(rows []map[string]any, side string) (map[string][]map[string]any, error) {
		byKey := map[string][]map[string]any{}
		seenIDs := map[string]bool{}
		for _, row := range rows {
			id, e := crocoID([]string{crocoString(row["_ID"])}, 0)
			if e != nil {
				return nil, configErr(fmt.Errorf("%s CCT row lacks a valid _ID", side))
			}
			if seenIDs[id] {
				return nil, configErr(fmt.Errorf("%s CCT capture repeats row ID %s", side, id))
			}
			seenIDs[id] = true
			for _, name := range fields {
				if _, exists := row[name]; !exists && name != key {
					return nil, configErr(fmt.Errorf("%s row %s lacks selected field %q; field coverage is incomplete", side, id, name))
				}
			}
			value, present := row[key]
			identity, valid := crocoReconcileIdentity(value)
			if !present || !valid {
				code := "invalid_key"
				if !present || value == nil || value == "" {
					code = "missing_key"
				}
				issues = append(issues, map[string]any{"kind": code, "side": side, "id": id, "field": key})
				unknownIdentity = true
				continue
			}
			byKey[identity] = append(byKey[identity], row)
		}
		return byKey, nil
	}
	b, e := index(before.Rows, "before")
	if e != nil {
		return nil, e
	}
	a, e := index(after.Rows, "after")
	if e != nil {
		return nil, e
	}
	keys := map[string]bool{}
	for k := range b {
		keys[k] = true
	}
	for k := range a {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	counts := map[string]int{"created": 0, "deleted": 0, "changed": 0, "unchanged": 0, "ambiguous_keys": 0, "unmatched_suppressed": 0}
	for _, k := range ordered {
		bs, as := b[k], a[k]
		if len(bs) > 1 || len(as) > 1 {
			ids := func(rows []map[string]any) []string {
				out := []string{}
				for _, r := range rows {
					out = append(out, crocoString(r["_ID"]))
				}
				sort.Strings(out)
				return out
			}
			issues = append(issues, map[string]any{"kind": "duplicate_key", "field": key, "before_ids": ids(bs), "after_ids": ids(as)})
			counts["ambiguous_keys"]++
			continue
		}
		difference := map[string]any{"before_id": nil, "after_id": nil, "fields": []string{}}
		if len(bs) == 0 || len(as) == 0 {
			if unknownIdentity {
				counts["unmatched_suppressed"]++
				continue
			}
			if len(bs) == 0 {
				difference["kind"] = "created"
				difference["after_id"] = crocoString(as[0]["_ID"])
				counts["created"]++
			} else {
				difference["kind"] = "deleted"
				difference["before_id"] = crocoString(bs[0]["_ID"])
				counts["deleted"]++
			}
			differences = append(differences, difference)
			continue
		}
		changed := []string{}
		for _, name := range fields {
			if !reflect.DeepEqual(bs[0][name], as[0][name]) {
				changed = append(changed, name)
			}
		}
		if len(changed) == 0 {
			counts["unchanged"]++
			continue
		}
		sort.Strings(changed)
		difference["kind"] = "changed"
		difference["before_id"] = crocoString(bs[0]["_ID"])
		difference["after_id"] = crocoString(as[0]["_ID"])
		difference["fields"] = changed
		counts["changed"]++
		differences = append(differences, difference)
	}
	totalDifferences, totalIssues := len(differences), len(issues)
	complete := totalIssues == 0
	if len(differences) > limit {
		differences = differences[:limit]
	}
	if len(issues) > limit {
		issues = issues[:limit]
	}
	return map[string]any{"type": slug, "key_field": key, "fields": fields, "before": before.Capture, "after": after.Capture, "before_schema": before.SchemaCapture, "after_schema": after.SchemaCapture, "cross_site": before.Capture.Site != after.Capture.Site, "analysis_complete": complete, "differences": differences, "issues": issues, "counts": counts, "scanned": map[string]int{"before_rows": len(before.Rows), "after_rows": len(after.Rows)}, "total_differences": totalDifferences, "total_issues": totalIssues, "truncated": totalDifferences > limit || totalIssues > limit, "limit": limit, "value_policy": "Field values and matching-key values are excluded; changes identify row IDs and field names only", "compatibility": "Identical JetEngine versions and selected field definitions, excluding display title and description"}, nil
}
