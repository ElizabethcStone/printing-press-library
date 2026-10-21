package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/cliutil/testenv"
)

func runWorkflowExport(t *testing.T, url string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("N8N_BASE_URL", url)
	t.Setenv("N8N_API_KEY", "test")
	t.Setenv("N8N_CONFIG", filepath.Join(t.TempDir(), "c.toml"))
	cmd := RootCmd()
	cmd.SetArgs(append([]string{"workflows", "export"}, args...))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

func newExportTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func TestWorkflowsExportPagesAndStableFiles(t *testing.T) {
	testenv.Isolate(t)
	srv := newExportTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "next" {
			_, _ = w.Write([]byte(`{"data":[{"id":"2","name":"Beta","nodes":[],"updatedAt":"then","isArchived":true}],"nextCursor":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"1","name":"Order Intake","nodes":[],"updatedAt":"now","createdAt":"then","versionId":"v1","isArchived":false},{"id":"3","name":"Second Flow","nodes":[],"isArchived":false}],"nextCursor":"next"}`))
	}))
	dir := t.TempDir()
	out, err := runWorkflowExport(t, srv.URL, "--dir", dir, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if got["written"] != float64(2) || got["skipped_archived"] != float64(1) || got["scanned_pages"] != float64(2) {
		t.Fatalf("summary: %s", out)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files=%v", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "order-intake--1.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "second-flow--3.json")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "order-intake--1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wf map[string]any
	if err := json.Unmarshal(b, &wf); err != nil {
		t.Fatal(err)
	}
	if _, ok := wf["updatedAt"]; ok {
		t.Fatal("updatedAt was not stripped")
	}
	out, err = runWorkflowExport(t, srv.URL, "--dir", dir, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["unchanged"] != float64(2) {
		t.Fatalf("second run summary: %s", out)
	}
}
