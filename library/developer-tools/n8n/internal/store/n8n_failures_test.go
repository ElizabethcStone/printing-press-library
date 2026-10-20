// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"context"
	"testing"
)

func TestExecutionFailuresCache(t *testing.T) {
	ctx := context.Background()
	s, err := OpenWithContext(ctx, t.TempDir()+"/store.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.EnsureExecutionFailures(ctx); err != nil {
		t.Fatal(err)
	}
	for _, row := range []ExecutionFailureRow{{ExecutionID: "1", Node: "Boom", Message: "first"}, {ExecutionID: "2", Node: "Oops", Message: "second"}} {
		if err := s.PutExecutionFailure(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PutExecutionFailure(ctx, ExecutionFailureRow{ExecutionID: "1", Node: "Fixed", Message: "updated"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetExecutionFailures(ctx, []string{"1", "2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["1"].Node != "Fixed" || got["1"].Message != "updated" || got["2"].Node != "Oops" {
		t.Fatalf("unexpected cache rows: %#v", got)
	}
}
