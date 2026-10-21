package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// CrocoCapture describes the observed scope, not a promise that all site data is present.
type CrocoCapture struct {
	Site       string            `json:"site"`
	Resource   string            `json:"resource"`
	CapturedAt string            `json:"captured_at"`
	Complete   bool              `json:"complete"`
	Fields     []string          `json:"fields"`
	Versions   map[string]string `json:"versions"`
	Scope      map[string]any    `json:"scope"`
	Note       string            `json:"note,omitempty"`
}

type CrocoRow struct {
	ID   string
	Data json.RawMessage
}

var (
	ErrCrocoSiteMismatch = errors.New("crocoblock database is pinned to another site")
	ErrCrocoInvalidInput = errors.New("invalid crocoblock store input")
	crocoIdentifierRE    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:/-]{0,255}$`)
)

func crocoInvalid(message string) error {
	return fmt.Errorf("%w: %s", ErrCrocoInvalidInput, message)
}

func crocoSite(site string) (string, error) {
	site = strings.TrimRight(strings.TrimSpace(site), "/")
	if site == "" || len(site) > 2048 || strings.ContainsAny(site, "\r\n\t @?#") {
		return "", crocoInvalid("site must be a nonempty identifier or URL without credentials, query or fragment")
	}
	if strings.Contains(site, "://") {
		u, err := url.Parse(site)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return "", crocoInvalid("site must use an HTTP(S) URL without credentials")
		}
	}
	return site, nil
}

// crocoInitTx is write-only lazy initialization; read methods never call it.
func crocoInitTx(ctx context.Context, tx *sql.Tx, site string) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS croco_site (singleton INTEGER PRIMARY KEY CHECK(singleton = 1), site TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS croco_captures (resource TEXT PRIMARY KEY, capture TEXT NOT NULL CHECK(json_valid(capture)))`,
		`CREATE TABLE IF NOT EXISTS croco_events (id INTEGER PRIMARY KEY AUTOINCREMENT, site TEXT NOT NULL, kind TEXT NOT NULL, resource TEXT NOT NULL, fields TEXT NOT NULL CHECK(json_valid(fields)), detail TEXT NOT NULL CHECK(json_valid(detail)), created_at TEXT NOT NULL)`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize crocoblock store: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO croco_site(singleton, site) VALUES (1, ?)`, site); err != nil {
		return fmt.Errorf("pin crocoblock site: %w", err)
	}
	var existing string
	if err := tx.QueryRowContext(ctx, `SELECT site FROM croco_site WHERE singleton = 1`).Scan(&existing); err != nil {
		return fmt.Errorf("read crocoblock site: %w", err)
	}
	if existing != site {
		return ErrCrocoSiteMismatch
	}
	return nil
}

// CrocoReplace replaces a selected collection and its coverage in one transaction.
// Incomplete observations replace older rows too, but never retain old complete coverage.
func (s *Store) CrocoReplace(ctx context.Context, capture CrocoCapture, rows []CrocoRow) error {
	var err error
	capture.Site, err = crocoSite(capture.Site)
	if err != nil {
		return err
	}
	if !crocoIdentifierRE.MatchString(capture.Resource) {
		return crocoInvalid("resource must be a nonempty resource identifier")
	}
	if _, err := time.Parse(time.RFC3339Nano, capture.CapturedAt); err != nil {
		return crocoInvalid("captured_at must be an RFC3339 timestamp")
	}
	if capture.Fields == nil {
		capture.Fields = []string{}
	}
	if capture.Versions == nil {
		capture.Versions = map[string]string{}
	}
	if capture.Scope == nil {
		capture.Scope = map[string]any{}
	}
	encoded, err := json.Marshal(capture)
	if err != nil {
		return crocoInvalid("capture must be JSON serializable")
	}
	scope, err := json.Marshal(capture.Scope)
	if err != nil {
		return crocoInvalid("scope must be JSON serializable")
	}
	seen := make(map[string]bool, len(rows))
	for i, row := range rows {
		if strings.TrimSpace(row.ID) == "" || seen[row.ID] {
			return crocoInvalid(fmt.Sprintf("row %d has an empty or duplicate ID", i))
		}
		seen[row.ID] = true
		if !json.Valid(row.Data) {
			return crocoInvalid(fmt.Sprintf("row %d contains invalid JSON", i))
		}
	}
	s.lockForWrite()
	defer s.unlockAfterWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin crocoblock replacement: %w", err)
	}
	defer tx.Rollback()
	if err := crocoInitTx(ctx, tx, capture.Site); err != nil {
		return err
	}
	old, err := tx.QueryContext(ctx, `SELECT id FROM resources WHERE resource_type = ?`, capture.Resource)
	if err != nil {
		return fmt.Errorf("read previous crocoblock rows: %w", err)
	}
	ids := make([]string, 0)
	for old.Next() {
		var id string
		if err := old.Scan(&id); err != nil {
			old.Close()
			return fmt.Errorf("scan previous crocoblock rows: %w", err)
		}
		ids = append(ids, id)
	}
	rowsErr := old.Err()
	closeErr := old.Close() // Drain/close before follow-up SQL on the same connection.
	if rowsErr != nil {
		return fmt.Errorf("read previous crocoblock rows: %w", rowsErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close previous crocoblock rows: %w", closeErr)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM resources_fts WHERE rowid = ?`, ftsRowID(capture.Resource, id)); err != nil {
			return fmt.Errorf("prune crocoblock search rows: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM resources WHERE resource_type = ?`, capture.Resource); err != nil {
		return fmt.Errorf("prune crocoblock rows: %w", err)
	}
	for _, row := range rows {
		if err := s.upsertGenericResourceTx(tx, capture.Resource, row.ID, row.Data); err != nil {
			return fmt.Errorf("replace crocoblock row: %w", err)
		}
		// The generated helper treats FTS failures as warnings. This operation
		// requires an atomic searchable capture, so verify its actual index result.
		var id, resource, content string
		if err := tx.QueryRowContext(ctx, `SELECT id, resource_type, content FROM resources_fts WHERE rowid = ?`, ftsRowID(capture.Resource, row.ID)).Scan(&id, &resource, &content); err != nil {
			return fmt.Errorf("verify crocoblock search row: %w", err)
		}
		actualTokens := strings.Fields(content)
		expectedTokens := strings.Fields(searchableResourceContent(row.Data))
		sort.Strings(actualTokens)
		sort.Strings(expectedTokens)
		if id != row.ID || resource != capture.Resource || strings.Join(actualTokens, " ") != strings.Join(expectedTokens, " ") {
			return fmt.Errorf("verify crocoblock search row: indexed content differs")
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO croco_captures(resource, capture) VALUES (?, ?) ON CONFLICT(resource) DO UPDATE SET capture = excluded.capture`, capture.Resource, string(encoded)); err != nil {
		return fmt.Errorf("save crocoblock coverage: %w", err)
	}
	var completeAt any
	if capture.Complete {
		completeAt = capture.CapturedAt
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_state(resource_type, last_cursor, last_synced_at, total_count, last_attempt_complete, scope_key) VALUES (?, '', ?, ?, ?, ?) ON CONFLICT(resource_type) DO UPDATE SET last_cursor = '', last_synced_at = excluded.last_synced_at, total_count = excluded.total_count, last_attempt_complete = excluded.last_attempt_complete, scope_key = excluded.scope_key`, capture.Resource, completeAt, len(rows), capture.Complete, string(scope)); err != nil {
		return fmt.Errorf("save crocoblock sync coverage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit crocoblock replacement: %w", err)
	}
	return nil
}

func (s *Store) crocoHasTable(ctx context.Context, table string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`, table).Scan(&exists)
	return exists, err
}

// CrocoCapture returns sql.ErrNoRows if no observation exists; never invents coverage.
func (s *Store) CrocoCapture(ctx context.Context, resource string) (CrocoCapture, error) {
	var capture CrocoCapture
	exists, err := s.crocoHasTable(ctx, "croco_captures")
	if err != nil {
		return capture, fmt.Errorf("find crocoblock coverage: %w", err)
	}
	if !exists {
		return capture, sql.ErrNoRows
	}
	var data string
	if err := s.db.QueryRowContext(ctx, `SELECT capture FROM croco_captures WHERE resource = ?`, resource).Scan(&data); err != nil {
		return capture, fmt.Errorf("read crocoblock coverage: %w", err)
	}
	if err := json.Unmarshal([]byte(data), &capture); err != nil {
		return capture, fmt.Errorf("decode crocoblock coverage: %w", err)
	}
	return capture, nil
}

func (s *Store) CrocoCaptures(ctx context.Context) ([]CrocoCapture, error) {
	result := make([]CrocoCapture, 0)
	exists, err := s.crocoHasTable(ctx, "croco_captures")
	if err != nil || !exists {
		return result, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT capture FROM croco_captures ORDER BY resource`)
	if err != nil {
		return nil, fmt.Errorf("list crocoblock coverage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		var capture CrocoCapture
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan crocoblock coverage: %w", err)
		}
		if err := json.Unmarshal([]byte(data), &capture); err != nil {
			return nil, fmt.Errorf("decode crocoblock coverage: %w", err)
		}
		result = append(result, capture)
	}
	return result, rows.Err()
}

func (s *Store) CrocoRows(ctx context.Context, resource string) ([]json.RawMessage, error) {
	result := make([]json.RawMessage, 0)
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM resources WHERE resource_type = ? ORDER BY id`, resource)
	if err != nil {
		return nil, fmt.Errorf("list crocoblock rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan crocoblock row: %w", err)
		}
		if !json.Valid([]byte(data)) {
			return nil, fmt.Errorf("decode crocoblock row: invalid stored JSON")
		}
		result = append(result, json.RawMessage(data))
	}
	return result, rows.Err()
}

func crocoEventIdentifier(raw json.RawMessage) bool {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		var number json.Number
		if err := json.Unmarshal(raw, &number); err != nil {
			return false
		}
		n, err := number.Int64()
		return err == nil && n >= 0
	}
	if len(value) > 128 || !crocoIdentifierRE.MatchString(value) || strings.Contains(value, "://") {
		return false
	}
	// Operational identifiers are not a channel for tokens disguised as IDs.
	for _, prefix := range []string{"sk_", "sk-", "ghp_", "github_pat_", "xoxb-", "xoxp-", "bearer", "basic", "eyj"} {
		if strings.HasPrefix(strings.ToLower(value), prefix) {
			return false
		}
	}
	return true
}

func crocoEventScope(raw json.RawMessage) bool {
	var scope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &scope); err != nil || scope == nil || len(scope) > 3 {
		return false
	}
	for key, value := range scope {
		if (key != "type" && key != "resource" && key != "id") || !crocoEventIdentifier(value) {
			return false
		}
	}
	return true
}

// crocoEventDetail admits operational metadata, not API payloads or free text.
// IDs are numeric/slug identifiers; statuses and transport are constrained enums.
func crocoEventDetail(detail map[string]any) ([]byte, error) {
	if detail == nil {
		detail = map[string]any{}
	}
	data, err := json.Marshal(detail)
	if err != nil {
		return nil, crocoInvalid("event detail must be JSON serializable")
	}
	var normalized map[string]json.RawMessage
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, crocoInvalid("event detail must be an object")
	}
	for key, raw := range normalized {
		switch key {
		case "id", "item_id", "record_id", "filter_id", "form_id", "provider_id", "query_id", "snapshot_id":
			if !crocoEventIdentifier(raw) {
				return nil, crocoInvalid("event detail ID must be a numeric or slug identifier without credentials")
			}
		case "count", "row_count", "affected_count", "total", "status_code", "http_status", "since_event", "last_event_id", "start_event_id", "end_event_id", "reindex_event_id":
			var number json.Number
			if err := json.Unmarshal(raw, &number); err != nil {
				return nil, crocoInvalid("event detail identifier/count must be a nonnegative integer")
			}
			if n, err := number.Int64(); err != nil || n < 0 {
				return nil, crocoInvalid("event detail identifier/count must be a nonnegative integer")
			}
		case "success", "complete", "confirmed", "reindexed", "verified":
			if string(raw) != "true" && string(raw) != "false" {
				return nil, crocoInvalid("event detail flag must be boolean")
			}
		case "changed_fields":
			var names []string
			if err := json.Unmarshal(raw, &names); err != nil || names == nil {
				return nil, crocoInvalid("changed_fields must be an array of field names")
			}
			for _, name := range names {
				if !crocoIdentifierRE.MatchString(name) {
					return nil, crocoInvalid("changed_fields must contain names, not values")
				}
			}
		case "scope":
			if !crocoEventScope(raw) {
				return nil, crocoInvalid("event scope accepts only type/resource/id identifiers")
			}
		case "method", "transport", "status", "outcome", "source", "operation":
			if key == "status" && len(raw) > 0 && raw[0] != '"' {
				var code int
				if err := json.Unmarshal(raw, &code); err != nil || code < 100 || code > 599 {
					return nil, crocoInvalid("numeric event status must be an HTTP status code")
				}
				continue
			}
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, crocoInvalid("event detail enum must be a string")
			}
			allowed := map[string]string{
				"method":    "GET POST PUT PATCH DELETE HEAD OPTIONS",
				"transport": "rest mcp ssh wp-cli local",
				"status":    "success succeeded failed failure confirmed pending unknown partial complete incomplete publish draft private trash cancelled canceled completed on-hold processing",
				"outcome":   "success succeeded failed failure confirmed pending unknown partial complete incomplete",
				"source":    "cli rest mcp ssh wp-cli local import",
				"operation": "create get list read update patch delete trash restore reindex activate deactivate connect disconnect import sync replace capture duplicate",
			}
			if !strings.Contains(" "+allowed[key]+" ", " "+value+" ") || value == "" {
				return nil, crocoInvalid("event detail contains an unsupported operational value")
			}
		default:
			return nil, crocoInvalid("event detail accepts only operational metadata; payloads and free text are not permitted")
		}
	}
	return data, nil
}

func (s *Store) CrocoEvent(ctx context.Context, site, kind, resource string, fields []string, detail map[string]any) (int64, error) {
	site, err := crocoSite(site)
	if err != nil {
		return 0, err
	}
	if !crocoIdentifierRE.MatchString(kind) || !crocoIdentifierRE.MatchString(resource) {
		return 0, crocoInvalid("event kind/resource must be identifiers")
	}
	if fields == nil {
		fields = []string{}
	}
	for _, field := range fields {
		if !crocoIdentifierRE.MatchString(field) {
			return 0, crocoInvalid("event fields must be names, not values")
		}
	}
	encodedFields, _ := json.Marshal(fields)
	encodedDetail, err := crocoEventDetail(detail)
	if err != nil {
		return 0, err
	}
	s.lockForWrite()
	defer s.unlockAfterWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin crocoblock event: %w", err)
	}
	defer tx.Rollback()
	if err := crocoInitTx(ctx, tx, site); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO croco_events(site, kind, resource, fields, detail, created_at) VALUES (?, ?, ?, ?, ?, ?)`, site, kind, resource, string(encodedFields), string(encodedDetail), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("save crocoblock event: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read crocoblock event ID: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit crocoblock event: %w", err)
	}
	return id, nil
}

func (s *Store) CrocoEvents(ctx context.Context, since int64) ([]map[string]any, error) {
	result := make([]map[string]any, 0)
	if since < 0 {
		return nil, crocoInvalid("since event ID must be nonnegative")
	}
	exists, err := s.crocoHasTable(ctx, "croco_events")
	if err != nil || !exists {
		return result, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, site, kind, resource, fields, detail, created_at FROM croco_events WHERE id > ? ORDER BY id`, since)
	if err != nil {
		return nil, fmt.Errorf("list crocoblock events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var site, kind, resource, fieldsJSON, detailJSON, createdAt string
		if err := rows.Scan(&id, &site, &kind, &resource, &fieldsJSON, &detailJSON, &createdAt); err != nil {
			return nil, fmt.Errorf("scan crocoblock event: %w", err)
		}
		fields := make([]string, 0)
		detail := make(map[string]any)
		if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
			return nil, fmt.Errorf("decode crocoblock event fields: %w", err)
		}
		if err := json.Unmarshal([]byte(detailJSON), &detail); err != nil {
			return nil, fmt.Errorf("decode crocoblock event detail: %w", err)
		}
		result = append(result, map[string]any{"id": id, "site": site, "kind": kind, "resource": resource, "fields": fields, "detail": detail, "created_at": createdAt})
	}
	return result, rows.Err()
}
