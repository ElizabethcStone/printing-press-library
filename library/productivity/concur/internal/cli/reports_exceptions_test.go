// Copyright 2026 Allen Lew and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestReportsExceptions_BareArrayResponse(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{
				"exceptionCode": "UNDEFEXP",
				"exceptionVisibility": "visible",
				"isBlocking": true,
				"message": "A line still has expense type Undefined",
				"parameters": null,
				"allocationId": "550e8400-e29b-41d4-a716-446655440002",
				"expenseId": "550e8400-e29b-41d4-a716-446655440001",
				"parentExpenseId": null
			}
		]`))
	}))
	defer server.Close()

	t.Setenv("CONCUR_BASE_URL", server.URL)
	t.Setenv("PRINTING_PRESS_VERIFY", "1")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "1")

	cmd := RootCmd()
	cmd.SetArgs([]string{
		"reports", "exceptions", "550e8400-e29b-41d4-a716-446655440000",
		"--user-id", "test-user-id",
		"--json",
	})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(os.Stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPath := "/expensereports/v4/users/test-user-id/context/TRAVELER/reports/550e8400-e29b-41d4-a716-446655440000/exceptions"
	if gotPath != wantPath {
		t.Errorf("request path = %q, want %q", gotPath, wantPath)
	}

	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to unmarshal output JSON: %v", err)
	}

	results, ok := envelope["results"].([]any)
	if !ok {
		t.Fatalf("expected a results array in envelope, got %+v", envelope)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 exception, got %d", len(results))
	}

	exception, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected result shape: %+v", results[0])
	}
	if exception["exceptionCode"] != "UNDEFEXP" {
		t.Errorf("exceptionCode = %v, want %q", exception["exceptionCode"], "UNDEFEXP")
	}
	if exception["isBlocking"] != true {
		t.Errorf("isBlocking = %v, want true", exception["isBlocking"])
	}
}

func TestReportsExceptions_MissingArgsAndFlags(t *testing.T) {
	// Missing positional arg
	cmd := RootCmd()
	cmd.SetArgs([]string{
		"reports", "exceptions",
		"--user-id", "test-user-id",
		"--json",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error with missing positional arg, got nil")
	}

	// Missing user-id
	cmd = RootCmd()
	cmd.SetArgs([]string{
		"reports", "exceptions", "550e8400-e29b-41d4-a716-446655440000",
		"--json",
	})
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error with missing --user-id, got nil")
	}

	// Invalid context-type
	cmd = RootCmd()
	cmd.SetArgs([]string{
		"reports", "exceptions", "550e8400-e29b-41d4-a716-446655440000",
		"--user-id", "test-user-id",
		"--context-type", "INVALID_CONTEXT_TYPE",
		"--json",
	})
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error with invalid --context-type, got nil")
	}
}

// PATCH(amend-2026-10-18: reports exceptions served a fixed error from cache) --
// the report is edited in the web UI as well as through the CLI, and nothing
// can evict the response cache for the former, so this view must not be cached.
func TestReportsExceptions_ReadsLiveWhenAResponseIsCached(t *testing.T) {
	t.Setenv("CONCUR_CACHE_DIR", t.TempDir())

	hits := 0
	blocking := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		if blocking {
			_, _ = w.Write([]byte(`[{"exceptionCode":"UNDEFEXP","isBlocking":true,"message":"A line still has expense type Undefined","expenseId":"line-1"}]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	t.Setenv("CONCUR_BASE_URL", server.URL)

	exceptionCount := func() int {
		t.Helper()
		cmd := RootCmd()
		cmd.SetArgs([]string{"reports", "exceptions", "report-1", "--user-id", "test-user-id", "--json"})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(os.Stderr)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var envelope struct {
			Results []json.RawMessage `json:"results"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatalf("failed to unmarshal output JSON: %v\n%s", err, out.String())
		}
		return len(envelope.Results)
	}

	if got := exceptionCount(); got != 1 {
		t.Fatalf("first read: %d exceptions, want 1", got)
	}

	// The error gets fixed (here, as it would be in the web UI) and the same
	// report is read again well inside the cache TTL.
	blocking = false
	if got := exceptionCount(); got != 0 {
		t.Errorf("second read: %d exceptions, want 0 -- the cleared error was served from cache", got)
	}
	if hits != 2 {
		t.Errorf("server was asked %d times, want 2 (every read must go to Concur)", hits)
	}
}
