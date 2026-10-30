// Hand-authored capture inspection; runtime behavior is configured in whatsapp_ops.go.
// pp:data-source local
package cli

import "github.com/spf13/cobra"

func newNovelOpsCatalogDriftCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "catalog-drift", Short: "Compare saved MCP catalogs and identify removed tools, new required inputs and changed schema paths that could break saved commands.", Example: "  whatsapp-business-pp-cli ops catalog-drift --dry-run --json", Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"}}
	configureWhatsAppInspectionCmd(cmd, "catalog-drift", flags)
	return cmd
}
