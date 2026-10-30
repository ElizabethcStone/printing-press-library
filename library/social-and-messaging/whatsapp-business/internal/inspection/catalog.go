package inspection

import (
	"fmt"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/captures"
	"sort"
	"strings"
)

// CatalogDrift reports observed tool/schema changes without claiming complete
// compatibility for arbitrary JSON Schema or incomplete catalogs.
func CatalogDrift(before, after captures.Snapshot) (map[string]any, error) {
	for _, s := range []captures.Snapshot{before, after} {
		if err := explicit(s); err != nil {
			return nil, err
		}
		if s.Tool != "catalog" && s.Tool != "tools/list" {
			return nil, fmt.Errorf("catalog drift requires saved tools/list catalogs")
		}
	}
	if !equal(before.Scope, after.Scope) || text(before.Scope["endpoint"]) == "" || text(before.Scope["protocol"]) == "" {
		return nil, fmt.Errorf("catalogs must identify the same endpoint, protocol and exact scope")
	}
	a, err := catalogTools(before.Data)
	if err != nil {
		return nil, err
	}
	b, err := catalogTools(after.Data)
	if err != nil {
		return nil, err
	}
	added, removed, unobserved := []string{}, []string{}, []map[string]any{}
	breaking, schemas, descriptions, metadata := []map[string]any{}, []map[string]any{}, []map[string]any{}, []map[string]any{}
	all := map[string]any{}
	for k := range a {
		all[k] = nil
	}
	for k := range b {
		all[k] = nil
	}
	full := before.Complete && after.Complete
	for _, name := range keys(all) {
		left, lok := a[name]
		right, rok := b[name]
		if !lok || !rok {
			if !full {
				side := "before"
				if !rok {
					side = "after"
				}
				unobserved = append(unobserved, map[string]any{"tool": name, "missing_from": side})
				continue
			}
			if !lok {
				added = append(added, name)
			} else {
				removed = append(removed, name)
				breaking = append(breaking, map[string]any{"tool": name, "kind": "removed_tool"})
			}
			continue
		}
		toolSchemaChanges := 0
		for _, schema := range []string{"inputSchema", "outputSchema"} {
			for _, change := range diffValues(left[schema], right[schema], "/"+schema, true) {
				change["tool"] = name
				if strings.HasSuffix(text(change["path"]), "/description") {
					descriptions = append(descriptions, change)
				} else {
					schemas = append(schemas, change)
					toolSchemaChanges++
				}
			}
		}
		if !equal(left["description"], right["description"]) {
			descriptions = append(descriptions, map[string]any{"tool": name, "path": "/description", "before": left["description"], "after": right["description"], "description_only": toolSchemaChanges == 0 && equal(left["annotations"], right["annotations"])})
		}
		for _, k := range []string{"annotations", "title", "icons", "_meta"} {
			if !equal(left[k], right[k]) {
				metadata = append(metadata, map[string]any{"tool": name, "path": "/" + k, "before": left[k], "after": right[k]})
			}
		}
		var required func(any, any, string)
		required = func(x, y any, path string) {
			xm, ym := object(x), object(y)
			if ym != nil {
				old := stringSet(xm["required"])
				for _, field := range sortedSet(stringSet(ym["required"])) {
					if !old[field] {
						breaking = append(breaking, map[string]any{"tool": name, "kind": "new_required_input", "path": path + "/required", "parameter": field})
					}
				}
				for _, k := range keys(ym) {
					if k != "required" {
						required(xm[k], ym[k], path+"/"+pointer(k))
					}
				}
			} else if list, ok := y.([]any); ok {
				old, _ := x.([]any)
				for i, v := range list {
					var prior any
					if i < len(old) {
						prior = old[i]
					}
					required(prior, v, fmt.Sprintf("%s/%d", path, i))
				}
			}
		}
		required(left["inputSchema"], right["inputSchema"], "/inputSchema")
	}
	return map[string]any{"before": evidence(before), "after": evidence(after), "complete": full, "added_tools": added, "removed_tools": removed, "unobserved_tools": unobserved, "potentially_breaking": breaking, "schema_changes": schemas, "description_changes": descriptions, "metadata_changes": metadata, "note": "Potentially breaking findings are conservative signals, not a complete JSON Schema compatibility proof."}, nil
}
func catalogTools(v any) (map[string]map[string]any, error) {
	v, err := normalized(v)
	if err != nil {
		return nil, err
	}
	v = unwrap(v)
	m := object(v)
	if m != nil {
		v = m["tools"]
	}
	result := map[string]map[string]any{}
	add := func(name string, entry any) error {
		t := object(entry)
		if t == nil {
			return fmt.Errorf("catalog tool must be an object")
		}
		if actual := text(t["name"]); actual != "" {
			if name != "" && actual != name {
				return fmt.Errorf("catalog registry key does not match tool name")
			}
			name = actual
		}
		if name == "" {
			return fmt.Errorf("catalog tool is missing its name")
		}
		if _, exists := result[name]; exists {
			return fmt.Errorf("catalog contains duplicate tool %q", name)
		}
		result[name] = t
		return nil
	}
	switch rows := v.(type) {
	case []any:
		for _, row := range rows {
			if err := add("", row); err != nil {
				return nil, err
			}
		}
	case map[string]any:
		for _, k := range keys(rows) {
			if err := add(k, rows[k]); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("saved catalog must contain a tools array or object")
	}
	return result, nil
}
func stringSet(v any) map[string]bool {
	out := map[string]bool{}
	for _, value := range asSlice(v) {
		if s, ok := value.(string); ok {
			out[s] = true
		}
	}
	return out
}
func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
