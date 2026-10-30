// Declarative command wiring; shared behavior follows the authenticated tool schemas.
package cli

import (
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/metamcp"
	"github.com/spf13/cobra"
)

func registerDeclaredWhatsAppTools(root *cobra.Command, flags *rootFlags, o *whatsappOptions) {
	root.AddCommand(newMetaBusinessesCmd(flags, o))
	root.AddCommand(newMetaConnectionCmd(flags, o))
	root.AddCommand(newMetaPhonesCmd(flags, o))
	root.AddCommand(newMetaTemplatesCmd(flags, o))
	root.AddCommand(newMetaAppsCmd(flags, o))
	root.AddCommand(newMetaOnboardingCmd(flags, o))
	whatsappGroup(root, "auth", flags).AddCommand(newMetaAuthSystemUserLinkCmd(flags, o))
	root.AddCommand(newMetaWebhooksCmd(flags, o))
	root.AddCommand(newMetaAccountsCmd(flags, o))
	root.AddCommand(newMetaMessagesCmd(flags, o))
	root.AddCommand(newMetaPaymentsCmd(flags, o))
}
func newMetaBusinessesCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "businesses", Short: "Manage businesses through official WhatsApp Business Tools", RunE: parentNoSubcommandRunE(flags)}
	cmd.AddCommand(newMetaBusinessesListCmd(flags, o))
	cmd.AddCommand(newMetaBusinessesVerifyCmd(flags, o))
	return cmd
}
func newMetaBusinessesListCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "START HERE", Example: "  whatsapp-business-pp-cli businesses list --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_businesses")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaBusinessesVerifyCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "verify", Short: "BEFORE calling this tool, make sure the user has picked a business (call whatsapp_biz_businesses if not done yet in this conversation)", Example: "  whatsapp-business-pp-cli businesses verify --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_verify_business")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaConnectionCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "connection", Short: "Manage connection through official WhatsApp Business Tools", RunE: parentNoSubcommandRunE(flags)}
	cmd.AddCommand(newMetaConnectionPingCmd(flags, o))
	return cmd
}
func newMetaConnectionPingCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "ping", Short: "Connectivity check for the WhatsApp Business Tools MCP server", Example: "  whatsapp-business-pp-cli connection ping --dry-run --json"}
	tool, _ := metamcp.Lookup("ping")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaPhonesCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "phones", Short: "Manage phones through official WhatsApp Business Tools", RunE: parentNoSubcommandRunE(flags)}
	cmd.AddCommand(newMetaPhonesAddCmd(flags, o))
	cmd.AddCommand(newMetaPhonesSendCodeCmd(flags, o))
	cmd.AddCommand(newMetaPhonesListCmd(flags, o))
	cmd.AddCommand(newMetaPhonesRegisterCmd(flags, o))
	cmd.AddCommand(newMetaPhonesVerifyCmd(flags, o))
	return cmd
}
func newMetaPhonesAddCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "add", Short: "BEFORE calling this tool, call whatsapp_biz_businesses so the user can pick a business and you can check profile_complete \u2014 do NOT skip that step even if the user already told you what to do", Example: "  whatsapp-business-pp-cli phones add --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_add_phone_number")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaPhonesSendCodeCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "send-code", Short: "Send a one-time ownership code to a WhatsApp Business phone number (SMS by default, or set code_method=VOICE)", Example: "  whatsapp-business-pp-cli phones send-code --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_send_verification_code")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaPhonesListCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "BEFORE calling this tool, make sure the user has picked a business (call whatsapp_biz_businesses if not done yet in this conversation)", Example: "  whatsapp-business-pp-cli phones list --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_phone_numbers")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaPhonesRegisterCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "register", Short: "Register a verified WhatsApp Business phone number on the Cloud API so it can be used as a sender (sets its two-step-verification PIN)", Example: "  whatsapp-business-pp-cli phones register --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_register_phone_number")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaPhonesVerifyCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "verify", Short: "Confirm ownership of a WhatsApp Business phone number by submitting the one-time code the user received", Example: "  whatsapp-business-pp-cli phones verify --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_verify_phone_number")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaTemplatesCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "templates", Short: "Manage templates through official WhatsApp Business Tools", RunE: parentNoSubcommandRunE(flags)}
	cmd.AddCommand(newMetaTemplatesGetCmd(flags, o))
	cmd.AddCommand(newMetaTemplatesArchivalCmd(flags, o))
	cmd.AddCommand(newMetaTemplatesListCmd(flags, o))
	cmd.AddCommand(newMetaTemplatesUpdateCmd(flags, o))
	cmd.AddCommand(newMetaTemplatesCreateCmd(flags, o))
	cmd.AddCommand(newMetaTemplatesFromLibraryCmd(flags, o))
	cmd.AddCommand(newMetaTemplatesDeleteCmd(flags, o))
	cmd.AddCommand(newMetaTemplatesLibraryCmd(flags, o))
	return cmd
}
func newMetaTemplatesGetCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "get", Short: "Get the full details of a WhatsApp message template the user administers (state, category, rejection reason, components)", Example: "  whatsapp-business-pp-cli templates get --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_get_template")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaTemplatesArchivalCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "archival", Short: "Archive or unarchive WhatsApp message templates", Example: "  whatsapp-business-pp-cli templates archival --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_manage_templates_archival")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaTemplatesListCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "BEFORE calling this tool, make sure the user has picked a business (call whatsapp_biz_businesses if not done yet in this conversation)", Example: "  whatsapp-business-pp-cli templates list --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_list_templates")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaTemplatesUpdateCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "update", Short: "Update (edit) the content of an existing WhatsApp message template", Example: "  whatsapp-business-pp-cli templates update --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_update_template")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaTemplatesCreateCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "create", Short: "Create a WhatsApp message template on an account the user manages: MARKETING or UTILITY content with optional placeholders, text, location, product or media header, footer and supported buttons", Example: "  whatsapp-business-pp-cli templates create --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_create_template")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaTemplatesFromLibraryCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "from-library", Short: "BEFORE calling this tool, make sure the user has picked a business (call whatsapp_biz_businesses if not done yet in this conversation), and pick the library template with whatsapp_biz_template_library so you have its exact library_template_name and language", Example: "  whatsapp-business-pp-cli templates from-library --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_create_template_from_library")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaTemplatesDeleteCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "delete", Short: "Delete WhatsApp message templates (irreversible)", Example: "  whatsapp-business-pp-cli templates delete --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_delete_template")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaTemplatesLibraryCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "library", Short: "Browse the WhatsApp Template Library: pre-written UTILITY and AUTHENTICATION templates for common use cases (order updates, delivery notifications, appointment reminders, payment requests, one-time passcodes, and so on)", Example: "  whatsapp-business-pp-cli templates library --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_template_library")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaAppsCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "apps", Short: "Manage apps through official WhatsApp Business Tools", RunE: parentNoSubcommandRunE(flags)}
	cmd.AddCommand(newMetaAppsPublishCmd(flags, o))
	return cmd
}
func newMetaAppsPublishCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "publish", Short: "Check whether the developer app is published (live) and, if it is not, return the link to publish it", Example: "  whatsapp-business-pp-cli apps publish --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_publish_app")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaOnboardingCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "onboarding", Short: "Manage onboarding through official WhatsApp Business Tools", RunE: parentNoSubcommandRunE(flags)}
	cmd.AddCommand(newMetaOnboardingStatusCmd(flags, o))
	return cmd
}
func newMetaOnboardingStatusCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "status", Short: "Where this business is in WhatsApp onboarding, as a checklist: business profile, developer app, phone number, webhooks, account subscription, app published, payment method", Example: "  whatsapp-business-pp-cli onboarding status --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_onboarding_status")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaAuthSystemUserLinkCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "system-user-link", Short: "Generate a system-user access token", Example: "  whatsapp-business-pp-cli auth system-user-link --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_system_user_token")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaWebhooksCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "webhooks", Short: "Manage webhooks through official WhatsApp Business Tools", RunE: parentNoSubcommandRunE(flags)}
	cmd.AddCommand(newMetaWebhooksConfigureCmd(flags, o))
	cmd.AddCommand(newMetaWebhooksSubscribeCmd(flags, o))
	return cmd
}
func newMetaWebhooksConfigureCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "configure", Short: "Configure WhatsApp webhooks for a business", Example: "  whatsapp-business-pp-cli webhooks configure --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_configure_webhooks")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaWebhooksSubscribeCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "subscribe", Short: "Subscribe an app to a WhatsApp Business Account's webhooks so it receives message and status events", Example: "  whatsapp-business-pp-cli webhooks subscribe --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_subscribe_webhook")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaAccountsCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "accounts", Short: "Manage accounts through official WhatsApp Business Tools", RunE: parentNoSubcommandRunE(flags)}
	cmd.AddCommand(newMetaAccountsListCmd(flags, o))
	return cmd
}
func newMetaAccountsListCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List the active WhatsApp business accounts you manage under a business, or resolve the account a phone number belongs to: account_id, name, account_status", Example: "  whatsapp-business-pp-cli accounts list --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_accounts")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaMessagesCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "messages", Short: "Manage messages through official WhatsApp Business Tools", RunE: parentNoSubcommandRunE(flags)}
	cmd.AddCommand(newMetaMessagesSendCmd(flags, o))
	return cmd
}
func newMetaMessagesSendCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "send", Short: "BEFORE calling this tool, make sure the user has picked a business (call whatsapp_biz_businesses if not done yet in this conversation)", Example: "  whatsapp-business-pp-cli messages send --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_send_message")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
func newMetaPaymentsCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "payments", Short: "Manage payments through official WhatsApp Business Tools", RunE: parentNoSubcommandRunE(flags)}
	cmd.AddCommand(newMetaPaymentsConfigureCmd(flags, o))
	return cmd
}
func newMetaPaymentsConfigureCmd(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "configure", Short: "BEFORE calling this tool, make sure the user has picked a business (call whatsapp_biz_businesses if not done yet in this conversation)", Example: "  whatsapp-business-pp-cli payments configure --dry-run --json"}
	tool, _ := metamcp.Lookup("whatsapp_biz_configure_payments")
	configureWhatsAppTool(cmd, tool, flags, o)
	return cmd
}
