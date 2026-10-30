package captures

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "evidence.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func save(t *testing.T, db *store.Store, tool string, scope map[string]any, data any) Snapshot {
	t.Helper()
	snapshot, err := Save(context.Background(), db, tool, scope, data)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestSaveGetRoundTripAndCoverage(t *testing.T) {
	tests := []struct {
		name  string
		scope map[string]any
		data  any
		full  bool
	}{
		{"full exact scope", map[string]any{"business_id": "business-demo", "status": []string{"APPROVED"}}, map[string]any{"items": []any{}, "count": 0}, true},
		{"default scope", nil, []any{}, true},
		{"nil data unknown", nil, nil, false},
		{"after continuation", map[string]any{"after": "cursor-a"}, map[string]any{"accounts": []any{}}, false},
		{"shared continuation", map[string]any{"shared_after": "cursor-b"}, map[string]any{}, false},
		{"apps continuation", map[string]any{"apps_after": "cursor-c"}, map[string]any{}, false},
		{"accounts continuation", map[string]any{"accounts_after": "cursor-d"}, map[string]any{}, false},
		{"empty continuation", map[string]any{"after": " "}, map[string]any{}, true},
		{"next owned page", nil, map[string]any{"next_after": "cursor-e"}, false},
		{"next shared page", nil, map[string]any{"shared_next_after": "cursor-f"}, false},
		{"next app page", nil, map[string]any{"next_apps_after": "cursor-g"}, false},
		{"next account page", nil, map[string]any{"next_accounts_after": "cursor-h"}, false},
		{"next catalog page", nil, map[string]any{"nextCursor": "cursor-i"}, false},
		{"truncated", nil, map[string]any{"truncated": true}, false},
		{"incomplete", nil, map[string]any{"incomplete": true}, false},
		{"more rows", nil, map[string]any{"more_than_this_page": true}, false},
		{"explicit complete", nil, map[string]any{"truncated": false, "next_after": ""}, true},
		{"nested metadata", nil, map[string]any{"meta": map[string]any{"pagination": map[string]any{"next_after": "more"}}}, false},
		{"unknown state retained", nil, map[string]any{"steps": []any{map[string]any{"status": "unknown"}}}, true},
		{"arbitrary components ignored", nil, map[string]any{"components": []any{map[string]any{"incomplete": true, "next_after": "content"}}}, true},
		{"large number preserved", nil, map[string]any{"counter": int64(9007199254740993)}, true},
	}
	db := testStore(t)
	ids := map[string]bool{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := save(t, db, "whatsapp_biz_accounts", tc.scope, tc.data)
			if got.Complete != tc.full {
				t.Fatalf("complete=%v, want %v", got.Complete, tc.full)
			}
			if !strings.HasPrefix(got.ID, "capture-") || len(got.ID) != len("capture-")+32 || ids[got.ID] {
				t.Fatalf("capture ID missing, malformed or repeated: %q", got.ID)
			}
			ids[got.ID] = true
			if parsed, err := time.Parse(time.RFC3339Nano, got.CapturedAt); err != nil || parsed.Location() != time.UTC {
				t.Fatalf("captured_at must be a UTC RFC3339 timestamp: %q (%v)", got.CapturedAt, err)
			}
			loaded, err := Get(context.Background(), db, got.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, loaded) {
				t.Fatalf("Get changed saved snapshot: got=%#v loaded=%#v", got, loaded)
			}
			wantScope := tc.scope
			if wantScope == nil {
				wantScope = map[string]any{}
			}
			wantJSON, _ := json.Marshal(wantScope)
			gotJSON, _ := json.Marshal(loaded.Scope)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("scope changed: %s != %s", gotJSON, wantJSON)
			}
			wantJSON, _ = json.Marshal(tc.data)
			gotJSON, _ = json.Marshal(loaded.Data)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("payload changed: %s != %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestSaveDetachesObjectsAndNeverOverwrites(t *testing.T) {
	db := testStore(t)
	ctx := context.Background()
	scope := map[string]any{"business_id": "business-a", "status": []any{"APPROVED"}}
	data := map[string]any{"name": "original", "steps": []any{map[string]any{"status": "unknown"}}}
	first := save(t, db, "whatsapp_biz_accounts", scope, data)
	scope["business_id"] = "business-b"
	scope["status"].([]any)[0] = "REJECTED"
	data["name"] = "mutated"
	data["steps"].([]any)[0].(map[string]any)["status"] = "todo"
	if first.Scope["business_id"] != "business-a" || first.Data.(map[string]any)["name"] != "original" {
		t.Fatal("Save returned aliases of caller-owned maps")
	}
	second := save(t, db, "whatsapp_biz_accounts", scope, data)
	first.Scope["business_id"] = "another-mutation"
	first.Data.(map[string]any)["name"] = "mutated-return"
	loaded, err := Get(ctx, db, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Scope["business_id"] != "business-a" || loaded.Data.(map[string]any)["name"] != "original" {
		t.Fatalf("stored capture was replaced: %#v", loaded)
	}
	status := loaded.Data.(map[string]any)["steps"].([]any)[0].(map[string]any)["status"]
	if status != "unknown" {
		t.Fatalf("unknown was changed to %v", status)
	}
	if first.ID == second.ID {
		t.Fatal("repeated saves must produce different captures")
	}
}

func TestGetMissingAndReadOnlyEmptyStore(t *testing.T) {
	db := testStore(t)
	ctx := context.Background()
	for _, id := range []string{"capture-missing", `capture-' OR 1=1 --`} {
		if _, err := Get(ctx, db, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(%q) should wrap ErrNotFound, got %v", id, err)
		}
	}
	for _, call := range []func() ([]Snapshot, error){
		func() ([]Snapshot, error) { return List(ctx, db, "", 20) },
		func() ([]Snapshot, error) { return Search(ctx, db, "missing", 20) },
	} {
		rows, err := call()
		if err != nil || rows == nil || len(rows) != 0 {
			t.Fatalf("empty store should return []: %#v, %v", rows, err)
		}
	}
	var tables int
	if err := db.DB().QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='mcp_captures'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("read paths should not create capture tables: %d, %v", tables, err)
	}
}

func TestListToolScopeAndLimits(t *testing.T) {
	db := testStore(t)
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		save(t, db, "whatsapp_biz_accounts", map[string]any{"business_id": "business-a", "order": i}, map[string]any{"index": i})
	}
	other := save(t, db, "whatsapp_biz_phone_numbers", map[string]any{"business_id": "business-b"}, map[string]any{"phone": "synthetic"})
	for _, tc := range []struct {
		tool  string
		limit int
		want  int
	}{
		{"", 0, 20}, {"", -10, 20}, {"", 1, 1}, {"", 5000, 26},
		{"whatsapp_biz_accounts", 100, 25}, {"whatsapp_biz_phone_numbers", 20, 1},
		{`whatsapp_biz_accounts' OR 1=1 --`, 20, 0}, {"missing", 20, 0},
	} {
		rows, err := List(ctx, db, tc.tool, tc.limit)
		if err != nil || rows == nil || len(rows) != tc.want {
			t.Fatalf("List(%q,%d)=%d rows, want %d (%v)", tc.tool, tc.limit, len(rows), tc.want, err)
		}
		for i, row := range rows {
			if tc.tool != "" && row.Tool != tc.tool {
				t.Fatalf("wrong source tool in restricted result: %#v", row)
			}
			if i > 0 && row.CapturedAt > rows[i-1].CapturedAt {
				t.Fatal("captures are not newest first")
			}
		}
		if tc.tool == "" && len(rows) > 0 && rows[0].ID != other.ID {
			t.Fatal("latest saved capture should be first")
		}
	}
	for _, tc := range []struct{ in, want int }{{-1, 20}, {0, 20}, {1, 1}, {999, 999}, {1000, 1000}, {1001, 1000}} {
		if got := boundedLimit(tc.in); got != tc.want {
			t.Fatalf("limit %d became %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestSearchLiteralTermsPhrasesAndNegativeQueries(t *testing.T) {
	db := testStore(t)
	ctx := context.Background()
	saved := []Snapshot{
		save(t, db, "catalog", nil, map[string]any{"message": "brown fox customer support café"}),
		save(t, db, "whatsapp_biz_get_template", map[string]any{"waba_id": "account-demo"}, map[string]any{"message": "brown quick fox", "language": "es_MX"}),
		save(t, db, "whatsapp_biz_accounts", nil, map[string]any{"message": "coffee OR tea"}),
	}
	for _, tc := range []struct {
		query string
		ids   []string
	}{
		{"brown fox", []string{saved[0].ID, saved[1].ID}},
		{`"brown fox"`, []string{saved[0].ID}},
		{`"customer support" café`, []string{saved[0].ID}},
		{"account-demo es_MX", []string{saved[1].ID}},
		{"coffee OR tea", []string{saved[2].ID}},
		{"brown OR tea", []string{}},
		{"zz_unrelated_query_zz", []string{}},
		{`' OR 1=1 -- DROP TABLE mcp_captures`, []string{}},
		{`" NEAR(1) * :`, []string{}},
		{"", []string{}},
		{"***", []string{}},
	} {
		rows, err := Search(ctx, db, tc.query, 20)
		if err != nil || rows == nil {
			t.Fatalf("Search(%q): %v", tc.query, err)
		}
		want := map[string]bool{}
		for _, id := range tc.ids {
			want[id] = true
		}
		if len(rows) != len(want) {
			t.Fatalf("Search(%q)=%d rows, want %d", tc.query, len(rows), len(want))
		}
		for _, row := range rows {
			if !want[row.ID] {
				t.Fatalf("Search(%q) returned unrelated capture %s", tc.query, row.ID)
			}
		}
	}
	limited, err := Search(ctx, db, "brown", 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("search limit not applied: %d %v", len(limited), err)
	}
	if _, err := Get(ctx, db, saved[0].ID); err != nil {
		t.Fatalf("injection-shaped search modified stored data: %v", err)
	}
}

func TestSaveIndexFailureRollsBackSnapshot(t *testing.T) {
	db := testStore(t)
	ctx := context.Background()
	first := save(t, db, "catalog", nil, map[string]any{"text": "initial"})
	if _, err := db.DB().Exec(`DROP TABLE mcp_captures_fts`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`CREATE TABLE mcp_captures_fts(capture_id TEXT CHECK(capture_id='never'),content TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(ctx, db, "catalog", nil, map[string]any{"text": "should roll back"}); err == nil {
		t.Fatal("failed FTS insert must fail Save")
	}
	rows, err := List(ctx, db, "", 100)
	if err != nil || len(rows) != 1 || rows[0].ID != first.ID {
		t.Fatalf("snapshot transaction did not roll back: %#v %v", rows, err)
	}
}

func TestValidationAndCancellation(t *testing.T) {
	db := testStore(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		fn   func() error
	}{
		{"nil Save", func() error { _, e := Save(ctx, nil, "catalog", nil, nil); return e }},
		{"nil Get", func() error { _, e := Get(ctx, nil, "capture-x"); return e }},
		{"nil List", func() error { _, e := List(ctx, nil, "", 1); return e }},
		{"nil Search", func() error { _, e := Search(ctx, nil, "text", 1); return e }},
		{"blank tool", func() error { _, e := Save(ctx, db, " ", nil, map[string]any{}); return e }},
		{"blank id", func() error { _, e := Get(ctx, db, ""); return e }},
		{"invalid JSON", func() error { _, e := Save(ctx, db, "catalog", nil, make(chan int)); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); err == nil {
				t.Fatal("expected an actionable error")
			}
		})
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Save(cancelled, db, "catalog", nil, map[string]any{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Save: %v", err)
	}
}
