package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// CrocoSnapshot reads coverage and rows from one SQLite read transaction. A
// concurrent CrocoReplace can commit before or after this observation, but
// cannot mix the coverage of one generation with the rows of another.
// It never initializes tables or upgrades a read-only database.
func (s *Store) CrocoSnapshot(ctx context.Context, resource string) (CrocoCapture, []json.RawMessage, error) {
	var capture CrocoCapture
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return capture, nil, fmt.Errorf("begin crocoblock snapshot: %w", err)
	}
	defer tx.Rollback()
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'croco_captures')`).Scan(&exists); err != nil {
		return capture, nil, fmt.Errorf("find crocoblock snapshot: %w", err)
	}
	if !exists {
		return capture, nil, sql.ErrNoRows
	}
	var encoded string
	if err = tx.QueryRowContext(ctx, `SELECT capture FROM croco_captures WHERE resource = ?`, resource).Scan(&encoded); err != nil {
		return capture, nil, fmt.Errorf("read crocoblock snapshot coverage: %w", err)
	}
	if err = json.Unmarshal([]byte(encoded), &capture); err != nil {
		return CrocoCapture{}, nil, fmt.Errorf("decode crocoblock snapshot coverage: %w", err)
	}
	if capture.Resource != resource {
		return CrocoCapture{}, nil, fmt.Errorf("crocoblock snapshot coverage names a different or missing resource")
	}
	rows, err := tx.QueryContext(ctx, `SELECT data FROM resources WHERE resource_type = ? ORDER BY id`, resource)
	if err != nil {
		return CrocoCapture{}, nil, fmt.Errorf("read crocoblock snapshot rows: %w", err)
	}
	result := make([]json.RawMessage, 0)
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			rows.Close()
			return CrocoCapture{}, nil, fmt.Errorf("scan crocoblock snapshot row: %w", err)
		}
		if !json.Valid([]byte(data)) {
			rows.Close()
			return CrocoCapture{}, nil, fmt.Errorf("decode crocoblock snapshot row: invalid stored JSON")
		}
		result = append(result, json.RawMessage(data))
	}
	rowErr, closeErr := rows.Err(), rows.Close()
	if rowErr != nil {
		return CrocoCapture{}, nil, fmt.Errorf("read crocoblock snapshot rows: %w", rowErr)
	}
	if closeErr != nil {
		return CrocoCapture{}, nil, fmt.Errorf("close crocoblock snapshot rows: %w", closeErr)
	}
	if err = tx.Commit(); err != nil {
		return CrocoCapture{}, nil, fmt.Errorf("finish crocoblock snapshot: %w", err)
	}
	return capture, result, nil
}
