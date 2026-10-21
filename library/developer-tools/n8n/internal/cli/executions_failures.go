// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source auto

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/n8nwf"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/store"
	"github.com/spf13/cobra"
)

type failureExecution struct{ ID, WorkflowID, StartedAt, Status string }

// noExecutionDataMarker is cached for finished executions whose run data was pruned.
const noExecutionDataMarker = "(no execution data available)"

func newNovelExecutionsFailuresCmd(flags *rootFlags) *cobra.Command {
	var since, workflow, node, dbPath string
	var maxFetch, limit int
	var noFetch bool
	cmd := &cobra.Command{Use: "failures", Short: "See what broke recently, grouped by workflow and failing node, with the latest execution to retry.",
		Long: "Use this command for \"what broke recently\" and errors grouped by workflow and failing node. Do NOT use it to find workflows that are silent or never run; use 'workflows stale' instead.\n\nReads synced executions (run 'n8n-pp-cli sync --resources executions,workflows' first). Failing node and message come from a local cache; executions not yet cached are fetched once with includeData (bounded by --max-fetch) unless --no-fetch or --data-source local is set. There is no live-only mode: the failure list always comes from the synced store.",
		Example: strings.Trim(`
  n8n-pp-cli executions failures --since 24h --agent
  n8n-pp-cli executions failures --since 7d --workflow 0vB2cZqgHEmb3RKs
  n8n-pp-cli executions failures --no-fetch`, "\n"),
		Annotations: map[string]string{"mcp:local-write": "true", "pp:data-source": "auto", "pp:happy-args": "--since=7d;--max-fetch=3", "pp:live-happy-path": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "executions failures")
			}
			if flags.dataSource == "live" {
				return usageErr(fmt.Errorf("executions failures has no live-only mode; it groups synced executions (use --data-source auto or local)"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			dur, err := cliutil.ParseDurationLoose(since)
			if err != nil || dur <= 0 {
				if err == nil {
					err = fmt.Errorf("duration must be positive")
				}
				return usageErr(fmt.Errorf("invalid --since: %w", err))
			}
			if maxFetch < 0 || limit < 0 {
				return usageErr(fmt.Errorf("--max-fetch and --limit must be non-negative"))
			}
			cutoff := time.Now().UTC().Add(-dur)
			cutoffText := cutoff.Format("2006-01-02T15:04:05.000Z") // same fixed-millis shape n8n stores, so string comparison is exact
			result := map[string]any{"since": cutoffText, "total_failures": 0, "groups": []map[string]any{}, "fetched_details": 0, "uncached_remaining": 0}
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				fmt.Fprintf(cmd.ErrOrStderr(), "no local mirror at %s\nrun: n8n-pp-cli sync --resources executions,workflows --db %s\n", dbPath, dbPath)
				result["note"] = "no failed executions since " + cutoffText + " in the local store; run n8n-pp-cli sync --resources executions to refresh"
				return printFailureOutput(cmd, result, flags, nil)
			} else if err != nil {
				return fmt.Errorf("stat local mirror: %w", err)
			}
			db, err := store.OpenWithContext(ctx, dbPath)
			if err != nil {
				return fmt.Errorf("open local mirror: %w", err)
			}
			defer db.Close()
			hintIfUnsynced(cmd, db, "executions")
			hintIfStale(cmd, db, "executions", flags.maxAge)
			q := `SELECT id, COALESCE(workflow_id,''), COALESCE(started_at,''), COALESCE(status,'') FROM executions WHERE status IN ('error','crashed') AND started_at >= ?`
			argsQ := []any{cutoffText}
			if workflow != "" {
				q += ` AND workflow_id = ?`
				argsQ = append(argsQ, workflow)
			}
			q += ` ORDER BY started_at DESC,id DESC`
			rows, err := db.DB().QueryContext(ctx, q, argsQ...)
			if err != nil {
				return err
			}
			exs := make([]failureExecution, 0)
			ids := make([]string, 0)
			for rows.Next() {
				var e failureExecution
				if err := rows.Scan(&e.ID, &e.WorkflowID, &e.StartedAt, &e.Status); err != nil {
					rows.Close()
					return err
				}
				exs = append(exs, e)
				ids = append(ids, e.ID)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
			result["total_failures"] = len(exs)
			nameRows, err := db.DB().QueryContext(ctx, `SELECT id, COALESCE(name,'') FROM workflows`)
			if err != nil {
				return err
			}
			names := map[string]string{}
			for nameRows.Next() {
				var id, n string
				if err := nameRows.Scan(&id, &n); err != nil {
					nameRows.Close()
					return err
				}
				names[id] = n
			}
			if err := nameRows.Err(); err != nil {
				nameRows.Close()
				return err
			}
			if err := nameRows.Close(); err != nil {
				return err
			}
			if err := db.EnsureExecutionFailures(ctx); err != nil {
				return err
			}
			cached, err := db.GetExecutionFailures(ctx, ids)
			if err != nil {
				return err
			}
			uncached := make([]string, 0)
			for _, id := range ids {
				if _, ok := cached[id]; !ok {
					uncached = append(uncached, id)
				}
			}
			fetchFailures := make([]map[string]string, 0)
			fetched, noDetail := 0, 0
			capFetch := maxFetch
			if cliutil.IsDogfoodEnv() && capFetch > 3 {
				capFetch = 3
			}
			if !noFetch && flags.dataSource != "local" && capFetch > 0 && len(uncached) > 0 {
				c, e := flags.newClient()
				if e != nil {
					return e
				}
				for _, id := range uncached[:min(len(uncached), capFetch)] {
					raw, e := c.Get(ctx, "/executions/"+id, map[string]string{"includeData": "true"})
					if e != nil {
						fetchFailures = append(fetchFailures, map[string]string{"execution_id": id, "error": e.Error()})
						continue
					}
					f := n8nwf.ExecutionFailure(raw)
					if f.Node == "" && f.Message == "" {
						noDetail++
						// Cache a marker only for a finished execution whose data is gone (pruned);
						// anything unparsable or still in flight is retried on a later run.
						var head struct {
							Status string `json:"status"`
						}
						if json.Unmarshal(raw, &head) != nil || head.Status == "" || head.Status == "running" || head.Status == "new" || head.Status == "waiting" {
							continue
						}
						f.Message = noExecutionDataMarker
					}
					row := store.ExecutionFailureRow{ExecutionID: id, Node: f.Node, Message: f.Message}
					if e := db.PutExecutionFailure(ctx, row); e != nil {
						return e
					}
					cached[id] = row
					fetched++
				}
			}
			result["fetched_details"] = fetched
			if noDetail > 0 {
				result["no_detail"] = noDetail
			}
			remaining := len(uncached) - fetched
			result["uncached_remaining"] = remaining
			if len(fetchFailures) > 0 {
				result["fetch_failures"] = fetchFailures
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d of %d detail fetches failed\n", len(fetchFailures), min(len(uncached), capFetch))
			}
			type group struct {
				workflow, node               string
				count                        int
				first, last, latest, message string
			}
			groups := map[string]*group{}
			for _, e := range exs {
				f := cached[e.ID]
				key := e.WorkflowID + "\x00" + f.Node
				g := groups[key]
				if g == nil {
					g = &group{workflow: e.WorkflowID, node: f.Node, first: e.StartedAt, last: e.StartedAt, latest: e.ID, message: f.Message}
					groups[key] = g
				}
				g.count++
				if e.StartedAt < g.first {
					g.first = e.StartedAt
				}
				if e.StartedAt > g.last {
					g.last = e.StartedAt
					g.latest = e.ID
					g.message = f.Message
				}
			}
			ordered := make([]*group, 0, len(groups))
			for _, g := range groups {
				if node != "" && !strings.EqualFold(g.node, node) {
					continue
				}
				ordered = append(ordered, g)
			}
			sort.Slice(ordered, func(i, j int) bool {
				if ordered[i].count != ordered[j].count {
					return ordered[i].count > ordered[j].count
				}
				return ordered[i].last > ordered[j].last
			})
			if limit > 0 && limit < len(ordered) { // 0 means no limit
				ordered = ordered[:limit]
			}
			out := make([]map[string]any, 0, len(ordered))
			for _, g := range ordered {
				out = append(out, map[string]any{"workflow_id": g.workflow, "workflow_name": names[g.workflow], "node": g.node, "count": g.count, "first_seen": g.first, "last_seen": g.last, "latest_execution_id": g.latest, "sample_message": g.message})
			}
			result["groups"] = out
			if len(exs) == 0 {
				result["note"] = "no failed executions since " + cutoffText + " in the local store; run n8n-pp-cli sync --resources executions to refresh"
			}
			return printFailureOutput(cmd, result, flags, out)
		},
	}
	cmd.Flags().StringVar(&since, "since", "24h", "Include failures started within this duration")
	cmd.Flags().StringVar(&workflow, "workflow", "", "Filter by workflow id")
	cmd.Flags().StringVar(&node, "node", "", "Filter by failing node name")
	cmd.Flags().IntVar(&maxFetch, "max-fetch", 25, "Maximum uncached execution details to fetch (0 disables fetching)")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "Do not fetch uncached execution details")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum groups to return")
	cmd.Flags().StringVar(&dbPath, "db", defaultDBPath("n8n-pp-cli"), "Local store path")
	return cmd
}

func printFailureOutput(cmd *cobra.Command, result map[string]any, flags *rootFlags, groups []map[string]any) error {
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return printJSONFiltered(cmd.OutOrStdout(), result, flags)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "WORKFLOW | NODE | COUNT | LAST SEEN | LATEST EXEC | MESSAGE")
	for _, g := range groups {
		n, _ := g["node"].(string)
		if n == "" {
			n = "(unknown)"
		}
		m, _ := g["sample_message"].(string)
		m = clip(m, 80)
		fmt.Fprintf(cmd.OutOrStdout(), "%s | %s | %v | %s | %s | %s\n", g["workflow_name"], n, g["count"], g["last_seen"], g["latest_execution_id"], m)
	}
	return nil
}
