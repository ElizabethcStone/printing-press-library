// Hand-authored capture inspection; runtime behavior is configured in whatsapp_ops.go.
// pp:data-source local
package cli

import "github.com/spf13/cobra"

func newNovelOpsScopeMapCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "scope-map", Short: "See the captured business, account and phone relationships for an explicitly selected scope, including gaps and shared-account roles.", Example: "  whatsapp-business-pp-cli ops scope-map --dry-run --json", Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"}}
	configureWhatsAppInspectionCmd(cmd, "scope-map", flags)
	return cmd
}
