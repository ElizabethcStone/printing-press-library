package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/captures"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/metamcp"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/store"
)

func TestRawImportCannotReachMCP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	fixtureWhatsAppEnv(t, server.URL)
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	if err := os.WriteFile(path, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"whatsapp_biz_send_message","arguments":{"confirm":true}}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range [][]string{{}, {"--yes"}, {"--agent"}, {"--dry-run"}} {
		args := append([]string{"import", "whatsapp-business-tools", "--input", path}, mode...)
		_, err := executeWhatsApp(t, args...)
		if err == nil {
			t.Fatal("raw import accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("raw import reached transport")
	}
	if _, err := resourceWritePath("whatsapp-business-tools"); err == nil {
		t.Fatal("raw resource remains writable")
	}
}

func TestAgentChangesPreservesRareBeforeAfterValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	scope := map[string]any{"business_id": "fixture-business"}
	before, err := captures.Save(context.Background(), db, "whatsapp_biz_accounts", scope, map[string]any{"first_removed": "old-first", "second_removed": "old-second", "status": "ACTIVE"})
	if err != nil {
		t.Fatal(err)
	}
	after, err := captures.Save(context.Background(), db, "whatsapp_biz_accounts", scope, map[string]any{"status": "PENDING"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := executeWhatsApp(t, "ops", "changes", "--before", before.ID, "--after", after.ID, "--db", path, "--agent")
	if err != nil {
		t.Fatal(err)
	}
	var view map[string]any
	json.Unmarshal([]byte(out), &view)
	rows := view["results"].(map[string]any)["changes"].([]any)
	if len(rows) != 3 {
		t.Fatalf("expected three changes: %s", out)
	}
	found := false
	for _, v := range rows {
		row := v.(map[string]any)
		if row["path"] == "/status" {
			found = true
			if row["before"] != "ACTIVE" || row["after"] != "PENDING" {
				t.Fatalf("rare comparison value dropped: %s", out)
			}
		}
	}
	if !found {
		t.Fatal("changed row missing")
	}
	out, err = executeWhatsApp(t, "ops", "changes", "--before", before.ID, "--after", after.ID, "--db", path, "--agent", "--select", "changes")
	if err != nil || !strings.Contains(out, "PENDING") {
		t.Fatalf("projection lost change: %s %v", out, err)
	}
}

func TestSyncJSONArgumentsKeepsTypedArrayFilters(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["method"] == "notifications/initialized" {
			w.WriteHeader(202)
			return
		}
		var result any = map[string]any{"protocolVersion": "2025-03-26"}
		if body["method"] == "tools/list" {
			result = map[string]any{"tools": metamcp.Catalog()}
		}
		if body["method"] == "tools/call" {
			args := body["params"].(map[string]any)["arguments"].(map[string]any)
			status, ok := args["status"].([]any)
			if !ok || len(status) != 2 || status[0] != "APPROVED" || status[1] != "PENDING" {
				t.Errorf("typed status not forwarded: %#v", args)
			}
			if args["waba_id"] != "fixture-account" {
				t.Error("account scope lost")
			}
			called.Store(true)
			result = map[string]any{"structuredContent": map[string]any{"templates": []any{}}}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": body["id"], "result": result})
	}))
	defer server.Close()
	fixtureWhatsAppEnv(t, server.URL)
	db := filepath.Join(t.TempDir(), "captures.db")
	_, err := executeWhatsApp(t, "sync", "--resources", "whatsapp_biz_list_templates", "--arguments", `{"waba_id":"fixture-account","status":["APPROVED","PENDING"]}`, "--db", db, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !called.Load() {
		t.Fatal("read never reached fixture")
	}
	for _, input := range []string{`[]`, `{} {}`, `null`} {
		if _, err := executeWhatsApp(t, "sync", "--resources", "ping", "--arguments", input, "--db", db, "--json"); err == nil {
			t.Fatalf("invalid JSON arguments accepted: %s", input)
		}
	}
	if _, err := executeWhatsApp(t, "sync", "--resources", "ping", "--arguments", `{}`, "--param", "message=fixture"); err == nil {
		t.Fatal("ambiguous argument sources accepted")
	}
}
