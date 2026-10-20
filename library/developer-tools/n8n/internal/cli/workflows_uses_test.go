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

// TestNovelWorkflowsUsesHelpWires smoke-tests that the workflows uses command
// resolves at runtime and renders useful --help output. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Keep this smoke test when adding behavior-specific cases.
func TestNovelWorkflowsUsesHelpWires(t *testing.T) {
	testenv.Isolate(t)
	cmd := RootCmd()
	cmd.SetArgs([]string{"workflows", "uses", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workflows uses --help error = %v (novel command not wired correctly?)", err)
	}
	help := out.String()
	for _, want := range []string{"Usage:", "uses"} {
		if !strings.Contains(help, want) {
			t.Fatalf("workflows uses --help missing %q in output:\n%s", want, help)
		}
	}
}

func TestWorkflowsUsesBehavior(t *testing.T) {
	testenv.Isolate(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	data := `{"id":"w1","name":"Shop","active":true,"nodes":[{"name":"Slack","type":"n8n-nodes-base.slack"},{"name":"Google","type":"n8n-nodes-base.googleSheets"}]}`
	if _, err := db.DB().Exec(`INSERT INTO workflows(id,data,name,active,is_archived) VALUES(?,?,?,?,?)`, "w1", data, "Shop", 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want int
		bad  bool
	}{
		{"short node type", []string{"workflows", "uses", "--node-type", "slack"}, 1, false},
		{"no credential match", []string{"workflows", "uses", "--credential", "missing"}, 0, false},
		{"missing selector", []string{"workflows", "uses"}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := RootCmd()
			args := append(append([]string{}, tc.args...), "--db", dbPath, "--json")
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)
			err := cmd.Execute()
			if tc.bad {
				if err == nil {
					t.Fatal("expected selector validation error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got []map[string]any
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatalf("decode %q: %v", out.String(), err)
			}
			if len(got) != tc.want {
				t.Fatalf("got %d rows, want %d: %s", len(got), tc.want, out.String())
			}
			if tc.name == "short node type" && got[0]["node"] != "Slack" {
				t.Fatalf("unexpected row: %#v", got)
			}
		})
	}
}
