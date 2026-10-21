package cli

import "github.com/spf13/cobra"

// Register the same constructors for extension-only trees and generated roots.
func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		crocoAttach(root, "schema", newNovelSchemaDiffCmd(f))
		crocoAttach(root, "forms", newNovelFormsFieldUsageCmd(f))
		crocoAttach(root, "reservations", newNovelReservationsOverlapsCmd(f))
		crocoAttach(root, "filters", newNovelFiltersImpactCmd(f))
		crocoAttach(root, "cct", newNovelCctReconcileCmd(f))
		crocoAttach(root, "relations", newNovelRelationsAuditCmd(f))
	})
}
