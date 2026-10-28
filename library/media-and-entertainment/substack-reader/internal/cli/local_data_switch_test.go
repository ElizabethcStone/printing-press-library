// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"strings"
	"testing"
)

// The README promises that SUBSTACK_READER_NO_LOCAL_DATA=1 writes nothing new
// to the store. The generated sync and workflow archive honour it in root.go;
// the hand-written archive must refuse the same way.
func TestArchiveRefusesUnderNoLocalData(t *testing.T) {
	t.Setenv(noLocalDataEnvVar, "1")
	err := archiveAllowed()
	if err == nil {
		t.Fatal("archiveAllowed() = nil with the no-local-data switch on, want an error")
	}
	if !strings.Contains(err.Error(), noLocalDataEnvVar) {
		t.Fatalf("error %q does not name %s", err, noLocalDataEnvVar)
	}
}

func TestArchiveAllowedByDefault(t *testing.T) {
	t.Setenv(noLocalDataEnvVar, "")
	if err := archiveAllowed(); err != nil {
		t.Fatalf("archiveAllowed() = %v with the switch off, want nil", err)
	}
}
