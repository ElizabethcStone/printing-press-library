// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"context"
	"database/sql"
	"fmt"
)

// delhiveryMapsSchema holds the tables behind the hand-written address,
// trip, coverage and fleet commands. It is applied lazily by
// EnsureDelhiveryMapsTables rather than from migrate(), so the generated
// store keeps its own schema version untouched.
//
//   - dm_cache: one row per distinct API request, keyed by endpoint plus the
//     sha256 of the normalized request, so a repeated request costs no quota.
//   - dm_address: one row per distinct raw address across every clean run;
//     address report and address dupes read it.
//   - dm_calls: a log of live API calls per scope (base URL and token
//     fingerprint), used to estimate the remaining share of the quota.
//   - dm_lockout: the time of the last HTTP 429 per scope and quota bucket, so
//     the next run in the same window plans zero calls instead of spending one
//     to rediscover it.
//
// dm_cache and dm_address hold addresses and coordinates, which are personal
// data; 'address purge' deletes them by age.
const delhiveryMapsSchema = `
CREATE TABLE IF NOT EXISTS dm_cache (
	endpoint   TEXT NOT NULL,
	key        TEXT NOT NULL,
	request    TEXT,
	response   TEXT,
	fetched_at TEXT,
	PRIMARY KEY (endpoint, key)
);
CREATE TABLE IF NOT EXISTS dm_address (
	raw_key           TEXT PRIMARY KEY,
	raw               TEXT,
	standardized      TEXT,
	validated_quality TEXT,
	granularity       TEXT,
	formatted         TEXT,
	lat               REAL,
	lng               REAL,
	error_radius      INTEGER,
	pincode           TEXT,
	city              TEXT,
	state             TEXT,
	updated_at        TEXT
);
CREATE TABLE IF NOT EXISTS dm_calls (
	called_at INTEGER NOT NULL,
	endpoint  TEXT NOT NULL,
	scope     TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS dm_lockout (
	locked_at INTEGER NOT NULL,
	bucket    TEXT NOT NULL DEFAULT '',
	scope     TEXT NOT NULL DEFAULT ''
);
`

// delhiveryMapsColumns are columns added after the first release. They are
// added to existing databases with ALTER TABLE; rows written before then carry
// an empty scope and are ignored by the quota queries.
var delhiveryMapsColumns = []struct{ table, column, ddl string }{
	{"dm_calls", "scope", "TEXT NOT NULL DEFAULT ''"},
	{"dm_lockout", "bucket", "TEXT NOT NULL DEFAULT ''"},
	{"dm_lockout", "scope", "TEXT NOT NULL DEFAULT ''"},
}

// delhiveryMapsIndexes run after the column migration, since they use the
// added columns.
const delhiveryMapsIndexes = `
CREATE INDEX IF NOT EXISTS dm_calls_scope_called_at ON dm_calls (scope, called_at);
`

// EnsureDelhiveryMapsTables creates the dm_* tables when they are missing.
// It is idempotent and cheap enough to call at the start of every command.
func EnsureDelhiveryMapsTables(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("ensure dm tables: nil database handle")
	}
	if _, err := db.ExecContext(ctx, delhiveryMapsSchema); err != nil {
		return fmt.Errorf("ensure dm tables: %w", err)
	}
	for _, c := range delhiveryMapsColumns {
		has, err := hasColumn(ctx, db, c.table, c.column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := db.ExecContext(ctx, "ALTER TABLE "+c.table+" ADD COLUMN "+c.column+" "+c.ddl); err != nil {
			return fmt.Errorf("ensure dm tables: adding %s.%s: %w", c.table, c.column, err)
		}
	}
	if _, err := db.ExecContext(ctx, delhiveryMapsIndexes); err != nil {
		return fmt.Errorf("ensure dm tables: %w", err)
	}
	return nil
}

// hasColumn reports whether table has a column. The table and column names
// are package constants, never user input.
func hasColumn(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, fmt.Errorf("inspecting %s: %w", table, err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, fmt.Errorf("inspecting %s: %w", table, err)
		}
		if name == column {
			found = true
		}
	}
	return found, rows.Err()
}

// HasDelhiveryMapsAddressTable reports whether dm_address exists, without
// creating it. Read-only commands use it to answer "nothing cleaned yet"
// against a database they must not modify.
func HasDelhiveryMapsAddressTable(ctx context.Context, db *sql.DB) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'dm_address'`).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check dm_address table: %w", err)
	}
	return n > 0, nil
}
