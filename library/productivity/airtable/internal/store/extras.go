// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateExtras runs after the generated store migrations and before the
// schema-version stamp. It is the canonical place for novel-feature auxiliary
// tables that need to live in the local store.
//
// Edit this file when adding tables for novel commands. Keep migrations
// idempotent with CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS so
// every store open can safely re-run them.
func (s *Store) migrateExtras(ctx context.Context, conn *sql.Conn) error {
	migrations := []string{
		// Add CREATE TABLE IF NOT EXISTS statements here.
	}
	for _, m := range migrations {
		if _, err := conn.ExecContext(ctx, m); err != nil {
			return fmt.Errorf("extra migration failed: %w", err)
		}
	}
	return nil
}

// ReconcileAirtableRecords prunes a fully fetched base/table partition.
// Only rows synced before syncedBefore (RFC3339 UTC) are candidates.
func (s *Store) ReconcileAirtableRecords(baseID, table string, seenIDs []string, syncedBefore string) (int, error) {
	return s.reconcileUnseen("records", seenIDs, "", nil,
		`SELECT id FROM resources
		 WHERE resource_type = ?
		   AND (CASE WHEN json_valid(data) THEN json_extract(data, ?) END) = ?
		   AND (CASE WHEN json_valid(data) THEN json_extract(data, ?) END) = ?
		   AND synced_at < ?`,
		"records", "$.base_id", baseID, "$.table", table, syncedBefore)
}
