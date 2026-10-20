// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/n8nwf"
	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if wf, _, err := root.Find([]string{"workflows"}); err == nil && wf != root {
			addNovelCommandIfAbsent(wf, newWorkflowsPushCmd(flags))
		}
	})
}

func newWorkflowsPushCmd(flags *rootFlags) *cobra.Command {
	var id string
	var create, apply bool
	cmd := &cobra.Command{Use: "push [file]", Short: "Push a local workflow JSON file to the instance, showing the node-level diff first; writes only with --apply.",
		Long: "Reads a workflow JSON file, compares it with the live workflow that has the same id, and prints the semantic diff. --apply updates the live workflow. --create creates a new workflow from the file instead of updating.",
		Example: strings.Trim(`
  n8n-pp-cli workflows push ./n8n-workflows/order-intake--0vB2cZqgHEmb3RKs.json
  n8n-pp-cli workflows push ./order-intake.json --apply`, "\n"),
		Annotations: map[string]string{"pp:data-source": "live", "pp:preview-happy-path": "true", "mcp:read-only-unless-flags": "apply", "mcp:read-flags": "file", "pp:happy-args": "file=example-value"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "workflows push")
			}
			if len(args) != 1 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("provide one workflow file"))
			}
			data, err := os.ReadFile(args[0])
			if err != nil {
				return usageErr(fmt.Errorf("read workflow file: %w", err))
			}
			var local map[string]any
			if err := json.Unmarshal(data, &local); err != nil || local == nil {
				_ = cmd.Usage()
				if err == nil {
					err = fmt.Errorf("expected a JSON object")
				}
				return usageErr(fmt.Errorf("invalid workflow file: %w", err))
			}
			nodes, ok := local["nodes"].([]any)
			if !ok {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("workflow file must contain nodes[]"))
			}
			targetID := id
			if targetID == "" {
				targetID, _ = local["id"].(string)
			}
			if !create && targetID == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("workflow id is required in the file or with --id"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			if create {
				name, _ := local["name"].(string)
				out := map[string]any{"mode": "create", "name": name, "node_count": len(nodes), "applied": false}
				if apply {
					if _, ok := local["settings"].(map[string]any); !ok {
						local["settings"] = map[string]any{} // required by POST /workflows
					}
					result, _, e := c.Post(ctx, "/workflows", n8nwf.UpdatableBody(local))
					if e != nil {
						return classifyAPIError(cmd.OutOrStdout(), e, flags)
					}
					var saved map[string]any
					_ = json.Unmarshal(result, &saved)
					out["applied"] = true
					if saved["id"] != nil {
						out["workflow_id"] = saved["id"]
					}
				}
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}
			liveRaw, err := c.GetNoCache(ctx, "/workflows/"+targetID, nil)
			if err != nil {
				if isNotFound(err) {
					return notFoundErr(fmt.Errorf("workflow %s not found; use --create to create it", targetID))
				}
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			localRaw, err := json.Marshal(local)
			if err != nil {
				return err
			}
			live, err := n8nwf.ParseWorkflow(liveRaw)
			if err != nil {
				return err
			}
			desired, err := n8nwf.ParseWorkflow(localRaw)
			if err != nil {
				return err
			}
			d := n8nwf.Diff(live, desired)
			var liveBody map[string]any
			_ = json.Unmarshal(liveRaw, &liveBody)
			// PUT requires settings; a file without them keeps the live settings rather than clearing them.
			settingsKept := false
			if _, ok := local["settings"].(map[string]any); !ok {
				if ls, ok := liveBody["settings"]; ok && ls != nil {
					local["settings"] = ls
					settingsKept = true
				}
			}
			// export strips staticData by default; keep the live trigger state so an untouched file is not a change.
			if _, ok := local["staticData"]; !ok {
				if sd, ok := liveBody["staticData"]; ok {
					local["staticData"] = sd
				}
			}
			// Diff ignores layout noise, but a push must still write positions, webhookIds, and the like.
			changed := d.ContentChanged() || n8nwf.BodiesDiffer(liveBody, local)
			out := map[string]any{"workflow_id": targetID, "file": args[0], "mode": "update", "applied": false, "changed": changed, "diff": d}
			if settingsKept {
				out["settings_kept_from_live"] = true
			}
			if changed && !d.ContentChanged() {
				out["note"] = "only fields outside the semantic diff differ (for example node positions or webhook ids)"
			}
			if dropped := n8nwf.DroppedSettings(local); len(dropped) > 0 {
				out["unwritable_settings"] = dropped
			}
			if apply && changed {
				result, _, e := c.Put(ctx, "/workflows/"+targetID, n8nwf.UpdatableBody(local))
				if e != nil {
					return classifyAPIError(cmd.OutOrStdout(), e, flags)
				}
				out["applied"] = true
				if v, ok := savedVersionID(result); ok {
					out["version_id"] = v
				}
			}
			return printJSONFiltered(cmd.OutOrStdout(), out, flags)
		}}
	cmd.Flags().StringVar(&id, "id", "", "Override the workflow id in the file")
	cmd.Flags().BoolVar(&create, "create", false, "Create a new workflow instead of updating")
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply the update or create the workflow")
	return cmd
}
