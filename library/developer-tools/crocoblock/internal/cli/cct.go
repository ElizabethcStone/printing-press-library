// Copyright 2026 bobe and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source auto
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelCctCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "cct",
		Short:       "Work with cct",
		Example:     "  crocoblock-pp-cli cct reconcile rooms --before baseline.db --after candidate.db --key external_id --fields external_id,title --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto", "pp:typed-exit-codes": "0,2"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	addNovelCommandIfAbsent(cmd, newNovelCctReconcileCmd(flags))
	return cmd
}
