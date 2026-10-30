package inspection

import (
	"encoding/json"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/captures"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/metamcp"
	"strings"
	"testing"
)

func snap(id, tool string, scope map[string]any, data any) captures.Snapshot {
	return captures.Snapshot{ID: id, Tool: tool, Scope: scope, CapturedAt: "2026-10-29T18:00:00Z", Complete: true, Data: data}
}
func must(t *testing.T, value map[string]any, err error) map[string]any {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func jsonText(t *testing.T, v any) string {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func maps(t *testing.T, m map[string]any, key string) []map[string]any {
	t.Helper()
	r, ok := m[key].([]map[string]any)
	if !ok || r == nil {
		t.Fatalf("%s must be a nonnil array: %#v", key, m[key])
	}
	return r
}

func TestScopeMapSelectionRelationshipsAndGaps(t *testing.T) {
	scoped := map[string]any{"business_id": "business-a"}
	evidence := []captures.Snapshot{
		snap("businesses", "whatsapp_biz_businesses", nil, []any{map[string]any{"business_id": "business-a", "prerequisites_met": true}, map[string]any{"business_id": "business-b"}}),
		snap("accounts", "whatsapp_biz_accounts", scoped, map[string]any{"accounts": []any{map[string]any{"account_id": "account-owned", "type": "messaging_account", "whatsapp_account": map[string]any{"id": "parent-account"}}}, "shared_accounts": []any{map[string]any{"account_id": "account-shared", "business_id": "client-business", "type": "whatsapp_business_account"}}}),
		snap("phones", "whatsapp_biz_phone_numbers", scoped, map[string]any{"phone_numbers": []any{map[string]any{"phone_number_id": "phone-known", "whatsapp_account_id": "parent-account", "onboarding_state": "unknown"}, map[string]any{"phone_number_id": "phone-unknown"}}}),
		snap("wrong", "whatsapp_biz_accounts", map[string]any{"business_id": "business-b"}, map[string]any{"accounts": []any{map[string]any{"account_id": "must-not-appear", "business_id": "business-a"}}}),
	}
	evidence[2].Complete = false
	for _, tc := range []struct {
		name, business  string
		items           []captures.Snapshot
		relations, gaps int
		wantError       bool
	}{
		{"selected only", "business-a", evidence, 4, 2, false},
		{"no evidence", "business-a", nil, 0, 1, false},
		{"unselected business", "business-c", evidence, 0, 1, false},
		{"direct phone lookup", "business-a", []captures.Snapshot{snap("direct", "whatsapp_biz_phone_numbers", nil, map[string]any{"business_id": "business-a", "phone_number_id": "phone-direct", "whatsapp_account_id": "account-direct"})}, 1, 0, false},
		{"requires explicit choice", "", evidence, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, err := ScopeMap(tc.items, tc.business)
			if tc.wantError {
				if err == nil {
					t.Fatal("missing selection was accepted")
				}
				return
			}
			view = must(t, view, err)
			if got := len(maps(t, view, "relationships")); got != tc.relations {
				t.Fatalf("relationships=%d, want %d: %s", got, tc.relations, jsonText(t, view))
			}
			if got := len(maps(t, view, "gaps")); got != tc.gaps {
				t.Fatalf("gaps=%d, want %d", got, tc.gaps)
			}
			serialized := jsonText(t, view)
			if strings.Contains(serialized, "must-not-appear") {
				t.Fatal("cross-business capture leaked into selected scope")
			}
			if tc.name == "selected only" {
				if !strings.Contains(serialized, `"role":"shared"`) || !strings.Contains(serialized, `"reported_business_id":"client-business"`) || !strings.Contains(serialized, `"onboarding_state":"unknown"`) {
					t.Fatal("source roles or unknown state lost")
				}
				if strings.Contains(serialized, `"ready":true`) {
					t.Fatal("prerequisites were converted into readiness")
				}
			}
		})
	}
}

func TestChangesContentScopeAndPartialCoverage(t *testing.T) {
	before := snap("before", "whatsapp_biz_onboarding_status", map[string]any{"business_id": "business-a", "app_id": "app-a"}, map[string]any{"steps": []any{map[string]any{"step": "webhooks", "status": "unknown"}, map[string]any{"step": "publish", "status": "todo"}}})
	after := before
	after.ID = "after"
	after.Data = map[string]any{"steps": []any{map[string]any{"step": "webhooks", "status": "done"}}}
	for _, tc := range []struct {
		name  string
		b, a  captures.Snapshot
		count int
		kind  string
		err   bool
	}{
		{"same capture", before, before, 0, "", false},
		{"complete removal", before, after, 2, "removed", false},
		{"partial is unobserved", before, func() captures.Snapshot { x := after; x.Complete = false; return x }(), 2, "unobserved_after", false},
		{"different app scope", before, func() captures.Snapshot {
			x := after
			x.Scope = map[string]any{"business_id": "business-a", "app_id": "app-b"}
			return x
		}(), 0, "", true},
		{"different filters", before, func() captures.Snapshot {
			x := after
			x.Scope = map[string]any{"business_id": "business-a", "app_id": "app-a", "status": "APPROVED"}
			return x
		}(), 0, "", true},
		{"different tool", before, func() captures.Snapshot { x := after; x.Tool = "whatsapp_biz_phone_numbers"; return x }(), 0, "", true},
		{"explicit IDs", before, func() captures.Snapshot { x := after; x.ID = ""; return x }(), 0, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, err := Changes(tc.b, tc.a)
			if tc.err {
				if err == nil {
					t.Fatal("invalid comparison accepted")
				}
				return
			}
			view = must(t, view, err)
			changes := maps(t, view, "changes")
			if len(changes) != tc.count {
				t.Fatalf("changes=%d want %d", len(changes), tc.count)
			}
			if tc.kind != "" {
				found := false
				for _, c := range changes {
					if c["kind"] == tc.kind {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing %s: %s", tc.kind, jsonText(t, view))
				}
			}
			if tc.count > 0 {
				serialized := jsonText(t, changes)
				if !strings.Contains(serialized, `"before":"unknown"`) || !strings.Contains(serialized, `"after":"done"`) {
					t.Fatalf("actual unknown-to-done change lost: %s", serialized)
				}
			}
		})
	}
	left := snap("a", "tool", nil, []any{map[string]any{"id": "one"}, map[string]any{"id": "two"}})
	right := left
	right.ID = "b"
	right.Data = []any{map[string]any{"id": "two"}, map[string]any{"id": "one"}}
	v, e := Changes(left, right)
	view := must(t, v, e)
	if len(maps(t, view, "changes")) != 1 || maps(t, view, "changes")[0]["kind"] != "reordered" {
		t.Fatal("list reordering was hidden")
	}
}

func TestChangesRequiresEstablishedSelection(t *testing.T) {
	for _, tool := range []string{"whatsapp_biz_accounts", "whatsapp_biz_phone_numbers", "whatsapp_biz_onboarding_status", "whatsapp_biz_verify_business", "whatsapp_biz_publish_app"} {
		a := snap("before", tool, map[string]any{}, map[string]any{"needs_selection": true})
		b := a
		b.ID = "after"
		if _, err := Changes(a, b); err == nil {
			t.Fatalf("unestablished selection accepted for %s", tool)
		}
	}
	for _, tc := range []struct{ tool, key string }{
		{"whatsapp_biz_accounts", "phone_number_id"}, {"whatsapp_biz_phone_numbers", "waba_id"}, {"whatsapp_biz_phone_numbers", "phone_number"},
		{"whatsapp_biz_get_template", "waba_id"}, {"whatsapp_biz_list_templates", "waba_id"}, {"whatsapp_biz_onboarding_status", "app_id"}, {"whatsapp_biz_onboarding_status", "phone_number_id"}, {"whatsapp_biz_publish_app", "app_id"},
	} {
		a := snap("before", tc.tool, map[string]any{tc.key: "fixture-stable-selector"}, map[string]any{"status": "unknown"})
		b := a
		b.ID = "after"
		if _, err := Changes(a, b); err != nil {
			t.Fatalf("stable asset rejected %s %s: %v", tc.tool, tc.key, err)
		}
	}
}

func TestChangesEffectiveBusinessSelection(t *testing.T) {
	payload := func(business string) map[string]any {
		return map[string]any{"business_id": business, "accounts": []any{map[string]any{"account_id": "account-demo"}}}
	}
	base := snap("before", "whatsapp_biz_accounts", map[string]any{}, payload("sensitive-business-a"))
	for _, tc := range []struct {
		name          string
		before, after captures.Snapshot
		wantError     bool
	}{
		{"same implicit business", base, snap("after", base.Tool, map[string]any{}, payload("sensitive-business-a")), false},
		{"different implicit businesses", base, snap("after", base.Tool, map[string]any{}, payload("sensitive-business-b")), true},
		{"resolved selection becomes unknown", base, snap("after", base.Tool, map[string]any{}, map[string]any{"accounts": []any{}}), true},
		{"unknown selection becomes resolved", snap("before", base.Tool, map[string]any{}, map[string]any{"accounts": []any{}}), base, true},
		{"explicit response conflict", snap("before", base.Tool, map[string]any{"business_id": "sensitive-business-a"}, payload("sensitive-business-a")), snap("after", base.Tool, map[string]any{"business_id": "sensitive-business-a"}, payload("sensitive-business-b")), true},
		{"explicit conflict on first capture", snap("before", base.Tool, map[string]any{"business_id": "sensitive-business-a"}, payload("sensitive-business-b")), snap("after", base.Tool, map[string]any{"business_id": "sensitive-business-a"}, payload("sensitive-business-a")), true},
		{"explicit request supplies missing response identity", snap("before", base.Tool, map[string]any{"business_id": "sensitive-business-a"}, map[string]any{"accounts": []any{}}), snap("after", base.Tool, map[string]any{"business_id": "sensitive-business-a"}, payload("sensitive-business-a")), false},
		{"result envelope", base, snap("after", base.Tool, map[string]any{}, map[string]any{"result": payload("sensitive-business-a")}), false},
		{"result envelope with metadata", base, snap("after", base.Tool, map[string]any{}, map[string]any{"result": payload("sensitive-business-a"), "warning": "captured warning"}), false},
		{"changed business in result envelope", base, snap("after", base.Tool, map[string]any{}, map[string]any{"result": payload("sensitive-business-b"), "warning": "captured warning"}), true},
		{"global businesses listing is not selection", snap("before", "whatsapp_biz_businesses", map[string]any{"action": "list"}, payload("sensitive-business-a")), snap("after", "whatsapp_biz_businesses", map[string]any{"action": "list"}, payload("sensitive-business-b")), false},
		{"shared account row owner is not selection", snap("before", base.Tool, map[string]any{"business_id": "partner"}, map[string]any{"shared_accounts": []any{map[string]any{"account_id": "shared", "business_id": "client-a"}}}), snap("after", base.Tool, map[string]any{"business_id": "partner"}, map[string]any{"shared_accounts": []any{map[string]any{"account_id": "shared", "business_id": "client-b"}}}), false},
		{"nested account IDs alone are not selection", snap("before", base.Tool, map[string]any{}, map[string]any{"accounts": []any{map[string]any{"account_id": "owned", "business_id": "client-a"}}}), snap("after", base.Tool, map[string]any{}, map[string]any{"accounts": []any{map[string]any{"account_id": "owned", "business_id": "client-b"}}}), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, err := Changes(tc.before, tc.after)
			if tc.wantError {
				if err == nil || view != nil {
					t.Fatalf("incompatible effective selections must fail: %s", jsonText(t, view))
				}
				if strings.Contains(err.Error(), "sensitive-business") {
					t.Fatal("scope error exposed a captured business identifier")
				}
				return
			}
			view = must(t, view, err)
			if tc.name == "same implicit business" && len(maps(t, view, "changes")) != 0 {
				t.Fatal("unchanged implicit selection fabricated changes")
			}
			if tc.name == "shared account row owner is not selection" && len(maps(t, view, "changes")) != 1 {
				t.Fatal("actual shared-owner field change was lost")
			}
		})
	}
	// Every known scoped read must enforce the same boundary. Global list tools
	// are deliberately absent: their row IDs do not identify a chosen business.
	for _, tool := range []string{"whatsapp_biz_accounts", "whatsapp_biz_phone_numbers", "whatsapp_biz_onboarding_status", "whatsapp_biz_get_template", "whatsapp_biz_list_templates", "whatsapp_biz_publish_app", "whatsapp_biz_verify_business"} {
		t.Run(tool, func(t *testing.T) {
			before := snap("before", tool, map[string]any{}, payload("sensitive-business-a"))
			after := snap("after", tool, map[string]any{}, payload("sensitive-business-b"))
			if _, err := Changes(before, after); err == nil {
				t.Fatalf("%s accepted distinct effective selections", tool)
			}
		})
	}
}

func TestTemplateCompareExplicitDetailsAcrossAccounts(t *testing.T) {
	left := snap("left", "whatsapp_biz_get_template", map[string]any{"waba_id": "account-left", "template_id": "template-left"}, map[string]any{"id": "template-left", "name": "order_update", "components": []any{map[string]any{"type": "BODY", "text": "Order {{1}}"}}})
	right := snap("right", "whatsapp_biz_get_template", map[string]any{"waba_id": "account-right", "template_id": "template-right"}, map[string]any{"result": `{"id":"template-right","name":"order_update","components":[{"type":"BODY","text":"Shipment {{1}}"}]}`})
	for _, tc := range []struct {
		name    string
		a, b    captures.Snapshot
		error   bool
		changed bool
	}{
		{"cross-account", left, right, false, true}, {"unchanged", left, left, false, false},
		{"wrong source", left, func() captures.Snapshot { x := right; x.Tool = "whatsapp_biz_list_templates"; return x }(), true, false},
		{"missing ID", left, func() captures.Snapshot { x := right; x.ID = ""; return x }(), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, e := TemplateCompare(tc.a, tc.b)
			if tc.error {
				if e == nil {
					t.Fatal("invalid template comparison accepted")
				}
				return
			}
			v = must(t, v, e)
			changes := maps(t, v, "changes")
			if (len(changes) > 0) != tc.changed {
				t.Fatalf("unexpected diff %s", jsonText(t, v))
			}
			if tc.changed {
				out := jsonText(t, v)
				if !strings.Contains(out, "account-left") || !strings.Contains(out, "account-right") || !strings.Contains(out, "Shipment {{1}}") {
					t.Fatal("account metadata or content difference lost")
				}
			}
		})
	}
}

func TestTemplateChecklistComponentsAndFailClosedMappings(t *testing.T) {
	send, ok := metamcp.Lookup("whatsapp_biz_send_message")
	if !ok {
		t.Fatal("live send contract missing")
	}
	makeTemplate := func(components []any, format string) captures.Snapshot {
		return snap("template-capture", "whatsapp_biz_get_template", map[string]any{"waba_id": "account-demo", "template_id": "template-demo"}, map[string]any{"id": "template-demo", "name": "order_update", "parameter_format": format, "components": components})
	}
	for _, tc := range []struct {
		name          string
		capture       captures.Snapshot
		required, bad int
	}{
		{"independent positional components", makeTemplate([]any{map[string]any{"type": "HEADER", "format": "TEXT", "text": "Hi {{1}}", "example": map[string]any{"header_text": []string{"REVIEW_SECRET_HEADER"}}}, map[string]any{"type": "BODY", "text": "Order {{1}} again {{1}}", "example": map[string]any{"body_text": []any{[]string{"REVIEW_SECRET_BODY"}}}}, map[string]any{"type": "BUTTONS", "buttons": []any{map[string]any{"type": "URL", "url": "https://example.com/{{1}}"}, map[string]any{"type": "QUICK_REPLY", "text": "Yes"}, map[string]any{"type": "COPY_CODE", "example": "REVIEW_SECRET_COUPON"}}}}, "POSITIONAL"), 5, 0},
		{"named unsupported", makeTemplate([]any{map[string]any{"type": "BODY", "text": "Hi {{customer_name}}"}}, "NAMED"), 1, 1},
		{"media header", makeTemplate([]any{map[string]any{"type": "HEADER", "format": "IMAGE", "example": map[string]any{"header_handle": []string{"REVIEW_SECRET_MEDIA"}}}, map[string]any{"type": "BODY", "text": "Static"}}, "POSITIONAL"), 1, 0},
		{"static template", makeTemplate([]any{map[string]any{"type": "BODY", "text": "Static hello"}}, "POSITIONAL"), 0, 0},
		{"unknown component", makeTemplate([]any{map[string]any{"type": "FUTURE_COMPONENT", "example": "REVIEW_SECRET_UNKNOWN"}}, "POSITIONAL"), 0, 1},
		{"unsupported full components", makeTemplate([]any{map[string]any{"type": "CAROUSEL"}, map[string]any{"type": "LIMITED_TIME_OFFER"}, map[string]any{"type": "BUTTONS", "buttons": []any{map[string]any{"type": "FLOW"}}}}, "POSITIONAL"), 0, 3},
		{"unsupported URL placement", makeTemplate([]any{map[string]any{"type": "BUTTONS", "buttons": []any{map[string]any{"type": "URL", "url": "https://example.com/{{1}}/suffix"}}}}, "POSITIONAL"), 1, 1},
		{"missing component shape", snap("missing", "whatsapp_biz_get_template", nil, map[string]any{"name": "unknown"}), 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, e := TemplateChecklist(tc.capture, send)
			v = must(t, v, e)
			if got := len(maps(t, v, "requirements")); got != tc.required {
				t.Fatalf("requirements=%d want %d: %s", got, tc.required, jsonText(t, v))
			}
			if got := len(maps(t, v, "unsupported")); got != tc.bad {
				t.Fatalf("unsupported=%d want %d: %s", got, tc.bad, jsonText(t, v))
			}
			out := jsonText(t, v)
			if strings.Contains(out, "REVIEW_SECRET") {
				t.Fatal("template review examples were reused")
			}
			if strings.Contains(out, `"eligible":true`) || strings.Contains(out, `"sendable":true`) {
				t.Fatal("checklist claimed delivery eligibility")
			}
			if tc.name == "independent positional components" {
				paths := map[string]bool{}
				for _, r := range maps(t, v, "requirements") {
					paths[text(r["path"])] = true
				}
				if !paths["/components/0/text"] || !paths["/components/1/text"] || !paths["/components/2/buttons/0/url"] {
					t.Fatal("repeated placeholders across components were merged")
				}
			}
		})
	}
	withoutChannels := send
	withoutChannels.InputSchema = map[string]any{"properties": map[string]any{}}
	v, e := TemplateChecklist(makeTemplate([]any{map[string]any{"type": "BODY", "text": "Hi {{1}}"}}, "POSITIONAL"), withoutChannels)
	v = must(t, v, e)
	if len(maps(t, v, "unsupported")) != 1 {
		t.Fatal("missing send parameter channel should fail closed")
	}
	if _, e := TemplateChecklist(makeTemplate(nil, ""), metamcp.Tool{Name: "different_tool"}); e == nil {
		t.Fatal("unrelated send contract accepted")
	}
}

func TestCatalogDriftCompatibilitySignalsAndDescriptionOnly(t *testing.T) {
	scope := map[string]any{"endpoint": metamcp.Endpoint, "protocol": "2025-03-26"}
	base := func(desc string, required []string) map[string]any {
		return map[string]any{"name": "lookup", "description": desc, "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"business_id": map[string]any{"type": "string"}}, "required": required}, "outputSchema": map[string]any{"type": "object"}}
	}
	before := snap("catalog-before", "catalog", scope, map[string]any{"tools": []any{base("before", []string{})}})
	for _, tc := range []struct {
		name                            string
		after                           captures.Snapshot
		breaking, schemas, descriptions int
		error                           bool
	}{
		{"unchanged", before, 0, 0, 0, false},
		{"description only", snap("catalog-description", "catalog", scope, map[string]any{"tools": map[string]any{"lookup": base("after", []string{})}}), 0, 0, 1, false},
		{"new required input", snap("catalog-required", "catalog", scope, map[string]any{"tools": []any{base("before", []string{"business_id"})}}), 1, 1, 0, false},
		{"different endpoint", snap("bad", "catalog", map[string]any{"endpoint": "https://other.invalid/mcp", "protocol": "2025-03-26"}, map[string]any{"tools": []any{}}), 0, 0, 0, true},
		{"missing protocol", snap("bad", "catalog", map[string]any{"endpoint": metamcp.Endpoint}, map[string]any{"tools": []any{}}), 0, 0, 0, true},
		{"wrong source", snap("bad", "whatsapp_biz_accounts", scope, map[string]any{"tools": []any{}}), 0, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, e := CatalogDrift(before, tc.after)
			if tc.error {
				if e == nil {
					t.Fatal("unrelated catalogs accepted")
				}
				return
			}
			v = must(t, v, e)
			if len(maps(t, v, "potentially_breaking")) != tc.breaking || len(maps(t, v, "schema_changes")) != tc.schemas || len(maps(t, v, "description_changes")) != tc.descriptions {
				t.Fatalf("unexpected drift signals: %s", jsonText(t, v))
			}
			if tc.name == "description only" && maps(t, v, "description_changes")[0]["description_only"] != true {
				t.Fatal("description-only edit mislabeled")
			}
		})
	}
	removed := snap("removed", "catalog", scope, map[string]any{"tools": []any{}})
	v, e := CatalogDrift(before, removed)
	v = must(t, v, e)
	if len(v["removed_tools"].([]string)) != 1 || len(maps(t, v, "potentially_breaking")) != 1 {
		t.Fatal("removed tool not flagged")
	}
	removed.Complete = false
	v, e = CatalogDrift(before, removed)
	v = must(t, v, e)
	if len(v["removed_tools"].([]string)) != 0 || len(maps(t, v, "unobserved_tools")) != 1 {
		t.Fatal("partial catalog falsely claimed removal")
	}
	added := snap("added", "catalog", scope, map[string]any{"tools": []metamcp.Tool{{Name: "new_tool", InputSchema: map[string]any{"type": "object"}}}})
	empty := snap("empty", "catalog", scope, map[string]any{"tools": []any{}})
	v, e = CatalogDrift(empty, added)
	v = must(t, v, e)
	if len(v["added_tools"].([]string)) != 1 {
		t.Fatal("typed live catalog normalization lost added tool")
	}
}
