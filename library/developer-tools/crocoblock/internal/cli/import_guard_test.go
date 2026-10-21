package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
)

func importGuardInput(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.jsonl")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportGuardNeverReplaysCommittedFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{
		{"standard", map[string]string{"Idempotency-Key": "fixture"}},
		{"x-prefixed", map[string]string{"X-Idempotency-Key": "fixture"}},
		{"standard-mixed-case", map[string]string{"iDEMPOTENCY-kEY": "fixture"}},
		{"x-prefixed-mixed-case", map[string]string{"x-IDEMPOTENCY-key": "fixture"}},
		{"both", map[string]string{"Idempotency-Key": "fixture", "X-Idempotency-Key": "fixture-x"}},
		{"both-mixed-case", map[string]string{"idempotency-key": "fixture", "x-IDEMPOTENCY-key": "fixture-x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commits atomic.Int32
			f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				commits.Add(1)
				if r.Method != "POST" || r.URL.Path != "/wp/v2/jet-form-builder" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Idempotency-Key") != "" || r.Header.Get("X-Idempotency-Key") != "" {
					t.Error("configured key reached server")
				}
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"fixture failure after commit"}`))
			})
			clientHooks = append(clientHooks, func(c *client.Client) error { c.Config.Headers = tc.headers; return nil })
			out, err := crocoEngineTestRun(newImportCmd(f), "forms-api", "--input", importGuardInput(t, "{\"title\":\"Fixture\"}\n"))
			if err == nil || !strings.Contains(err.Error(), "inspect the site before replaying") {
				t.Fatalf("expected inspect-before-replay error: %v", err)
			}
			if commits.Load() != 1 {
				t.Fatalf("committed %d writes, want 1", commits.Load())
			}
			var counts map[string]int
			if json.Unmarshal([]byte(out), &counts) != nil || counts["failed"] != 1 || counts["succeeded"] != 0 {
				t.Fatalf("unexpected counts: %s", out)
			}
		})
	}
}

func TestImportGuardDryRunDoesNotAccessInputClientOrStore(t *testing.T) {
	var calls atomic.Int32
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	f.dryRun = true
	// Invalid config and missing input must not be opened during a dry run.
	f.configPath = t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "must-not-create.db")
	t.Setenv("CROCOBLOCK_DB", dbPath)
	clientHooks = append(clientHooks, func(c *client.Client) error { t.Error("dry run constructed a client"); return nil })
	out, err := crocoEngineTestRun(newImportCmd(f), "forms-api", "--input", filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if json.Unmarshal([]byte(out), &result) != nil || result["dry_run"] != true {
		t.Fatalf("unexpected dry-run output: %s", out)
	}
	if _, exists := result["succeeded"]; exists {
		t.Fatalf("dry run reported successes: %s", out)
	}
	if calls.Load() != 0 {
		t.Fatal("dry run reached server")
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("dry run accessed store: %v", err)
	}
}

func TestImportGuardStopsTypedAuthAndRefusal(t *testing.T) {
	for _, tc := range []struct {
		name              string
		status            int
		body, contentType string
		code              int
	}{
		{"auth", 401, `{"message":"invalid credentials"}`, "application/json", 4},
		{"permission", 403, `{"message":"permission denied"}`, "application/json", 4},
		{"challenge", 200, `<html><title>Just a moment...</title><body>Cloudflare challenge</body></html>`, "text/html", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "challenge" {
				// Exercise a typed refusal from the HTML classifier even with
				// HTTP 200, where a status-only import check cannot stop it.
				old := classifyHTMLPayload
				classifyHTMLPayload = func([]byte) error { return rateLimitErr(errors.New("synthetic firewall refusal")) }
				t.Cleanup(func() { classifyHTMLPayload = old })
			}
			var calls atomic.Int32
			f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			out, err := crocoEngineTestRun(newImportCmd(f), "forms-api", "--input", importGuardInput(t, "{}\n{}\n{}\n"))
			if err == nil || ExitCode(err) != tc.code {
				t.Fatalf("expected code %d, got %d: %v", tc.code, ExitCode(err), err)
			}
			if calls.Load() != 1 {
				t.Fatalf("auth/refusal sent %d requests", calls.Load())
			}
			var counts map[string]int
			if json.Unmarshal([]byte(out), &counts) != nil || counts["failed"] != 1 || counts["skipped"] != 2 {
				t.Fatalf("unexpected counts: %s", out)
			}
		})
	}
}

func TestImportGuardRejectsOtherSiteStore(t *testing.T) {
	var calls atomic.Int32
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	db, err := crocoOpenStore(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CrocoEvent(context.Background(), "https://another-fixture.invalid", "create", "forms", nil, nil)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, err = crocoEngineTestRun(newImportCmd(f), "forms-api", "--input", importGuardInput(t, "{}\n"))
	if err == nil || !strings.Contains(err.Error(), "failed record") {
		t.Fatalf("expected rejected import: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("cross-site import sent %d writes", calls.Load())
	}
}

func TestImportGuardPreservesJSONLCounts(t *testing.T) {
	var calls atomic.Int32
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":123}`))
	})
	out, err := crocoEngineTestRun(newImportCmd(f), "forms-api", "--input", importGuardInput(t, "# comment\n\n{}\ninvalid\n{}\n"))
	if err == nil || ExitCode(err) != 5 {
		t.Fatalf("expected failed line: %v", err)
	}
	var counts map[string]int
	if json.Unmarshal([]byte(out), &counts) != nil || counts["failed"] != 1 || counts["succeeded"] != 2 || counts["skipped"] != 2 {
		t.Fatalf("unexpected counts: %s", out)
	}
	if calls.Load() != 2 {
		t.Fatalf("sent %d requests", calls.Load())
	}
}
