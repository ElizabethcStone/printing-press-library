// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Novel command scaffold tests. Keep the wiring smoke test and add behavior cases as needed.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/cliutil/testenv"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/store"
)

// TestNovelWorkflowsStaleHelpWires smoke-tests that the workflows stale command
// resolves at runtime and renders useful --help output. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Keep this smoke test when adding behavior-specific cases.
func TestNovelWorkflowsStaleHelpWires(t *testing.T) {
	testenv.Isolate(t)
	cmd := RootCmd()
	cmd.SetArgs([]string{"workflows", "stale", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workflows stale --help error = %v (novel command not wired correctly?)", err)
	}
	help := out.String()
	for _, want := range []string{"Usage:", "stale"} {
		if !strings.Contains(help, want) {
			t.Fatalf("workflows stale --help missing %q in output:\n%s", want, help)
		}
	}
}

func TestWorkflowsStaleBehavior(t *testing.T) {
	testenv.Isolate(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	workflows := []struct {
		id, name, settings string
		archived           int
	}{
		{"never", "Never", "{}", 0},
		{"recent", "Recent", `{"errorWorkflow":"err-wf"}`, 0},
		{"unprotected", "Unprotected", "{}", 0},
		{"archived", "Archived", "{}", 1},
	}
	for _, w := range workflows {
		data := fmt.Sprintf(`{"id":%q,"name":%q,"active":true,"isArchived":%t,"settings":%s,"nodes":[]}`, w.id, w.name, w.archived == 1, w.settings)
		if _, err := db.DB().Exec(`INSERT INTO workflows(id,data,name,active,is_archived) VALUES(?,?,?,?,?)`, w.id, data, w.name, 1, w.archived); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	for _, id := range []string{"recent", "unprotected"} {
		if _, err := db.DB().Exec(`INSERT INTO executions(id,data,workflow_id,status,started_at) VALUES(?,?,?,?,?)`, "e-"+id, `{}`, id, "success", started); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := RootCmd()
	cmd.SetArgs([]string{"workflows", "stale", "--days", "7", "--db", dbPath, "--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Workflows []struct {
			WorkflowID string   `json:"workflow_id"`
			Reasons    []string `json:"reasons"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", out.String(), err)
	}
	ids := map[string][]string{}
	for _, w := range got.Workflows {
		ids[w.WorkflowID] = w.Reasons
	}
	if _, ok := ids["never"]; !ok {
		t.Fatalf("never-run workflow missing: %#v", ids)
	}
	if !slices.Contains(ids["never"], "never_ran") {
		t.Fatalf("never reasons = %#v", ids["never"])
	}
	if _, ok := ids["recent"]; ok {
		t.Fatalf("recent protected workflow unexpectedly listed: %#v", ids)
	}
	if !slices.Contains(ids["unprotected"], "no_error_workflow") {
		t.Fatalf("unprotected reasons = %#v", ids["unprotected"])
	}
	if _, ok := ids["archived"]; ok {
		t.Fatalf("archived workflow unexpectedly listed: %#v", ids)
	}
}

func TestWorkflowsStaleWithoutExecutionsSkipsRunReasons(t *testing.T) {
	testenv.Isolate(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []struct{ id, settings string }{{"a", `{"errorWorkflow":"e"}`}, {"b", `{}`}} {
		data := fmt.Sprintf(`{"id":%q,"name":%q,"active":true,"settings":%s,"nodes":[]}`, w.id, w.id, w.settings)
		if _, err := db.DB().Exec(`INSERT INTO workflows(id,data,name,active,is_archived) VALUES(?,?,?,1,0)`, w.id, data, w.id); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()
	cmd := RootCmd()
	cmd.SetArgs([]string{"workflows", "stale", "--db", dbPath, "--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Workflows []struct {
			WorkflowID string   `json:"workflow_id"`
			Reasons    []string `json:"reasons"`
		} `json:"workflows"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Workflows) != 1 || got.Workflows[0].WorkflowID != "b" || len(got.Workflows[0].Reasons) != 1 || got.Workflows[0].Reasons[0] != "no_error_workflow" || got.Note == "" {
		t.Fatalf("want only b/no_error_workflow plus a note, got %s", out.String())
	}
}
