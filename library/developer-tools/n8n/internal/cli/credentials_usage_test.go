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

// TestNovelCredentialsUsageHelpWires smoke-tests that the credentials usage command
// resolves at runtime and renders useful --help output. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Keep this smoke test when adding behavior-specific cases.
func TestNovelCredentialsUsageHelpWires(t *testing.T) {
	testenv.Isolate(t)
	cmd := RootCmd()
	cmd.SetArgs([]string{"credentials", "usage", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("credentials usage --help error = %v (novel command not wired correctly?)", err)
	}
	help := out.String()
	for _, want := range []string{"Usage:", "usage"} {
		if !strings.Contains(help, want) {
			t.Fatalf("credentials usage --help missing %q in output:\n%s", want, help)
		}
	}
}

func TestCredentialsUsageBehavior(t *testing.T) {
	testenv.Isolate(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ id, name, typ string }{{"c1", "Used", "slackApi"}, {"c2", "Unused", "googleApi"}} {
		if _, err := db.DB().Exec(`INSERT INTO credentials(id,data,name,type) VALUES(?,?,?,?)`, c.id, `{}`, c.name, c.typ); err != nil {
			t.Fatal(err)
		}
	}
	workflow := `{"id":"w1","name":"Shop","active":true,"nodes":[{"name":"Slack","type":"n8n-nodes-base.slack","credentials":{"slackApi":{"id":"c1","name":"Used"},"otherApi":{"id":"missing","name":"Gone"}}}]}`
	if _, err := db.DB().Exec(`INSERT INTO workflows(id,data,name,active,is_archived) VALUES(?,?,?,?,?)`, "w1", workflow, "Shop", 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) map[string]any {
		t.Helper()
		cmd := RootCmd()
		all := append([]string{"credentials", "usage"}, args...)
		all = append(all, "--db", dbPath, "--json")
		cmd.SetArgs(all)
		var out bytes.Buffer
		cmd.SetOut(&out)
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("decode %q: %v", out.String(), err)
		}
		return got
	}
	got := run()
	unused, ok := got["unused"].([]any)
	if !ok || len(unused) != 1 || unused[0].(map[string]any)["id"] != "c2" {
		t.Fatalf("unused = %#v", got["unused"])
	}
	dangling := got["dangling"].([]any)
	if len(dangling) != 1 || dangling[0].(map[string]any)["credential_id"] != "missing" {
		t.Fatalf("dangling = %#v", got["dangling"])
	}
	filtered := run("--min-refs", "2")
	if rows, ok := filtered["credentials"].([]any); !ok || len(rows) != 0 {
		t.Fatalf("min-refs credentials = %#v", filtered["credentials"])
	}
}
