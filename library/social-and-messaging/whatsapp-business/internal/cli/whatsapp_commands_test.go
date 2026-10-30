package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/metamcp"
)

func executeWhatsApp(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var flags rootFlags
	cmd := newRootCmd(&flags)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"--no-learn"}, args...))
	err := cmd.Execute()
	return out.String(), err
}
func fixtureWhatsAppEnv(t *testing.T, url string) {
	t.Helper()
	t.Setenv("WHATSAPP_BUSINESS_BASE_URL", url)
	t.Setenv("WHATSAPP_BUSINESS_MCP_TOKEN", "fixture-only")
	t.Setenv("WHATSAPP_BUSINESS_USE_CODEX_AUTH", "")
}
func TestAllOfficialCommandsDryRunAndWritesPreview(t *testing.T) {
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.Add(1); w.WriteHeader(500) }))
	defer s.Close()
	fixtureWhatsAppEnv(t, s.URL)
	for _, tool := range metamcp.Catalog() {
		t.Run(tool.Name, func(t *testing.T) {
			path := strings.Fields(metamcp.Command(tool.Name))
			args := append(path, "--dry-run", "--json")
			out, err := executeWhatsApp(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			var v map[string]any
			if json.Unmarshal([]byte(out), &v) != nil || v["dry_run"] != true {
				t.Fatalf("invalid dry run %s", out)
			}
			if !tool.Annotations.ReadOnly {
				args = append(strings.Fields(metamcp.Command(tool.Name)), "--arguments", `{"confirm":true}`, "--json")
				out, err = executeWhatsApp(t, args...)
				if err != nil {
					t.Fatal(err)
				}
				if json.Unmarshal([]byte(out), &v) != nil || v["executed"] != false || v["preview"] != true {
					t.Fatalf("write did not preview %s", out)
				}
			}
		})
	}
	if n.Load() != 0 {
		t.Fatalf("preview made %d network calls", n.Load())
	}
}
func TestHarnessRefusesConfirmedWrite(t *testing.T) {
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.Add(1) }))
	defer s.Close()
	fixtureWhatsAppEnv(t, s.URL)
	t.Setenv("PRINTING_PRESS_DOGFOOD", "1")
	out, err := executeWhatsApp(t, "messages", "send", "--phone-number-id", "fixture-phone", "--to", "+15550100123", "--message", "Fixture message", "--confirm", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	json.Unmarshal([]byte(out), &v)
	if v["executed"] != false || v["harness_refused"] != true || n.Load() != 0 {
		t.Fatalf("harness reached write: %s calls=%d", out, n.Load())
	}
}
func TestLiveReadCaptureAndProjection(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
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
			result = map[string]any{"structuredContent": map[string]any{"response": "pong", "message": "fixture"}}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": body["id"], "result": result})
	}))
	defer s.Close()
	fixtureWhatsAppEnv(t, s.URL)
	db := filepath.Join(t.TempDir(), "capture.db")
	out, err := executeWhatsApp(t, "connection", "ping", "--db", db, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	json.Unmarshal([]byte(out), &v)
	if v["data"].(map[string]any)["response"] != "pong" || v["capture_id"] == nil {
		t.Fatalf("missing actual capture: %s", out)
	}
	captureID := v["capture_id"].(string)
	out, err = executeWhatsApp(t, "snapshots", "show", "--snapshot", captureID, "--db", db, "--json")
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(out), &v)
	if v["tool"] != "ping" {
		t.Fatal(out)
	}
	out, err = executeWhatsApp(t, "connection", "ping", "--db", db, "--no-cache", "--json", "--select", "data.response")
	if err != nil {
		t.Fatal(err)
	}
	var projected map[string]any
	json.Unmarshal([]byte(out), &projected)
	if len(projected) != 1 || projected["data"].(map[string]any)["response"] != "pong" {
		t.Fatal(out)
	}
	if calls.Load() != 8 {
		t.Fatalf("expected two handshakes and calls, got %d", calls.Load())
	}
}
func TestSQLCannotMutateCaptureStore(t *testing.T) {
	fixtureWhatsAppEnv(t, "http://127.0.0.1:1")
	db := filepath.Join(t.TempDir(), "capture.db")
	out, err := executeWhatsApp(t, "snapshots", "list", "--db", db, "--json")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("fresh store=%s %v", out, err)
	}
	for _, query := range []string{"DELETE FROM resources", "SELECT 1; DELETE FROM resources", "WITH x AS (SELECT 1) DELETE FROM resources"} {
		_, err = executeWhatsApp(t, "sql", query, "--db", db, "--json")
		if err == nil {
			t.Fatalf("mutation permitted: %s", query)
		}
	}
	out, err = executeWhatsApp(t, "sql", "SELECT COUNT(*) AS rows FROM resources", "--db", db, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"rows": 0`) {
		t.Fatal(out)
	}
}
func TestArgumentSchemaAndSecretPreview(t *testing.T) {
	fixtureWhatsAppEnv(t, "http://127.0.0.1:1")
	for _, args := range [][]string{{"businesses", "list", "--arguments", `{"bogus":1}`, "--json"}, {"templates", "get", "--json"}, {"connection", "ping", "--data-source", "local", "--json"}} {
		if _, err := executeWhatsApp(t, args...); err == nil {
			t.Fatal("invalid inputs succeeded", args)
		}
	}
	out, err := executeWhatsApp(t, "webhooks", "configure", "--business-id", "fixture", "--callback-url", "https://example.com/hook", "--verify-token", "fixture-secret", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "fixture-secret") {
		t.Fatal("preview leaked verification token")
	}
	out, err = executeWhatsApp(t, "whatsapp-business-tools", "--method", "tools/call", "--jsonrpc", "2.0", "--yes", "--json")
	if err != nil || !strings.Contains(out, "dry_run") {
		t.Fatalf("raw path not preview-only: %s %v", out, err)
	}
}
func TestApprovedPathsResolveAsLeaves(t *testing.T) {
	var flags rootFlags
	root := newRootCmd(&flags)
	for _, tool := range metamcp.Catalog() {
		path := strings.Fields(metamcp.Command(tool.Name))
		leaf, rest, err := root.Find(path)
		if err != nil || len(rest) != 0 || leaf.RunE == nil || leaf.CommandPath() != root.Name()+" "+strings.Join(path, " ") {
			t.Fatalf("path missing: %v %v", path, err)
		}
	}
}

func TestLiveSafetyDriftBlocksExecution(t *testing.T) {
	var executed atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["method"] == "notifications/initialized" {
			w.WriteHeader(202)
			return
		}
		result := any(map[string]any{"protocolVersion": "2025-03-26"})
		if req["method"] == "tools/list" {
			result = map[string]any{"tools": []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": false}}}}
		}
		if req["method"] == "tools/call" {
			executed.Add(1)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
	}))
	defer s.Close()
	fixtureWhatsAppEnv(t, s.URL)
	_, err := executeWhatsApp(t, "connection", "ping", "--no-cache", "--json")
	if err == nil || !strings.Contains(err.Error(), "safety classification changed") || executed.Load() != 0 {
		t.Fatalf("drift not blocked: err=%v calls=%d", err, executed.Load())
	}
}

func TestReviewLimitAndNegotiatedCatalogProvenance(t *testing.T) {
	if _, err := executeWhatsApp(t, "connection", "ping", "--limit", "0", "--dry-run", "--json"); err == nil {
		t.Fatal("zero display limit accepted")
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["method"] == "notifications/initialized" {
			w.WriteHeader(202)
			return
		}
		var result any = map[string]any{"protocolVersion": "2025-06-18"}
		if body["method"] == "tools/list" {
			if r.Header.Get("MCP-Protocol-Version") != "2025-06-18" {
				t.Error("negotiated request header missing")
			}
			result = map[string]any{"tools": metamcp.Catalog()}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": body["id"], "result": result})
	}))
	defer s.Close()
	fixtureWhatsAppEnv(t, s.URL)
	db := filepath.Join(t.TempDir(), "catalog.db")
	out, err := executeWhatsApp(t, "catalog", "capture", "--db", db, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if json.Unmarshal([]byte(out), &v) != nil {
		t.Fatal(out)
	}
	if v["scope"].(map[string]any)["endpoint"] != s.URL+"/whatsapp_business_tools" {
		t.Fatal("capture endpoint differs from actual resource")
	}
	if v["scope"].(map[string]any)["protocol"] != "2025-06-18" {
		t.Fatalf("false protocol evidence: %s", out)
	}
}
