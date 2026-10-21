package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrocoExportDryRunPreservesOutput(t *testing.T) {
	calls := 0
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		t.Error("dry run reached network")
	})
	f.dryRun = true
	// A configuration read would fail, proving preview precedes auth/config I/O.
	f.configPath = t.TempDir()
	dir := t.TempDir()
	existing := filepath.Join(dir, "backup.jsonl")
	if err := os.WriteFile(existing, []byte("sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{existing, filepath.Join(dir, "absent", "backup.jsonl")} {
		out, err := crocoEngineTestRun(newExportCmd(f), "forms-api", "--output", output)
		if err != nil || !strings.Contains(out, `"dry_run":true`) {
			t.Fatalf("preview = %q, %v", out, err)
		}
	}
	if calls != 0 {
		t.Fatalf("requests = %d", calls)
	}
	body, err := os.ReadFile(existing)
	if err != nil || string(body) != "sentinel" {
		t.Fatalf("backup = %q, %v", body, err)
	}
	info, err := os.Stat(existing)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("dry run changed permissions: %v, %v", info, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("dry run created files/directories: %v, %v", entries, err)
	}
}

func TestCrocoExportFailurePreservesBackup(t *testing.T) {
	for _, format := range []string{"jsonl", "json"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "backup")
			if err := os.WriteFile(path, []byte("previous backup"), 0o644); err != nil {
				t.Fatal(err)
			}
			old := resourceReadConfigs["forms-api"]
			resourceReadConfigs["forms-api"] = resourceReadConfig{paginationType: "page", cursorParam: "page", limitParam: "per_page", pageSize: 1}
			t.Cleanup(func() { resourceReadConfigs["forms-api"] = old })
			calls := 0
			f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("page") == "1" {
					// Larger than the writer buffer: JSONL has reached staging before failure.
					json.NewEncoder(w).Encode([]any{map[string]any{"id": 1, "padding": strings.Repeat("x", 8192)}})
					return
				}
				http.Error(w, `{"code":"fixture_failure","message":"fixture unavailable"}`, http.StatusBadRequest)
			})
			_, err := crocoEngineTestRun(newExportCmd(f), "forms-api", "--format", format, "--output", path, "--no-cache")
			if err == nil || calls != 2 {
				t.Fatalf("calls = %d, error = %v", calls, err)
			}
			body, err := os.ReadFile(path)
			if err != nil || string(body) != "previous backup" {
				t.Fatalf("backup = %q, %v", body, err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o644 {
				t.Fatalf("failed export changed backup permissions: %v, %v", info, err)
			}
			assertCrocoExportNoTemps(t, dir)
		})
	}
}

func TestCrocoExportPublishesPrivateCompleteFile(t *testing.T) {
	for _, format := range []string{"jsonl", "json"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "backup")
			if err := os.WriteFile(path, []byte("previous backup"), 0o644); err != nil {
				t.Fatal(err)
			}
			oldFile, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer oldFile.Close()
			oldInfo, err := oldFile.Stat()
			if err != nil {
				t.Fatal(err)
			}
			f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				body, err := os.ReadFile(path)
				if err != nil || string(body) != "previous backup" {
					t.Errorf("destination changed before fetch completed: %q, %v", body, err)
				}
				staged, err := filepath.Glob(filepath.Join(dir, ".crocoblock-export-*"))
				if err != nil || len(staged) != 1 {
					t.Errorf("staging files = %v, %v", staged, err)
				} else if info, err := os.Stat(staged[0]); err != nil || info.Mode().Perm() != 0o600 {
					t.Errorf("staging permissions = %v, %v", info, err)
				}
				fmt.Fprint(w, `[{"id":1},{"id":2}]`)
			})
			_, err = crocoEngineTestRun(newExportCmd(f), "forms-api", "--format", format, "--output", path, "--limit", "1", "--no-cache")
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("output permissions = %v, %v", info, err)
			}
			if os.SameFile(oldInfo, info) {
				t.Fatal("destination was edited in place instead of atomically replaced")
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var value any
			if err := json.Unmarshal(body, &value); err != nil {
				t.Fatalf("output is invalid %s: %q, %v", format, body, err)
			}
			if format == "json" {
				items, ok := value.([]any)
				if !ok || len(items) != 1 {
					t.Fatalf("limited output = %v", value)
				}
			} else if value.(map[string]any)["id"] != float64(1) {
				t.Fatalf("JSONL output = %v", value)
			}
			assertCrocoExportNoTemps(t, dir)
		})
	}
}

func assertCrocoExportNoTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "backup" {
		t.Fatalf("export left temporary files: %v, %v", entries, err)
	}
}
