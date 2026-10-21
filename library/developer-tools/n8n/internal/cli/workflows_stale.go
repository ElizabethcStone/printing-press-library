// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source local

package cli

import (
	"database/sql"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/n8nwf"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/store"
	"github.com/spf13/cobra"
)

type staleWorkflowRow struct {
	WorkflowID    string   `json:"workflow_id"`
	WorkflowName  string   `json:"workflow_name"`
	Active        bool     `json:"active"`
	LastSuccessAt *string  `json:"last_success_at"`
	LastRunAt     *string  `json:"last_run_at"`
	Reasons       []string `json:"reasons"`
}

func newNovelWorkflowsStaleCmd(flags *rootFlags) *cobra.Command {
	var days, dbPath string
	var neverRun, noErrorWorkflow, includeInactive bool
	cmd := &cobra.Command{
		Use: "stale", Short: "Find active workflows with no recent successful run, workflows that never ran, and active workflows without an error workflow.",
		Long: "Use this command for silent, dead, or unprotected workflows. Do NOT use it for workflows that are actively erroring; use 'executions failures' instead.",
		Example: strings.Trim(`
  n8n-pp-cli workflows stale --days 7 --agent
  n8n-pp-cli workflows stale --never-run
  n8n-pp-cli workflows stale --no-error-workflow --include-inactive`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local", "pp:happy-args": "--days=7"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "workflows stale")
			}
			var window time.Duration
			if n, err := strconv.Atoi(days); err == nil {
				if n <= 0 {
					_ = cmd.Usage()
					return usageErr(fmt.Errorf("--days must be positive"))
				}
				window = time.Duration(n) * 24 * time.Hour
			} else {
				var err error
				window, err = cliutil.ParseDurationLoose(days)
				if err != nil || window <= 0 {
					_ = cmd.Usage()
					return usageErr(fmt.Errorf("invalid --days %q", days))
				}
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			if dbPath == "" {
				dbPath = defaultDBPath("n8n-pp-cli")
			}
			cutoff := time.Now().UTC().Add(-window)
			empty := map[string]any{"cutoff": cutoff.Format(time.RFC3339), "window_days": window.Hours() / 24, "workflows": make([]staleWorkflowRow, 0), "scanned_workflows": 0}
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				fmt.Fprintf(cmd.ErrOrStderr(), "no local mirror at %s\nrun: n8n-pp-cli sync --resources workflows,executions,credentials --db %s\n", dbPath, dbPath)
				if wantsHumanTable(cmd.OutOrStdout(), flags) {
					return nil
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
			if !hintIfUnsynced(cmd, db, "workflows") {
				hintIfStale(cmd, db, "workflows", flags.maxAge)
			}
			if !hintIfUnsynced(cmd, db, "executions") {
				hintIfStale(cmd, db, "executions", flags.maxAge)
			}
			wr, err := db.DB().QueryContext(ctx, `SELECT id,data,COALESCE(name,''),COALESCE(active,0),COALESCE(is_archived,0) FROM workflows`)
			if err != nil {
				return fmt.Errorf("reading workflows: %w", err)
			}
			type wfRow struct {
				id, name         string
				data             []byte
				active, archived int64
			}
			wfs := make([]wfRow, 0)
			for wr.Next() {
				var x wfRow
				if err := wr.Scan(&x.id, &x.data, &x.name, &x.active, &x.archived); err != nil {
					wr.Close()
					return fmt.Errorf("scanning workflow: %w", err)
				}
				wfs = append(wfs, x)
			}
			err = wr.Err()
			ce := wr.Close()
			if err != nil {
				return fmt.Errorf("reading workflows: %w", err)
			}
			if ce != nil {
				return fmt.Errorf("closing workflows: %w", ce)
			}
			er, err := db.DB().QueryContext(ctx, `SELECT workflow_id,MAX(CASE WHEN status='success' THEN started_at END),MAX(started_at),COUNT(*) FROM executions GROUP BY workflow_id`)
			if err != nil {
				return fmt.Errorf("reading executions: %w", err)
			}
			type exRow struct {
				success, last sql.NullString
				count         int64
			}
			ex := make(map[string]exRow)
			var executionCount int64
			for er.Next() {
				var id sql.NullString
				var x exRow
				if err := er.Scan(&id, &x.success, &x.last, &x.count); err != nil {
					er.Close()
					return fmt.Errorf("scanning execution summary: %w", err)
				}
				executionCount += x.count
				if id.Valid {
					ex[id.String] = x
				}
			}
			err = er.Err()
			ce = er.Close()
			if err != nil {
				return fmt.Errorf("reading executions: %w", err)
			}
			if ce != nil {
				return fmt.Errorf("closing executions: %w", ce)
			}
			// With no synced executions, run-history reasons would flag every workflow; report only config reasons.
			// A completed empty executions sync is real data; only a never-synced table means "no run history".
			haveRuns := executionCount > 0 || db.GetLastSyncedAt("executions") != ""
			out := make([]staleWorkflowRow, 0)
			scanned, unparsed := 0, 0
			for _, x := range wfs {
				if x.archived != 0 || (!includeInactive && x.active == 0) {
					continue
				}
				wf, e := n8nwf.ParseWorkflow(x.data)
				if e != nil {
					unparsed++
					continue
				}
				scanned++
				if wf.Archived {
					continue
				}
				name := wf.Name
				if name == "" {
					name = x.name
				}
				r, ok := ex[x.id]
				reasons := make([]string, 0, 3)
				var success, last *string
				if ok && r.success.Valid {
					s := r.success.String
					success = &s
				}
				if ok && r.last.Valid {
					s := r.last.String
					last = &s
				}
				if !haveRuns {
					// no run-history reasons without synced executions
				} else if !ok || r.count == 0 {
					reasons = append(reasons, "never_ran")
				} else if success == nil {
					reasons = append(reasons, "no_success_in_window")
				} else if t, e := parseLocalTime(*success); e != nil || t.Before(cutoff) {
					reasons = append(reasons, "no_success_in_window")
				}
				if wf.ErrorWorkflow == "" {
					reasons = append(reasons, "no_error_workflow")
				}
				if neverRun || noErrorWorkflow {
					// Filters combine with AND: each selected filter must match.
					keep := (!neverRun || slices.Contains(reasons, "never_ran")) && (!noErrorWorkflow || slices.Contains(reasons, "no_error_workflow"))
					if !keep {
						continue
					}
				}
				if len(reasons) > 0 {
					out = append(out, staleWorkflowRow{x.id, name, x.active != 0, success, last, reasons})
				}
			}
			sort.Slice(out, func(i, j int) bool {
				a, b := out[i], out[j]
				an, bn := slices.Contains(a.Reasons, "never_ran"), slices.Contains(b.Reasons, "never_ran")
				if an != bn {
					return an
				}
				if (a.LastSuccessAt == nil) != (b.LastSuccessAt == nil) {
					return a.LastSuccessAt == nil
				}
				if a.LastSuccessAt != nil && b.LastSuccessAt != nil && *a.LastSuccessAt != *b.LastSuccessAt {
					return *a.LastSuccessAt < *b.LastSuccessAt
				}
				return a.WorkflowName < b.WorkflowName
			})
			if unparsed > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d workflow(s) could not be parsed\n", unparsed)
			}
			result := map[string]any{"cutoff": cutoff.Format(time.RFC3339), "window_days": window.Hours() / 24, "workflows": out, "scanned_workflows": scanned}
			if !haveRuns {
				result["note"] = "no executions in the local store, so never_ran and no_success_in_window were not evaluated; run n8n-pp-cli sync --resources executions"
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), result, flags)
			}
			items := make([]map[string]any, 0, len(out))
			for _, r := range out {
				items = append(items, map[string]any{"WORKFLOW": r.WorkflowName, "ACTIVE": r.Active, "LAST SUCCESS": r.LastSuccessAt, "REASONS": strings.Join(r.Reasons, ",")})
			}
			return printAutoTable(cmd.OutOrStdout(), items)
		},
	}
	cmd.Flags().StringVar(&days, "days", "7", "Successful run lookback window in days or duration")
	cmd.Flags().BoolVar(&neverRun, "never-run", false, "Only show workflows that never ran (combines with --no-error-workflow as AND)")
	cmd.Flags().BoolVar(&noErrorWorkflow, "no-error-workflow", false, "Only show workflows without an error workflow (combines with --never-run as AND)")
	cmd.Flags().BoolVar(&includeInactive, "include-inactive", false, "Include inactive workflows")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database file path")
	return cmd
}

func parseLocalTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02 15:04:05.999999999-07:00"} {
		if t, e := time.Parse(layout, s); e == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid timestamp %q", s)
}
