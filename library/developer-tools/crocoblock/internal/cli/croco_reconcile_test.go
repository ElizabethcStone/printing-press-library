package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
)

func crocoP2TestCapture(resource string, fields []string, scope map[string]any) store.CrocoCapture {
	return store.CrocoCapture{Site: "https://one.example/wp-json", Resource: resource, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano), Complete: true, Fields: fields, Versions: map[string]string{"jet-engine": "3.9.0"}, Scope: scope}
}
func crocoReconcileTestSnapshot(rows ...map[string]any) crocoReconcileSnapshot {
	return crocoReconcileSnapshot{Capture: crocoP2TestCapture("cct:rooms", []string{"_ID", "sku", "label", "price"}, map[string]any{"type": "rooms"}), SchemaCapture: crocoP2TestCapture("engine:cct", []string{"schema"}, map[string]any{}), Rows: rows, Definitions: []map[string]any{{"id": "1", "slug": "rooms", "meta_fields": []any{map[string]any{"name": "sku", "type": "text"}, map[string]any{"name": "label", "type": "text"}, map[string]any{"name": "price", "type": "number"}}}}}
}
func crocoReconcileTestRow(id, key, label string) map[string]any {
	return map[string]any{"_ID": id, "sku": key, "label": label, "price": json.Number("10")}
}
func TestCrocoReconcileDifferenceTable(t *testing.T) {
	tests := []struct {
		name                                         string
		before, after                                []map[string]any
		created, deleted, changed, unchanged, issues int
		complete                                     bool
	}{
		{"real changes", []map[string]any{crocoReconcileTestRow("1", "sku-a", "before"), crocoReconcileTestRow("2", "sku-b", "gone")}, []map[string]any{crocoReconcileTestRow("11", "sku-a", "after"), crocoReconcileTestRow("12", "sku-c", "new")}, 1, 1, 1, 0, 0, true},
		{"unchanged", []map[string]any{crocoReconcileTestRow("1", "sku-a", "same")}, []map[string]any{crocoReconcileTestRow("9", "sku-a", "same")}, 0, 0, 0, 1, 0, true},
		{"empty", nil, nil, 0, 0, 0, 0, 0, true},
		{"duplicate before", []map[string]any{crocoReconcileTestRow("1", "sku-a", "one"), crocoReconcileTestRow("2", "sku-a", "two")}, []map[string]any{crocoReconcileTestRow("9", "sku-a", "other")}, 0, 0, 0, 0, 1, false},
		{"duplicate after", []map[string]any{crocoReconcileTestRow("1", "sku-a", "one")}, []map[string]any{crocoReconcileTestRow("9", "sku-a", "one"), crocoReconcileTestRow("10", "sku-a", "two")}, 0, 0, 0, 0, 1, false},
		{"missing key suppresses arbitrary create delete", []map[string]any{crocoReconcileTestRow("1", "", "unknown"), crocoReconcileTestRow("2", "sku-b", "old")}, []map[string]any{crocoReconcileTestRow("9", "sku-a", "new")}, 0, 0, 0, 0, 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, e := crocoComputeReconcile(crocoReconcileTestSnapshot(tc.before...), crocoReconcileTestSnapshot(tc.after...), "rooms", "sku", []string{"label", "price"}, 100)
			if e != nil {
				t.Fatal(e)
			}
			c := got["counts"].(map[string]int)
			for k, w := range map[string]int{"created": tc.created, "deleted": tc.deleted, "changed": tc.changed, "unchanged": tc.unchanged} {
				if c[k] != w {
					t.Fatalf("%s=%d want %d: %+v", k, c[k], w, got)
				}
			}
			if got["analysis_complete"] != tc.complete || got["total_issues"] != tc.issues {
				t.Fatal(got)
			}
			diffs := got["differences"].([]any)
			if len(diffs) != tc.created+tc.deleted+tc.changed {
				t.Fatal(diffs)
			}
			if tc.changed > 0 {
				found := false
				for _, d := range diffs {
					m := d.(map[string]any)
					if m["kind"] == "changed" {
						found = m["before_id"] == "1" && m["after_id"] == "11" && len(m["fields"].([]string)) == 1
					}
				}
				if !found {
					t.Fatal("changed record lacks both IDs and exact fields", diffs)
				}
			}
			if got["scanned"].(map[string]int)["before_rows"] != len(tc.before) {
				t.Fatal(got["scanned"])
			}
			t.Log("PASS exact created/deleted/changed counts, IDs, ambiguity handling and real empty results")
		})
	}
}
func TestCrocoReconcileGuardsTable(t *testing.T) {
	tests := []struct {
		name    string
		alter   func(*crocoReconcileSnapshot, *crocoReconcileSnapshot)
		wantErr string
	}{
		{"cross site compatible", func(b, a *crocoReconcileSnapshot) {
			a.Capture.Site = "https://two.example/wp-json"
			a.SchemaCapture.Site = a.Capture.Site
		}, ""},
		{"version incompatible", func(b, a *crocoReconcileSnapshot) {
			a.Capture.Versions = map[string]string{"jet-engine": "4.0.0"}
			a.SchemaCapture.Versions = a.Capture.Versions
		}, "incompatible JetEngine"},
		{"schema incompatible", func(b, a *crocoReconcileSnapshot) {
			a.Definitions[0]["meta_fields"].([]any)[1].(map[string]any)["type"] = "number"
		}, "schemas are incompatible"},
		{"incomplete", func(b, a *crocoReconcileSnapshot) { a.Capture.Complete = false }, "incomplete"},
		{"missing captured field", func(b, a *crocoReconcileSnapshot) { a.Capture.Fields = []string{"_ID", "sku"} }, "was not captured"},
		{"missing row field", func(b, a *crocoReconcileSnapshot) { delete(a.Rows[0], "label") }, "lacks selected field"},
		{"filtered capture", func(b, a *crocoReconcileSnapshot) { a.Capture.Scope["status"] = "publish" }, "unfiltered"},
		{"schema site mismatch", func(b, a *crocoReconcileSnapshot) { a.SchemaCapture.Site = "https://wrong.example/wp-json" }, "provenance differ"},
		{"missing definition", func(b, a *crocoReconcileSnapshot) { a.Definitions = nil }, "missing from engine:cct"},
		{"duplicate row ID", func(b, a *crocoReconcileSnapshot) {
			a.Rows = append(a.Rows, crocoReconcileTestRow("2", "sku-other", "label"))
		}, "repeats row ID"},
		{"missing version", func(b, a *crocoReconcileSnapshot) { a.Capture.Versions = map[string]string{} }, "no jet-engine version"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, a := crocoReconcileTestSnapshot(crocoReconcileTestRow("1", "sku-a", "before")), crocoReconcileTestSnapshot(crocoReconcileTestRow("2", "sku-a", "after"))
			tc.alter(&b, &a)
			got, e := crocoComputeReconcile(b, a, "rooms", "sku", []string{"label"}, 100)
			if tc.wantErr != "" {
				if e == nil || !strings.Contains(e.Error(), tc.wantErr) {
					t.Fatalf("want %s got %+v %v", tc.wantErr, got, e)
				}
			} else if e != nil || got["cross_site"] != true || got["total_differences"] != 1 {
				t.Fatal(got, e)
			}
			t.Log("PASS capture, version, schema and cross-site contract")
		})
	}
}
func TestCrocoReconcileBoundPrivacyAndIdentity(t *testing.T) {
	b := crocoReconcileTestSnapshot(crocoReconcileTestRow("1", "customer-private-key", "PERSONAL BEFORE"), crocoReconcileTestRow("2", "another-private-key", "PRIVATE SECOND"))
	a := crocoReconcileTestSnapshot(crocoReconcileTestRow("10", "customer-private-key", "PERSONAL AFTER"))
	got, e := crocoComputeReconcile(b, a, "rooms", "sku", []string{"label"}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if len(got["differences"].([]any)) != 1 || got["total_differences"] != 2 || got["truncated"] != true {
		t.Fatal(got)
	}
	raw, _ := json.Marshal(got)
	for _, s := range []string{"customer-private-key", "PERSONAL BEFORE", "PERSONAL AFTER", "PRIVATE SECOND"} {
		if strings.Contains(string(raw), s) {
			t.Fatal("value leaked", s)
		}
	}
	for _, tc := range []struct {
		v     any
		valid bool
	}{{"sku", true}, {json.Number("12"), true}, {"", false}, {nil, false}, {[]any{"a"}, false}, {map[string]any{"x": "y"}, false}, {true, false}} {
		_, valid := crocoReconcileIdentity(tc.v)
		if valid != tc.valid {
			t.Fatalf("identity %T valid=%v", tc.v, valid)
		}
	}
	for _, tc := range []struct {
		key    string
		fields []string
		limit  int
	}{{"", []string{"label"}, 100}, {"sku", nil, 100}, {"sku", []string{"password"}, 100}, {"sku", []string{"label", "label"}, 100}, {"sku", []string{"label"}, 0}, {"sku", []string{"label"}, 501}} {
		if crocoReconcileInput(tc.key, tc.fields, tc.limit) == nil {
			t.Fatal("accepted invalid input", tc)
		}
	}
	t.Log("PASS bounded output retains totals; values never printed; key identity types and invalid inputs guarded")
}
func crocoP2TestSave(t *testing.T, path string, capture store.CrocoCapture, rows []map[string]any) {
	t.Helper()
	db, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	items := []store.CrocoRow{}
	for i, row := range rows {
		raw, _ := json.Marshal(row)
		items = append(items, store.CrocoRow{ID: strconv.Itoa(i + 1), Data: raw})
	}
	if e = db.CrocoReplace(context.Background(), capture, items); e != nil {
		t.Fatal(e)
	}
}
func crocoP2TestSaveReconcile(t *testing.T, s crocoReconcileSnapshot) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.db")
	crocoP2TestSave(t, path, s.Capture, s.Rows)
	crocoP2TestSave(t, path, s.SchemaCapture, s.Definitions)
	return path
}
func TestCrocoReconcileCLIReadOnlySelectAndGuards(t *testing.T) {
	b, a := crocoReconcileTestSnapshot(crocoReconcileTestRow("1", "sku-a", "before")), crocoReconcileTestSnapshot(crocoReconcileTestRow("2", "sku-a", "after"))
	before, after := crocoP2TestSaveReconcile(t, b), crocoP2TestSaveReconcile(t, a)
	rawBefore, _ := os.ReadFile(before)
	rawAfter, _ := os.ReadFile(after)
	f := &rootFlags{asJSON: true, timeout: 3 * time.Second, selectFields: "differences"}
	out, e := crocoEngineTestRun(newCrocoReconcileCommand(f), "rooms", "--before", before, "--after", after, "--key", "sku", "--fields", "label")
	if e != nil {
		t.Fatal(e)
	}
	var decoded map[string]any
	if e = json.Unmarshal([]byte(out), &decoded); e != nil {
		t.Fatal(out, e)
	}
	// The framework projects fields within an existing results envelope.
	results, ok := decoded["results"].(map[string]any)
	if !ok {
		results = decoded
	}
	if len(results) != 1 || results["differences"] == nil {
		t.Fatal(out)
	}
	for path, want := range map[string][]byte{before: rawBefore, after: rawAfter} {
		got, _ := os.ReadFile(path)
		if sha256.Sum256(got) != sha256.Sum256(want) {
			t.Fatal("read-only command altered SQLite", path)
		}
	}
	for _, tc := range []struct {
		name    string
		flags   rootFlags
		wantErr bool
	}{{"live rejected", rootFlags{asJSON: true, dataSource: "live"}, true}, {"dryrun skips missing files", rootFlags{asJSON: true, dryRun: true}, false}} {
		t.Run(tc.name, func(t *testing.T) {
			out, e := crocoEngineTestRun(newCrocoReconcileCommand(&tc.flags), "rooms", "--before", "/missing-before.db", "--after", "/missing-after.db")
			if (e != nil) != tc.wantErr {
				t.Fatal(out, e)
			}
			if !tc.wantErr && !strings.Contains(out, "dry_run") {
				t.Fatal(out)
			}
		})
	}
	t.Log("PASS actual SQLite read-only execution, --select, local transport, live rejection and dry-run before file I/O")
}
