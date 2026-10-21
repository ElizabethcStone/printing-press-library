package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestCrocoInvalidatePreservesObservedRows(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cap := CrocoCapture{Site: "https://site.invalid/wp-json", Resource: "forms", CapturedAt: time.Now().UTC().Format(time.RFC3339Nano), Complete: true, Fields: []string{"id"}}
	e = s.CrocoReplace(ctx, cap, []CrocoRow{{ID: "71", Data: json.RawMessage(`{"id":71}`)}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CrocoInvalidate(ctx, "forms", "latest attempt denied"); e != nil {
		t.Fatal(e)
	}
	now, e := s.CrocoCapture(ctx, "forms")
	if e != nil || now.Complete || now.CapturedAt != cap.CapturedAt {
		t.Fatal(now, e)
	}
	rows, e := s.CrocoRows(ctx, "forms")
	if e != nil || len(rows) != 1 || string(rows[0]) != `{"id":71}` {
		t.Fatal(rows, e)
	}
}
