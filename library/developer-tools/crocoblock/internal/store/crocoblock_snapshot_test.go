package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCrocoSnapshotReadOnlyCoverageAndRows(t *testing.T) {
	for _, tc := range []struct {
		name              string
		present, complete bool
		rows              []CrocoRow
	}{{"missing", false, false, nil}, {"complete empty", true, true, nil}, {"incomplete retains rows", true, false, []CrocoRow{crocoTestRow("2", `{"_ID":"2","label":"observed"}`)}}, {"ordered full rows", true, true, []CrocoRow{crocoTestRow("2", `{"_ID":"2"}`), crocoTestRow("1", `{"_ID":"1"}`)}}} {
		t.Run(tc.name, func(t *testing.T) {
			writable, path := crocoTestStore(t)
			ctx := context.Background()
			if tc.present {
				cap := crocoTestCapture("cct:rooms")
				cap.Complete = tc.complete
				if e := writable.CrocoReplace(ctx, cap, tc.rows); e != nil {
					t.Fatal(e)
				}
			}
			writable.Close()
			before, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			ro, e := OpenReadOnly(path)
			if e != nil {
				t.Fatal(e)
			}
			defer ro.Close()
			ro.DB().SetMaxOpenConns(1)
			cap, rows, e := ro.CrocoSnapshot(ctx, "cct:rooms")
			if !tc.present {
				if !errors.Is(e, sql.ErrNoRows) || cap.Complete || len(rows) > 0 {
					t.Fatal(cap, rows, e)
				}
			} else {
				if e != nil || cap.Complete != tc.complete || rows == nil || len(rows) != len(tc.rows) {
					t.Fatal(cap, rows, e)
				}
				if len(rows) == 2 && !strings.Contains(string(rows[0]), `"1"`) {
					t.Fatal("row order changed", rows)
				}
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("read-only snapshot mutated database")
			}
			t.Log("PASS atomic snapshot preserves exact coverage, row count/order and actual empty state without writes")
		})
	}
}
func TestCrocoSnapshotConcurrentReplacementCannotTear(t *testing.T) {
	writer, path := crocoTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	replace := func(generation int) error {
		cap := crocoTestCapture("cct:rooms")
		cap.Note = fmt.Sprint(generation)
		cap.Complete = generation%2 == 0
		rows := []CrocoRow{}
		for i := 1; i <= 3; i++ {
			raw, _ := json.Marshal(map[string]string{"_ID": fmt.Sprint(i), "generation": cap.Note})
			rows = append(rows, CrocoRow{ID: fmt.Sprint(i), Data: raw})
		}
		return writer.CrocoReplace(ctx, cap, rows)
	}
	if e := replace(0); e != nil {
		t.Fatal(e)
	}
	reader, e := OpenReadOnly(path)
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Close()
	reader.DB().SetMaxOpenConns(1)
	done := make(chan error, 1)
	go func() {
		for i := 1; i <= 60; i++ {
			if e := replace(i); e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	check := func() {
		cap, rows, e := reader.CrocoSnapshot(ctx, "cct:rooms")
		if e != nil {
			t.Fatal(e)
		}
		if len(rows) != 3 {
			t.Fatal("torn collection", cap, len(rows))
		}
		for _, raw := range rows {
			var row map[string]string
			if e = json.Unmarshal(raw, &row); e != nil {
				t.Fatal(e)
			}
			if row["generation"] != cap.Note {
				t.Fatalf("torn snapshot: coverage generation %s, row generation %s", cap.Note, row["generation"])
			}
		}
		var g int
		fmt.Sscan(cap.Note, &g)
		if cap.Complete != (g%2 == 0) {
			t.Fatal("coverage differs from generation", cap)
		}
	}
	for i := 0; i < 200; i++ {
		check()
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	check()
	t.Log("PASS 201 read snapshots remain coherent across 60 concurrent committed replacements")
}
func TestCrocoSnapshotRejectsCorruptionAndCancellation(t *testing.T) {
	s, _ := crocoTestStore(t)
	ctx := context.Background()
	cap := crocoTestCapture("cct:rooms")
	if e := s.CrocoReplace(ctx, cap, []CrocoRow{crocoTestRow("1", `{"_ID":"1"}`)}); e != nil {
		t.Fatal(e)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, e := s.CrocoSnapshot(cancelled, "cct:rooms"); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := s.DB().Exec(`UPDATE resources SET data = '{' WHERE resource_type = 'cct:rooms'`); e != nil {
		t.Fatal(e)
	}
	if cap, rows, e := s.CrocoSnapshot(ctx, "cct:rooms"); e == nil || len(rows) > 0 || cap.Complete {
		t.Fatal("invalid JSON presented as usable snapshot", cap, rows, e)
	}
	if _, e := s.DB().Exec(`UPDATE croco_captures SET capture = 'null' WHERE resource = 'cct:rooms'`); e != nil {
		t.Fatal(e)
	}
	if cap, rows, e := s.CrocoSnapshot(ctx, "cct:rooms"); e == nil || len(rows) > 0 || cap.Complete {
		t.Fatal("null metadata presented as usable coverage", cap, rows, e)
	}
	t.Log("PASS cancellation and corrupt coverage/rows fail without phantom complete observations")
}
