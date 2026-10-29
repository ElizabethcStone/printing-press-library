//go:build unix

// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package addrkit

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadTableRefusesFIFOsWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pipe.csv")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	// If this opened the pipe it would block forever waiting for a writer.
	if _, err := ReadTable(fifo); !errors.Is(err, ErrLimit) {
		t.Fatalf("FIFO err = %v, want ErrLimit", err)
	}
	if _, err := ReadTable("/dev/null"); !errors.Is(err, ErrLimit) {
		t.Fatalf("/dev/null err = %v, want ErrLimit", err)
	}
}
