package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// CrocoInvalidate retains prior rows and observation time but withdraws completeness.
func (s *Store) CrocoInvalidate(ctx context.Context, resource, note string) error {
	capture, e := s.CrocoCapture(ctx, resource)
	if e != nil {
		return e
	}
	capture.Complete = false
	capture.Note = note
	b, e := json.Marshal(capture)
	if e != nil {
		return e
	}
	s.lockForWrite()
	defer s.unlockAfterWrite()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE croco_captures SET capture=? WHERE resource=?`, string(b), resource); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE sync_state SET last_attempt_complete=0,last_synced_at=NULL WHERE resource_type=?`, resource); e != nil {
		return fmt.Errorf("invalidate capture: %w", e)
	}
	return tx.Commit()
}

// CrocoCheckSite rejects a write when existing captures or events bind this DB elsewhere.
func (s *Store) CrocoCheckSite(ctx context.Context, site string) error {
	site, e := crocoSite(site)
	if e != nil {
		return e
	}
	exists, e := s.crocoHasTable(ctx, "croco_site")
	if e != nil || !exists {
		return e
	}
	var pinned string
	if e = s.db.QueryRowContext(ctx, `SELECT site FROM croco_site WHERE singleton=1`).Scan(&pinned); e != nil {
		return e
	}
	if pinned != site {
		return ErrCrocoSiteMismatch
	}
	return nil
}
