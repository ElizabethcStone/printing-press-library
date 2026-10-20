// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Novel command scaffold tests. Keep the wiring smoke test and add behavior cases as needed.

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/cliutil/testenv"
)

// TestNovelWorkflowsDiffHelpWires smoke-tests that the workflows diff command
// resolves at runtime and renders useful --help output. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Keep this smoke test when adding behavior-specific cases.
func TestNovelWorkflowsDiffHelpWires(t *testing.T) {
	testenv.Isolate(t)
	cmd := RootCmd()
	cmd.SetArgs([]string{"workflows", "diff", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workflows diff --help error = %v (novel command not wired correctly?)", err)
	}
	help := out.String()
	for _, want := range []string{"Usage:", "diff"} {
		if !strings.Contains(help, want) {
			t.Fatalf("workflows diff --help missing %q in output:\n%s", want, help)
		}
	}
}

func TestWorkflowsDiffVersion(t *testing.T) {
	testenv.Isolate(t)
	base := `{"id":"W","name":"Flow","nodes":[{"id":"1","name":"HTTP","type":"n8n-nodes-base.httpRequest","parameters":{"url":"old"}}],"connections":{},"active":false}`
	target := `{"versionId":"V","workflowId":"W","name":"Flow","nodes":[{"id":"1","name":"HTTP","type":"n8n-nodes-base.httpRequest","parameters":{"url":"new"}},{"id":"2","name":"Added","type":"n8n-nodes-base.noOp","parameters":{}}],"connections":{}}`
	identical := `{"id":"W","name":"Flow","nodes":[{"id":"1","name":"HTTP","type":"n8n-nodes-base.httpRequest","parameters":{"url":"old"}}],"connections":{},"active":false}`
	useTarget := true
	srv := startDiffTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/workflows/W":
			if useTarget {
				fmt.Fprint(w, base)
			} else {
				fmt.Fprint(w, identical)
			}
		case "/api/v1/workflows/W/versions/V":
			if useTarget {
				fmt.Fprint(w, target)
			} else {
				fmt.Fprint(w, identical)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("N8N_BASE_URL", srv.URL+"/api/v1")
	t.Setenv("N8N_API_KEY", "test")
	t.Setenv("N8N_CONFIG", t.TempDir()+"/c.toml")
	run := func() map[string]any {
		t.Helper()
		cmd := RootCmd()
		cmd.SetArgs([]string{"workflows", "diff", "W", "--version", "V", "--json"})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatalf("output %q: %v", out.String(), err)
		}
		return v
	}
	v := run()
	if v["changed"] != true {
		t.Fatalf("result=%#v", v)
	}
	d := v["diff"].(map[string]any)
	// base is saved version V (url "new" + node Added), target is the current workflow (url "old").
	pc := d["param_changes"].([]any)
	if len(pc) != 1 || pc[0].(map[string]any)["before"] != "new" || pc[0].(map[string]any)["after"] != "old" ||
		len(d["nodes_removed"].([]any)) != 1 || len(d["nodes_added"].([]any)) != 0 || d["active_changed"] != false {
		t.Fatalf("diff=%#v", d)
	}
	useTarget = false
	v = run()
	if v["changed"] != false {
		t.Fatalf("identical diff=%#v", v)
	}
}

func startDiffTestServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local test server unavailable: %v", err)
	}
	s := httptest.NewUnstartedServer(h)
	s.Listener = l
	s.Start()
	return s
}
