// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"context"
	"path/filepath"
	"testing"
)

// TestEnsureDelhiveryMapsTablesUpgradesAnOlderDatabase builds the first-release
// quota tables (no scope or bucket columns) with rows in them, then checks the
// additive migration keeps those rows and adds the columns exactly once.
func TestEnsureDelhiveryMapsTablesUpgradesAnOlderDatabase(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db := s.DB()
	for _, stmt := range []string{
		`CREATE TABLE dm_calls (called_at INTEGER NOT NULL, endpoint TEXT NOT NULL)`,
		`CREATE TABLE dm_lockout (locked_at INTEGER NOT NULL)`,
		`INSERT INTO dm_calls VALUES (1700000000, '/geocode')`,
		`INSERT INTO dm_lockout VALUES (1700000000)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ { // idempotent
		if err := EnsureDelhiveryMapsTables(ctx, db); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	for _, c := range delhiveryMapsColumns {
		if has, err := hasColumn(ctx, db, c.table, c.column); err != nil || !has {
			t.Fatalf("%s.%s missing after migration (err %v)", c.table, c.column, err)
		}
	}
	var scope string
	var n int
	if err := db.QueryRow(`SELECT scope, COUNT(*) FROM dm_calls`).Scan(&scope, &n); err != nil || scope != "" || n != 1 {
		t.Fatalf("old call row = scope %q count %d, err %v: it must survive with an empty scope", scope, n, err)
	}
	var bucket string
	if err := db.QueryRow(`SELECT bucket FROM dm_lockout`).Scan(&bucket); err != nil || bucket != "" {
		t.Fatalf("old lockout bucket = %q, %v", bucket, err)
	}
	// And the other tables exist.
	if ok, err := HasDelhiveryMapsAddressTable(ctx, db); err != nil || !ok {
		t.Fatalf("dm_address missing: %v", err)
	}
}
