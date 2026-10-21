// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Novel command scaffold tests. Keep the wiring smoke test and add behavior cases as needed.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/cliutil/testenv"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/store"
)

// TestNovelExecutionsFailuresHelpWires smoke-tests that the executions failures command
// resolves at runtime and renders useful --help output. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Keep this smoke test when adding behavior-specific cases.
func TestNovelExecutionsFailuresHelpWires(t *testing.T) {
	testenv.Isolate(t)
	cmd := RootCmd()
	cmd.SetArgs([]string{"executions", "failures", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("executions failures --help error = %v (novel command not wired correctly?)", err)
	}
	help := out.String()
	for _, want := range []string{"Usage:", "failures"} {
		if !strings.Contains(help, want) {
			t.Fatalf("executions failures --help missing %q in output:\n%s", want, help)
		}
	}
}

func TestExecutionsFailuresLocalGroups(t *testing.T) {
	testenv.Isolate(t)
	dbPath := filepath.Join(t.TempDir(), "mirror.db")
	s, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ id, wf, at string }{{"e1", "A", "2026-10-20T10:00:00.000Z"}, {"e2", "A", "2026-10-20T11:00:00.000Z"}, {"e3", "B", "2026-10-20T12:00:00.000Z"}} {
		_, err = s.DB().Exec(`INSERT INTO executions(id,data,workflow_id,status,started_at) VALUES(?, '{}', ?, 'error', ?)`, x.id, x.wf, x.at)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, x := range []struct{ id, name string }{{"A", "Alpha"}, {"B", "Beta"}} {
		_, err = s.DB().Exec(`INSERT INTO workflows(id,data,name) VALUES(?, '{}', ?)`, x.id, x.name)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.EnsureExecutionFailures(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"e1", "e2"} {
		if err := s.PutExecutionFailure(context.Background(), store.ExecutionFailureRow{ExecutionID: id, Node: "Boom", Message: "bad"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	run := func(extra ...string) map[string]any {
		t.Helper()
		cmd := RootCmd()
		cmd.SetArgs(append([]string{"executions", "failures", "--since", "30d", "--no-fetch", "--db", dbPath, "--json"}, extra...))
		var out bytes.Buffer
		var stderr bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&stderr)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatalf("decode %q: %v", out.String(), err)
		}
		return v
	}
	v := run()
	groups := v["groups"].([]any)
	if len(groups) != 2 {
		t.Fatalf("groups=%#v", groups)
	}
	g0 := groups[0].(map[string]any)
	g1 := groups[1].(map[string]any)
	if g0["workflow_id"] != "A" || g0["node"] != "Boom" || g0["count"] != float64(2) {
		t.Fatalf("first group=%#v", g0)
	}
	if g1["workflow_id"] != "B" || g1["node"] != "" || g1["count"] != float64(1) || v["uncached_remaining"] != float64(1) {
		t.Fatalf("second group/result=%#v / %#v", g1, v)
	}
	if got := run("--limit", "0")["groups"].([]any); len(got) != 2 {
		t.Fatalf("--limit 0 must mean no limit, got %d groups", len(got))
	}
	if got := run("--limit", "1")["groups"].([]any); len(got) != 1 {
		t.Fatalf("--limit 1 got %d groups", len(got))
	}
	if got := run("--node", "boom")["groups"].([]any); len(got) != 1 {
		t.Fatalf("node filter groups=%#v", got)
	}
	w := run("--workflow", "B")
	if w["total_failures"] != float64(1) {
		t.Fatalf("workflow result=%#v", w)
	}
	s, err = store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`DELETE FROM executions`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	empty := run()
	if got := empty["groups"].([]any); len(got) != 0 {
		t.Fatalf("empty groups=%#v", got)
	}
	if empty["note"] == nil {
		t.Fatalf("empty result missing note: %#v", empty)
	}
}
