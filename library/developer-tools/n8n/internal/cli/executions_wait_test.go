// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Novel command scaffold tests. Keep the wiring smoke test and add behavior cases as needed.

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/cliutil/testenv"
)

// TestNovelExecutionsWaitHelpWires smoke-tests that the executions wait command
// resolves at runtime and renders useful --help output. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Keep this smoke test when adding behavior-specific cases.
func TestNovelExecutionsWaitHelpWires(t *testing.T) {
	testenv.Isolate(t)
	cmd := RootCmd()
	cmd.SetArgs([]string{"executions", "wait", "--help"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("executions wait --help error = %v (novel command not wired correctly?)", err)
	}
	help := out.String()
	for _, want := range []string{"Usage:", "wait"} {
		if !strings.Contains(help, want) {
			t.Fatalf("executions wait --help missing %q in output:\n%s", want, help)
		}
	}
}

func TestExecutionsWaitTerminalAndTimeout(t *testing.T) {
	testenv.Isolate(t)
	var calls int
	srv := startTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/executions/42" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("includeData") == "true" {
			fmt.Fprint(w, `{"id":"42","workflowId":"W","status":"error","finished":true,"data":{"resultData":{"error":{"message":"boom","node":{"name":"Bad Node"}}}}}`)
			return
		}
		calls++
		if calls == 1 {
			fmt.Fprint(w, `{"id":"42","workflowId":"W","status":"running","finished":false}`)
		} else {
			fmt.Fprint(w, `{"id":"42","workflowId":"W","status":"error","finished":true,"startedAt":"2026-10-20T10:00:00Z","stoppedAt":"2026-10-20T10:00:01Z"}`)
		}
	}))
	defer srv.Close()
	configureWaitServer(t, srv.URL)
	cmd := RootCmd()
	cmd.SetArgs([]string{"executions", "wait", "42", "--interval", "10ms", "--max-wait", "5s", "--json"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err := cmd.Execute()
	var ce *cliError
	if !errors.As(err, &ce) || ce.code != 1 {
		t.Fatalf("error=%v, want exit code 1", err)
	}
	var result map[string]any
	if e := json.Unmarshal(out.Bytes(), &result); e != nil {
		t.Fatalf("output %q: %v", out.String(), e)
	}
	if result["status"] != "error" || result["failed_node"] != "Bad Node" {
		t.Fatalf("result=%#v", result)
	}

	success := startTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"42","workflowId":"W","status":"success","finished":true}`)
	}))
	defer success.Close()
	configureWaitServer(t, success.URL)
	cmd = RootCmd()
	cmd.SetArgs([]string{"executions", "wait", "42", "--interval", "10ms", "--max-wait", "1s", "--json"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("success error=%v", err)
	}

	never := startTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"42","workflowId":"W","status":"running","finished":false}`)
	}))
	defer never.Close()
	configureWaitServer(t, never.URL)
	cmd = RootCmd()
	cmd.SetArgs([]string{"executions", "wait", "42", "--interval", "10ms", "--max-wait", "50ms", "--json"})
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	err = cmd.Execute()
	if !errors.As(err, &ce) || ce.code != 6 {
		t.Fatalf("timeout error=%v", err)
	}
	if e := json.Unmarshal(out.Bytes(), &result); e != nil || result["timed_out"] != true {
		t.Fatalf("timeout output %q, error %v", out.String(), e)
	}

	cmd = RootCmd()
	cmd.SetArgs([]string{"executions", "wait", "--json"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err = cmd.Execute()
	if !errors.As(err, &ce) || ce.code != 2 {
		t.Fatalf("missing input error=%v", err)
	}
}

func configureWaitServer(t *testing.T, base string) {
	t.Helper()
	t.Setenv("N8N_BASE_URL", base+"/api/v1")
	t.Setenv("N8N_API_KEY", "test")
	t.Setenv("N8N_CONFIG", t.TempDir()+"/c.toml")
}

func startTestServer(t *testing.T, h http.Handler) *httptest.Server {
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
