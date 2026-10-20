// Copyright 2026 elizabethcstone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/productivity/airtable/internal/store"
)

// commonPrimaryFieldNames are tried in order when picking a display value for
// a resolved linked record. Airtable has no fixed primary-field convention
// (it is whatever field the base owner made first/primary), so this is a
// best-effort heuristic, not a schema-accurate lookup — resolving the real
// primaryFieldId would require a live schema fetch per base, which conflicts
// with this flag's no-extra-API-calls design.
var commonPrimaryFieldNames = []string{"Name", "Title", "Product", "Company", "Email"}

// expandLinkedRecords resolves linked-record ID arrays in a record's fields
// to their referenced record's display value, using records already present
// in the local store (populated via `records sync-table`). IDs with no local
// match are left as-is. Handles the three shapes the framework's client can
// hand back: a bare array (the shape --all produces once every page has been
// flattened), a single-page list envelope {"records": [...], "offset": ...},
// and a single record object (records get).
func expandLinkedRecords(ctx context.Context, data json.RawMessage) json.RawMessage {
	dbPath := defaultDBPath("airtable-pp-cli")
	db, err := store.OpenReadOnlyContext(ctx, dbPath)
	if err != nil {
		return data
	}
	defer db.Close()

	lookup := func(id string) string {
		row := db.DB().QueryRowContext(ctx, `SELECT data FROM resources WHERE resource_type = 'records' AND id = ?`, id)
		var raw string
		if err := row.Scan(&raw); err != nil {
			return id
		}
		var rec struct {
			Fields map[string]any `json:"fields"`
		}
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			return id
		}
		for _, candidate := range commonPrimaryFieldNames {
			if v, ok := rec.Fields[candidate].(string); ok && v != "" {
				return v
			}
		}
		return id
	}

	expandFields := func(fields map[string]any) {
		for key, val := range fields {
			arr, ok := val.([]any)
			if !ok || len(arr) == 0 {
				continue
			}
			allLinks := true
			ids := make([]string, 0, len(arr))
			for _, item := range arr {
				s, ok := item.(string)
				if !ok || !strings.HasPrefix(s, "rec") {
					allLinks = false
					break
				}
				ids = append(ids, s)
			}
			if !allLinks {
				continue
			}
			expanded := make([]string, len(ids))
			for i, id := range ids {
				expanded[i] = lookup(id)
			}
			fields[key] = expanded
		}
	}

	var asArray []map[string]any
	if err := json.Unmarshal(data, &asArray); err == nil {
		for _, rec := range asArray {
			if fields, ok := rec["fields"].(map[string]any); ok {
				expandFields(fields)
			}
		}
		if out, err := json.Marshal(asArray); err == nil {
			return out
		}
		return data
	}

	var asObject map[string]any
	if err := json.Unmarshal(data, &asObject); err != nil {
		return data
	}

	if rawRecords, ok := asObject["records"].([]any); ok {
		for _, item := range rawRecords {
			if rec, ok := item.(map[string]any); ok {
				if fields, ok := rec["fields"].(map[string]any); ok {
					expandFields(fields)
				}
			}
		}
		if out, err := json.Marshal(asObject); err == nil {
			return out
		}
		return data
	}

	if fields, ok := asObject["fields"].(map[string]any); ok {
		expandFields(fields)
		if out, err := json.Marshal(asObject); err == nil {
			return out
		}
	}
	return data
}
