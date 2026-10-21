// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/client"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/n8nwf"
	"github.com/spf13/cobra"
)

type diffSide struct {
	Label     string `json:"label"`
	VersionID string `json:"version_id,omitempty"`
}

func newNovelWorkflowsDiffCmd(flags *rootFlags) *cobra.Command {
	var versions []string
	var againstConfig, againstID string
	cmd := &cobra.Command{Use: "diff [workflowId]", Short: "Compare a workflow across two instances or two saved versions at the node and connection level, ignoring layout noise.",
		Long: "Use this command to compare a workflow across two instances or two saved versions. Do NOT use it to preview pushing a local workflow file to the instance; use 'workflows push' instead.\n\nModes (base -> target):\n  --version V          saved version V -> current workflow\n  --version A --version B   version A -> version B\n  --against-config F   this instance -> the same workflow id on the instance configured in config file F\n  --against-id ID      this workflow -> another workflow ID on the same instance",
		Example: strings.Trim(`
  n8n-pp-cli workflows diff 0vB2cZqgHEmb3RKs --version 7c1e2b40-4e64-4b5f-9a59-0d3c6f1a2b9e --agent
  n8n-pp-cli workflows diff 0vB2cZqgHEmb3RKs --against-config ~/.config/n8n-pp-cli/prod.toml`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "workflowId=example-value;--version=example-value", "pp:verified-by-proof": "workflows-diff-lifecycle.md"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "workflows diff")
			}
			modeCount := 0
			if len(versions) > 0 {
				modeCount++
			}
			if againstConfig != "" {
				modeCount++
			}
			if againstID != "" {
				modeCount++
			}
			if len(args) != 1 || modeCount != 1 || len(versions) > 2 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("provide workflowId and exactly one mode: --version [V [B]], --against-config F, or --against-id ID"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			id := args[0]
			fetch := func(cl *client.Client, path, side string) (json.RawMessage, error) {
				b, e := cl.GetNoCache(ctx, path, nil)
				if e != nil {
					if isNotFound(e) {
						return nil, notFoundErr(fmt.Errorf("%s not found on the %s side (%s); check the id and which instance it lives on", strings.TrimPrefix(path, "/workflows/"), side, cl.BaseURL))
					}
					return nil, classifyAPIError(cmd.OutOrStdout(), e, flags)
				}
				return b, nil
			}
			var baseRaw, targetRaw json.RawMessage
			baseSide, targetSide := diffSide{}, diffSide{}
			switch {
			case len(versions) > 0:
				if len(versions) == 1 {
					baseSide = diffSide{Label: "version:" + versions[0], VersionID: versions[0]}
					targetSide = diffSide{Label: "current"}
					baseRaw, err = fetch(c, "/workflows/"+id+"/versions/"+versions[0], "base")
					if err == nil {
						targetRaw, err = fetch(c, "/workflows/"+id, "target")
					}
				} else {
					baseSide = diffSide{Label: "version:" + versions[0], VersionID: versions[0]}
					targetSide = diffSide{Label: "version:" + versions[1], VersionID: versions[1]}
					baseRaw, err = fetch(c, "/workflows/"+id+"/versions/"+versions[0], "base")
					if err == nil {
						targetRaw, err = fetch(c, "/workflows/"+id+"/versions/"+versions[1], "target")
					}
				}
			case againstConfig != "":
				baseSide = diffSide{Label: "current"}
				targetSide = diffSide{Label: "config:" + againstConfig}
				baseRaw, err = fetch(c, "/workflows/"+id, "base")
				if err == nil {
					other, e := clientFromConfigFile(flags, againstConfig)
					if e != nil {
						return e
					}
					if sameInstance(other.BaseURL, c.BaseURL) {
						return usageErr(fmt.Errorf("--against-config %s resolves to the same instance (%s); point base_url in that file at the other instance", againstConfig, c.BaseURL))
					}
					targetRaw, err = fetch(other, "/workflows/"+id, "target")
				}
			case againstID != "":
				baseSide = diffSide{Label: "current"}
				targetSide = diffSide{Label: "workflow:" + againstID}
				baseRaw, err = fetch(c, "/workflows/"+id, "base")
				if err == nil {
					targetRaw, err = fetch(c, "/workflows/"+againstID, "target")
				}
			}
			if err != nil {
				return err
			}
			base, e := n8nwf.ParseWorkflow(baseRaw)
			if e != nil {
				return e
			}
			target, e := n8nwf.ParseWorkflow(targetRaw)
			if e != nil {
				return e
			}
			d := n8nwf.Diff(base, target)
			if len(versions) > 0 {
				d.ActiveChanged = false
			}
			out := map[string]any{"workflow_id": id, "base": baseSide, "target": targetSide, "changed": d.Changed(), "diff": d}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}
			printWorkflowDiff(cmd, d, base.Active, target.Active)
			return nil
		}}
	cmd.Flags().StringArrayVar(&versions, "version", nil, "Compare saved versions (one version to current, or two versions)")
	cmd.Flags().StringVar(&againstConfig, "against-config", "", "Compare against the same workflow on another configured instance")
	cmd.Flags().StringVar(&againstID, "against-id", "", "Compare against another workflow id on this instance")
	return cmd
}

func printWorkflowDiff(cmd *cobra.Command, d n8nwf.DiffResult, baseActive, targetActive bool) {
	if !d.Changed() {
		fmt.Fprintln(cmd.OutOrStdout(), "No differences.")
		return
	}
	writeDiffLines(cmd.OutOrStdout(), d, baseActive, targetActive)
}
