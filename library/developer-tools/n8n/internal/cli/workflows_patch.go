// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/n8nwf"
	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if wf, _, err := root.Find([]string{"workflows"}); err == nil && wf != root {
			addNovelCommandIfAbsent(wf, newWorkflowsPatchCmd(flags))
		}
	})
}

func newWorkflowsPatchCmd(flags *rootFlags) *cobra.Command {
	var node, renameTo string
	var sets []string
	var disable, enable, apply bool
	cmd := &cobra.Command{Use: "patch [workflowId]", Short: "Edit one node in a workflow by name with a diff preview before applying.",
		Long: "Fetches the workflow, applies node-level edits locally, and prints the semantic diff. Nothing is written unless --apply is passed.\n\n--set takes path=value; value is parsed as JSON when valid and used as a plain string otherwise. Paths are relative to node parameters and dotted for nesting.",
		Example: strings.Trim(`
  n8n-pp-cli workflows patch 0vB2cZqgHEmb3RKs --node 'HTTP Request' --set url=https://api.example.com/v2
  n8n-pp-cli workflows patch 0vB2cZqgHEmb3RKs --node Notify --disable --apply`, "\n"),
		Annotations: map[string]string{"pp:data-source": "live", "pp:preview-happy-path": "true", "mcp:read-only-unless-flags": "apply", "pp:happy-args": "workflowId=example-value;--node=example-value;--set=example-value"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "workflows patch")
			}
			if len(args) != 1 || node == "" || (len(sets) == 0 && renameTo == "" && !disable && !enable) || (disable && enable) {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("provide workflowId, --node, and at least one edit; --disable and --enable cannot be combined"))
			}
			for _, s := range sets {
				if !strings.Contains(s, "=") || strings.SplitN(s, "=", 2)[0] == "" {
					_ = cmd.Usage()
					return usageErr(fmt.Errorf("--set must be path=value: %q", s))
				}
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			original, err := c.GetNoCache(ctx, "/workflows/"+args[0], nil)
			if err != nil {
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			var body map[string]any
			if err := json.Unmarshal(original, &body); err != nil {
				return fmt.Errorf("decode workflow: %w", err)
			}
			for _, s := range sets {
				parts := strings.SplitN(s, "=", 2)
				var value any
				if json.Unmarshal([]byte(parts[1]), &value) != nil {
					value = parts[1]
				}
				if _, err := n8nwf.SetNodeParam(body, node, parts[0], value); err != nil {
					return usageErr(err)
				}
			}
			if disable || enable {
				if _, err := n8nwf.SetNodeDisabled(body, node, disable); err != nil {
					return usageErr(err)
				}
			}
			if renameTo != "" {
				if err := n8nwf.RenameNode(body, node, renameTo); err != nil {
					return usageErr(err)
				}
			}
			edited, err := json.Marshal(body)
			if err != nil {
				return err
			}
			before, err := n8nwf.ParseWorkflow(original)
			if err != nil {
				return err
			}
			after, err := n8nwf.ParseWorkflow(edited)
			if err != nil {
				return err
			}
			d := n8nwf.Diff(before, after)
			var origBody map[string]any
			_ = json.Unmarshal(original, &origBody)
			changed := d.ContentChanged() || n8nwf.BodiesDiffer(origBody, body)
			out := map[string]any{"workflow_id": args[0], "applied": false, "changed": changed, "diff": d}
			if dropped := n8nwf.DroppedSettings(body); len(dropped) > 0 {
				out["unwritable_settings"] = dropped
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: settings not accepted by the public API will be left out of the update: %s\n", strings.Join(dropped, ", "))
			}
			if apply && changed {
				result, _, err := c.Put(ctx, "/workflows/"+args[0], n8nwf.UpdatableBody(body))
				if err != nil {
					return classifyAPIError(cmd.OutOrStdout(), err, flags)
				}
				out["applied"] = true
				if v, ok := savedVersionID(result); ok {
					out["version_id"] = v
				}
			} else if apply {
				out["note"] = "no change"
			} else {
				out["hint"] = "re-run with --apply to write this change"
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}
			if !changed {
				fmt.Fprintln(cmd.OutOrStdout(), "No differences.")
			}
			writeDiffLines(cmd.OutOrStdout(), d, before.Active, after.Active)
			if changed && !d.ContentChanged() {
				fmt.Fprintln(cmd.OutOrStdout(), "Only fields outside the semantic diff differ (for example node positions, webhook ids, or credential names).")
			}
			if apply && changed {
				fmt.Fprintln(cmd.OutOrStdout(), "Applied.")
			} else if !apply {
				fmt.Fprintln(cmd.OutOrStdout(), "Preview only; re-run with --apply to write.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "No change.")
			}
			return nil
		}}
	cmd.Flags().StringVar(&node, "node", "", "Target node name (required)")
	cmd.Flags().StringArrayVar(&sets, "set", nil, "Set a node parameter as path=value (repeatable)")
	cmd.Flags().StringVar(&renameTo, "rename-to", "", "Rename the node")
	cmd.Flags().BoolVar(&disable, "disable", false, "Disable the node")
	cmd.Flags().BoolVar(&enable, "enable", false, "Enable the node")
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply the change to the live workflow")
	return cmd
}
