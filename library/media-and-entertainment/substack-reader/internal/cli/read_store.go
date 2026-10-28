// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.
// read -> corpus: what read fetched is kept in the local SQLite corpus, with its
// body, so search and sql find a post you have read in full.

package cli

import (
	"encoding/json"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/store"
	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/substack"
)

// readFieldKeys are the record fields read adds on top of the post object, and
// that an archive re-run must not wipe (archive replaces a post's whole record
// with the fresh archive item).
var readFieldKeys = []string{"body_html", "_pp_body_text", "_pp_access"}

// corpusRecord is the post object as Substack returned it, plus the plain body
// text (the field the corpus indexes for search), the publication host and the
// access verdict ("free", "entitled", "full" or "preview").
func corpusRecord(raw json.RawMessage, host, bodyText string, access substack.Access) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for k, v := range map[string]string{
		"_pp_body_text":   bodyText,
		"_pp_source_host": host,
		"_pp_access":      access.Tier,
	} {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		m[k] = b
	}
	return json.Marshal(m)
}

// storedIsFull reports whether a stored record holds a full body that a read
// verified (a preview marker, or an archive record with no marker, is not full).
func storedIsFull(rec json.RawMessage) bool {
	var m struct {
		Access string `json:"_pp_access"`
	}
	if json.Unmarshal(rec, &m) != nil {
		return false
	}
	return m.Access != "" && m.Access != "preview"
}

// storeRead upserts a post read into the corpus. A full copy is never replaced
// by a preview (a later rate-limited read would otherwise delete the text a
// good read stored), while a preview, from archive or from an earlier read, is
// always replaced by a fuller one. The check runs inside the write, so a full
// copy another process stores meanwhile is never overwritten by this preview.
// It reports whether the record was written.
func storeRead(db *store.Store, id string, rec json.RawMessage, access substack.Access) (bool, error) {
	stored, err := db.UpsertWith("posts", id, func(prev json.RawMessage) (json.RawMessage, bool) {
		return rec, access.Full || !storedIsFull(prev)
	})
	if err != nil || !stored {
		return false, err
	}
	markCorpusSynced(db)
	return true, nil
}

// markCorpusSynced stamps the sync state of a corpus that has none, so the
// local-first commands (search, digest) trust it instead of warning that the
// store "has not been synced yet". archive does the same for its own corpus. A
// corpus archive already stamped keeps its recorded total. Best effort: the
// post is already stored, and a missing stamp only costs a warning later.
func markCorpusSynced(db *store.Store) {
	if _, at, _, err := db.GetSyncState("posts"); err != nil || !at.IsZero() {
		return
	}
	if n, err := db.Count("posts"); err == nil {
		_ = db.SaveSyncState("posts", "", n)
	}
}

// keepReadFields carries the fields read stored over onto a fresh archive item
// for the same post, so re-archiving a publication does not turn the full texts
// you read back into previews. prev is the record stored now (nil when there is
// none). The archive item's own fields win; a post read never touched comes
// back unchanged.
func keepReadFields(prev, item json.RawMessage) json.RawMessage {
	if !storedIsFull(prev) {
		return item
	}
	var old, fresh map[string]json.RawMessage
	if json.Unmarshal(prev, &old) != nil || json.Unmarshal(item, &fresh) != nil {
		return item
	}
	for _, k := range readFieldKeys {
		if v, ok := old[k]; ok {
			fresh[k] = v
		}
	}
	out, err := json.Marshal(fresh)
	if err != nil {
		return item
	}
	return out
}
