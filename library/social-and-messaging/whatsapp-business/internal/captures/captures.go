// Copyright 2026 bobe and contributors. Licensed under Apache-2.0.

// Package captures stores immutable, explicitly scoped MCP read evidence.
package captures

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/store"
)

// ErrNotFound means an explicit capture ID was not saved in this store.
var ErrNotFound = errors.New("capture not found")

// Snapshot is one successful read, including the exact selection and coverage.
// Complete describes this capture, never the readiness of a business asset.
type Snapshot struct {
	ID         string         `json:"id"`
	Tool       string         `json:"tool"`
	Scope      map[string]any `json:"scope"`
	CapturedAt string         `json:"captured_at"`
	Complete   bool           `json:"complete"`
	Data       any            `json:"data"`
}

const schemaSQL = `CREATE TABLE IF NOT EXISTS mcp_captures (
	id TEXT PRIMARY KEY NOT NULL,
	tool TEXT NOT NULL,
	captured_at TEXT NOT NULL,
	snapshot_json TEXT NOT NULL
)`
const ftsSQL = `CREATE VIRTUAL TABLE IF NOT EXISTS mcp_captures_fts USING fts5(
	capture_id UNINDEXED, content, tokenize='unicode61'
)`

// Save appends a capture and its full-text index atomically. It never replaces
// an earlier capture and never changes the supplied scope or payload objects.
// Callers must pass only successful read replies, with MCP envelopes decoded.
func Save(ctx context.Context, db *store.Store, tool string, args map[string]any, data any) (Snapshot, error) {
	var empty Snapshot
	if err := checkStore(db); err != nil {
		return empty, err
	}
	if strings.TrimSpace(tool) == "" {
		return empty, errors.New("capture source tool must not be empty")
	}
	if args == nil {
		args = map[string]any{}
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return empty, fmt.Errorf("create capture ID: %w", err)
	}
	snapshot := Snapshot{
		ID: "capture-" + hex.EncodeToString(id), Tool: tool, Scope: args,
		CapturedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z"),
		Data:       data,
	}
	// Round-trip once to detach maps/slices from the caller and base coverage
	// on the exact JSON that will be saved, including typed Go input values.
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return empty, fmt.Errorf("encode capture: %w", err)
	}
	if snapshot, err = decode(raw); err != nil {
		return empty, err
	}
	snapshot.Complete = complete(snapshot.Scope, snapshot.Data)
	raw, err = json.Marshal(snapshot)
	if err != nil {
		return empty, fmt.Errorf("encode normalized capture: %w", err)
	}
	tx, err := db.DB().BeginTx(ctx, nil)
	if err != nil {
		return empty, fmt.Errorf("begin capture transaction: %w", err)
	}
	defer tx.Rollback()
	for _, stmt := range []string{schemaSQL, ftsSQL, `CREATE INDEX IF NOT EXISTS mcp_captures_tool_time ON mcp_captures(tool, captured_at DESC)`} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return empty, fmt.Errorf("initialize capture tables: %w", err)
		}
	}
	row, err := tx.ExecContext(ctx, `INSERT INTO mcp_captures(id,tool,captured_at,snapshot_json) VALUES(?,?,?,?)`, snapshot.ID, tool, snapshot.CapturedAt, string(raw))
	if err != nil {
		return empty, fmt.Errorf("save capture: %w", err)
	}
	rowID, err := row.LastInsertId()
	if err != nil {
		return empty, fmt.Errorf("read saved capture row: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_captures_fts(rowid,capture_id,content) VALUES(?,?,?)`, rowID, snapshot.ID, string(raw)); err != nil {
		return empty, fmt.Errorf("index capture: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return empty, fmt.Errorf("commit capture: %w", err)
	}
	return snapshot, nil
}

// Get returns one immutable capture. Missing IDs wrap ErrNotFound.
func Get(ctx context.Context, db *store.Store, id string) (Snapshot, error) {
	var empty Snapshot
	if err := checkStore(db); err != nil {
		return empty, err
	}
	if strings.TrimSpace(id) == "" {
		return empty, errors.New("capture ID must not be empty")
	}
	exists, err := hasTable(ctx, db)
	if err != nil {
		return empty, err
	}
	if !exists {
		return empty, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	var raw []byte
	err = db.DB().QueryRowContext(ctx, `SELECT snapshot_json FROM mcp_captures WHERE id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return empty, fmt.Errorf("read capture: %w", err)
	}
	return decode(raw)
}

// List returns newest captures, optionally restricted to the exact source tool.
// A non-positive limit uses 20; limits larger than 1000 are capped at 1000.
// An unused store returns [] without creating tables as a read side effect.
func List(ctx context.Context, db *store.Store, tool string, limit int) ([]Snapshot, error) {
	if err := checkStore(db); err != nil {
		return nil, err
	}
	exists, err := hasTable(ctx, db)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []Snapshot{}, nil
	}
	query := `SELECT snapshot_json FROM mcp_captures`
	args := make([]any, 0, 2)
	if tool != "" {
		query += ` WHERE tool=?`
		args = append(args, tool)
	}
	query += ` ORDER BY captured_at DESC,id DESC LIMIT ?`
	args = append(args, boundedLimit(limit))
	return readRows(ctx, db, query, args...)
}

var searchParts = regexp.MustCompile(`"[^"]+"|[\pL\pN_]+`)

// Search matches literal terms (AND) and quoted phrases in saved JSON content.
// FTS operators are always quoted as data. Query text never becomes SQL.
func Search(ctx context.Context, db *store.Store, query string, limit int) ([]Snapshot, error) {
	if err := checkStore(db); err != nil {
		return nil, err
	}
	parts := searchParts.FindAllString(query, -1)
	if len(parts) == 0 {
		return []Snapshot{}, nil
	}
	for i, part := range parts {
		if strings.HasPrefix(part, `"`) && strings.HasSuffix(part, `"`) {
			part = part[1 : len(part)-1]
		}
		parts[i] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}
	exists, err := hasTable(ctx, db)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []Snapshot{}, nil
	}
	return readRows(ctx, db, `SELECT c.snapshot_json FROM mcp_captures_fts
JOIN mcp_captures c ON c.rowid=mcp_captures_fts.rowid
WHERE mcp_captures_fts MATCH ?
ORDER BY bm25(mcp_captures_fts),c.captured_at DESC,c.id DESC LIMIT ?`, strings.Join(parts, " AND "), boundedLimit(limit))
}

func readRows(ctx context.Context, db *store.Store, query string, args ...any) ([]Snapshot, error) {
	rows, err := db.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query captures: %w", err)
	}
	defer rows.Close()
	result := make([]Snapshot, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan capture: %w", err)
		}
		snapshot, err := decode(raw)
		if err != nil {
			return nil, err
		}
		result = append(result, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate captures: %w", err)
	}
	return result, nil
}

func decode(raw []byte) (Snapshot, error) {
	var snapshot Snapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber() // Do not round numeric identifiers or captured counters.
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode stored capture: %w", err)
	}
	return snapshot, nil
}

func checkStore(db *store.Store) error {
	if db == nil || db.DB() == nil {
		return errors.New("capture store is not open")
	}
	return nil
}

func hasTable(ctx context.Context, db *store.Store) (bool, error) {
	var exists int
	if err := db.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='mcp_captures')`).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect capture schema: %w", err)
	}
	return exists != 0, nil
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func complete(scope map[string]any, data any) bool {
	for _, key := range []string{"after", "shared_after", "apps_after", "accounts_after"} {
		if nonempty(scope[key]) {
			return false
		}
	}
	return data != nil && !partialEnvelope(data)
}

func nonempty(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

// Only follow recognized envelope/metadata objects. A template component named
// "incomplete" or a cursor in arbitrary content cannot change capture coverage.
func partialEnvelope(data any) bool {
	obj, ok := data.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"next_after", "shared_next_after", "next_apps_after", "next_accounts_after", "nextCursor"} {
		if nonempty(obj[key]) {
			return true
		}
	}
	for _, key := range []string{"truncated", "incomplete", "more_than_this_page"} {
		if flag, ok := obj[key].(bool); ok && flag {
			return true
		}
	}
	for _, key := range []string{"meta", "metadata", "pagination", "paging", "page_info", "pageInfo", "result", "data"} {
		if partialEnvelope(obj[key]) {
			return true
		}
	}
	return false
}
