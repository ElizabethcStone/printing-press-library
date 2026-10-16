package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/productivity/concur/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/productivity/concur/internal/cliutil/testenv"
	"github.com/spf13/cobra"
)

func TestNewestReportIDs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		n    int
		want []string
	}{
		{
			name: "newest reportDate first, wrapped in a content envelope",
			raw:  `{"content":[{"reportId":"A","reportDate":"2026-01-01"},{"reportId":"C","reportDate":"2026-09-28"},{"reportId":"B","reportDate":"2026-05-05"}]}`,
			n:    2,
			want: []string{"C", "B"},
		},
		{
			name: "bare array works too",
			raw:  `[{"reportId":"A","reportDate":"2026-01-01"},{"reportId":"B","reportDate":"2026-02-02"}]`,
			n:    5,
			want: []string{"B", "A"},
		},
		{
			name: "same-day reports tie-break deterministically",
			raw:  `[{"reportId":"A","reportDate":"2026-10-15"},{"reportId":"B","reportDate":"2026-10-15"}]`,
			n:    2,
			want: []string{"B", "A"},
		},
		{
			name: "objects without a reportId are ignored",
			raw:  `{"links":[{"rel":"next"}],"content":[{"name":"x"}]}`,
			n:    3,
			want: []string{},
		},
		{
			name: "garbage yields nothing",
			raw:  `<html>not json</html>`,
			n:    3,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newestReportIDs([]byte(tt.raw), tt.n)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newestReportIDs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPolicyFromReportHeader(t *testing.T) {
	id, name := policyFromReportHeader([]byte(`{"reportId":"R","policyId":"P1","policy":"Example Policy (EX-1)"}`))
	if id != "P1" || name != "Example Policy (EX-1)" {
		t.Errorf("got (%q, %q)", id, name)
	}
	if id, _ := policyFromReportHeader([]byte(`{"reportId":"R"}`)); id != "" {
		t.Errorf("header without a policy should yield no id, got %q", id)
	}
	if id, _ := policyFromReportHeader([]byte(`not json`)); id != "" {
		t.Errorf("unparsable header should yield no id, got %q", id)
	}
}

// A caller-supplied policyId must never be silently replaced by an inferred
// one; the helper has to bail out before it makes any request.
func TestRetryReportCreate_NeverOverridesExplicitPolicy(t *testing.T) {
	origErr := errors.New("policyId is required")
	data, status, err := retryReportCreateWithInferredPolicy(&cobra.Command{}, nil, "", nil,
		map[string]any{"name": "x", "policyId": "MINE"}, "user", "TRAVELER", origErr)
	if data != nil || status != 0 || !errors.Is(err, origErr) {
		t.Errorf("got (%v, %d, %v), want (nil, 0, origErr)", data, status, err)
	}
}

// browserTripwire puts a fake agent-browser first on PATH. Any invocation of
// the browser fallback leaves a marker the caller then checks for.
func browserTripwire(t *testing.T) (marker string) {
	t.Helper()
	dir := t.TempDir()
	marker = filepath.Join(dir, "browser_invoked")
	script := "#!/bin/bash\ntouch \"$MOCK_BROWSER_MARKER\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "agent-browser"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing mock agent-browser: %v", err)
	}
	t.Setenv("PATH", dir+string(filepath.ListSeparator)+os.Getenv("PATH"))
	t.Setenv("MOCK_BROWSER_MARKER", marker)
	return marker
}

// policyGatedServer fakes a policy-gated tenant: a create without a policyId
// is rejected, the user's most recent report carries a policy, and a create
// that does carry one is answered by onRetry. It records every create body.
type policyGatedServer struct {
	mu    sync.Mutex
	posts []map[string]any
}

func (s *policyGatedServer) recorded() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.posts...)
}

func startPolicyGatedServer(t *testing.T, onRetry func(w http.ResponseWriter)) *policyGatedServer {
	t.Helper()
	testenv.Isolate(t, cliutil.StateDir)
	s := &policyGatedServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reports"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			s.posts = append(s.posts, body)
			s.mu.Unlock()
			if _, has := body["policyId"]; !has {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"validationErrors":[{"source":"policyId","message":"policyId is required","id":"field.required"}]}`))
				return
			}
			onRetry(w)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/reports"):
			_, _ = w.Write([]byte(`{"content":[{"reportId":"OLDER","reportDate":"2026-01-01"},{"reportId":"NEWEST","reportDate":"2026-09-28"}]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/reports/NEWEST"):
			_, _ = w.Write([]byte(`{"reportId":"NEWEST","policyId":"POLICY-EX","policy":"Example Policy (EX-1)"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("CONCUR_BASE_URL", server.URL)
	// Lets a mistaken browser fallback get as far as the tripwire. Without it
	// the fallback aborts early on this untrusted test host, and the "never
	// rerouted" assertions would pass for the wrong reason.
	t.Setenv("CONCUR_UI_BASE_URL", "https://us2.concursolutions.com")
	t.Setenv("PRINTING_PRESS_VERIFY", "1")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "1")
	return s
}

func runReportsCreate(t *testing.T) (stdout, stderr string, err error) {
	t.Helper()
	cmd := RootCmd()
	cmd.SetArgs([]string{"reports", "create", "--user-id", "test-user-id", "--name", "Example Trip", "--yes", "--json"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestReportsCreate_InfersPolicyOverHTTP(t *testing.T) {
	marker := browserTripwire(t)
	srv := startPolicyGatedServer(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"uri":"https://example.invalid/expensereports/v4/users/u/context/TRAVELER/reports/NEWREPORT"}`))
	})

	out, errOut, err := runReportsCreate(t)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, errOut)
	}

	posts := srv.recorded()
	if len(posts) != 2 {
		t.Fatalf("expected 2 POSTs (rejected, then retried with a policy), got %d: %v", len(posts), posts)
	}
	if _, has := posts[0]["policyId"]; has {
		t.Errorf("first POST should not carry a policyId: %v", posts[0])
	}
	if posts[1]["policyId"] != "POLICY-EX" || posts[1]["name"] != "Example Trip" {
		t.Errorf("retry POST should carry the inferred policy and the original name: %v", posts[1])
	}
	if !strings.Contains(errOut, "Example Policy (EX-1)") {
		t.Errorf("stderr should name the policy that was used, got: %q", errOut)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("browser fallback was invoked, but the HTTP retry should have made it unnecessary")
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if ok, _ := envelope["success"].(bool); !ok {
		t.Errorf("expected success envelope, got %v", envelope)
	}
}

// A retry that fails after the API may have accepted it is ambiguous: the
// report could already exist. It must surface as an error, never be sent a
// third time, and never be rerouted through the browser fallback, which would
// create a duplicate report.
func TestReportsCreate_FailedRetryIsNeitherRepeatedNorRerouted(t *testing.T) {
	failures := []struct {
		name string
		fail func(w http.ResponseWriter)
	}{
		{"503 on the retry", func(w http.ResponseWriter) { w.WriteHeader(http.StatusServiceUnavailable) }},
		{"connection dropped on the retry", func(w http.ResponseWriter) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = conn.Close()
		}},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			marker := browserTripwire(t)
			srv := startPolicyGatedServer(t, tt.fail)

			_, errOut, err := runReportsCreate(t)
			if err == nil {
				t.Fatalf("expected an error when the retry fails; stderr: %s", errOut)
			}
			if posts := srv.recorded(); len(posts) != 2 {
				t.Errorf("expected exactly 2 POSTs (rejected, then one failed retry), got %d: %v", len(posts), posts)
			}
			if _, statErr := os.Stat(marker); statErr == nil {
				t.Error("browser fallback was invoked after an ambiguous retry failure; that can create a duplicate report")
			}
		})
	}
}
