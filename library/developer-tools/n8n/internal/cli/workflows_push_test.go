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

func runWorkflowPush(t *testing.T, url string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("N8N_BASE_URL", url)
	t.Setenv("N8N_API_KEY", "test")
	t.Setenv("N8N_CONFIG", filepath.Join(t.TempDir(), "c.toml"))
	cmd := RootCmd()
	cmd.SetArgs(append([]string{"workflows", "push"}, args...))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

func newPushTestServer(t *testing.T, handler http.Handler) *httptest.Server {
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

func TestWorkflowsPushPreviewApplyAndCreate(t *testing.T) {
	testenv.Isolate(t)
	puts, posts := 0, 0
	srv := newPushTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/workflows/W" {
			_, _ = w.Write([]byte(`{"id":"W","name":"Flow","nodes":[{"id":"n1","name":"Fetch","type":"x","parameters":{"url":"a"}}],"connections":{},"settings":{}}`))
			return
		}
		if r.Method == http.MethodPut {
			puts++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["id"] != nil || body["active"] != nil {
				t.Errorf("PUT has read-only fields: %#v", body)
			}
			_, _ = w.Write([]byte(`{"versionId":"v2"}`))
			return
		}
		if r.Method == http.MethodPost {
			posts++
			_, _ = w.Write([]byte(`{"id":"NEW"}`))
			return
		}
		http.NotFound(w, r)
	}))
	file := filepath.Join(t.TempDir(), "workflow.json")
	data := `{"id":"W","name":"Flow","nodes":[{"id":"n1","name":"Fetch","type":"x","parameters":{"url":"b"}}],"connections":{},"settings":{},"active":true}`
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runWorkflowPush(t, srv.URL, file, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["applied"] != false || got["changed"] != true || puts != 0 {
		t.Fatalf("preview=%s puts=%d", out, puts)
	}
	out, err = runWorkflowPush(t, srv.URL, file, "--apply", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if puts != 1 {
		t.Fatalf("PUT count=%d output=%s", puts, out)
	}
	out, err = runWorkflowPush(t, srv.URL, file, "--create", "--apply", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Fatalf("POST count=%d output=%s", posts, out)
	}
	if !bytes.Contains([]byte(out), []byte("NEW")) {
		t.Fatalf("create output missing id: %s", out)
	}
}

func TestWorkflowsPushKeepsLiveStaticData(t *testing.T) {
	testenv.Isolate(t)
	puts := 0
	srv := newPushTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"id":"W","name":"Flow","nodes":[{"id":"n1","name":"Tick","type":"x","parameters":{}}],"connections":{},"settings":{},"staticData":{"node:Tick":{"last":1}}}`))
			return
		}
		puts++
		_, _ = w.Write([]byte(`{"versionId":"v2"}`))
	}))
	file := filepath.Join(t.TempDir(), "w.json")
	// An exported file: staticData stripped.
	if err := os.WriteFile(file, []byte(`{"id":"W","name":"Flow","nodes":[{"id":"n1","name":"Tick","type":"x","parameters":{}}],"connections":{},"settings":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runWorkflowPush(t, srv.URL, file, "--apply", "--json")
	if err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse %q: %v", out, err)
	}
	if got["changed"] != false || puts != 0 {
		t.Fatalf("untouched export reported a change: changed=%v puts=%d\n%s", got["changed"], puts, out)
	}
}
