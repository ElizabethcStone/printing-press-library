// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source auto
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelCoverageCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "coverage",
		Short:       "Work with coverage",
		Example:     "  delhivery-maps-pp-cli coverage check --hub 28.4599,77.0679 --minutes 45 --file orders.csv --out flagged.csv",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto", "pp:typed-exit-codes": "0,2", "pp:parent-group": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	addNovelCommandIfAbsent(cmd, newNovelCoverageCheckCmd(flags))
	return cmd
}
