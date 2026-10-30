// Hand-authored capture inspection; runtime behavior is configured in whatsapp_ops.go.
// pp:data-source local
package cli

import "github.com/spf13/cobra"

func newNovelOpsTemplateChecklistCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "template-checklist", Short: "Extract the values a saved template needs and flag components the MCP send tool cannot express, without sending or inventing parameter values.", Example: "  whatsapp-business-pp-cli ops template-checklist --dry-run --json", Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"}}
	configureWhatsAppInspectionCmd(cmd, "template-checklist", flags)
	return cmd
}
