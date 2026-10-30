// Package inspection computes local, evidence-preserving views of MCP captures.
package inspection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/captures"
)

func normalized(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("capture must contain JSON: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var result any
	if err := d.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}
func equal(a, b any) bool {
	x, e := json.Marshal(a)
	y, f := json.Marshal(b)
	return e == nil && f == nil && bytes.Equal(x, y)
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func text(v any) string           { s, _ := v.(string); return s }
func pointer(s string) string     { return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1") }
func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func evidence(s captures.Snapshot) map[string]any {
	return map[string]any{"capture_id": s.ID, "source_tool": s.Tool, "scope": s.Scope, "captured_at": s.CapturedAt, "complete": s.Complete}
}
func explicit(s captures.Snapshot) error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("an explicit saved capture ID is required")
	}
	return nil
}

func unwrap(v any) any {
	for depth := 0; depth < 5; depth++ {
		m := object(v)
		if m == nil {
			return v
		}
		next, ok := m["result"]
		if !ok {
			return v
		}
		if s, ok := next.(string); ok {
			var parsed any
			d := json.NewDecoder(strings.NewReader(s))
			d.UseNumber()
			if d.Decode(&parsed) != nil {
				return v
			}
			next = parsed
		}
		if len(m) != 1 {
			return v
		}
		v = next
	}
	return v
}

func diffValues(a, b any, path string, full bool) []map[string]any {
	out := make([]map[string]any, 0)
	var walk func(any, bool, any, bool, string)
	walk = func(a any, ha bool, b any, hb bool, path string) {
		if !ha || !hb {
			kind := "added"
			if !hb {
				kind = "removed"
			}
			if !full {
				if !ha {
					kind = "unobserved_before"
				} else {
					kind = "unobserved_after"
				}
			}
			change := map[string]any{"path": path, "kind": kind}
			if ha {
				change["before"] = a
			}
			if hb {
				change["after"] = b
			}
			out = append(out, change)
			return
		}
		if equal(a, b) {
			return
		}
		am, aok := a.(map[string]any)
		bm, bok := b.(map[string]any)
		if aok && bok {
			union := map[string]any{}
			for k := range am {
				union[k] = nil
			}
			for k := range bm {
				union[k] = nil
			}
			for _, k := range keys(union) {
				av, ae := am[k]
				bv, be := bm[k]
				walk(av, ae, bv, be, path+"/"+pointer(k))
			}
			return
		}
		aa, aok := a.([]any)
		ba, bok := b.([]any)
		if aok && bok {
			if key := identityKey(aa, ba); key != "" {
				left, right := map[string]any{}, map[string]any{}
				lo, ro := []string{}, []string{}
				for _, v := range aa {
					id := text(object(v)[key])
					left[id] = v
					lo = append(lo, id)
				}
				for _, v := range ba {
					id := text(object(v)[key])
					right[id] = v
					ro = append(ro, id)
				}
				union := map[string]any{}
				for k := range left {
					union[k] = nil
				}
				for k := range right {
					union[k] = nil
				}
				for _, id := range keys(union) {
					av, ae := left[id]
					bv, be := right[id]
					walk(av, ae, bv, be, path+"/"+pointer(key+"="+id))
				}
				if len(left) == len(right) && !reflect.DeepEqual(lo, ro) {
					same := true
					for k := range left {
						if _, ok := right[k]; !ok {
							same = false
						}
					}
					if same {
						out = append(out, map[string]any{"path": path + "/$order", "kind": "reordered", "before": lo, "after": ro})
					}
				}
				return
			}
			n := len(aa)
			if len(ba) > n {
				n = len(ba)
			}
			for i := 0; i < n; i++ {
				var av, bv any
				if i < len(aa) {
					av = aa[i]
				}
				if i < len(ba) {
					bv = ba[i]
				}
				walk(av, i < len(aa), bv, i < len(ba), path+"/"+strconv.Itoa(i))
			}
			return
		}
		out = append(out, map[string]any{"path": path, "kind": "changed", "before": a, "after": b})
	}
	walk(a, true, b, true, path)
	return out
}
func identityKey(a, b []any) string {
	if len(a)+len(b) == 0 {
		return ""
	}
	for _, k := range []string{"phone_number_id", "template_id", "account_id", "business_id", "id", "step"} {
		valid := true
		for _, list := range [][]any{a, b} {
			seen := map[string]bool{}
			for _, v := range list {
				id := text(object(v)[k])
				if id == "" || seen[id] {
					valid = false
					break
				}
				seen[id] = true
			}
			if !valid {
				break
			}
		}
		if valid {
			return k
		}
	}
	return ""
}

// Changes compares two explicit captures of the identical tool and selection.
// Missing content becomes an observation gap whenever either side is partial.
func Changes(before, after captures.Snapshot) (map[string]any, error) {
	if err := explicit(before); err != nil {
		return nil, err
	}
	if err := explicit(after); err != nil {
		return nil, err
	}
	if before.Tool != after.Tool || !equal(before.Scope, after.Scope) {
		return nil, fmt.Errorf("captures must have the same source tool and exact scope/filter metadata")
	}
	a, err := normalized(before.Data)
	if err != nil {
		return nil, err
	}
	b, err := normalized(after.Data)
	if err != nil {
		return nil, err
	}
	beforeBusiness, err := effectiveBusiness(before, a)
	if err != nil {
		return nil, err
	}
	afterBusiness, err := effectiveBusiness(after, b)
	if err != nil {
		return nil, err
	}
	// An omitted request business_id may resolve differently after a login or
	// asset change. A missing identity on one side cannot prove equal scope.
	if beforeBusiness != afterBusiness {
		return nil, fmt.Errorf("captures do not establish the same effective business selection")
	}
	return map[string]any{"before": evidence(before), "after": evidence(after), "complete": before.Complete && after.Complete, "changes": diffValues(a, b, "", before.Complete && after.Complete)}, nil
}

// effectiveBusiness reads only a known scoped tool's explicit request and its
// top-level response selection. Nested account rows can belong to customers of
// the selected partner business, so their business_id is never a scope source.
func effectiveBusiness(s captures.Snapshot, payload any) (string, error) {
	switch s.Tool {
	case "whatsapp_biz_accounts", "whatsapp_biz_phone_numbers", "whatsapp_biz_onboarding_status",
		"whatsapp_biz_get_template", "whatsapp_biz_list_templates", "whatsapp_biz_publish_app",
		"whatsapp_biz_verify_business":
	default:
		return "", nil // Includes the global businesses and template-library lists.
	}
	requested := strings.TrimSpace(text(s.Scope["business_id"]))
	response := object(unwrap(payload))
	// Normalize can preserve a result envelope alongside warnings or metadata.
	// Follow only that documented envelope, never arbitrary data/row objects.
	if _, exists := response["business_id"]; !exists {
		if result, ok := response["result"]; ok {
			response = object(unwrap(map[string]any{"result": result}))
		}
	}
	resolved := strings.TrimSpace(text(response["business_id"]))
	if requested != "" && resolved != "" && requested != resolved {
		return "", fmt.Errorf("captured response business_id conflicts with requested business_id")
	}
	if resolved != "" {
		return resolved, nil
	}
	if requested != "" {
		return requested, nil
	}
	// No selected business is established by two empty identities. A known
	// tool-specific asset selector can still establish an exact stable scope.
	selectors := map[string][]string{
		"whatsapp_biz_accounts":          {"phone_number_id"},
		"whatsapp_biz_phone_numbers":     {"waba_id", "phone_number"},
		"whatsapp_biz_get_template":      {"waba_id"},
		"whatsapp_biz_list_templates":    {"waba_id"},
		"whatsapp_biz_onboarding_status": {"app_id", "phone_number_id"},
		"whatsapp_biz_publish_app":       {"app_id"},
	}
	for _, key := range selectors[s.Tool] {
		if strings.TrimSpace(text(s.Scope[key])) != "" {
			return "", nil
		}
	}
	return "", fmt.Errorf("capture does not establish a selected business or stable asset scope")
}

func template(s captures.Snapshot) (map[string]any, error) {
	if err := explicit(s); err != nil {
		return nil, err
	}
	if s.Tool != "whatsapp_biz_get_template" {
		return nil, fmt.Errorf("template inspection requires a whatsapp_biz_get_template capture")
	}
	v, err := normalized(s.Data)
	if err != nil {
		return nil, err
	}
	v = unwrap(v)
	for depth := 0; depth < 5; depth++ {
		if list, ok := v.([]any); ok {
			if len(list) != 1 {
				return nil, fmt.Errorf("capture must identify exactly one template")
			}
			v = list[0]
			continue
		}
		m := object(v)
		if m == nil {
			return nil, fmt.Errorf("saved template payload is not an object")
		}
		if _, ok := m["components"]; ok {
			return m, nil
		}
		moved := false
		for _, k := range []string{"template", "data", "result"} {
			if next, ok := m[k]; ok {
				v = next
				moved = true
				break
			}
		}
		if !moved {
			return m, nil
		}
	}
	return nil, fmt.Errorf("saved template has an unsupported envelope")
}

// TemplateCompare compares two selected template details, including across accounts.
func TemplateCompare(left, right captures.Snapshot) (map[string]any, error) {
	a, err := template(left)
	if err != nil {
		return nil, err
	}
	b, err := template(right)
	if err != nil {
		return nil, err
	}
	return map[string]any{"left": evidence(left), "right": evidence(right), "left_template": a, "right_template": b, "complete": left.Complete && right.Complete, "changes": diffValues(a, b, "", left.Complete && right.Complete)}, nil
}
