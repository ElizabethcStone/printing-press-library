// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.
// UpsertWith: build a record from the one stored now and write it, as one step.

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// UpsertWith stores the record build returns for id, given the record stored
// now (nil when there is none). The read and the write share one write
// transaction (the read-write DSN begins every transaction IMMEDIATE), so a
// second process writing the same record waits instead of committing in
// between and then being overwritten by a record built from what it replaced.
// build returns write=false to leave the stored record as it is. UpsertWith
// reports whether a record was written.
func (s *Store) UpsertWith(resourceType, id string, build func(stored json.RawMessage) (rec json.RawMessage, write bool)) (bool, error) {
	s.lockForWrite()
	defer s.unlockAfterWrite()
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var stored json.RawMessage
	var data string
	switch err := tx.QueryRow(
		`SELECT data FROM resources WHERE resource_type = ? AND id = ?`,
		resourceType, id,
	).Scan(&data); {
	case err == nil:
		stored = json.RawMessage(data)
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}

	rec, write := build(stored)
	if !write {
		return false, nil
	}
	if err := s.upsertGenericResourceTx(tx, resourceType, id, s.mergeIncomingResourceData(tx, resourceType, id, rec)); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
