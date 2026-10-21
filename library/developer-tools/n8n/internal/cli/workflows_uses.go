// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source local

package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/n8nwf"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/store"
	"github.com/spf13/cobra"
)

type workflowUseRow struct {
	WorkflowID   string `json:"workflow_id"`
	WorkflowName string `json:"workflow_name"`
	Active       bool   `json:"active"`
	Node         string `json:"node"`
	NodeType     string `json:"node_type"`
	Disabled     bool   `json:"disabled"`
	Match        string `json:"match"`
}

func newNovelWorkflowsUsesCmd(flags *rootFlags) *cobra.Command {
	var credential, nodeType, webhook, variable, subworkflow, dbPath string
	var includeArchived bool
	var limit int
	cmd := &cobra.Command{
		Use: "uses", Short: "Find every workflow and node that uses a credential, node type, webhook path, variable, or sub-workflow.",
		Long: "Use this command for exact references to a credential, node type, webhook path, variable, or sub-workflow. Do NOT use it for free-text lookup of arbitrary strings in node parameters; use 'search' instead.",
		Example: strings.Trim(`
  n8n-pp-cli workflows uses --node-type n8n-nodes-base.slack --agent
  n8n-pp-cli workflows uses --credential 'Slack Bot'
  n8n-pp-cli workflows uses --webhook orders/new`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local", "pp:happy-args": "--node-type=n8n-nodes-base.webhook"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "workflows uses")
			}
			q := n8nwf.UsageQuery{Credential: credential, NodeType: nodeType, Webhook: webhook, Variable: variable, Subworkflow: subworkflow}
			if q.Empty() {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("requires at least one of --credential, --node-type, --webhook, --variable, or --subworkflow"))
			}
			if limit < 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--limit must be zero or greater"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			if dbPath == "" {
				dbPath = defaultDBPath("n8n-pp-cli")
			}
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				fmt.Fprintf(cmd.ErrOrStderr(), "no local mirror at %s\nrun: n8n-pp-cli sync --resources workflows,executions,credentials --db %s\n", dbPath, dbPath)
				if wantsHumanTable(cmd.OutOrStdout(), flags) {
					return nil
				}
				return printJSONFiltered(cmd.OutOrStdout(), make([]workflowUseRow, 0), flags)
			} else if err != nil {
				return fmt.Errorf("checking local mirror: %w", err)
			}
			db, err := store.OpenWithContext(ctx, dbPath)
			if err != nil {
				return fmt.Errorf("opening local store: %w", err)
			}
			defer db.Close()
			if !hintIfUnsynced(cmd, db, "workflows") {
				hintIfStale(cmd, db, "workflows", flags.maxAge)
			}
			query := `SELECT id, data, COALESCE(name,''), COALESCE(active,0), COALESCE(is_archived,0) FROM workflows`
			rows, err := db.DB().QueryContext(ctx, query)
			if err != nil {
				return fmt.Errorf("reading workflows: %w", err)
			}
			type stored struct {
				id, name         string
				data             []byte
				active, archived int64
			}
			all := make([]stored, 0)
			for rows.Next() {
				var x stored
				if err := rows.Scan(&x.id, &x.data, &x.name, &x.active, &x.archived); err != nil {
					rows.Close()
					return fmt.Errorf("scanning workflow: %w", err)
				}
				all = append(all, x)
			}
			err = rows.Err()
			closeErr := rows.Close()
			if err != nil {
				return fmt.Errorf("reading workflows: %w", err)
			}
			if closeErr != nil {
				return fmt.Errorf("closing workflows: %w", closeErr)
			}
			result := make([]workflowUseRow, 0)
			bad := 0
			for _, x := range all {
				if !includeArchived && x.archived != 0 {
					continue
				}
				wf, e := n8nwf.ParseWorkflow(x.data)
				if e != nil {
					bad++
					continue
				}
				name := wf.Name
				if name == "" {
					name = x.name
				}
				for _, n := range wf.Nodes {
					if !q.Matches(n) {
						continue
					}
					matched := q.MatchedFields(n)
					result = append(result, workflowUseRow{x.id, name, x.active != 0, n.Name, n.Type, n.Disabled, strings.Join(matched, ",")})
				}
			}
			if bad > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d workflow(s) could not be parsed\n", bad)
			}
			sort.Slice(result, func(i, j int) bool {
				if result[i].WorkflowName == result[j].WorkflowName {
					return result[i].Node < result[j].Node
				}
				return result[i].WorkflowName < result[j].WorkflowName
			})
			if limit > 0 && len(result) > limit {
				result = result[:limit]
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), result, flags)
			}
			items := make([]map[string]any, 0, len(result))
			for _, r := range result {
				items = append(items, map[string]any{"WORKFLOW": r.WorkflowName, "ACTIVE": r.Active, "NODE": r.Node, "TYPE": r.NodeType})
			}
			return printAutoTable(cmd.OutOrStdout(), items)
		},
	}
	cmd.Flags().StringVar(&credential, "credential", "", "Match credential id or name")
	cmd.Flags().StringVar(&nodeType, "node-type", "", "Match full or short node type")
	cmd.Flags().StringVar(&webhook, "webhook", "", "Match webhook path")
	cmd.Flags().StringVar(&variable, "variable", "", "Match variable key")
	cmd.Flags().StringVar(&subworkflow, "subworkflow", "", "Match called workflow id")
	cmd.Flags().BoolVar(&includeArchived, "include-archived", false, "Include archived workflows")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum rows (0 means unlimited)")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database file path")
	return cmd
}
