package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func crocoTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "capture.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	// Exercise the drain-first constraint even if the generated pool grows.
	s.DB().SetMaxOpenConns(1)
	return s, path
}

func crocoTestCapture(resource string) CrocoCapture {
	return CrocoCapture{Site: "https://example.test", Resource: resource, CapturedAt: "2026-10-20T12:00:00Z", Complete: true, Fields: []string{"title"}, Versions: map[string]string{"jet-engine": "3.9.0"}, Scope: map[string]any{"type": resource}}
}

func crocoTestRow(id, raw string) CrocoRow {
	return CrocoRow{ID: id, Data: json.RawMessage(raw)}
}

func TestCrocoReplacePrunesRowsAndSearch(t *testing.T) {
	s, _ := crocoTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	capture := crocoTestCapture("cct:rooms")
	initial := []CrocoRow{crocoTestRow("1", `{"id":"1","title":"Obsolete","old_field":"removed"}`), crocoTestRow("2", `{"id":"2","title":"Archived"}`)}
	if err := s.CrocoReplace(ctx, capture, initial); err != nil {
		t.Fatal(err)
	}
	other := crocoTestCapture("forms")
	if err := s.CrocoReplace(ctx, other, []CrocoRow{crocoTestRow("9", `{"id":"9","title":"Inquiry"}`)}); err != nil {
		t.Fatal(err)
	}
	capture.CapturedAt = "2026-10-20T13:00:00Z"
	replacement := []CrocoRow{crocoTestRow("1", `{"id":"1","title":"Current"}`)}
	if err := s.CrocoReplace(ctx, capture, replacement); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		resource string
		want     []json.RawMessage
	}{
		{"cct:rooms", []json.RawMessage{replacement[0].Data}},
		{"forms", []json.RawMessage{json.RawMessage(`{"id":"9","title":"Inquiry"}`)}},
		{"missing", []json.RawMessage{}},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			got, err := s.CrocoRows(ctx, tc.resource)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("rows = %s, %v; want %s", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		query string
		want  int
	}{{"Obsolete", 0}, {"Archived", 0}, {"Current", 1}, {"Inquiry", 1}, {"unmatched", 0}} {
		t.Run("search_"+tc.query, func(t *testing.T) {
			got, err := s.Search(tc.query, 20)
			if err != nil || len(got) != tc.want {
				t.Fatalf("search = %s, %v; want %d", got, err, tc.want)
			}
			if len(got) > 0 && !strings.Contains(string(got[0]), tc.query) {
				t.Fatalf("search returned unrelated row: %s", got[0])
			}
		})
	}
	var resourcesCount, ftsCount int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM resources`).Scan(&resourcesCount); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM resources_fts`).Scan(&ftsCount); err != nil {
		t.Fatal(err)
	}
	if resourcesCount != 2 || ftsCount != 2 {
		t.Fatalf("resources=%d fts=%d; want both 2", resourcesCount, ftsCount)
	}
	gotCapture, err := s.CrocoCapture(ctx, capture.Resource)
	if err != nil || !reflect.DeepEqual(gotCapture, capture) {
		t.Fatalf("capture = %+v, %v; want %+v", gotCapture, err, capture)
	}
	captures, err := s.CrocoCaptures(ctx)
	if err != nil || len(captures) != 2 || captures[0].Resource != "cct:rooms" || captures[1].Resource != "forms" {
		t.Fatalf("captures = %+v, %v", captures, err)
	}
	t.Log("PASS rows pruned, removed fields not merged, unrelated collection preserved, FTS/SQL agree, capture metadata exact")
}

func TestCrocoReplaceIncompleteAndEmptyCoverage(t *testing.T) {
	s, _ := crocoTestStore(t)
	ctx := context.Background()
	capture := crocoTestCapture("forms")
	if err := s.CrocoReplace(ctx, capture, []CrocoRow{crocoTestRow("1", `{"id":1}`)}); err != nil {
		t.Fatal(err)
	}
	for _, complete := range []bool{false, true} {
		capture.Complete = complete
		capture.Note = "observed selected collection"
		if err := s.CrocoReplace(ctx, capture, nil); err != nil {
			t.Fatal(err)
		}
		got, err := s.CrocoCapture(ctx, "forms")
		if err != nil || got.Complete != complete || got.Note != capture.Note {
			t.Fatalf("coverage = %+v, %v; complete want %t", got, err, complete)
		}
		var flag bool
		var at sql.NullString
		var count int
		if err := s.DB().QueryRow(`SELECT last_attempt_complete, last_synced_at, total_count FROM sync_state WHERE resource_type = 'forms'`).Scan(&flag, &at, &count); err != nil {
			t.Fatal(err)
		}
		if flag != complete || at.Valid != complete || count != 0 {
			t.Fatalf("sync state flag=%t at=%+v count=%d; want complete=%t and count=0", flag, at, count, complete)
		}
		rows, err := s.CrocoRows(ctx, "forms")
		if err != nil || rows == nil || len(rows) != 0 {
			t.Fatalf("empty selected collection = %s, %v", rows, err)
		}
	}
	t.Log("PASS incomplete capture remains incomplete in both metadata and sync_state; captured-empty collection has zero rows")
}

func TestCrocoReplaceInvalidInputPreservesCapture(t *testing.T) {
	s, _ := crocoTestStore(t)
	ctx := context.Background()
	capture := crocoTestCapture("cct:rooms")
	old := []CrocoRow{crocoTestRow("1", `{"id":1,"title":"Original"}`)}
	if err := s.CrocoReplace(ctx, capture, old); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		rows []CrocoRow
	}{
		{"invalid_json", []CrocoRow{crocoTestRow("2", `{"id":2}`), crocoTestRow("3", `{"id":`)}},
		{"duplicate_ids", []CrocoRow{crocoTestRow("2", `{"id":2}`), crocoTestRow("2", `{"id":3}`)}},
		{"empty_id", []CrocoRow{crocoTestRow(" ", `{"id":2}`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := capture
			changed.Complete = false
			changed.Note = "must not persist"
			if err := s.CrocoReplace(ctx, changed, tc.rows); !errors.Is(err, ErrCrocoInvalidInput) {
				t.Fatalf("expected typed validation failure, got %v", err)
			}
			got, err := s.CrocoRows(ctx, capture.Resource)
			if err != nil || len(got) != 1 || string(got[0]) != string(old[0].Data) {
				t.Fatalf("old row lost: %s, %v", got, err)
			}
			metadata, err := s.CrocoCapture(ctx, capture.Resource)
			if err != nil || !reflect.DeepEqual(metadata, capture) {
				t.Fatalf("old metadata changed: %+v, %v", metadata, err)
			}
		})
	}
	t.Log("PASS invalid JSON, duplicate IDs and empty ID retain exact previous rows and coverage")
}

func TestCrocoReplaceSQLFailureRollsBackRowsFTSAndCoverage(t *testing.T) {
	s, _ := crocoTestStore(t)
	ctx := context.Background()
	capture := crocoTestCapture("forms")
	if err := s.CrocoReplace(ctx, capture, []CrocoRow{crocoTestRow("1", `{"id":1,"title":"Original"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`CREATE TRIGGER croco_test_failure BEFORE INSERT ON resources WHEN NEW.id = 'fail' BEGIN SELECT RAISE(ABORT, 'fixture write failure'); END`); err != nil {
		t.Fatal(err)
	}
	changed := capture
	changed.Complete = false
	err := s.CrocoReplace(ctx, changed, []CrocoRow{crocoTestRow("2", `{"id":2,"title":"Transient"}`), crocoTestRow("fail", `{"id":"fail"}`)})
	if err == nil || !strings.Contains(err.Error(), "fixture write failure") {
		t.Fatalf("expected actual transactional failure, got %v", err)
	}
	rows, err := s.CrocoRows(ctx, "forms")
	if err != nil || len(rows) != 1 || !strings.Contains(string(rows[0]), "Original") {
		t.Fatalf("rollback rows = %s, %v", rows, err)
	}
	for _, tc := range []struct {
		query string
		count int
	}{{"Original", 1}, {"Transient", 0}} {
		found, err := s.Search(tc.query, 10)
		if err != nil || len(found) != tc.count {
			t.Fatalf("rollback search %s = %s, %v", tc.query, found, err)
		}
	}
	got, err := s.CrocoCapture(ctx, "forms")
	if err != nil || !reflect.DeepEqual(got, capture) {
		t.Fatalf("rollback coverage = %+v, %v", got, err)
	}
	t.Log("PASS forced second-row SQL failure rolls back deletion, first insert, FTS and metadata")
}

func TestCrocoSitePinAppliesToCapturesAndEvents(t *testing.T) {
	for _, first := range []string{"capture", "event"} {
		t.Run(first, func(t *testing.T) {
			s, _ := crocoTestStore(t)
			ctx := context.Background()
			capture := crocoTestCapture("forms")
			if first == "capture" {
				if err := s.CrocoReplace(ctx, capture, nil); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.CrocoEvent(ctx, capture.Site, "update", "forms", nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			wrong := capture
			wrong.Site = "https://other.test"
			wrong.Resource = "filters"
			if err := s.CrocoReplace(ctx, wrong, nil); !errors.Is(err, ErrCrocoSiteMismatch) {
				t.Fatalf("cross-site capture accepted: %v", err)
			}
			if _, err := s.CrocoEvent(ctx, wrong.Site, "update", "forms", nil, nil); !errors.Is(err, ErrCrocoSiteMismatch) {
				t.Fatalf("cross-site event accepted: %v", err)
			}
			if _, err := s.CrocoCapture(ctx, "filters"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("mismatched site left capture: %v", err)
			}
			capture.Site += "/"
			if err := s.CrocoReplace(ctx, capture, nil); err != nil {
				t.Fatal(err)
			}
			got, err := s.CrocoCapture(ctx, "forms")
			if err != nil || got.Site != "https://example.test" {
				t.Fatalf("canonical site = %+v, %v", got, err)
			}
		})
	}
	t.Log("PASS capture-first and event-first pinning reject the other site; trailing slash canonicalized")
}

func TestCrocoReadOnlyMethodsAndNoPhantomCoverage(t *testing.T) {
	for _, populated := range []bool{false, true} {
		name := "fresh"
		if populated {
			name = "populated"
		}
		t.Run(name, func(t *testing.T) {
			s, path := crocoTestStore(t)
			ctx := context.Background()
			if populated {
				if err := s.CrocoReplace(ctx, crocoTestCapture("forms"), []CrocoRow{crocoTestRow("1", `{"id":1}`)}); err != nil {
					t.Fatal(err)
				}
				if _, err := s.CrocoEvent(ctx, "https://example.test", "update", "forms", []string{"title"}, nil); err != nil {
					t.Fatal(err)
				}
			}
			ro, err := OpenReadOnly(path)
			if err != nil {
				t.Fatal(err)
			}
			defer ro.Close()
			var before, after int
			if err := ro.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'croco_%'`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			captures, err := ro.CrocoCaptures(ctx)
			if err != nil || captures == nil {
				t.Fatalf("captures = %+v, %v", captures, err)
			}
			events, err := ro.CrocoEvents(ctx, 0)
			if err != nil || events == nil {
				t.Fatalf("events = %+v, %v", events, err)
			}
			rows, err := ro.CrocoRows(ctx, "forms")
			if err != nil || rows == nil {
				t.Fatalf("rows = %s, %v", rows, err)
			}
			capture, err := ro.CrocoCapture(ctx, "forms")
			if populated {
				if err != nil || !capture.Complete || len(captures) != 1 || len(events) != 1 || len(rows) != 1 {
					t.Fatalf("populated capture=%+v, %v; lengths=%d/%d/%d", capture, err, len(captures), len(events), len(rows))
				}
			} else {
				if !errors.Is(err, sql.ErrNoRows) || capture.Complete || len(captures)+len(events)+len(rows) != 0 {
					t.Fatalf("phantom coverage: %+v, %v", capture, err)
				}
				for _, collection := range []any{captures, events, rows} {
					encoded, err := json.Marshal(collection)
					if err != nil || string(encoded) != "[]" {
						t.Fatalf("empty JSON = %s, %v", encoded, err)
					}
				}
			}
			if err := ro.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'croco_%'`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if before != after || (!populated && after != 0) {
				t.Fatalf("read initialized tables: before=%d after=%d", before, after)
			}
			if err := ro.CrocoReplace(ctx, crocoTestCapture("forms"), nil); err == nil {
				t.Fatal("read-only write succeeded")
			}
		})
	}
	t.Log("PASS OpenReadOnly supports every read method; fresh reads create no tables and emit []/sql.ErrNoRows, not coverage")
}

func TestCrocoEventsOrderingSinceAndMetadata(t *testing.T) {
	s, _ := crocoTestStore(t)
	ctx := context.Background()
	detail := map[string]any{"id": "filter-12", "operation": "update", "method": "POST", "status": 200, "success": true, "count": 1, "scope": map[string]any{"type": "filter", "resource": "filters", "id": 12}, "reindex_event_id": 0, "changed_fields": []string{"_query_var"}, "snapshot_id": "capture-01", "verified": true}
	first, err := s.CrocoEvent(ctx, "https://example.test", "update", "filters", []string{"_query_var"}, detail)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CrocoEvent(ctx, "https://example.test", "reindex", "filters", nil, map[string]any{"operation": "reindex", "status": "success", "transport": "wp-cli", "verified": true})
	if err != nil || second <= first {
		t.Fatalf("event IDs %d/%d: %v", first, second, err)
	}
	for _, tc := range []struct {
		since int64
		ids   []int64
	}{{0, []int64{first, second}}, {first, []int64{second}}, {second, []int64{}}, {second + 10, []int64{}}} {
		events, err := s.CrocoEvents(ctx, tc.since)
		if err != nil || len(events) != len(tc.ids) || events == nil {
			t.Fatalf("since %d: %+v, %v", tc.since, events, err)
		}
		for i, id := range tc.ids {
			if events[i]["id"] != id || events[i]["site"] != "https://example.test" || events[i]["resource"] != "filters" {
				t.Fatalf("event mismatch: %+v", events[i])
			}
			if _, err := time.Parse(time.RFC3339Nano, events[i]["created_at"].(string)); err != nil {
				t.Fatal(err)
			}
		}
	}
	events, err := s.CrocoEvents(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	gotDetail := events[0]["detail"].(map[string]any)
	if gotDetail["id"] != "filter-12" || gotDetail["status"] != float64(200) || gotDetail["verified"] != true {
		t.Fatalf("metadata lost: %+v", gotDetail)
	}
	if !reflect.DeepEqual(events[0]["fields"], []string{"_query_var"}) || !reflect.DeepEqual(events[1]["fields"], []string{}) {
		t.Fatalf("fields differ: %+v", events)
	}
	if _, err := s.CrocoEvents(ctx, -1); !errors.Is(err, ErrCrocoInvalidInput) {
		t.Fatalf("negative since accepted: %v", err)
	}
	t.Log("PASS monotonic IDs, strict since cutoff, ascending order, metadata shapes and empty []")
}

func TestCrocoEventsRejectPayloadAndSecrets(t *testing.T) {
	s, _ := crocoTestStore(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		detail map[string]any
	}{
		{"email", map[string]any{"email": "person@example.test"}},
		{"raw_body", map[string]any{"body": map[string]any{"title": "private"}}},
		{"secret_as_id", map[string]any{"id": "sk_live_example"}},
		{"email_as_id", map[string]any{"id": "person@example.test"}},
		{"credential_url", map[string]any{"id": "https://user:password@example.test"}},
		{"scope_payload", map[string]any{"scope": map[string]any{"email": "person@example.test"}}},
		{"status_free_text", map[string]any{"status": "private personal details"}},
		{"field_values", map[string]any{"changed_fields": []string{"person@example.test"}}},
		{"negative_count", map[string]any{"count": -1}},
		{"fractional_count", map[string]any{"count": 1.5}},
		{"null_flag", map[string]any{"verified": nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if id, err := s.CrocoEvent(ctx, "https://example.test", "update", "forms", nil, tc.detail); id != 0 || !errors.Is(err, ErrCrocoInvalidInput) {
				t.Fatalf("unsafe event accepted: id=%d err=%v", id, err)
			}
		})
	}
	if _, err := s.CrocoEvent(ctx, "https://example.test", "update", "forms", []string{"person@example.test"}, nil); !errors.Is(err, ErrCrocoInvalidInput) {
		t.Fatalf("field value accepted: %v", err)
	}
	events, err := s.CrocoEvents(ctx, 0)
	if err != nil || len(events) != 0 {
		t.Fatalf("rejected events persisted: %+v, %v", events, err)
	}
	var tables int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'croco_%'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("invalid events pinned or initialized database: %d", tables)
	}
	t.Log("PASS unsupported payloads, PII, credential-shaped IDs and invalid types rejected without rows or site pin")
}
