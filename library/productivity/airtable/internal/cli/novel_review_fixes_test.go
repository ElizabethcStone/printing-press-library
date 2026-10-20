package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/productivity/airtable/internal/store"
)

func TestRecordsUpsertCSVReviewFixes(t *testing.T) {
	tests := []struct {
		name        string
		csv         string
		wantCode    int
		wantError   string
		wantBatches []int
		wantHeader  string
	}{
		{name: "BOM header", csv: "\uFEFFEmail,Name\na@example.test,A\n", wantBatches: []int{1}, wantHeader: "Email"},
		{name: "duplicate header", csv: "Email,Email\na@example.test,A\n", wantCode: 2, wantError: `duplicate CSV header "Email"`},
		{name: "empty row skipped", csv: "Email,Name\n,\n" + strings.Repeat("x@example.test,A\n", 11), wantBatches: []int{10, 1}},
		{name: "empty merge key", csv: "Email,Name\na@example.test,A\n,B\n", wantCode: 2, wantError: "CSV line 3 has an empty --merge-on value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var batches []int
			var firstFields map[string]any
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var request struct {
					Records []struct {
						Fields map[string]any `json:"fields"`
					} `json:"records"`
				}
				if err := json.Unmarshal(body, &request); err != nil {
					t.Errorf("decode PATCH body: %v", err)
				}
				batches = append(batches, len(request.Records))
				if firstFields == nil && len(request.Records) > 0 {
					firstFields = request.Records[0].Fields
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"createdRecords":[],"updatedRecords":[]}`)
			})}}
			server.Start()
			t.Cleanup(server.Close)
			t.Setenv("AIRTABLE_API_KEY", "synthetic-test-token")
			t.Setenv("AIRTABLE_BASE_URL", server.URL)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			csvPath := filepath.Join(t.TempDir(), "input.csv")
			if err := os.WriteFile(csvPath, []byte(tt.csv), 0600); err != nil {
				t.Fatal(err)
			}
			root := RootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{"records", "upsert-csv", "--base", "app1", "--table", "Things", "--file", csvPath, "--merge-on", "Email", "--json"})
			err = root.Execute()
			got := 0
			if err != nil {
				got = ExitCode(err)
			}
			if got != tt.wantCode {
				t.Fatalf("exit code = %d, want %d (err=%v output=%s)", got, tt.wantCode, err, out.String())
			}
			if tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantError)
			}
			if fmt.Sprint(batches) != fmt.Sprint(tt.wantBatches) {
				t.Fatalf("batch sizes = %v, want %v", batches, tt.wantBatches)
			}
			if tt.wantHeader != "" && firstFields[tt.wantHeader] != "a@example.test" {
				t.Fatalf("first fields = %#v, want clean header %q", firstFields, tt.wantHeader)
			}
		})
	}
}

func TestAnalyticsTableFilterReviewFix(t *testing.T) {
	for _, xdg := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		t.Setenv(xdg, t.TempDir())
	}
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	items := []json.RawMessage{
		json.RawMessage(`{"id":"rec1","fields":{"Status":"Open"},"base_id":"app1","table":"Alpha"}`),
		json.RawMessage(`{"id":"rec2","fields":{"Status":"Open"},"base_id":"app1","table":"Beta"}`),
		json.RawMessage(`{"id":"rec3","fields":{"Status":"Closed"},"base_id":"app1","table":"Alpha"}`),
	}
	if stored, _, err := db.UpsertBatch("records", items); err != nil || stored != len(items) {
		t.Fatalf("UpsertBatch stored=%d err=%v", stored, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"analytics", "--type", "records", "--group-by", "fields.Status", "--table", "Alpha", "--db", dbPath, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("analytics: %v\n%s", err, out.String())
	}
	var result struct {
		Total  int              `json:"total_records"`
		Counts []analyticsCount `json:"counts"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode analytics output %q: %v", out.String(), err)
	}
	if result.Total != 2 || len(result.Counts) != 2 {
		t.Fatalf("analytics result = %#v, want only Alpha's two records", result)
	}
}
