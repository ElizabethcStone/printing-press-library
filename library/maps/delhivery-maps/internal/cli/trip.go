// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source auto
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelTripCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "trip",
		Short:       "Work with trip",
		Example:     "  delhivery-maps-pp-cli trip quote --from 'Sector 45 Gurgaon' --to Jaipur --vehicle bus_truck --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto", "pp:typed-exit-codes": "0,2", "pp:parent-group": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	addNovelCommandIfAbsent(cmd, newNovelTripQuoteCmd(flags))
	return cmd
}
