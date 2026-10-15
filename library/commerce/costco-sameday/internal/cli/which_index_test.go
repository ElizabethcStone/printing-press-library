// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"strings"
	"testing"
)

func TestCostcoWhichIndexCommandsExist(t *testing.T) {
	if len(whichIndex) == 0 {
		t.Fatal("which index is empty")
	}
	root := RootCmd()
	for _, e := range whichIndex {
		cmd, rest, err := root.Find(strings.Fields(e.Command))
		if err != nil || len(rest) != 0 || cmd == root {
			t.Errorf("which entry %q does not resolve to a command (%v, rest %v)", e.Command, err, rest)
		}
	}
}

func TestWhichFindsCartShow(t *testing.T) {
	out, _, err := runRootSplit(t, "which", "cart price", "--json")
	if err != nil || !strings.Contains(out, `"cart show"`) {
		t.Fatalf("which cart price: %v\n%s", err, out)
	}
}
