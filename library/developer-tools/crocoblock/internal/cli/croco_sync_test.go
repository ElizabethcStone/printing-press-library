package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCrocoPaginationCoverage(t *testing.T) {
	cases := []struct {
		name           string
		pages          [][]any
		totals         []int64
		cap            int
		complete, fail bool
	}{
		{"empty", [][]any{{}}, []int64{0}, 1, true, false},
		{"full", [][]any{{map[string]any{"id": 1}}, {map[string]any{"id": 2}}}, []int64{2, 2}, 2, true, false},
		{"capped", [][]any{{map[string]any{"id": 1}}}, []int64{2}, 1, false, false},
		{"changing", [][]any{{map[string]any{"id": 1}}, {map[string]any{"id": 2}}}, []int64{2, 3}, 2, false, true},
		{"missing_page", [][]any{{}}, []int64{2}, 1, false, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, complete, scanned, e := crocoPageAll(tt.cap, func(page int) ([]any, int64, error) { return tt.pages[page-1], tt.totals[page-1], nil })
			if complete != tt.complete || (e != nil) != tt.fail || scanned > tt.cap {
				t.Fatalf("complete=%v scans=%d error=%v", complete, scanned, e)
			}
		})
	}
}
func TestCrocoWPEnvelopeRequiresTotals(t *testing.T) {
	for _, ok := range []bool{true, false} {
		t.Run(fmt.Sprint(ok), func(t *testing.T) {
			c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("_envelope") != "1" {
					t.Error("missing envelope")
				}
				h := map[string]any{}
				if ok {
					h["X-WP-Total"] = 0
				}
				json.NewEncoder(w).Encode(map[string]any{"status": 200, "headers": h, "body": []any{}})
			})
			a, total, e := crocoWPPage(context.Background(), c, "/wp/v2/posts", 1, 100, nil)
			if (e == nil) != ok {
				t.Fatal(e)
			}
			if ok && (len(a) != 0 || total != 0) {
				t.Fatal(a, total)
			}
		})
	}
}
func TestCrocoSafeTypedData(t *testing.T) {
	v := []map[string]any{{"user_email": "private@example.invalid", "name": "safe"}}
	got := crocoSafe(v)
	a := got.([]any)
	m := a[0].(map[string]any)
	if m["user_email"] != "[redacted]" || m["name"] != "safe" {
		t.Fatal(got)
	}
}
func TestCrocoSyncRowsStableIdentityAndRedaction(t *testing.T) {
	rows, e := crocoSyncRows([]any{map[string]any{"ID": "7", "token": "secret-value", "name": "ok"}}, "ID")
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	json.Unmarshal(rows[0].Data, &v)
	if rows[0].ID != "7" || v["token"] != "[redacted]" {
		t.Fatal(rows)
	}
	if _, e = crocoSyncRows([]any{map[string]any{"name": "missing"}}, "ID"); e == nil {
		t.Fatal("missing stable ID accepted")
	}
}
func TestCrocoFilterReadCannotInjectMutation(t *testing.T) {
	calls := 0
	c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		for _, k := range []string{"move_to_trash", "delete", "restore", "update"} {
			if _, ok := b[k]; ok {
				t.Fatal("mutation on read", b)
			}
		}
		if r.Method != "POST" || r.URL.Path != "/jet-smart-filters-api/v1/filters" {
			t.Fatal(r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"filters": []any{}, "count": "0"})
	})
	m, e := crocoFilterPage(context.Background(), c, 25, 1, "publish", "")
	if e != nil || !reflect.DeepEqual(m["filters"], []any{}) {
		t.Fatal(m, e)
	}
	for _, limit := range []int{0, 101} {
		if _, e = crocoFilterPage(context.Background(), c, limit, 1, "publish", ""); e == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
func TestCrocoFilterUnknownSetting(t *testing.T) {
	if crocoFilterData(map[string]any{"made_up_setting": "x"}) == nil {
		t.Fatal("unknown setting accepted")
	}
	if e := crocoFilterData(map[string]any{"title": "Sample", "_query_var": "price"}); e != nil {
		t.Fatal(e)
	}
}
func TestCrocoSSHInputsCannotBecomeShellSyntax(t *testing.T) {
	for _, host := range []string{"-oProxyCommand=touch /tmp/no", "host;id", "host$(id)", "host\ncmd"} {
		if crocoSSHHostRE.MatchString(host) {
			t.Fatal(host)
		}
	}
	if crocoShellQuote("a'b") != "'a'\"'\"'b'" {
		t.Fatal("quote does not protect apostrophe")
	}
}

func crocoSyncTestCommand(f *rootFlags) *cobra.Command {
	root := &cobra.Command{Use: "test"}
	for _, hook := range novelCommandHooks {
		hook(root, f)
	}
	for _, cmd := range root.Commands() {
		if cmd.Name() == "sync" {
			root.RemoveCommand(cmd)
			return cmd
		}
	}
	panic("sync command missing")
}
func crocoSyncTestSchema(slug string) map[string]any {
	return map[string]any{"success": true, "data": []any{map[string]any{"slug": slug, "args": map[string]any{"has_single": false}, "meta_fields": []any{map[string]any{"name": "label", "type": "text", "object_type": "field"}, map[string]any{"name": "api_token", "type": "text", "object_type": "field"}, map[string]any{"name": "layout", "type": "html", "object_type": "field"}, map[string]any{"name": "tab", "type": "text", "object_type": "tab"}}}}}
}
func TestCrocoSyncCCTFieldSchemaGuards(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fields   []string
		response any
		success  bool
		requests int
	}{{"declared", []string{"label"}, crocoSyncTestSchema("rooms"), true, 1}, {"dedup builtins", []string{"_ID", "cct_status", "label", "label"}, crocoSyncTestSchema("rooms"), true, 1}, {"unknown", []string{"typo_label"}, crocoSyncTestSchema("rooms"), false, 1}, {"secret declared", []string{"api_token"}, crocoSyncTestSchema("rooms"), false, 0}, {"syntax", []string{"../label"}, crocoSyncTestSchema("rooms"), false, 0}, {"html not data", []string{"layout"}, crocoSyncTestSchema("rooms"), false, 1}, {"tab not data", []string{"tab"}, crocoSyncTestSchema("rooms"), false, 1}, {"missing type", []string{"label"}, crocoSyncTestSchema("other"), false, 1}, {"missing data", []string{"label"}, map[string]any{"success": true}, false, 1}, {"null data", []string{"label"}, map[string]any{"success": true, "data": nil}, false, 1}, {"failure", []string{"label"}, map[string]any{"success": false, "data": []any{}}, false, 1}, {"no fields", nil, crocoSyncTestSchema("rooms"), false, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/jet-engine/v2/get-content-types" {
					t.Error(r.Method, r.URL.Path)
				}
				json.NewEncoder(w).Encode(tc.response)
			})
			fields, e := crocoSyncCCTFields(context.Background(), c, "rooms", tc.fields)
			if (e == nil) != tc.success || calls != tc.requests {
				t.Fatal(fields, e, calls)
			}
			if tc.success && !reflect.DeepEqual(fields, []string{"_ID", "cct_status", "label"}) {
				t.Fatal(fields)
			}
			t.Log("PASS CCT selected fields must exist in the actual engine schema; secrets and layout fields rejected")
		})
	}
}
func TestCrocoSyncFullFalseAndDryRunBeforeIO(t *testing.T) {
	calls := 0
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	db := filepath.Join(t.TempDir(), "must-not-exist.db")
	out, e := crocoEngineTestRun(crocoSyncTestCommand(f), "--full=false", "--db", db)
	if e == nil || !strings.Contains(e.Error(), "--full=false is not supported") || calls != 0 {
		t.Fatal(out, e, calls)
	}
	if _, e = os.Stat(db); !os.IsNotExist(e) {
		t.Fatal("invalid sync created a database", e)
	}
	f.dryRun = true
	out, e = crocoEngineTestRun(crocoSyncTestCommand(f), "--full=false", "--db", db, "--resources", "cct", "--cct", "rooms", "--fields", "api_token")
	if e != nil || !strings.Contains(out, "dry_run") || calls != 0 {
		t.Fatal(out, e, calls)
	}
	t.Log("PASS --full=false fails concretely before HTTP/store; dry-run still exits before validation and I/O")
}
func TestCrocoSyncCCTSchemaValidatedBeforeCapture(t *testing.T) {
	for _, tc := range []struct {
		name, field              string
		missingRowField, success bool
	}{{"declared complete", "label", false, true}, {"unknown field", "typo", false, false}, {"declared missing in row", "label", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			paths := []string{}
			f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				switch r.URL.Path {
				case "/jet-engine/v2/get-content-types":
					json.NewEncoder(w).Encode(crocoSyncTestSchema("rooms"))
				case "/wp/v2/plugins":
					json.NewEncoder(w).Encode([]any{map[string]any{"plugin": "jet-engine/jet-engine.php", "status": "active", "version": "3.9.0"}})
				case "/jet-cct/rooms":
					row := map[string]any{"_ID": "1", "cct_status": "publish"}
					if !tc.missingRowField {
						row["label"] = "fixture"
					}
					json.NewEncoder(w).Encode([]any{row})
				default:
					t.Error("unexpected HTTP", r.URL.Path)
					w.WriteHeader(500)
				}
			})
			db := filepath.Join(t.TempDir(), "capture.db")
			out, e := crocoEngineTestRun(crocoSyncTestCommand(f), "--resources", "cct", "--cct", "rooms", "--fields", tc.field, "--db", db)
			if (e == nil) != tc.success {
				t.Fatal(out, e)
			}
			if len(paths) == 0 || paths[0] != "/jet-engine/v2/get-content-types" {
				t.Fatal(paths)
			}
			if tc.field == "typo" {
				if len(paths) != 1 {
					t.Fatal("invalid selection fetched rows", paths)
				}
				if _, e = os.Stat(db); !os.IsNotExist(e) {
					t.Fatal("invalid selection wrote a store", e)
				}
			} else {
				ro, e := store.OpenReadOnly(db)
				if e != nil {
					t.Fatal(e)
				}
				defer ro.Close()
				cap, rows, e := ro.CrocoSnapshot(context.Background(), "cct:rooms")
				if tc.success {
					if e != nil || !cap.Complete || len(rows) != 1 || !reflect.DeepEqual(cap.Fields, []string{"_ID", "cct_status", "label"}) {
						t.Fatal(cap, rows, e)
					}
				} else if e == nil || cap.Complete {
					t.Fatal("missing fields fabricated complete coverage", cap, e)
				}
			}
			t.Log("PASS actual sync execution validates schema before capture and does not claim missing row fields")
		})
	}
}
