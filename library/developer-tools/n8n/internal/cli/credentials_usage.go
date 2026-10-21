// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source local
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/n8nwf"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/store"
	"github.com/spf13/cobra"
)

type credentialSummary struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Type                string   `json:"type"`
	WorkflowCount       int      `json:"workflow_count"`
	ActiveWorkflowCount int      `json:"active_workflow_count"`
	NodeCount           int      `json:"node_count"`
	Workflows           []string `json:"workflows"`
}
type credentialBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}
type danglingCredential struct {
	WorkflowID     string `json:"workflow_id"`
	WorkflowName   string `json:"workflow_name"`
	Node           string `json:"node"`
	CredentialType string `json:"credential_type"`
	CredentialID   string `json:"credential_id"`
	CredentialName string `json:"credential_name"`
}

func newNovelCredentialsUsageCmd(flags *rootFlags) *cobra.Command {
	var unused, includeArchived bool
	var minRefs int
	var dbPath string
	cmd := &cobra.Command{
		Use: "usage", Short: "List unused credentials, credentials many workflows depend on, and references to credentials that no longer exist.",
		Example: strings.Trim(`
  n8n-pp-cli credentials usage --agent
  n8n-pp-cli credentials usage --unused
  n8n-pp-cli credentials usage --min-refs 5`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "credentials usage")
			}
			if minRefs < 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--min-refs must be zero or greater"))
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
				empty := map[string]any{"credentials": make([]credentialSummary, 0), "unused": make([]credentialBrief, 0), "dangling": make([]danglingCredential, 0), "scanned_workflows": 0, "scanned_credentials": 0}
				if unused {
					empty = map[string]any{"unused": make([]credentialBrief, 0), "scanned_credentials": 0, "scanned_workflows": 0}
				}
				return printJSONFiltered(cmd.OutOrStdout(), empty, flags)
			} else if err != nil {
				return fmt.Errorf("checking local mirror: %w", err)
			}
			db, err := store.OpenWithContext(ctx, dbPath)
			if err != nil {
				return fmt.Errorf("opening local store: %w", err)
			}
			defer db.Close()
			if !hintIfUnsynced(cmd, db, "credentials") {
				hintIfStale(cmd, db, "credentials", flags.maxAge)
			}
			if !hintIfUnsynced(cmd, db, "workflows") {
				hintIfStale(cmd, db, "workflows", flags.maxAge)
			}
			cr, err := db.DB().QueryContext(ctx, `SELECT id,COALESCE(name,''),COALESCE(type,'') FROM credentials`)
			if err != nil {
				return fmt.Errorf("reading credentials: %w", err)
			}
			creds := make([]credentialBrief, 0)
			for cr.Next() {
				var c credentialBrief
				if err := cr.Scan(&c.ID, &c.Name, &c.Type); err != nil {
					cr.Close()
					return fmt.Errorf("scanning credential: %w", err)
				}
				creds = append(creds, c)
			}
			err = cr.Err()
			ce := cr.Close()
			if err != nil {
				return fmt.Errorf("reading credentials: %w", err)
			}
			if ce != nil {
				return fmt.Errorf("closing credentials: %w", ce)
			}
			wr, err := db.DB().QueryContext(ctx, `SELECT id,data,COALESCE(name,''),COALESCE(active,0),COALESCE(is_archived,0) FROM workflows`)
			if err != nil {
				return fmt.Errorf("reading workflows: %w", err)
			}
			type workflowRow struct {
				id, name         string
				data             []byte
				active, archived int64
			}
			wfs := make([]workflowRow, 0)
			for wr.Next() {
				var w workflowRow
				if err := wr.Scan(&w.id, &w.data, &w.name, &w.active, &w.archived); err != nil {
					wr.Close()
					return fmt.Errorf("scanning workflow: %w", err)
				}
				wfs = append(wfs, w)
			}
			err = wr.Err()
			ce = wr.Close()
			if err != nil {
				return fmt.Errorf("reading workflows: %w", err)
			}
			if ce != nil {
				return fmt.Errorf("closing workflows: %w", ce)
			}
			// A completed empty credentials sync means every reference is dangling; only a never-synced table is unknown.
			credsSynced := len(creds) > 0 || db.GetLastSyncedAt("credentials") != ""
			byID := make(map[string]*credentialSummary, len(creds))
			usedBy := make(map[string]map[string]bool, len(creds))
			for _, c := range creds {
				byID[c.ID] = &credentialSummary{ID: c.ID, Name: c.Name, Type: c.Type, Workflows: make([]string, 0)}
				usedBy[c.ID] = make(map[string]bool)
			}
			dangling := make([]danglingCredential, 0)
			scanned, unparsed := 0, 0
			for _, w := range wfs {
				if !includeArchived && w.archived != 0 {
					continue
				}
				wf, e := n8nwf.ParseWorkflow(w.data)
				if e != nil {
					unparsed++
					continue
				}
				if wf.Archived && !includeArchived {
					continue
				}
				scanned++
				name := wf.Name
				if name == "" {
					name = w.name
				}
				for _, n := range wf.Nodes {
					for _, ref := range n.Credentials {
						if ref.ID == "" {
							continue
						}
						c, ok := byID[ref.ID]
						if !ok {
							if credsSynced {
								dangling = append(dangling, danglingCredential{w.id, name, n.Name, ref.Type, ref.ID, ref.Name})
							}
							continue
						}
						c.NodeCount++
						if !usedBy[ref.ID][w.id] {
							usedBy[ref.ID][w.id] = true
							c.WorkflowCount++
							if w.active != 0 {
								c.ActiveWorkflowCount++
							}
							c.Workflows = append(c.Workflows, name)
						}
					}
				}
			}
			if unparsed > 0 {
				// A workflow we cannot parse may still reference credentials; "unused" is then not authoritative.
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d workflow(s) could not be parsed; credentials they reference may be reported as unused\n", unparsed)
			}
			all, unusedRows := make([]credentialSummary, 0, len(creds)), make([]credentialBrief, 0)
			for _, brief := range creds {
				c := byID[brief.ID]
				sort.Strings(c.Workflows)
				if c.WorkflowCount == 0 {
					unusedRows = append(unusedRows, credentialBrief{c.ID, c.Name, c.Type})
				}
				if c.WorkflowCount >= minRefs {
					all = append(all, *c)
				}
			}
			sort.Slice(all, func(i, j int) bool {
				if all[i].WorkflowCount != all[j].WorkflowCount {
					return all[i].WorkflowCount > all[j].WorkflowCount
				}
				return all[i].Name < all[j].Name
			})
			sort.Slice(unusedRows, func(i, j int) bool { return unusedRows[i].Name < unusedRows[j].Name })
			sort.Slice(dangling, func(i, j int) bool {
				if dangling[i].WorkflowName != dangling[j].WorkflowName {
					return dangling[i].WorkflowName < dangling[j].WorkflowName
				}
				if dangling[i].Node != dangling[j].Node {
					return dangling[i].Node < dangling[j].Node
				}
				return dangling[i].CredentialID < dangling[j].CredentialID
			})
			if unused {
				return printJSONFiltered(cmd.OutOrStdout(), map[string]any{"unused": unusedRows, "scanned_credentials": len(creds), "scanned_workflows": scanned, "unparsed_workflows": unparsed}, flags)
			}
			result := map[string]any{"credentials": all, "unused": unusedRows, "dangling": dangling, "scanned_workflows": scanned, "scanned_credentials": len(creds), "unparsed_workflows": unparsed}
			if !credsSynced {
				result["note"] = "credentials not synced"
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), result, flags)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Most used")
			if err := printStructTable(out, all); err != nil {
				return err
			}
			fmt.Fprintln(out, "Unused")
			if err := printStructTable(out, unusedRows); err != nil {
				return err
			}
			fmt.Fprintln(out, "Dangling references")
			return printStructTable(out, dangling)
		},
	}
	cmd.Flags().BoolVar(&unused, "unused", false, "Show only unused credentials")
	cmd.Flags().IntVar(&minRefs, "min-refs", 0, "Only include credentials used by at least this many workflows")
	cmd.Flags().BoolVar(&includeArchived, "include-archived", false, "Include archived workflows")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database file path")
	return cmd
}
func printStructTable(w io.Writer, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encoding table rows: %w", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(b, &rows); err != nil {
		return fmt.Errorf("decoding table rows: %w", err)
	}
	return printAutoTable(w, rows)
}
