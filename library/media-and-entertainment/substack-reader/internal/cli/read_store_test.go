// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/store"
	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/substack"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func storedField(t *testing.T, db *store.Store, id, field string) string {
	t.Helper()
	raw, err := db.Get("posts", id)
	if err != nil {
		t.Fatalf("Get posts/%s: %v", id, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("stored record is not JSON: %v", err)
	}
	s, _ := m[field].(string)
	return s
}

var (
	fullAccess    = substack.Access{Full: true, Tier: "entitled"}
	previewAccess = substack.Access{Full: false, Tier: "preview"}
)

const rawPost = `{"id":5,"slug":"a","title":"T","audience":"only_paid","body_html":"<p>zyzzyva body</p>"}`

// TestCorpusRecordKeepsThePostAndAddsBodyAndProvenance.
func TestCorpusRecordKeepsThePostAndAddsBodyAndProvenance(t *testing.T) {
	rec, err := corpusRecord(json.RawMessage(rawPost), "pub.substack.com", "zyzzyva body", fullAccess)
	if err != nil {
		t.Fatalf("corpusRecord: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(rec, &m); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{
		"title": "T", "body_html": "<p>zyzzyva body</p>",
		"_pp_body_text": "zyzzyva body", "_pp_source_host": "pub.substack.com", "_pp_access": "entitled",
	} {
		if m[k] != want {
			t.Errorf("%s = %v, want %v", k, m[k], want)
		}
	}
}

// TestStoreReadIsFoundBySearch: what read fetched is in the corpus, with its
// body, so search finds a word that is only in the article text.
func TestStoreReadIsFoundBySearch(t *testing.T) {
	db := openTestStore(t)
	rec, _ := corpusRecord(json.RawMessage(rawPost), "pub.substack.com", "zyzzyva body", fullAccess)
	if stored, err := storeRead(db, "5", rec, fullAccess); err != nil || !stored {
		t.Fatalf("storeRead = %v, %v", stored, err)
	}
	hits, err := db.Search("zyzzyva", 10, "posts")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("search hits = %d, want 1", len(hits))
	}
}

// TestStoreReadNeverReplacesAFullCopyWithAPreview: a later rate-limited read
// that comes back as a preview must not overwrite the full text already stored;
// the reverse (a preview upgraded by a full read) must happen.
func TestStoreReadNeverReplacesAFullCopyWithAPreview(t *testing.T) {
	db := openTestStore(t)
	preview, _ := corpusRecord(json.RawMessage(rawPost), "pub.substack.com", "short teaser", previewAccess)
	full, _ := corpusRecord(json.RawMessage(rawPost), "pub.substack.com", "the whole article", fullAccess)

	if stored, err := storeRead(db, "5", preview, previewAccess); err != nil || !stored {
		t.Fatalf("first preview: stored=%v err=%v", stored, err)
	}
	if stored, err := storeRead(db, "5", full, fullAccess); err != nil || !stored {
		t.Fatalf("full over preview: stored=%v err=%v", stored, err)
	}
	if got := storedField(t, db, "5", "_pp_body_text"); got != "the whole article" {
		t.Fatalf("after the full read, body = %q", got)
	}
	stored, err := storeRead(db, "5", preview, previewAccess)
	if err != nil {
		t.Fatal(err)
	}
	if stored {
		t.Error("a preview replaced the stored full copy")
	}
	if got := storedField(t, db, "5", "_pp_body_text"); got != "the whole article" {
		t.Errorf("full copy lost: body = %q", got)
	}
}

// TestStoreReadUpgradesAnArchivePreview: archive stores anonymous previews with
// no access marker; a read must be able to replace one.
func TestStoreReadUpgradesAnArchivePreview(t *testing.T) {
	db := openTestStore(t)
	if err := db.Upsert("posts", "5", json.RawMessage(`{"id":5,"title":"T","description":"teaser"}`)); err != nil {
		t.Fatal(err)
	}
	full, _ := corpusRecord(json.RawMessage(rawPost), "pub.substack.com", "the whole article", fullAccess)
	if stored, err := storeRead(db, "5", full, fullAccess); err != nil || !stored {
		t.Fatalf("storeRead = %v, %v", stored, err)
	}
	if got := storedField(t, db, "5", "_pp_body_text"); got != "the whole article" {
		t.Errorf("body = %q", got)
	}
}

// TestKeepReadFieldsAcrossAnArchiveRerun: archive replaces a post's whole
// record, so a re-run would wipe the text read had stored. The fields read
// wrote are carried over onto the fresh archive item.
func TestKeepReadFieldsAcrossAnArchiveRerun(t *testing.T) {
	db := openTestStore(t)
	full, _ := corpusRecord(json.RawMessage(rawPost), "pub.substack.com", "the whole article", fullAccess)
	if _, err := storeRead(db, "5", full, fullAccess); err != nil {
		t.Fatal(err)
	}

	item := json.RawMessage(`{"id":5,"title":"T (renamed)","description":"teaser"}`)
	prev, err := db.Get("posts", "5")
	if err != nil {
		t.Fatal(err)
	}
	merged := keepReadFields(prev, item)
	var m map[string]any
	if err := json.Unmarshal(merged, &m); err != nil {
		t.Fatal(err)
	}
	if m["title"] != "T (renamed)" {
		t.Errorf("the fresh archive metadata must win, title = %v", m["title"])
	}
	for _, k := range []string{"_pp_body_text", "_pp_access", "body_html"} {
		if m[k] == nil || m[k] == "" {
			t.Errorf("%s was not carried over: %v", k, m)
		}
	}

	// A post read never touched passes through unchanged.
	if got := keepReadFields(nil, item); string(got) != string(item) {
		t.Errorf("an unread post changed: %s", got)
	}
}

// TestStoreReadMakesAFreshCorpusTrusted: search and digest refuse to trust a
// corpus whose sync state was never stamped ("local store has not been synced
// yet"). A corpus that only read has filled must be stamped, like archive does.
func TestStoreReadMakesAFreshCorpusTrusted(t *testing.T) {
	db := openTestStore(t)
	rec, _ := corpusRecord(json.RawMessage(rawPost), "pub.substack.com", "body", fullAccess)
	if _, err := storeRead(db, "5", rec, fullAccess); err != nil {
		t.Fatal(err)
	}
	_, at, count, err := db.GetSyncState("posts")
	if err != nil {
		t.Fatal(err)
	}
	if at.IsZero() || count != 1 {
		t.Errorf("sync state = (%v, %d), want stamped with 1 post", at, count)
	}
}

// TestStoreReadLeavesAnArchiveSyncStateAlone: a corpus archive already stamped
// keeps its stamp; read must not shrink the recorded total to the posts it read.
func TestStoreReadLeavesAnArchiveSyncStateAlone(t *testing.T) {
	db := openTestStore(t)
	if err := db.SaveSyncState("posts", "", 287); err != nil {
		t.Fatal(err)
	}
	rec, _ := corpusRecord(json.RawMessage(rawPost), "pub.substack.com", "body", fullAccess)
	if _, err := storeRead(db, "5", rec, fullAccess); err != nil {
		t.Fatal(err)
	}
	if _, _, count, _ := db.GetSyncState("posts"); count != 287 {
		t.Errorf("total_count = %d, want the archive's 287", count)
	}
}
