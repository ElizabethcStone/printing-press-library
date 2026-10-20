// Copyright 2026 elizabethcstone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import "github.com/spf13/cobra"

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if records, _, err := root.Find([]string{"records"}); err == nil {
			addNovelCommandIfAbsent(records, newRecordsSyncTableCmd(flags))
		}
		addNovelCommandIfAbsent(root, newNovelAnalyticsCmd(flags))
	})
}

// shortcut: the generator emits printStampedDryRunOutput unconditionally, but only
// store-less mutation commands call it, so dogfood flags it as dead here. Drop this
// reference once the press gates the helper on the same condition.
var _ = printStampedDryRunOutput

func init() {
	// The framework search command has a free-text positional that live dogfood
	// cannot synthesize; an ordinary word exercises the empty-store happy path.
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if search, _, err := root.Find([]string{"search"}); err == nil && search != root {
			if search.Annotations == nil {
				search.Annotations = map[string]string{}
			}
			search.Annotations["pp:happy-args"] = "query=status;--type=records"
		}
	})
}
