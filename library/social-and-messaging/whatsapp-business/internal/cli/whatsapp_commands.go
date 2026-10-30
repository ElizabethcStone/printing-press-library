// Hand-authored schema-aware commands for the official WhatsApp Business MCP.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/captures"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/config"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/metamcp"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/store"
	"github.com/spf13/cobra"
)

type whatsappOptions struct {
	native bool
	db     string
}

func init() {
	delete(resourceWritePaths, "whatsapp-business-tools") // One-time guard; root construction may be concurrent.
	registerNovelCommand(registerWhatsApp)
	// Extend the generated capability index at process initialization. The raw
	// transport is intentionally excluded; it is only a safe preview route.
	index := make([]whichEntry, 0, len(whichIndex)+len(metamcp.Catalog()))
	for _, entry := range whichIndex {
		if entry.Command != "whatsapp-business-tools" {
			index = append(index, entry)
		}
	}
	for _, tool := range metamcp.Catalog() {
		index = append(index, whichEntry{Command: metamcp.Command(tool.Name), Description: whatsappToolShort(tool), Group: "Official Meta MCP", WhyItMatters: tool.Description})
	}
	whichIndex = index
}
func registerWhatsApp(root *cobra.Command, flags *rootFlags) {
	root.Short = strings.ReplaceAll(root.Short, "Whatsapp Business CLI", "WhatsApp Business Tools CLI")
	root.Long = strings.ReplaceAll(root.Long, "Whatsapp Business CLI", "WhatsApp Business Tools CLI")
	o := &whatsappOptions{}
	root.PersistentFlags().BoolVar(&o.native, "codex-auth", cliutil.EnvOverride("WHATSAPP_BUSINESS_USE_CODEX_AUTH") == "1", "Explicitly use this Mac's existing native Codex Meta OAuth session")
	root.PersistentFlags().StringVar(&o.db, "db", "", "SQLite capture store path; defaults to the scoped CLI data directory")
	if err := root.PersistentFlags().SetAnnotation("db", "mcp:write-flags", []string{"true"}); err != nil {
		panic(err) // The db flag is registered immediately above; a missing flag is a programming error.
	}
	// The generic JSONL adapter must never expose the raw multiplexed MCP POST.
	if imp, _, err := root.Find([]string{"import"}); err == nil && imp != root {
		imp.Hidden = true
		if imp.Annotations == nil {
			imp.Annotations = map[string]string{}
		}
		imp.Annotations["mcp:hidden"] = "true"
		imp.Short = "Raw MCP bulk import is unavailable; use schema-aware domain commands"
		imp.RunE = func(cmd *cobra.Command, args []string) error {
			return usageErr(fmt.Errorf("raw MCP import is disabled; use schema-aware domain commands with explicit confirmation"))
		}
	}
	registerDeclaredWhatsAppTools(root, flags, o)
	auth := whatsappGroup(root, "auth", flags)
	auth.AddCommand(newCodexAuth(flags))
	catalog := whatsappGroup(root, "catalog", flags)
	catalog.AddCommand(newCatalogCapture(flags, o))
	snapshots := whatsappGroup(root, "snapshots", flags)
	snapshots.AddCommand(newCaptureList(flags, o), newCaptureShow(flags, o))
	root.AddCommand(newCaptureSearch(flags, o), newCaptureSQL(flags, o), newWhatsAppSync(flags, o))
	// The generated raw transport has no MCP handshake. Keep only its safe
	// preview; domain commands are the sole execution route and enforce writes.
	if raw, _, err := root.Find([]string{"whatsapp-business-tools"}); err == nil && raw != root {
		raw.Hidden = true
		raw.Annotations["mcp:hidden"] = "true"
		raw.RunE = func(cmd *cobra.Command, args []string) error {
			return writeDryRun(cmd.OutOrStdout(), flags, "raw MCP preview only; use the schema-aware domain command to execute")
		}
	}
	if doctor, _, err := root.Find([]string{"doctor"}); err == nil && doctor != root {
		doctor.Short = "Inspect local credential availability and capture configuration"
		doctor.Long = "Inspect local authentication and capture configuration without contacting Meta. Use connection ping for a live connectivity check."
		originalDoctor := doctor.RunE
		doctor.RunE = func(cmd *cobra.Command, args []string) error {
			if registeredPlatformSource != nil {
				return originalDoctor(cmd, args)
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "inspect MCP auth and local capture configuration")
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			cfg, err := config.Load(flags.configPath)
			if err != nil {
				return configErr(err)
			}
			_, authError := metamcp.ResolveAuth(ctx, cfg, o.native)
			report := map[string]any{"status": "ok", "auth_available": authError == nil, "native_codex_requested": o.native, "endpoint": metamcp.Endpoint, "network_checked": false, "capture_store": whatsappDBPath(o), "next_check": "connection ping --codex-auth"}
			if authError != nil {
				report["status"] = "auth_required"
				report["auth_error"] = authError.Error()
			}
			if err = flags.printJSON(cmd, report); err != nil {
				return err
			}
			if authError != nil {
				return authErr(authError)
			}
			return nil
		}
	}

	// Groups are navigation, not runnable API operations. Mark them explicitly
	// so MCP and command-tree clients enumerate executable leaves.
	var markGroups func(*cobra.Command)
	markGroups = func(c *cobra.Command) {
		if len(c.Commands()) > 0 {
			if c.Annotations == nil {
				c.Annotations = map[string]string{}
			}
			c.Annotations["pp:parent-group"] = "true"
		}
		for _, child := range c.Commands() {
			markGroups(child)
		}
	}
	markGroups(root)
	walkWhatsAppSourceGuards(root, flags)
}
func whatsappGroup(parent *cobra.Command, name string, flags *rootFlags) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	c := &cobra.Command{Use: name, Short: "Manage " + name + " through WhatsApp Business Tools", RunE: parentNoSubcommandRunE(flags)}
	parent.AddCommand(c)
	return c
}
func wireFlag(key string) string {
	if key == "waba_id" {
		return "account-id"
	}
	return strings.ReplaceAll(key, "_", "-")
}

// Keep action discovery concise while preserving the full upstream prerequisite
// narrative in Long. The tool names and wire schemas remain unchanged.
func whatsappToolShort(tool metamcp.Tool) string {
	descriptions := map[string]string{
		"whatsapp_biz_businesses":                   "List authorized businesses with profile and verification status",
		"whatsapp_biz_verify_business":              "Read business verification status and its Security Center link",
		"whatsapp_biz_add_phone_number":             "Preview adding a business phone number and starting onboarding",
		"whatsapp_biz_phone_numbers":                "List business or account phone numbers with onboarding state",
		"whatsapp_biz_list_templates":               "List account templates by status, category, language or content",
		"whatsapp_biz_create_template_from_library": "Preview creating a template from a library name and language",
		"whatsapp_biz_send_message":                 "Preview a text or template message from a registered business number",
		"whatsapp_biz_configure_payments":           "Inspect account payment status and return Billing Hub links",
		"whatsapp_biz_system_user_token":            "Return Business Settings and guide links for secure token generation",
	}
	if description, ok := descriptions[tool.Name]; ok {
		return description
	}
	return strings.Split(tool.Description, ". ")[0]
}

func configureWhatsAppTool(cmd *cobra.Command, tool metamcp.Tool, flags *rootFlags, o *whatsappOptions) {
	var arguments string
	var stdin, confirm bool
	var limit int
	cmd.Short = whatsappToolShort(tool)
	cmd.Long = tool.Description + "\n\nState-changing operations print a preview until --confirm is explicitly passed. Reads capture the returned scope locally unless --no-cache is set."
	cmd.Example = "  whatsapp-business-pp-cli " + metamcp.Command(tool.Name) + " --dry-run --json"
	if tool.Annotations.ReadOnly {
		cmd.Example = "  whatsapp-business-pp-cli " + metamcp.Command(tool.Name) + " --codex-auth --agent"
	}
	cmd.Annotations = map[string]string{"pp:data-source": "live", "mcp:read-only": strconv.FormatBool(tool.Annotations.ReadOnly)}
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error { flags.noLearn = true; return nil }

	if !tool.Annotations.ReadOnly {
		cmd.Annotations["pp:preview-happy-path"] = "true"
	}
	if tool.Annotations.ReadOnly && tool.Name != "whatsapp_biz_businesses" {
		if required, ok := tool.InputSchema["required"].([]any); ok && len(required) > 0 {
			parts := make([]string, 0, len(required))
			for _, v := range required {
				if key, ok := v.(string); ok {
					parts = append(parts, "--"+wireFlag(key)+"=example-value")
				}
			}
			cmd.Annotations["pp:happy-args"] = strings.Join(parts, ";")
		}
	}

	props, _ := tool.InputSchema["properties"].(map[string]any)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "confirm" {
			continue
		}
		m, _ := props[k].(map[string]any)
		description, _ := m["description"].(string)
		if description == "" {
			description = "Value for the upstream " + k + " parameter"
		}
		name := wireFlag(k)
		switch m["type"] {
		case "boolean":
			cmd.Flags().Bool(name, false, description)
		case "integer":
			cmd.Flags().Int(name, 0, description)
		case "number":
			cmd.Flags().Float64(name, 0, description)
		default:
			cmd.Flags().String(name, "", description+func() string {
				if m["type"] == "array" || m["type"] == "object" {
					return " (JSON value)"
				}
				return ""
			}())
		}
	}
	cmd.Flags().StringVar(&arguments, "arguments", "", "JSON object of upstream tool arguments; use --stdin for sensitive values")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read one JSON arguments object from standard input instead of command-line values")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Execute this state-changing operation after reviewing its exact target and arguments")
	if _, ok := props["limit"]; !ok {
		cmd.Flags().IntVar(&limit, "limit", 25, "Maximum rows displayed; the complete returned page remains captured")
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if _, upstreamLimit := props["limit"]; !upstreamLimit && (limit < 1 || limit > 1000) {
			return usageErr(fmt.Errorf("--limit must be between 1 and 1000"))
		}
		if len(args) > 0 {
			return usageErr(fmt.Errorf("unexpected positional arguments; use named flags or --arguments"))
		}
		dry := dryRunOK(flags)
		if flags.dataSource == "local" {
			return usageErr(fmt.Errorf("this command reads live MCP state; use snapshots show or ops for captured data"))
		}
		input := map[string]any{}
		if stdin && arguments != "" {
			return usageErr(fmt.Errorf("choose --stdin or --arguments"))
		}
		if stdin && !dry {
			raw, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 2<<20+1))
			if err != nil {
				return err
			}
			if len(raw) > 2<<20 {
				return usageErr(fmt.Errorf("stdin arguments exceed 2 MiB"))
			}
			arguments = string(raw)
		}
		if arguments != "" {
			if err := json.Unmarshal([]byte(arguments), &input); err != nil || input == nil {
				return usageErr(fmt.Errorf("arguments must be a JSON object"))
			}
		}
		for _, k := range keys {
			if k == "confirm" {
				continue
			}
			name := wireFlag(k)
			if !cmd.Flags().Changed(name) {
				continue
			}
			m, _ := props[k].(map[string]any)
			switch m["type"] {
			case "boolean":
				v, _ := cmd.Flags().GetBool(name)
				input[k] = v
			case "integer":
				v, _ := cmd.Flags().GetInt(name)
				input[k] = v
			case "number":
				v, _ := cmd.Flags().GetFloat64(name)
				input[k] = v
			default:
				v, _ := cmd.Flags().GetString(name)
				if m["type"] == "array" || m["type"] == "object" {
					var value any
					if json.Unmarshal([]byte(v), &value) != nil {
						return usageErr(fmt.Errorf("--%s requires a JSON %v", name, m["type"]))
					}
					input[k] = value
				} else {
					input[k] = v
				}
			}
		}
		if tool.Name == "whatsapp_biz_businesses" {
			if action, ok := input["action"]; ok && action != "list" {
				return usageErr(fmt.Errorf("businesses list only supports action=list"))
			}
			input["action"] = "list"
		}
		// A JSON body may never grant execution. Only the separate parsed flag can.
		delete(input, "confirm")
		if _, ok := props["confirm"]; ok {
			input["confirm"] = confirm
		}
		if dry {
			return flags.printJSON(cmd, map[string]any{"dry_run": true, "action": "preview MCP tool " + tool.Name, "success": true, "executed": false, "tool": tool.Name, "arguments": safePreview(input), "stdin_not_read": stdin})
		}
		if err := metamcp.Validate(tool, input); err != nil {
			if !tool.Annotations.ReadOnly && !confirm {
				return flags.printJSON(cmd, map[string]any{"preview": true, "executed": false, "tool": tool.Name, "arguments": safePreview(input), "validation_error": err.Error()})
			}
			return usageErr(err)
		}
		if !tool.Annotations.ReadOnly && (!confirm || cliutil.IsAnyHarness()) {
			return flags.printJSON(cmd, map[string]any{"preview": true, "executed": false, "harness_refused": confirm && cliutil.IsAnyHarness(), "tool": tool.Name, "arguments": safePreview(input)})
		}
		ctx, cancel := boundCtx(cmd.Context(), flags)
		defer cancel()
		c, err := newWhatsAppClient(ctx, flags, o)
		if err != nil {
			return whatsappError(err)
		}
		if err = c.Initialize(ctx); err != nil {
			return whatsappError(err)
		}
		if err = verifyWhatsAppLiveContract(ctx, c, tool, input); err != nil {
			return whatsappError(err)
		}
		result, err := c.Call(ctx, tool, input)
		if err != nil {
			return whatsappError(err)
		}
		view := map[string]any{"data": result, "tool": tool.Name, "source": "live"}
		if tool.Annotations.ReadOnly && !flags.noCache {
			db, err := openWhatsAppDB(o)
			if err != nil {
				return err
			}
			defer db.Close()
			snapshot, err := captures.Save(ctx, db, tool.Name, input, result)
			if err != nil {
				return err
			}
			view["capture_id"] = snapshot.ID
			view["captured_at"] = snapshot.CapturedAt
			view["complete"] = snapshot.Complete
		}
		if limit > 0 {
			bounded, truncated := boundWhatsAppRows(result, limit)
			view["data"] = bounded
			if truncated {
				view["output_truncated"] = true
				view["note"] = "Display cap reached; the full returned page is available in the capture."
			}
		}
		return flags.printJSON(cmd, view)
	}
	return
}
func safePreview(input map[string]any) map[string]any { return metamcp.PreviewArguments(input) }
func boundWhatsAppRows(v any, limit int) (any, bool) {
	if a, ok := v.([]any); ok && len(a) > limit {
		return a[:limit], true
	}
	if m, ok := v.(map[string]any); ok {
		copy := map[string]any{}
		cut := false
		for k, x := range m {
			if a, ok := x.([]any); ok && len(a) > limit {
				copy[k] = a[:limit]
				cut = true
			} else {
				copy[k] = x
			}
		}
		return copy, cut
	}
	return v, false
}
func newWhatsAppClient(ctx context.Context, flags *rootFlags, o *whatsappOptions) (*metamcp.Client, error) {
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		return nil, err
	}
	return metamcp.New(ctx, cfg, o.native, flags.timeout, flags.rateLimit)
}
func whatsappError(err error) error {
	var auth *metamcp.AuthError
	if errors.As(err, &auth) {
		return authErr(err)
	}
	var rate *cliutil.RateLimitError
	if errors.As(err, &rate) {
		return rateLimitErr(err)
	}
	return apiErr(err)
}
func whatsappDBPath(o *whatsappOptions) string {
	if o.db != "" {
		return o.db
	}
	return defaultDBPath("whatsapp-business-pp-cli")
}
func openWhatsAppDB(o *whatsappOptions) (*store.Store, error) { return store.Open(whatsappDBPath(o)) }
func newCodexAuth(flags *rootFlags) *cobra.Command {
	var launch bool
	cmd := &cobra.Command{Use: "codex", Short: "Prepare or launch the official native Codex OAuth sign-in", Example: "  whatsapp-business-pp-cli auth codex --launch", Annotations: map[string]string{"mcp:read-only": "false", "pp:preview-happy-path": "true"}, RunE: func(cmd *cobra.Command, args []string) error {
		if dryRunOK(flags) || !launch || cliutil.IsAnyHarness() {
			return writeDryRun(cmd.OutOrStdout(), flags, "launch native Codex Meta OAuth; then use --codex-auth")
		}
		ctx, cancel := boundCtx(cmd.Context(), flags)
		defer cancel()
		// #nosec G204 -- Native Codex executable with constant Meta server configuration and fixed login argv; no shell.
		child := exec.CommandContext(ctx, "codex", "-c", `mcp_servers.whatsapp_business_tools.url="`+metamcp.Endpoint+`"`, "mcp", "login", "whatsapp_business_tools")
		child.Stdin = cmd.InOrStdin()
		child.Stdout = cmd.OutOrStdout()
		child.Stderr = cmd.ErrOrStderr()
		return child.Run()
	}}
	cmd.Flags().BoolVar(&launch, "launch", false, "Open the native Codex Meta OAuth flow in your browser; never runs during verification")
	return cmd
}
func newCatalogCapture(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "capture", Short: "Capture the current authenticated MCP tool catalog", Example: "  whatsapp-business-pp-cli catalog capture --codex-auth --agent", Annotations: map[string]string{"pp:data-source": "live", "mcp:read-only": "true"}, RunE: func(cmd *cobra.Command, args []string) error {
		if dryRunOK(flags) {
			return writeDryRun(cmd.OutOrStdout(), flags, "capture authenticated MCP tool catalog")
		}
		ctx, cancel := boundCtx(cmd.Context(), flags)
		defer cancel()
		c, err := newWhatsAppClient(ctx, flags, o)
		if err != nil {
			return whatsappError(err)
		}
		if err = c.Initialize(ctx); err != nil {
			return whatsappError(err)
		}
		tools, err := c.Tools(ctx)
		if err != nil {
			return whatsappError(err)
		}
		db, err := openWhatsAppDB(o)
		if err != nil {
			return err
		}
		defer db.Close()
		s, err := captures.Save(ctx, db, "catalog", map[string]any{"endpoint": c.EndpointURL(), "protocol": c.ProtocolVersion()}, map[string]any{"tools": tools})
		if err != nil {
			return err
		}
		return flags.printJSON(cmd, s)
	}}
	return cmd
}
func newCaptureList(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	var tool string
	var limit int
	cmd := &cobra.Command{Use: "list", Short: "List immutable local captures with scope and coverage", Example: "  whatsapp-business-pp-cli snapshots list --limit 20 --agent", Annotations: map[string]string{"pp:data-source": "local", "mcp:read-only": "true"}, RunE: func(cmd *cobra.Command, args []string) error {
		if dryRunOK(flags) {
			return writeDryRun(cmd.OutOrStdout(), flags, "list local captures")
		}
		db, err := openWhatsAppDB(o)
		if err != nil {
			return err
		}
		defer db.Close()
		rows, err := captures.List(cmd.Context(), db, tool, limit)
		if err != nil {
			return err
		}
		return flags.printJSON(cmd, rows)
	}}
	cmd.Flags().StringVar(&tool, "tool", "", "Restrict captures to this exact upstream tool name")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum capture records returned, from 1 to 1000")
	return cmd
}
func newCaptureShow(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	var id string
	cmd := &cobra.Command{Use: "show", Short: "Read one explicit immutable capture by its ID", Example: "  whatsapp-business-pp-cli snapshots show --snapshot capture-001 --agent", Annotations: map[string]string{"pp:data-source": "local", "mcp:read-only": "true", "pp:happy-args": "--snapshot=example-value"}, RunE: func(cmd *cobra.Command, args []string) error {
		if dryRunOK(flags) {
			return writeDryRun(cmd.OutOrStdout(), flags, "read local capture")
		}
		if id == "" {
			return usageErr(fmt.Errorf("--snapshot is required; use snapshots list"))
		}
		db, err := openWhatsAppDB(o)
		if err != nil {
			return err
		}
		defer db.Close()
		s, err := captures.Get(cmd.Context(), db, id)
		if errors.Is(err, captures.ErrNotFound) {
			return notFoundErr(err)
		}
		if err != nil {
			return err
		}
		return flags.printJSON(cmd, s)
	}}
	cmd.Flags().StringVar(&id, "snapshot", "", "Exact capture ID returned by a prior read or snapshots list")
	return cmd
}
func newCaptureSearch(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	var tool string
	var limit, maxScan int
	cmd := &cobra.Command{Use: "search [query]", Short: "Search literal text in captured MCP evidence offline", Example: "  whatsapp-business-pp-cli search \"approved\" --type whatsapp_biz_list_templates --agent", Annotations: map[string]string{"pp:data-source": "local", "mcp:read-only": "true"}, RunE: func(cmd *cobra.Command, args []string) error {
		if dryRunOK(flags) {
			return writeDryRun(cmd.OutOrStdout(), flags, "search captured MCP evidence")
		}
		if flags.dataSource == "live" {
			return usageErr(fmt.Errorf("search has no live equivalent; capture the selected read first"))
		}
		if len(args) != 1 {
			return usageErr(fmt.Errorf("provide one quoted search string"))
		}
		if limit < 1 || limit > 1000 || maxScan < limit || maxScan > 1000 {
			return usageErr(fmt.Errorf("require 1 <= --limit <= --max-scan-records <= 1000"))
		}
		db, err := openWhatsAppDB(o)
		if err != nil {
			return err
		}
		defer db.Close()
		rows, err := captures.Search(cmd.Context(), db, args[0], maxScan)
		if err != nil {
			return err
		}
		filtered := make([]captures.Snapshot, 0)
		for _, s := range rows {
			if tool == "" || s.Tool == tool {
				if len(filtered) < limit {
					filtered = append(filtered, s)
				}
			}
		}
		view := map[string]any{"captures": filtered, "scanned_captures": len(rows), "scan_cap": maxScan, "scan_cap_reached": len(rows) == maxScan}
		if len(filtered) == 0 {
			view["note"] = "No matching captures in the bounded local search; raise --max-scan-records or refresh the selected scope."
		}
		return flags.printJSON(cmd, view)
	}}
	cmd.Flags().StringVar(&tool, "type", "", "Restrict returned captures to this exact upstream tool name")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum matching capture records returned, from 1 to 1000")
	cmd.Flags().IntVar(&maxScan, "max-scan-records", 250, "Maximum text-matching captures examined before applying the tool filter, at most 1000")
	return cmd
}
func newCaptureSQL(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "sql [query]", Short: "Run one read-only SQL query over local captures", Example: "  whatsapp-business-pp-cli sql 'SELECT tool, COUNT(*) AS captures FROM mcp_captures GROUP BY tool' --json", Annotations: map[string]string{"pp:data-source": "local", "mcp:read-only": "true"}, RunE: func(cmd *cobra.Command, args []string) error {
		if dryRunOK(flags) {
			return writeDryRun(cmd.OutOrStdout(), flags, "query captured SQLite data read-only")
		}
		if len(args) != 1 {
			return usageErr(fmt.Errorf("provide one quoted SELECT query"))
		}
		query := strings.TrimSpace(args[0])
		if !strings.HasPrefix(strings.ToUpper(query), "SELECT ") && !strings.HasPrefix(strings.ToUpper(query), "WITH ") {
			return usageErr(fmt.Errorf("only SELECT or read-only WITH queries are allowed"))
		}
		if strings.Contains(query, ";") {
			return usageErr(fmt.Errorf("one SQL statement only; omit semicolons"))
		}
		db, err := store.OpenReadOnly(whatsappDBPath(o))
		if err != nil {
			return err
		}
		defer db.Close()
		rows, err := db.DB().QueryContext(cmd.Context(), query)
		if err != nil {
			return err
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return err
		}
		out := make([]map[string]any, 0)
		for rows.Next() {
			if len(out) >= 1000 {
				return usageErr(fmt.Errorf("SQL output exceeds 1000 rows; add LIMIT"))
			}
			values := make([]any, len(cols))
			ptr := make([]any, len(cols))
			for i := range values {
				ptr[i] = &values[i]
			}
			if err = rows.Scan(ptr...); err != nil {
				return err
			}
			row := map[string]any{}
			for i, v := range values {
				if b, ok := v.([]byte); ok {
					v = string(b)
				}
				row[cols[i]] = v
			}
			out = append(out, row)
		}
		if err = rows.Err(); err != nil {
			return err
		}
		return flags.printJSON(cmd, out)
	}}
	return cmd
}
func newWhatsAppSync(flags *rootFlags, o *whatsappOptions) *cobra.Command {
	var tool, param, arguments string
	cmd := &cobra.Command{Use: "sync", Short: "Capture one explicit read-only MCP operation; never sweep unselected businesses", Example: "  whatsapp-business-pp-cli sync --resources ping --codex-auth --agent", Annotations: map[string]string{"pp:data-source": "live", "mcp:read-only": "true"}, RunE: func(cmd *cobra.Command, args []string) error {
		if dryRunOK(flags) {
			return writeDryRun(cmd.OutOrStdout(), flags, "capture one selected read-only MCP operation")
		}
		t, ok := metamcp.Lookup(tool)
		if !ok || !t.Annotations.ReadOnly {
			return usageErr(fmt.Errorf("--resources must name exactly one read-only MCP tool"))
		}
		input := map[string]any{}
		if arguments != "" && param != "" {
			return usageErr(fmt.Errorf("choose --arguments or --param"))
		}
		if arguments != "" {
			decoder := json.NewDecoder(strings.NewReader(arguments))
			decoder.UseNumber()
			if err := decoder.Decode(&input); err != nil || input == nil {
				return usageErr(fmt.Errorf("--arguments requires one JSON object"))
			}
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				return usageErr(fmt.Errorf("--arguments requires exactly one JSON object"))
			}
		}
		for _, part := range strings.Split(param, ",") {
			if part == "" {
				continue
			}
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				return usageErr(fmt.Errorf("--param requires key=value entries"))
			}
			input[kv[0]] = kv[1]
		}
		if t.Name == "whatsapp_biz_businesses" {
			input["action"] = "list"
		}
		if err := metamcp.Validate(t, input); err != nil {
			return usageErr(err)
		}
		ctx, cancel := boundCtx(cmd.Context(), flags)
		defer cancel()
		c, err := newWhatsAppClient(ctx, flags, o)
		if err != nil {
			return whatsappError(err)
		}
		if err = c.Initialize(ctx); err != nil {
			return whatsappError(err)
		}
		if err = verifyWhatsAppLiveContract(ctx, c, t, input); err != nil {
			return whatsappError(err)
		}
		data, err := c.Call(ctx, t, input)
		if err != nil {
			return whatsappError(err)
		}
		db, err := openWhatsAppDB(o)
		if err != nil {
			return err
		}
		defer db.Close()
		s, err := captures.Save(ctx, db, t.Name, input, data)
		if err != nil {
			return err
		}
		return flags.printJSON(cmd, s)
	}}
	cmd.Flags().StringVar(&tool, "resources", "", "Exactly one read-only upstream MCP tool to capture")
	cmd.Flags().StringVar(&param, "param", "", "Scalar upstream selection as comma-separated key=value entries; use --arguments for arrays or objects")
	cmd.Flags().StringVar(&arguments, "arguments", "", "One JSON object of upstream read arguments, including typed array filters")
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error { flags.noLearn = true; return nil }
	return cmd
}

func walkWhatsAppSourceGuards(cmd *cobra.Command, flags *rootFlags) {
	source := cmd.Annotations["pp:data-source"]
	if source == "local" || source == "live" {
		if source == "local" && cmd.RunE != nil {
			run := cmd.RunE
			cmd.RunE = func(c *cobra.Command, args []string) error {
				ctx, cancel := boundCtx(c.Context(), flags)
				defer cancel()
				previousContext := c.Context()
				c.SetContext(ctx)
				defer c.SetContext(previousContext)
				return run(c, args)
			}
		}
		previous := cmd.PreRunE
		cmd.PreRunE = func(c *cobra.Command, args []string) error {
			if previous != nil {
				if err := previous(c, args); err != nil {
					return err
				}
			}
			if flags.dataSource != "" && flags.dataSource != "auto" && flags.dataSource != source {
				return usageErr(fmt.Errorf("%s requires --data-source %s", c.CommandPath(), source))
			}
			return nil
		}
	}
	for _, child := range cmd.Commands() {
		walkWhatsAppSourceGuards(child, flags)
	}
}

// The beta server may change a tool from a status/link read to a mutation.
// Verify the live safety hint and schema before every executable domain call.
func verifyWhatsAppLiveContract(ctx context.Context, c *metamcp.Client, tool metamcp.Tool, input map[string]any) error {
	tools, err := c.Tools(ctx)
	if err != nil {
		return err
	}
	for _, live := range tools {
		if live.Name == tool.Name {
			if live.Annotations.ReadOnly != tool.Annotations.ReadOnly {
				return fmt.Errorf("MCP safety classification changed for %s; capture the new catalog and update this CLI before execution", tool.Name)
			}
			return metamcp.Validate(live, input)
		}
	}
	return fmt.Errorf("tool %s is absent from the current MCP catalog; capture and review catalog drift", tool.Name)
}
