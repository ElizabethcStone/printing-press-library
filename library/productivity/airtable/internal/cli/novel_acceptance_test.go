package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolateNovelCommand(t *testing.T, apiURL string) {
	t.Helper()
	t.Setenv("AIRTABLE_API_KEY", "synthetic-test-token")
	t.Setenv("AIRTABLE_BASE_URL", apiURL)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

func runNovelCommand(t *testing.T, args []string, input string) string {
	t.Helper()
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(input))
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("command %q: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func newNovelTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestNovelRecordsUpsertCSVAcceptance(t *testing.T) {
	requests := 0
	var requestBody map[string]any
	server := newNovelTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &requestBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"createdRecords":["rec1","rec2","rec3"]}`)
	}))
	isolateNovelCommand(t, server.URL)
	csvPath := filepath.Join(t.TempDir(), "records.csv")
	if err := os.WriteFile(csvPath, []byte("Email,Name\na@example.test,A\nb@example.test,B\nc@example.test,C\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"records", "upsert-csv", "--base", "app1", "--table", "Contacts", "--file", csvPath, "--merge-on", "Email", "--json"}
	runNovelCommand(t, args, "")
	if requests != 1 {
		t.Fatalf("PATCH requests = %d, want 1", requests)
	}
	upsert, ok := requestBody["performUpsert"].(map[string]any)
	if !ok || !reflectJSONEqual(upsert["fieldsToMergeOn"], []any{"Email"}) {
		t.Fatalf("performUpsert.fieldsToMergeOn = %#v, want [Email]", upsert["fieldsToMergeOn"])
	}
	records, ok := requestBody["records"].([]any)
	if !ok || len(records) != 3 {
		t.Fatalf("records = %#v, want 3 records", requestBody["records"])
	}
	requests = 0
	runNovelCommand(t, append(append([]string(nil), args...), "--dry-run"), "")
	if requests != 0 {
		t.Fatalf("dry-run made %d HTTP requests, want 0", requests)
	}
}

func TestNovelRecordsDiffAcceptance(t *testing.T) {
	server := newNovelTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"rec1","fields":{"Status":"Open","Name":"A"}}`)
	}))
	isolateNovelCommand(t, server.URL)
	args := []string{"records", "diff", "--base", "app1", "--table", "Things", "--stdin", "--json"}
	out := runNovelCommand(t, args, `[{"id":"rec1","fields":{"Status":"Closed","Name":"A"}}]`)
	var changed []recordsDiffResult
	if err := json.Unmarshal([]byte(out), &changed); err != nil {
		t.Fatalf("decode diff output %q: %v", out, err)
	}
	if len(changed) != 1 || len(changed[0].Changes) != 1 || changed[0].Changes[0].Field != "Status" {
		t.Fatalf("changed result = %#v, want only Status", changed)
	}
	out = runNovelCommand(t, args, `[{"id":"rec1","fields":{"Status":"Open","Name":"A"}}]`)
	var same []recordsDiffResult
	if err := json.Unmarshal([]byte(out), &same); err != nil {
		t.Fatalf("decode identical diff output %q: %v", out, err)
	}
	if len(same) != 1 || same[0].Changes == nil || len(same[0].Changes) != 0 || !strings.Contains(out, `"changes": []`) {
		t.Fatalf("identical result = %s, want empty changes array", out)
	}
}

func TestNovelBasesSchemaDiffAcceptance(t *testing.T) {
	added := false
	server := newNovelTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields := `[{"id":"fld1","name":"Name","type":"singleLineText"}]`
		if added {
			fields = `[{"id":"fld1","name":"Name","type":"singleLineText"},{"id":"fld2","name":"Status","type":"singleSelect"}]`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tables":[{"id":"tbl1","name":"Contacts","fields":`+fields+`}]}`)
	}))
	isolateNovelCommand(t, server.URL)
	args := []string{"bases", "schema-diff", "app1", "--json"}
	first := runNovelCommand(t, args, "")
	if !strings.Contains(first, "no prior snapshot") {
		t.Fatalf("first schema diff = %s, want baseline note", first)
	}
	added = true
	second := runNovelCommand(t, args, "")
	if !strings.Contains(squashJSON(second), `"added_fields":["Status"]`) {
		t.Fatalf("second schema diff = %s, want added field Status", second)
	}
}

func TestRecordsExpandLinksAcceptance(t *testing.T) {
	server := newNovelTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			if strings.Contains(r.URL.Path, "/Companies") {
				_, _ = io.WriteString(w, `{"records":[{"id":"rec2","fields":{"Name":"Acme"}}]}`)
			} else {
				_, _ = io.WriteString(w, `{"records":[{"id":"rec1","fields":{"Company":["rec2"]}}]}`)
			}
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	isolateNovelCommand(t, server.URL)
	runNovelCommand(t, []string{"records", "sync-table", "--base", "app1", "--table", "Companies", "--json"}, "")
	listArgs := []string{"records", "list", "--base", "app1", "--table", "Contacts", "--json"}
	plain := runNovelCommand(t, listArgs, "")
	if !strings.Contains(squashJSON(plain), `"Company":["rec2"]`) {
		t.Fatalf("without expand-links = %s, want record ID", plain)
	}
	expanded := runNovelCommand(t, append(append([]string(nil), listArgs...), "--expand-links"), "")
	if !strings.Contains(squashJSON(expanded), `"Company":["Acme"]`) {
		t.Fatalf("with expand-links = %s, want linked name", expanded)
	}
}

func reflectJSONEqual(got, want any) bool {
	a, errA := json.Marshal(got)
	b, errB := json.Marshal(want)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

// squashJSON drops whitespace so assertions ignore pretty-print layout.
func squashJSON(s string) string { return strings.Join(strings.Fields(s), "") }
