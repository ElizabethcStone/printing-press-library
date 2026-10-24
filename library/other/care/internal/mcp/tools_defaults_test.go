package mcp

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mvanhorn/printing-press-library/library/other/care/internal/cliutil"
)

func TestMCPGraphQLToolSuppliesDefaultQueryAndVariables(t *testing.T) {
	var receivedBody map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"getCalendar":{"id":"cal-1"}}}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("SetHomeOverride: %v", err)
	}
	defer restore()

	certPath := filepath.Join(home, "ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("writing CA cert: %v", err)
	}
	cfgDir, err := cliutil.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfgContent := "base_url = " + strconvQuote(srv.URL) + "\nskip_tls_verify = true\nca_cert = " + strconvQuote(certPath) + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("writing config.toml: %v", err)
	}
	t.Setenv("CARE_BASE_URL", srv.URL)
	t.Setenv("CARE_SESSION_COOKIE", "csc-session=test")

	s := server.NewMCPServer("graphql-defaults", "test")
	RegisterTools(s)
	tool := s.ListTools()["calendars_get"]
	if tool == nil {
		t.Fatal("calendars_get tool not registered")
	}

	res, err := tool.Handler(context.Background(), mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Name:      "calendars_get",
			Arguments: map[string]any{"operationName": "GetCalendar"},
		},
	})
	if err != nil {
		t.Fatalf("calendars_get returned error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("calendars_get returned tool error: %#v", res)
	}
	q, _ := receivedBody["query"].(string)
	if !strings.Contains(q, "GetCalendar") {
		t.Fatalf("expected default GraphQL query in request body, got: %#v", receivedBody)
	}
	vars, _ := receivedBody["variables"].(map[string]any)
	if vars == nil || vars["providerId"] == nil {
		t.Fatalf("expected default GraphQL variables in request body, got: %#v", receivedBody)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
