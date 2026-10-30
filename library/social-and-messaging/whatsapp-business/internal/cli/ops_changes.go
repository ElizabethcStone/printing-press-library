// Hand-authored capture inspection; runtime behavior is configured in whatsapp_ops.go.
// pp:data-source local
package cli

import "github.com/spf13/cobra"

func newNovelOpsChangesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "changes", Short: "Compare two captures of the same selected scope and see changed fields, additions and evidence gaps without mistaking incomplete pages for deletions.", Example: "  whatsapp-business-pp-cli ops changes --dry-run --json", Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"}}
	configureWhatsAppInspectionCmd(cmd, "changes", flags)
	return cmd
}
