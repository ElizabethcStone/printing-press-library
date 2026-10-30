package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/captures"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/metamcp"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/store"
)

func TestFiveInspectionCommandsConsumeCapturedEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	save := func(tool string, scope map[string]any, data any) string {
		t.Helper()
		s, e := captures.Save(context.Background(), db, tool, scope, data)
		if e != nil {
			t.Fatal(e)
		}
		return s.ID
	}
	scope := map[string]any{"business_id": "fixture-business"}
	before := save("whatsapp_biz_accounts", scope, map[string]any{"accounts": []any{map[string]any{"account_id": "fixture-account", "business_id": "fixture-business", "type": "whatsapp_business_account", "status": "ACTIVE"}}})
	after := save("whatsapp_biz_accounts", scope, map[string]any{"accounts": []any{map[string]any{"account_id": "fixture-account", "business_id": "fixture-business", "type": "whatsapp_business_account", "status": "PENDING"}}})
	save("whatsapp_biz_phone_numbers", scope, map[string]any{"phone_numbers": []any{map[string]any{"phone_number_id": "fixture-phone", "account_id": "fixture-account"}}})
	save("whatsapp_biz_accounts", map[string]any{"business_id": "unrelated-business"}, map[string]any{"accounts": []any{map[string]any{"account_id": "unrelated-account"}}})
	left := save("whatsapp_biz_get_template", map[string]any{"waba_id": "fixture-account", "template_id": "fixture-template"}, map[string]any{"id": "fixture-template", "name": "order_update", "components": []any{map[string]any{"type": "BODY", "text": "Order {{1}}"}, map[string]any{"type": "HEADER", "format": "TEXT", "text": "{{1}}"}, map[string]any{"type": "BUTTONS", "buttons": []any{map[string]any{"type": "FLOW"}}}}})
	right := save("whatsapp_biz_get_template", map[string]any{"waba_id": "other-account", "template_id": "other-template"}, map[string]any{"id": "other-template", "name": "order_update", "components": []any{map[string]any{"type": "BODY", "text": "Shipment {{1}}"}}})
	catalogScope := map[string]any{"endpoint": metamcp.Endpoint, "protocol": "2025-03-26"}
	catA := save("catalog", catalogScope, map[string]any{"tools": []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}}}}})
	catB := save("catalog", catalogScope, map[string]any{"tools": []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}, "required": []any{"message"}}}}})
	cases := []struct {
		kind string
		args []string
		key  string
		min  int
	}{
		{"scope-map", []string{"--snapshot", after, "--business-id", "fixture-business"}, "relationships", 2},
		{"changes", []string{"--before", before, "--after", after}, "changes", 1},
		{"template-compare", []string{"--left", left, "--right", right}, "changes", 1},
		{"template-checklist", []string{"--template", left}, "requirements", 2},
		{"catalog-drift", []string{"--before", catA, "--after", catB}, "potentially_breaking", 1},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			args := append([]string{"ops", tc.kind, "--db", path, "--json"}, tc.args...)
			out, e := executeWhatsApp(t, args...)
			if e != nil {
				t.Fatal(e)
			}
			var v map[string]any
			if json.Unmarshal([]byte(out), &v) != nil {
				t.Fatal(out)
			}
			rows, ok := v[tc.key].([]any)
			if !ok || len(rows) < tc.min {
				t.Fatalf("missing expected %s evidence: %s", tc.key, out)
			}
			if tc.kind == "scope-map" && strings.Contains(out, "unrelated-account") {
				t.Fatal("scope filter admitted unrelated account")
			}
			if tc.kind == "template-checklist" {
				if len(v["unsupported"].([]any)) == 0 {
					t.Fatal("unsupported flow hidden")
				}
			}
		})
	}
	// Agent projections must retain local provenance and useful payloads.
	out, err := executeWhatsApp(t, "ops", "scope-map", "--snapshot", after, "--business-id", "fixture-business", "--db", path, "--agent", "--select", "relationships,gaps")
	if err != nil {
		t.Fatal(err)
	}
	var agent map[string]any
	json.Unmarshal([]byte(out), &agent)
	if agent["meta"].(map[string]any)["source"] != "local" {
		t.Fatal(out)
	}
	if len(agent["results"].(map[string]any)["relationships"].([]any)) < 2 {
		t.Fatal(out)
	}
	// Re-comparing the identical capture cannot fabricate a change.
	out, err = executeWhatsApp(t, "ops", "changes", "--before", before, "--after", before, "--db", path, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var unchanged map[string]any
	json.Unmarshal([]byte(out), &unchanged)
	if len(unchanged["changes"].([]any)) != 0 {
		t.Fatal(out)
	}
}
func TestInspectionInputBoundariesAndEmptySelection(t *testing.T) {
	for _, kind := range []string{"scope-map", "changes", "template-compare", "template-checklist", "catalog-drift"} {
		out, err := executeWhatsApp(t, "ops", kind, "--json")
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		json.Unmarshal([]byte(out), &v)
		if len(v["results"].([]any)) != 0 || v["note"] == nil {
			t.Fatalf("unselected capture invented results: %s", out)
		}
		if _, err = executeWhatsApp(t, "ops", kind, "--data-source", "live", "--json"); err == nil {
			t.Fatalf("local command allowed live mode: %s", kind)
		}
	}
}
