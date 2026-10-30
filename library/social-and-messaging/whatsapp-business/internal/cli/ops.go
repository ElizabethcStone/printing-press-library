// Copyright 2026 bobe and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source auto
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelOpsCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "ops",
		Short:       "Work with ops",
		Example:     "  whatsapp-business-pp-cli ops catalog-drift --before catalog-001 --after catalog-002 --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto", "pp:typed-exit-codes": "0,2"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	addNovelCommandIfAbsent(cmd, newNovelOpsCatalogDriftCmd(flags))
	addNovelCommandIfAbsent(cmd, newNovelOpsChangesCmd(flags))
	addNovelCommandIfAbsent(cmd, newNovelOpsScopeMapCmd(flags))
	addNovelCommandIfAbsent(cmd, newNovelOpsTemplateChecklistCmd(flags))
	addNovelCommandIfAbsent(cmd, newNovelOpsTemplateCompareCmd(flags))
	return cmd
}
