package cli

import (
	"context"
	"encoding/json"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"path/filepath"
	"testing"
	"time"
)

func TestCrocoSchemaStructuralChanges(t *testing.T) {
	cases := []struct {
		name          string
		before, after map[string]any
		count         int
		kind          string
	}{
		{"same", map[string]any{"type": "text"}, map[string]any{"type": "text"}, 0, ""},
		{"type_changed", map[string]any{"type": "text"}, map[string]any{"type": "number"}, 1, "modified"},
		{"removed_field", map[string]any{"fields": map[string]any{"price": "number"}}, map[string]any{"fields": map[string]any{}}, 1, "removed"},
		{"added_field", map[string]any{}, map[string]any{"new_field": true}, 1, "added"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out := []any{}
			crocoSchemaChanges("engine:cct", "rooms", "", tt.before, tt.after, &out)
			if len(out) != tt.count {
				t.Fatal(out)
			}
			if len(out) > 0 && out[0].(map[string]any)["change"] != tt.kind {
				t.Fatal(out)
			}
		})
	}
}
func TestCrocoSchemaIdentitySafety(t *testing.T) {
	for _, tt := range []struct {
		name, key string
		cross     bool
		rows      []map[string]any
		fail      bool
	}{
		{"same_site_id", "", false, []map[string]any{{"id": "1", "slug": "rooms"}}, false},
		{"cross_site_without_key", "", true, []map[string]any{{"id": "1", "slug": "rooms"}}, true},
		{"cross_site_explicit_slug", "slug", true, []map[string]any{{"id": "1", "slug": "rooms"}}, false},
		{"missing_key", "slug", true, []map[string]any{{"id": "1"}}, true},
		{"duplicate_key", "slug", true, []map[string]any{{"id": "1", "slug": "rooms"}, {"id": "2", "slug": "rooms"}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, e := crocoSchemaIndex("engine:cct", tt.rows, tt.key, tt.cross)
			if (e != nil) != tt.fail {
				t.Fatal(m, e)
			}
			if !tt.fail && tt.cross {
				if _, ok := m["rooms"]["id"]; ok {
					t.Fatal("cross-site database ID leaked into structural comparison")
				}
			}
		})
	}
}
func TestCrocoSchemaIgnoresTransportMetadataAndActions(t *testing.T) {
	row, e := crocoSchemaRow("forms", map[string]any{"id": 1, "date": "now", "content": map[string]any{"raw": "<!-- wp:jet-forms/text-field {\"name\":\"reference\"} /-->"}, "meta": map[string]any{"_jf_actions": "private-value"}})
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := row["meta"]; ok {
		t.Fatal(row)
	}
	if _, ok := row["date"]; ok {
		t.Fatal(row)
	}
	a := row["fields"].([]any)
	if len(a) != 1 || a[0].(map[string]any)["name"] != "reference" {
		t.Fatal(row)
	}
}
func TestCrocoLocalCaptureMissingAndIncomplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.db")
	ctx := context.Background()
	if _, _, e := crocoLoadCapture(ctx, path, "forms"); e == nil {
		t.Fatal("missing snapshot accepted")
	}
	s, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	cap := store.CrocoCapture{Site: "https://site.invalid/wp-json", Resource: "forms", CapturedAt: time.Now().UTC().Format(time.RFC3339Nano), Complete: false}
	if e = s.CrocoReplace(ctx, cap, []store.CrocoRow{{ID: "1", Data: json.RawMessage(`{"id":1}`)}}); e != nil {
		t.Fatal(e)
	}
	s.Close()
	got, rows, e := crocoLoadCapture(ctx, path, "forms")
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	if e = crocoRequireComplete(got); e == nil {
		t.Fatal("incomplete snapshot accepted")
	}
}
