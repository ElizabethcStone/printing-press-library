// Hand-authored capture inspection; runtime behavior is configured in whatsapp_ops.go.
// pp:data-source local
package cli

import "github.com/spf13/cobra"

func newNovelOpsTemplateCompareCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "template-compare", Short: "Compare two explicitly chosen captured templates across accounts, with field and component differences tied to their original account and capture time.", Example: "  whatsapp-business-pp-cli ops template-compare --dry-run --json", Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"}}
	configureWhatsAppInspectionCmd(cmd, "template-compare", flags)
	return cmd
}
