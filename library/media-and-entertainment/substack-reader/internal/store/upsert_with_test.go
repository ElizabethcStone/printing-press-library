// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

// TestUpsertWithHoldsTheWriteWhileItBuilds: two handles on one file stand for
// two CLI processes. While the first decides what to write from the record it
// read, the second cannot commit; its write waits and lands after. Without the
// shared transaction, the second write would commit in between and the first
// would overwrite it with a record built from stale data (Greptile P1 on
// printing-press-library PR #2393: "concurrent reads lose full articles").
func TestUpsertWithHoldsTheWriteWhileItBuilds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	a, err := Open(path)
	if err != nil {
		t.Fatalf("Open a: %v", err)
	}
	defer a.Close()
	b, err := Open(path)
	if err != nil {
		t.Fatalf("Open b: %v", err)
	}
	defer b.Close()

	if err := a.Upsert("posts", "5", json.RawMessage(`{"v":"old"}`)); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	wrote, err := a.UpsertWith("posts", "5", func(stored json.RawMessage) (json.RawMessage, bool) {
		if string(stored) != `{"v":"old"}` {
			t.Errorf("stored = %s, want the old record", stored)
		}
		go func() { done <- b.Upsert("posts", "5", json.RawMessage(`{"v":"other"}`)) }()
		select {
		case err := <-done:
			t.Errorf("the other handle committed while UpsertWith was building (err %v)", err)
		case <-time.After(300 * time.Millisecond):
		}
		return json.RawMessage(`{"v":"built"}`), true
	})
	if err != nil || !wrote {
		t.Fatalf("UpsertWith = %v, %v", wrote, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the waiting write failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting write never landed")
	}
	got, err := a.Get("posts", "5")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"v":"other"}` {
		t.Errorf("final record = %s, want the write that waited", got)
	}
}

// TestUpsertWithCanDecline: write=false leaves the stored record untouched,
// and a missing record reaches build as nil.
func TestUpsertWithCanDecline(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	wrote, err := s.UpsertWith("posts", "9", func(stored json.RawMessage) (json.RawMessage, bool) {
		if stored != nil {
			t.Errorf("stored = %s, want nil for a missing record", stored)
		}
		return json.RawMessage(`{"v":1}`), true
	})
	if err != nil || !wrote {
		t.Fatalf("first UpsertWith = %v, %v", wrote, err)
	}
	wrote, err = s.UpsertWith("posts", "9", func(json.RawMessage) (json.RawMessage, bool) {
		return json.RawMessage(`{"v":2}`), false
	})
	if err != nil || wrote {
		t.Fatalf("declined UpsertWith = %v, %v", wrote, err)
	}
	if got, _ := s.Get("posts", "9"); string(got) != `{"v":1}` {
		t.Errorf("record = %s, want it unchanged", got)
	}
}
