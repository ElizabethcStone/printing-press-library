package cli

// pp:data-source local

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
	"reflect"
	"sort"
	"strings"
)

func crocoValueAt(m map[string]any, path string) (any, bool) {
	var v any = m
	for _, part := range strings.Split(path, ".") {
		o, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok = o[part]
		if !ok {
			return nil, false
		}
	}
	return v, true
}
func crocoSchemaRow(resource string, row map[string]any) (map[string]any, error) {
	b, _ := json.Marshal(crocoSafe(row))
	var out map[string]any
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	if e := d.Decode(&out); e != nil {
		return nil, e
	}
	for _, k := range []string{"date", "modified", "modified_gmt", "date_gmt", "settings_url", "items_url", "_links", "success"} {
		delete(out, k)
	}
	if resource == "engine:configuration" {
		for _, k := range []string{"post_types", "taxonomies", "meta_boxes", "queries", "relations", "custom_content_types"} {
			delete(out, k)
		}
	}
	if resource == "forms" {
		content, _ := out["content"].(map[string]any)
		fields, e := crocoParseFormFields(crocoString(content["raw"]))
		if e != nil {
			return nil, e
		}
		return map[string]any{"id": out["id"], "status": out["status"], "title": out["title"], "fields": fields}, nil
	}
	return out, nil
}
func crocoSchemaIndex(resource string, rows []map[string]any, key string, crossSite bool) (map[string]map[string]any, error) {
	index := map[string]map[string]any{}
	for _, row := range rows {
		identity := ""
		if resource == "engine:configuration" {
			identity = "configuration"
		} else {
			path := key
			if path == "" {
				if crossSite {
					return nil, usageErr(fmt.Errorf("cross-site comparison requires --identity %s=FIELD_PATH", resource))
				}
				path = "id"
				if row[path] == nil {
					path = "ID"
				}
			}
			v, ok := crocoValueAt(row, path)
			if !ok || v == nil || crocoString(v) == "" {
				return nil, usageErr(fmt.Errorf("%s row lacks identity field %s", resource, path))
			}
			switch v.(type) {
			case string, json.Number, float64, int, int64:
			default:
				return nil, usageErr(fmt.Errorf("identity %s must be scalar", path))
			}
			identity = crocoString(v)
		}
		if _, exists := index[identity]; exists {
			return nil, usageErr(fmt.Errorf("duplicate %s identity %q; comparison is ambiguous", resource, identity))
		}
		normalized, e := crocoSchemaRow(resource, row)
		if e != nil {
			return nil, e
		}
		if crossSite {
			delete(normalized, "id")
			delete(normalized, "ID")
		}
		index[identity] = normalized
	}
	return index, nil
}
func crocoSchemaChanges(resource, identity, path string, before, after any, out *[]any) {
	bm, bok := before.(map[string]any)
	am, aok := after.(map[string]any)
	if bok && aok {
		keys := map[string]bool{}
		for k := range bm {
			keys[k] = true
		}
		for k := range am {
			keys[k] = true
		}
		sorted := []string{}
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			bp, bexists := bm[k]
			ap, aexists := am[k]
			next := path + "/" + strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
			if !bexists || !aexists {
				kind := "added"
				if !aexists {
					kind = "removed"
				}
				*out = append(*out, map[string]any{"resource": resource, "identity": identity, "path": next, "change": kind, "before": bp, "after": ap})
				continue
			}
			crocoSchemaChanges(resource, identity, next, bp, ap, out)
		}
		return
	}
	if !reflect.DeepEqual(before, after) {
		*out = append(*out, map[string]any{"resource": resource, "identity": identity, "path": path, "change": "modified", "before": before, "after": after})
	}
}
func newNovelSchemaDiffCmd(f *rootFlags) *cobra.Command {
	var before, after string
	var resources, identities []string
	var limit int
	cmd := crocoLocalCommand(f, &cobra.Command{Use: "diff", Short: "Compare observed Crocoblock definitions in two complete local snapshots"}, func(ctx context.Context, cmd *cobra.Command, args []string) (any, error) {
		if before == "" || after == "" {
			return nil, usageErr(fmt.Errorf("--before and --after are required SQLite snapshot paths"))
		}
		if len(args) != 0 || limit < 1 || limit > 10000 {
			return nil, usageErr(fmt.Errorf("schema diff accepts no positional arguments; --limit must be 1..10000"))
		}
		identity := map[string]string{}
		for _, raw := range identities {
			k, v, ok := strings.Cut(raw, "=")
			if !ok || k == "" || v == "" {
				return nil, usageErr(fmt.Errorf("--identity must be resource=FIELD_PATH"))
			}
			if _, exists := identity[k]; exists {
				return nil, usageErr(fmt.Errorf("duplicate --identity for %s", k))
			}
			identity[k] = v
		}
		changes := []any{}
		provenance := []any{}
		versions := []any{}
		seen := map[string]bool{}
		scanned := 0
		for _, resource := range resources {
			if seen[resource] {
				return nil, usageErr(fmt.Errorf("duplicate resource %s", resource))
			}
			seen[resource] = true
			if !strings.HasPrefix(resource, "engine:") && resource != "forms" && resource != "filters" {
				return nil, usageErr(fmt.Errorf("schema diff accepts engine:* definitions, forms or filters; use cct reconcile for values"))
			}
			bc, br, e := crocoLoadCapture(ctx, before, resource)
			if e != nil {
				return nil, e
			}
			ac, ar, e := crocoLoadCapture(ctx, after, resource)
			if e != nil {
				return nil, e
			}
			plugin := "jet-engine"
			if resource == "forms" {
				plugin = "jetformbuilder"
			}
			if resource == "filters" {
				plugin = "jet-smart-filters"
			}
			for _, cap := range []store.CrocoCapture{bc, ac} {
				if e = crocoRequireComplete(cap); e != nil {
					return nil, e
				}
				if e = crocoRequireVersion(cap, plugin); e != nil {
					return nil, e
				}
			}
			if bc.Versions[plugin] != ac.Versions[plugin] {
				versions = append(versions, map[string]any{"resource": resource, "plugin": plugin, "before": bc.Versions[plugin], "after": ac.Versions[plugin], "compatibility": "unknown"})
			}
			bi, e := crocoSchemaIndex(resource, br, identity[resource], bc.Site != ac.Site)
			if e != nil {
				return nil, e
			}
			ai, e := crocoSchemaIndex(resource, ar, identity[resource], bc.Site != ac.Site)
			if e != nil {
				return nil, e
			}
			scanned += len(br) + len(ar)
			keys := map[string]bool{}
			for k := range bi {
				keys[k] = true
			}
			for k := range ai {
				keys[k] = true
			}
			ids := []string{}
			for k := range keys {
				ids = append(ids, k)
			}
			sort.Strings(ids)
			for _, id := range ids {
				b, bok := bi[id]
				a, aok := ai[id]
				if !bok || !aok {
					kind := "added"
					if !aok {
						kind = "removed"
					}
					changes = append(changes, map[string]any{"resource": resource, "identity": id, "path": "/", "change": kind, "before": b, "after": a})
				} else {
					crocoSchemaChanges(resource, id, "", b, a, &changes)
				}
			}
			provenance = append(provenance, map[string]any{"resource": resource, "before": bc, "after": ac})
		}
		total := len(changes)
		if total > limit {
			changes = changes[:limit]
		}
		status := "compared"
		if len(versions) > 0 {
			status = "unknown_compatibility"
		}
		return map[string]any{"status": status, "changes": changes, "total_changes": total, "truncated": total > limit, "scanned_definitions": scanned, "captures": provenance, "version_changes": versions, "note": "Observed definition differences only. Captures are not atomic site backups; action values and credentials are excluded."}, nil
	})
	cmd.Flags().StringVar(&before, "before", "", "Earlier complete SQLite snapshot to compare")
	cmd.Flags().StringVar(&after, "after", "", "Later complete SQLite snapshot to compare")
	cmd.Flags().StringSliceVar(&resources, "resources", []string{"engine:configuration", "engine:cct", "engine:post-types", "engine:taxonomies", "engine:meta-boxes", "engine:queries", "engine:relations", "forms", "filters"}, "Definition resources included in both snapshots")
	cmd.Flags().StringArrayVar(&identities, "identity", nil, "Explicit cross-site identity mapping resource=FIELD_PATH, repeat per collection")
	cmd.Flags().IntVar(&limit, "limit", 500, "Maximum differences returned after comparing all selected definitions")
	cmd.Annotations["mcp:read-flags"] = "before,after"
	cmd.Annotations["pp:verified-by-proof"] = "crocoblock-local-analysis-lifecycle.md"
	cmd.Example = "  crocoblock-pp-cli schema diff --before baseline.db --after candidate.db --agent\n  crocoblock-pp-cli schema diff --before baseline.db --after candidate.db --resources engine:cct --identity engine:cct=slug --json"
	return cmd
}
