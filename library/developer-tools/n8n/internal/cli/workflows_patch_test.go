package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/cliutil/testenv"
)

func runWorkflowPatch(t *testing.T, serverURL string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("N8N_BASE_URL", serverURL)
	t.Setenv("N8N_API_KEY", "test")
	t.Setenv("N8N_CONFIG", filepath.Join(t.TempDir(), "c.toml"))
	cmd := RootCmd()
	cmd.SetArgs(append([]string{"workflows", "patch"}, args...))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

func newPatchTestServer(t *testing.T, handler http.Handler) *httptest.Server {
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

func TestWorkflowsPatchPreviewAndApply(t *testing.T) {
	testenv.Isolate(t)
	puts := 0
	srv := newPatchTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workflows/W" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"W","name":"Example","active":true,"nodes":[{"id":"n1","name":"Fetch","type":"n8n-nodes-base.httpRequest","parameters":{"url":"a"}}],"connections":{},"settings":{}}`))
			return
		}
		if r.Method == http.MethodPut {
			puts++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["id"] != nil || body["active"] != nil {
				t.Errorf("PUT included read-only fields: %#v", body)
			}
			n := body["nodes"].([]any)[0].(map[string]any)
			p := n["parameters"].(map[string]any)
			if p["url"] != "b" {
				t.Errorf("PUT url = %v", p["url"])
			}
			w.Write([]byte(`{"versionId":"v2"}`))
			return
		}
		http.Error(w, "unexpected method", 500)
	}))
	out, err := runWorkflowPatch(t, srv.URL, "W", "--node", "Fetch", "--set", "url=b", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v: %s", err, out)
	}
	if got["applied"] != false || got["changed"] != true || puts != 0 {
		t.Fatalf("preview=%v puts=%d", got, puts)
	}
	d := got["diff"].(map[string]any)
	changes := d["param_changes"].([]any)
	if len(changes) != 1 {
		t.Fatalf("param changes = %v", changes)
	}
	out, err = runWorkflowPatch(t, srv.URL, "W", "--node", "Fetch", "--set", "url=b", "--apply", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if puts != 1 {
		t.Fatalf("PUT count=%d output=%s", puts, out)
	}
}

func TestWorkflowsPatchValidation(t *testing.T) {
	testenv.Isolate(t)
	srv := newPatchTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"W","nodes":[{"name":"Fetch","parameters":{}}]}`))
	}))
	for _, args := range [][]string{{"W", "--node", "Missing", "--disable"}, {"W", "--node", "Fetch", "--disable", "--enable"}, {"W", "--node", "Fetch"}, {"W", "--node", "Fetch", "--set", "broken"}} {
		if _, err := runWorkflowPatch(t, srv.URL, args...); err == nil {
			t.Errorf("args %v: expected usage error", args)
		}
	}
}
