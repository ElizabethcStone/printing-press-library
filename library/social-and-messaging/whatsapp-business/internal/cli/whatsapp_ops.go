// Shared wiring for local, explicitly selected capture inspections.
// pp:data-source local
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/captures"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/inspection"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/metamcp"
	"github.com/spf13/cobra"
)

var whatsappOpsDescriptions = map[string][2]string{
	"scope-map":          {"Map captured business, account and phone relationships in one chosen scope", "Use this command to inspect captured relationships. Use ops changes for historical changes."},
	"changes":            {"Compare two captures of the exact same tool and selection scope", "Use this command for before/after asset changes. Use ops template-compare for templates in different accounts."},
	"template-compare":   {"Compare two explicitly selected saved template documents", "Use this command for two templates, including different accounts. Use ops changes for same-scope history and ops template-checklist for required values."},
	"template-checklist": {"Extract template values and unsupported MCP send component shapes", "Use this command for one saved template's required values. Use ops template-compare for differences. This does not establish delivery eligibility or approval."},
	"catalog-drift":      {"Compare captured MCP tools and potentially breaking schema changes", "Use this command for upstream tool contracts. Use ops changes for account, phone or template data."},
}

func configureWhatsAppInspectionCmd(cmd *cobra.Command, kind string, flags *rootFlags) {
	var before, after, left, right, snapshot, template, businessID string
	var maxAge string
	desc := whatsappOpsDescriptions[kind]
	cmd.Use = kind
	cmd.Short = desc[0]
	cmd.Long = desc[1] + "\n\nReads only captured evidence. No API calls. Supply explicit capture IDs; snapshots list shows available captures. A bare invocation reports the inputs needed and does not infer a selected business."
	cmd.Example = "  whatsapp-business-pp-cli ops " + kind + " --dry-run --json"
	cmd.Annotations = map[string]string{"pp:data-source": "local", "mcp:read-only": "true"}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if flags.dataSource == "live" {
			return usageErr(fmt.Errorf("ops %s has no live equivalent; use --data-source local or capture its source first", kind))
		}
		if dryRunOK(flags) {
			return writeDryRun(cmd.OutOrStdout(), flags, "inspect local "+kind+" evidence")
		}
		if len(args) > 0 {
			return usageErr(fmt.Errorf("use named capture selection flags; no positional arguments accepted"))
		}
		age, err := cliutil.ParseDurationLoose(maxAge)
		if err != nil || age < 0 {
			return usageErr(fmt.Errorf("--max-age requires a nonnegative duration such as 24h or 7d"))
		}
		selected := before != "" || after != "" || left != "" || right != "" || snapshot != "" || template != "" || businessID != ""
		if !selected {
			return flags.printJSON(cmd, map[string]any{"results": []any{}, "note": "No capture selected. Use snapshots list and provide this command's explicit capture flags.", "required_inputs": opsRequired(kind)})
		}
		o := &whatsappOptions{}
		o.db, _ = cmd.Flags().GetString("db")
		db, err := openWhatsAppDB(o)
		if err != nil {
			return err
		}
		defer db.Close()
		get := func(id, label string) (captures.Snapshot, error) {
			if id == "" {
				return captures.Snapshot{}, usageErr(fmt.Errorf("--%s is required", label))
			}
			s, err := captures.Get(cmd.Context(), db, id)
			if errors.Is(err, captures.ErrNotFound) {
				return s, notFoundErr(err)
			}
			if err == nil && age > 0 {
				if t, e := time.Parse(time.RFC3339Nano, s.CapturedAt); e == nil && time.Since(t) > age {
					fmt.Fprintf(cmd.ErrOrStderr(), "Capture %s is older than --max-age; refresh its exact selection before treating it as current.\n", s.ID)
				}
			}
			return s, err
		}
		var view map[string]any
		switch kind {
		case "scope-map":
			chosen, e := get(snapshot, "snapshot")
			if e != nil {
				return e
			}
			if businessID == "" {
				businessID, _ = chosen.Scope["business_id"].(string)
			}
			if businessID == "" {
				return usageErr(fmt.Errorf("--business-id is required when the selected capture does not identify one business"))
			}
			all, e := captures.List(cmd.Context(), db, "", 1000)
			if e != nil {
				return e
			}
			matching := make([]captures.Snapshot, 0)
			matching = append(matching, chosen)
			// Only join exact selected scope records, and use newest capture per tool
			// and full filter scope. The selected capture always wins its own scope.
			key := func(s captures.Snapshot) string { b, _ := json.Marshal(s.Scope); return s.Tool + string(b) }
			seen := map[string]bool{key(chosen): true}
			for _, s := range all {
				scope, _ := s.Scope["business_id"].(string)
				if scope != businessID || seen[key(s)] {
					continue
				}
				matching = append(matching, s)
				seen[key(s)] = true
			}
			view, err = inspection.ScopeMap(matching, businessID)
		case "changes", "catalog-drift":
			a, e := get(before, "before")
			if e != nil {
				return e
			}
			b, e := get(after, "after")
			if e != nil {
				return e
			}
			if kind == "changes" {
				view, err = inspection.Changes(a, b)
			} else {
				view, err = inspection.CatalogDrift(a, b)
			}
		case "template-compare":
			a, e := get(left, "left")
			if e != nil {
				return e
			}
			b, e := get(right, "right")
			if e != nil {
				return e
			}
			view, err = inspection.TemplateCompare(a, b)
		case "template-checklist":
			a, e := get(template, "template")
			if e != nil {
				return e
			}
			tool, _ := metamcp.Lookup("whatsapp_biz_send_message")
			view, err = inspection.TemplateChecklist(a, tool)
		}
		if err != nil {
			return usageErr(err)
		}
		return printWhatsAppInspection(cmd, flags, view)
	}
	switch kind {
	case "scope-map":
		cmd.Flags().StringVar(&snapshot, "snapshot", "", "Explicit capture ID anchoring the selected inventory scope")
		cmd.Flags().StringVar(&businessID, "business-id", "", "Chosen business ID; required when absent from the capture scope")
		cmd.Example = "  whatsapp-business-pp-cli ops scope-map --snapshot capture-001 --agent --select relationships,gaps"
	case "changes", "catalog-drift":
		cmd.Flags().StringVar(&before, "before", "", "Exact earlier capture ID with matching source and scope")
		cmd.Flags().StringVar(&after, "after", "", "Exact later capture ID with matching source and scope")
		cmd.Example = "  whatsapp-business-pp-cli ops " + kind + " --before capture-001 --after capture-002 --agent"
	case "template-compare":
		cmd.Flags().StringVar(&left, "left", "", "Capture ID of the first get-template response")
		cmd.Flags().StringVar(&right, "right", "", "Capture ID of the second get-template response")
		cmd.Example = "  whatsapp-business-pp-cli ops template-compare --left template-001 --right template-002 --agent"
	case "template-checklist":
		cmd.Flags().StringVar(&template, "template", "", "Capture ID of the get-template document to inspect")
		cmd.Example = "  whatsapp-business-pp-cli ops template-checklist --template template-001 --agent"
	}
	// Start with discoverable no-selection guidance, followed by a selected
	// example whose IDs must come from snapshots list on the user's store.
	cmd.Example = "  whatsapp-business-pp-cli ops " + kind + " --agent\n" + cmd.Example
	cmd.Flags().StringVar(&maxAge, "max-age", "24h", "Warn when selected capture evidence is older than this duration; 0 disables the hint")
	return
}
func opsRequired(kind string) []string {
	switch kind {
	case "scope-map":
		return []string{"snapshot", "business-id when not captured"}
	case "template-compare":
		return []string{"left", "right"}
	case "template-checklist":
		return []string{"template"}
	default:
		return []string{"before", "after"}
	}
}

// Inspection evidence is heterogeneous. Keep all observed keys in compact
// output so a rare before/after value cannot disappear from an agent's view.
func printWhatsAppInspection(cmd *cobra.Command, flags *rootFlags, view map[string]any) error {
	raw, err := json.Marshal(view)
	if err != nil {
		return err
	}
	var normalized any
	if err = json.Unmarshal(raw, &normalized); err != nil {
		return err
	}
	seen := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				seen[k] = true
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	walk(normalized)
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return printJSONFilteredKeep(cmd.OutOrStdout(), view, flags, keys...)
}
