// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source local

package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
)

type ledgerToolTotal struct {
	Tool       string `json:"tool"`
	Calls      int    `json:"calls"`
	CreditsMin int    `json:"credits_min"`
	CreditsMax int    `json:"credits_max"`
}

type ledgerView struct {
	Path           string              `json:"path"`
	Since          string              `json:"since,omitempty"`
	Count          int                 `json:"count"`
	CreditsMin     int                 `json:"credits_min_total"`
	CreditsMax     int                 `json:"credits_max_total"`
	PerPinCalls    int                 `json:"per_pin_calls"`
	Unconfirmed    int                 `json:"unconfirmed"`
	ByTool         []ledgerToolTotal   `json:"by_tool"`
	Entries        []pgmcp.LedgerEntry `json:"entries"`
	MalformedLines int                 `json:"malformed_lines,omitempty"`
}

func newNovelLedgerCmd(flags *rootFlags) *cobra.Command {
	var since, tool string

	cmd := &cobra.Command{
		Use:   "ledger",
		Short: "See every credit-spending call this CLI executed, with totals by tool and time window.",
		Long: `Use this command to see what this CLI has spent. Do NOT use it to find unpinned posts; use 'unpinned-posts' instead.

Reads the local append-only ledger that every --execute credit spend writes
(PINGENERATOR_LEDGER overrides its path). AI pin and automation costs vary, so
totals are reported as a min-max range; automation batches are counted
separately because they bill per pin produced. Spends whose call timed out or
hit a transport or 5xx error are recorded as "unconfirmed": they may have
been accepted, and pin-post/pin-products treat them as already pinned.`,
		Example:     "  pingenerator-pp-cli ledger --since 7d --agent",
		Annotations: map[string]string{"pp:data-source": "local", "mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "ledger")
			}
			var cutoff time.Time
			if since != "" {
				d, err := cliutil.ParseDurationLoose(since)
				if err != nil || d <= 0 {
					return usageErr(fmt.Errorf("--since must be a positive duration like 7d, 24h or 2w"))
				}
				cutoff = time.Now().Add(-d)
			}
			wantTool := strings.ReplaceAll(strings.TrimSpace(tool), "-", "_")
			path, err := pgLedgerPath()
			if err != nil {
				return configErr(err)
			}
			entries, bad, err := pgmcp.ReadLedger(path)
			if err != nil {
				return fmt.Errorf("reading ledger %s: %w", path, err)
			}
			view := ledgerView{Path: path, Since: since, Entries: make([]pgmcp.LedgerEntry, 0), ByTool: make([]ledgerToolTotal, 0), MalformedLines: bad}
			totals := map[string]*ledgerToolTotal{}
			for _, e := range entries {
				if !cutoff.IsZero() && e.At.Before(cutoff) {
					continue
				}
				if wantTool != "" && e.Tool != wantTool {
					continue
				}
				view.Entries = append(view.Entries, e)
				view.Count++
				view.CreditsMin += e.CreditsMin
				view.CreditsMax += e.CreditsMax
				if e.PerPin {
					view.PerPinCalls++
				}
				if e.Status == pgmcp.StatusUnconfirmed {
					view.Unconfirmed++
				}
				t := totals[e.Tool]
				if t == nil {
					t = &ledgerToolTotal{Tool: e.Tool}
					totals[e.Tool] = t
				}
				t.Calls++
				t.CreditsMin += e.CreditsMin
				t.CreditsMax += e.CreditsMax
			}
			for _, t := range totals {
				view.ByTool = append(view.ByTool, *t)
			}
			sort.Slice(view.ByTool, func(i, j int) bool { return view.ByTool[i].Tool < view.ByTool[j].Tool })
			if bad > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipped %d malformed ledger lines in %s\n", bad, path)
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			w := cmd.OutOrStdout()
			if view.Count == 0 {
				fmt.Fprintf(w, "No credit spends recorded in %s.\n", path)
				return nil
			}
			for _, e := range view.Entries {
				fmt.Fprintf(w, "%s  %-30s %s\n", e.At.Format(time.RFC3339), e.Tool, e.Keys["url"]+e.Keys["postUrl"]+e.Keys["automationId"])
			}
			fmt.Fprintf(w, "\n%d spends, %d-%d credits", view.Count, view.CreditsMin, view.CreditsMax)
			if view.PerPinCalls > 0 {
				fmt.Fprintf(w, " plus %d per-pin automation batches", view.PerPinCalls)
			}
			fmt.Fprintln(w)
			if view.Unconfirmed > 0 {
				fmt.Fprintf(w, "%d unconfirmed spends (call failed ambiguously); check list-scheduled-pins\n", view.Unconfirmed)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "Only spends newer than this (e.g. 7d, 24h, 2w)")
	cmd.Flags().StringVar(&tool, "tool", "", "Only spends for this tool (e.g. generate-and-schedule-pin)")
	return cmd
}
