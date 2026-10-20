package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/productivity/airtable/internal/store"
)

func TestGreptileUpsertCSVDryRun(t *testing.T) {
	tests := []struct {
		name        string
		csv         string
		wantCode    int
		wantRows    int
		wantBatches int
	}{
		{name: "plan", csv: "\uFEFFEmail,Name\na@example.test,A\n,\n" + strings.Repeat("b@example.test,B\n", 10), wantRows: 11, wantBatches: 2},
		{name: "duplicate header", csv: "Email,Email\na@example.test,A\n", wantCode: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			server := newNovelTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				t.Errorf("dry-run made request %s %s", r.Method, r.URL)
			}))
			isolateNovelCommand(t, server.URL)
			csvPath := filepath.Join(t.TempDir(), "records.csv")
			if err := os.WriteFile(csvPath, []byte(tt.csv), 0600); err != nil {
				t.Fatal(err)
			}
			root := RootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{"records", "upsert-csv", "--base", "app1", "--table", "Contacts", "--file", csvPath, "--merge-on", "Email", "--dry-run", "--json"})
			err := root.Execute()
			got := 0
			if err != nil {
				got = ExitCode(err)
			}
			if got != tt.wantCode {
				t.Fatalf("exit code = %d, want %d (err=%v output=%s)", got, tt.wantCode, err, out.String())
			}
			if requests != 0 {
				t.Fatalf("requests = %d, want 0", requests)
			}
			if tt.wantCode != 0 {
				return
			}
			var plan struct {
				DryRun  bool              `json:"dry_run"`
				Action  string            `json:"action"`
				Rows    int               `json:"rows"`
				Batches int               `json:"batches"`
				MergeOn string            `json:"merge_on"`
				Sample  []upsertCSVRecord `json:"sample"`
			}
			if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
				t.Fatalf("decode plan %q: %v", out.String(), err)
			}
			if !plan.DryRun || plan.Action != "records upsert-csv" || plan.Rows != tt.wantRows || plan.Batches != tt.wantBatches || plan.MergeOn != "Email" || len(plan.Sample) != 3 {
				t.Fatalf("plan = %#v", plan)
			}
		})
	}
}

func TestGreptileUpsertCSVTypecast(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "default", want: true},
		{name: "disabled", args: []string{"--typecast=false"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				Typecast bool `json:"typecast"`
			}
			server := newNovelTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Errorf("decode PATCH body: %v", err)
				}
				_, _ = io.WriteString(w, `{"createdRecords":[],"updatedRecords":[]}`)
			}))
			isolateNovelCommand(t, server.URL)
			csvPath := filepath.Join(t.TempDir(), "records.csv")
			if err := os.WriteFile(csvPath, []byte("Email,Count\na@example.test,42\n"), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"records", "upsert-csv", "--base", "app1", "--table", "Contacts", "--file", csvPath, "--merge-on", "Email", "--json"}
			runNovelCommand(t, append(args, tt.args...), "")
			if got.Typecast != tt.want {
				t.Fatalf("typecast = %v, want %v", got.Typecast, tt.want)
			}
		})
	}
}

func TestGreptileRecordsDiffChanges(t *testing.T) {
	server := newNovelTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"rec1","fields":{"Status":"Open"}}`)
	}))
	isolateNovelCommand(t, server.URL)

	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{name: "changes", args: []string{"--changes", `[{"id":"rec1","fields":{"Status":"Closed"}}]`}},
		{name: "both", args: []string{"--stdin", "--changes", `[]`}, wantCode: 2},
		{name: "neither", wantCode: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := RootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetIn(strings.NewReader(`[]`))
			args := []string{"records", "diff", "--base", "app1", "--table", "Things", "--json"}
			root.SetArgs(append(args, tt.args...))
			err := root.Execute()
			got := 0
			if err != nil {
				got = ExitCode(err)
			}
			if got != tt.wantCode {
				t.Fatalf("exit code = %d, want %d (err=%v output=%s)", got, tt.wantCode, err, out.String())
			}
			if tt.wantCode == 0 {
				var results []recordsDiffResult
				if err := json.Unmarshal(out.Bytes(), &results); err != nil || len(results) != 1 || len(results[0].Changes) != 1 {
					t.Fatalf("results = %#v, decode error = %v", results, err)
				}
			}
		})
	}
}

func TestGreptileSyncTablePruning(t *testing.T) {
	tests := []struct {
		name       string
		secondArgs []string
		wantPruned int
		wantRows   int
	}{
		{name: "complete sync prunes", wantPruned: 1, wantRows: 1},
		{name: "view skips prune", secondArgs: []string{"--view", "Current"}, wantRows: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			server := newNovelTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					_, _ = io.WriteString(w, `{"records":[{"id":"rec1","fields":{"Name":"A"}},{"id":"rec2","fields":{"Name":"B"}}]}`)
					return
				}
				_, _ = io.WriteString(w, `{"records":[{"id":"rec1","fields":{"Name":"A"}}]}`)
			}))
			isolateNovelCommand(t, server.URL)
			args := []string{"records", "sync-table", "--base", "app1", "--table", "Contacts", "--json"}
			runNovelCommand(t, args, "")
			backdateAirtableRecords(t)
			out := runNovelCommand(t, append(args, tt.secondArgs...), "")
			var result struct {
				Pruned int `json:"records_pruned"`
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil || result.Pruned != tt.wantPruned {
				t.Fatalf("result = %q, pruned = %d, want %d (err=%v)", out, result.Pruned, tt.wantPruned, err)
			}
			db, err := store.Open(defaultDBPath("airtable-pp-cli"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var rows int
			if err := db.DB().QueryRow(`SELECT COUNT(*) FROM resources WHERE resource_type = ? AND json_extract(data, '$.base_id') = ? AND json_extract(data, '$.table') = ?`, "records", "app1", "Contacts").Scan(&rows); err != nil {
				t.Fatal(err)
			}
			if rows != tt.wantRows {
				t.Fatalf("stored rows = %d, want %d", rows, tt.wantRows)
			}
		})
	}
}

func TestGreptileSchemaDiffRenames(t *testing.T) {
	responses := []string{
		`{"tables":[{"id":"tbl1","name":"Contacts","fields":[{"id":"fld1","name":"Name","type":"singleLineText"}]}]}`,
		`{"tables":[{"id":"tbl1","name":"People","fields":[{"id":"fld1","name":"Full Name","type":"singleLineText"}]}]}`,
	}
	request := 0
	server := newNovelTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, responses[request])
		request++
	}))
	isolateNovelCommand(t, server.URL)
	args := []string{"bases", "schema-diff", "app1", "--json"}
	runNovelCommand(t, args, "")
	out := runNovelCommand(t, args, "")
	var result struct {
		RenamedTables []map[string]string `json:"renamed_tables"`
		ChangedTables []struct {
			RenamedFields []map[string]string `json:"renamed_fields"`
		} `json:"changed_tables"`
		NoChanges bool `json:"no_changes_detected"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode schema diff %q: %v", out, err)
	}
	if len(result.RenamedTables) != 1 || result.RenamedTables[0]["from"] != "Contacts" || result.RenamedTables[0]["to"] != "People" {
		t.Fatalf("renamed tables = %#v", result.RenamedTables)
	}
	if len(result.ChangedTables) != 1 || len(result.ChangedTables[0].RenamedFields) != 1 || result.ChangedTables[0].RenamedFields[0]["from"] != "Name" || result.ChangedTables[0].RenamedFields[0]["to"] != "Full Name" || result.NoChanges {
		t.Fatalf("schema diff = %#v", result)
	}
}

// backdateAirtableRecords marks every stored record as synced long ago, so a
// following sync-table run (which only prunes rows older than its start)
// treats them as stale regardless of same-second timing.
func backdateAirtableRecords(t *testing.T) {
	t.Helper()
	db, err := store.Open(defaultDBPath("airtable-pp-cli"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.DB().Exec(`UPDATE resources SET synced_at = ? WHERE resource_type = ?`, "2000-01-01T00:00:00Z", "records"); err != nil {
		t.Fatal(err)
	}
}

func TestGreptileSyncTableOverlapKeepsNewerRows(t *testing.T) {
	requests := 0
	server := newNovelTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			_, _ = io.WriteString(w, `{"records":[{"id":"rec1","fields":{"Name":"A"}},{"id":"recOld","fields":{"Name":"Stale"}},{"id":"recNew","fields":{"Name":"Newer"}}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"records":[{"id":"rec1","fields":{"Name":"A"}}]}`)
	}))
	isolateNovelCommand(t, server.URL)
	args := []string{"records", "sync-table", "--base", "app1", "--table", "Contacts", "--json"}
	runNovelCommand(t, args, "")
	db, err := store.Open(defaultDBPath("airtable-pp-cli"))
	if err != nil {
		t.Fatal(err)
	}
	// recOld is stale; recNew stands in for a row an overlapping newer run wrote
	// after this (older) run started, so it must survive the prune.
	if _, err := db.DB().Exec(`UPDATE resources SET synced_at = CASE id WHEN 'recNew' THEN '2999-01-01T00:00:00Z' ELSE '2000-01-01T00:00:00Z' END WHERE resource_type = 'records'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	out := runNovelCommand(t, args, "")
	var result struct {
		Pruned int `json:"records_pruned"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Pruned != 1 {
		t.Fatalf("result = %q, want exactly recOld pruned (err=%v)", out, err)
	}
	db, err = store.Open(defaultDBPath("airtable-pp-cli"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ids := map[string]bool{}
	rows, err := db.DB().Query(`SELECT id FROM resources WHERE resource_type = 'records'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[id] = true
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !ids["rec1"] || !ids["recNew"] || ids["recOld"] {
		t.Fatalf("stored ids = %v, want rec1 and recNew kept, recOld pruned", ids)
	}
}
