// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"context"
	"fmt"
	"strings"
)

const execFailuresDDL = `CREATE TABLE IF NOT EXISTS n8n_execution_failures (execution_id TEXT PRIMARY KEY, node TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT '', fetched_at DATETIME NOT NULL)`

type ExecutionFailureRow struct {
	ExecutionID string
	Node        string
	Message     string
}

func (s *Store) EnsureExecutionFailures(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, execFailuresDDL)
	return err
}

func (s *Store) GetExecutionFailures(ctx context.Context, ids []string) (map[string]ExecutionFailureRow, error) {
	out := make(map[string]ExecutionFailureRow, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	const chunk = 500 // stay under SQLite's bound-parameter limit
	for start := 0; start < len(ids); start += chunk {
		part := ids[start:min(start+chunk, len(ids))]
		marks := strings.TrimRight(strings.Repeat("?,", len(part)), ",")
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = id
		}
		if err := s.scanExecutionFailures(ctx, out, `SELECT execution_id, node, message FROM n8n_execution_failures WHERE execution_id IN (`+marks+`)`, args); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) scanExecutionFailures(ctx context.Context, out map[string]ExecutionFailureRow, query string, args []any) error {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row ExecutionFailureRow
		if err := rows.Scan(&row.ExecutionID, &row.Node, &row.Message); err != nil {
			return err
		}
		out[row.ExecutionID] = row
	}
	return rows.Err()
}

func (s *Store) PutExecutionFailure(ctx context.Context, row ExecutionFailureRow) error {
	if row.ExecutionID == "" {
		return fmt.Errorf("execution id is required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO n8n_execution_failures (execution_id,node,message,fetched_at) VALUES (?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(execution_id) DO UPDATE SET node=excluded.node,message=excluded.message,fetched_at=excluded.fetched_at`, row.ExecutionID, row.Node, row.Message)
	return err
}
