// Copyright 2026 bobe and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source auto
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelReservationsCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "reservations",
		Short:       "Work with reservations",
		Example:     "  crocoblock-pp-cli reservations overlaps --db candidate.db --policy policy.json --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto", "pp:typed-exit-codes": "0,2"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	addNovelCommandIfAbsent(cmd, newNovelReservationsOverlapsCmd(flags))
	return cmd
}
