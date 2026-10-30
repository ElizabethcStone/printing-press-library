// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
)

// preflightStep is one planned group of tool calls.
type preflightStep struct {
	Tool              string `json:"tool"`
	Count             int    `json:"count"`
	PinterestProfile  string `json:"pinterest_profile,omitempty"`
	BoardID           string `json:"board_id,omitempty"`
	DesignProfileName string `json:"design_profile_name,omitempty"`
}

type preflightCheck struct {
	Check  string `json:"check"`
	Status string `json:"status"` // ok | warn | fail | unknown
	Detail string `json:"detail"`
}

type preflightView struct {
	Verdict          string           `json:"verdict"` // ready | check | blocked
	Steps            []preflightStep  `json:"steps"`
	CreditsMin       int              `json:"credits_min"`
	CreditsMax       int              `json:"credits_max"`
	PerPinBatches    int              `json:"per_pin_batches,omitempty"`
	CreditsRemaining *float64         `json:"credits_remaining"`
	Checks           []preflightCheck `json:"checks"`
	FetchFailures    []pgFetchFailure `json:"fetch_failures,omitempty"`
}

// preflightCost totals the planned credit range from the tool cost table.
func preflightCost(steps []preflightStep) (min, max, perPin int, err error) {
	for _, s := range steps {
		t, ok := pgmcp.Lookup(s.Tool)
		if !ok {
			return 0, 0, 0, fmt.Errorf("unknown tool %q (see 'tools')", s.Tool)
		}
		if s.Count < 1 {
			return 0, 0, 0, fmt.Errorf("%s: count must be at least 1", s.Tool)
		}
		if t.Cost.PerPin {
			perPin += s.Count
			continue
		}
		min += t.Cost.Min * s.Count
		max += t.Cost.Max * s.Count
	}
	return min, max, perPin, nil
}

// creditCheck compares the planned range with the balance.
func creditCheck(remaining float64, min, max, perPin int) preflightCheck {
	c := preflightCheck{Check: "credits"}
	switch {
	case remaining < float64(min):
		c.Status, c.Detail = "fail", fmt.Sprintf("%.0f credits remaining; plan needs at least %d", remaining, min)
	case remaining < float64(max):
		c.Status, c.Detail = "warn", fmt.Sprintf("%.0f credits remaining; plan costs %d-%d, so AI pricing could run out", remaining, min, max)
	case perPin > 0 && remaining <= 0:
		c.Status, c.Detail = "fail", fmt.Sprintf("no credits remaining; %d automation batches bill per pin", perPin)
	case perPin > 0:
		c.Status, c.Detail = "warn", fmt.Sprintf("%.0f credits remaining covers the fixed part (%d-%d); %d automation batches bill per pin", remaining, min, max, perPin)
	default:
		c.Status, c.Detail = "ok", fmt.Sprintf("%.0f credits remaining covers %d-%d", remaining, min, max)
	}
	return c
}

func preflightVerdict(checks []preflightCheck) string {
	verdict := "ready"
	for _, c := range checks {
		switch c.Status {
		case "fail":
			return "blocked"
		case "warn", "unknown":
			verdict = "check"
		}
	}
	return verdict
}

func readPlan(path string) ([]preflightStep, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var steps []preflightStep
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var s preflightStep
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("%s line %d: %v", path, n, err)
		}
		if s.Count == 0 {
			s.Count = 1
		}
		steps = append(steps, s)
	}
	return steps, sc.Err()
}

func newNovelPreflightCmd(flags *rootFlags) *cobra.Command {
	var profile, boardID, tool, designProfile, planPath string
	var count int

	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Price a planned batch in credits and confirm the profile, board and design profile exist before spending anything.",
		Long: `Use this command to validate and price a plan before running it. Do NOT use it for an ongoing automation burn forecast; use 'credit-runway' instead.

Totals the plan's credit range from the tool cost table, then reads (free) the
account balance, connected profiles, the profile's boards and design profiles
to confirm every reference exists. It never calls a credit-spending tool.
--plan takes JSON lines: {"tool":"generate-and-schedule-ai-pin","count":5,
"pinterest_profile":"myprofile","board_id":"123","design_profile_name":"Brand"}.
Verdict: ready (all ok), check (warnings or unknowns), blocked (a check failed).`,
		Example:     "  pingenerator-pp-cli preflight --pinterest-profile myprofile --board-id 992269799088633298 --tool generate-and-schedule-ai-pin --count 5 --agent",
		Annotations: map[string]string{"pp:data-source": "live", "mcp:read-only": "true", "mcp:read-flags": "plan"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "preflight")
			}
			var steps []preflightStep
			if planPath != "" {
				var err error
				if steps, err = readPlan(planPath); err != nil {
					return usageErr(fmt.Errorf("--plan: %w", err))
				}
			}
			if cmd.Flags().Changed("tool") || cmd.Flags().Changed("count") || profile != "" || boardID != "" || designProfile != "" || len(steps) == 0 {
				steps = append(steps, preflightStep{Tool: tool, Count: count, PinterestProfile: profile, BoardID: boardID, DesignProfileName: designProfile})
			}
			min, max, perPin, err := preflightCost(steps)
			if err != nil {
				return usageErr(err)
			}
			view := preflightView{Steps: steps, CreditsMin: min, CreditsMax: max, PerPinBatches: perPin, Checks: make([]preflightCheck, 0)}
			fail := func(tool string, err error) {
				view.FetchFailures = append(view.FetchFailures, pgFetchFailure{Tool: tool, Error: err.Error()})
			}

			if acct, err := pgCall(cmd, flags, "get_account_status", nil); err != nil {
				// A bad key fails every later read too; report it as auth (exit 4)
				// instead of a page of "unknown" checks.
				var ce *cliError
				if errors.As(err, &ce) && ce.code == 4 {
					return err
				}
				fail("get_account_status", err)
				view.Checks = append(view.Checks, preflightCheck{Check: "credits", Status: "unknown", Detail: "could not read account status"})
			} else if rem, ok := creditsRemaining(pgmcp.Object(acct)); ok {
				view.CreditsRemaining = &rem
				view.Checks = append(view.Checks, creditCheck(rem, min, max, perPin))
			} else {
				view.Checks = append(view.Checks, preflightCheck{Check: "credits", Status: "unknown", Detail: "account status had no recognizable credit balance field"})
			}

			profiles := map[string]bool{}
			boards := map[string]map[string]bool{}
			designs := map[string]bool{}
			for _, s := range steps {
				if s.PinterestProfile != "" {
					profiles[s.PinterestProfile] = true
					if s.BoardID != "" {
						if boards[s.PinterestProfile] == nil {
							boards[s.PinterestProfile] = map[string]bool{}
						}
						boards[s.PinterestProfile][s.BoardID] = true
					}
				}
				if s.DesignProfileName != "" {
					designs[s.DesignProfileName] = true
				}
			}

			if len(profiles) > 0 {
				known := map[string]bool{}
				inactive := map[string]bool{}
				if payload, err := pgCall(cmd, flags, "list_pinterest_profiles", nil); err != nil {
					fail("list_pinterest_profiles", err)
					known = nil
				} else {
					for _, p := range pgmcp.List(payload, "connectedPinterestProfiles", "profiles", "pinterestProfiles", "accounts", "items") {
						off := false
						if v, ok := p["isActive"].(bool); ok && !v {
							off = true
						}
						for _, k := range []string{"username", "name", "profile", "id"} {
							if s := pgmcp.Str(p, k); s != "" {
								known[strings.ToLower(s)] = true
								if off {
									inactive[strings.ToLower(s)] = true
								}
							}
						}
					}
				}
				for _, name := range sortedKeys(profiles) {
					c := existsCheck("pinterest profile "+name, known, strings.ToLower(name))
					if c.Status == "ok" && inactive[strings.ToLower(name)] {
						c.Status, c.Detail = "warn", "connected but marked inactive; reconnect it in Pin Generator before scheduling"
					}
					view.Checks = append(view.Checks, c)
				}
			}
			for _, prof := range sortedKeys(boardsKeys(boards)) {
				known := map[string]bool{}
				if payload, err := pgCall(cmd, flags, "list_boards", map[string]any{"pinterestProfile": prof}); err != nil {
					fail("list_boards", err)
					known = nil
				} else {
					for _, b := range pgmcp.List(payload, "boards", "items", "data") {
						if s := pgmcp.Str(b, "id", "boardId", "board_id"); s != "" {
							known[s] = true
						}
					}
				}
				for _, id := range sortedKeys(boards[prof]) {
					view.Checks = append(view.Checks, existsCheck("board "+id+" on "+prof, known, id))
				}
			}
			if len(designs) > 0 {
				known := map[string]bool{}
				if payload, err := pgCall(cmd, flags, "list_design_profiles", nil); err != nil {
					fail("list_design_profiles", err)
					known = nil
				} else {
					for _, d := range pgmcp.List(payload, "designProfiles", "profiles", "items", "data") {
						if s := pgmcp.Str(d, "name", "designProfileName"); s != "" {
							known[strings.ToLower(s)] = true
						}
					}
				}
				for _, name := range sortedKeys(designs) {
					view.Checks = append(view.Checks, existsCheck("design profile "+name, known, strings.ToLower(name)))
				}
			}
			view.Verdict = preflightVerdict(view.Checks)
			if len(view.FetchFailures) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d of the preflight reads failed; affected checks are marked unknown\n", len(view.FetchFailures))
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "plan cost: %d-%d credits", view.CreditsMin, view.CreditsMax)
			if perPin > 0 {
				fmt.Fprintf(w, " + %d per-pin automation batches", perPin)
			}
			fmt.Fprintln(w)
			for _, c := range view.Checks {
				fmt.Fprintf(w, "  %-7s %s: %s\n", strings.ToUpper(c.Status), c.Check, c.Detail)
			}
			fmt.Fprintf(w, "verdict: %s\n", view.Verdict)
			return nil
		},
	}
	cmd.Flags().StringVar(&tool, "tool", "generate-and-schedule-pin", "Tool the plan will run (command or MCP name, see 'tools')")
	cmd.Flags().IntVar(&count, "count", 1, "How many times the plan runs the tool")
	cmd.Flags().StringVar(&profile, "pinterest-profile", "", "Pinterest profile the plan schedules to")
	cmd.Flags().StringVar(&boardID, "board-id", "", "Board ID the plan schedules into")
	cmd.Flags().StringVar(&designProfile, "design-profile-name", "", "Design profile the plan uses")
	cmd.Flags().StringVar(&planPath, "plan", "", "JSON-lines plan file (one step per line)")
	return cmd
}

// existsCheck reports whether want is among known; a nil known means the
// list could not be read.
func existsCheck(label string, known map[string]bool, want string) preflightCheck {
	switch {
	case known == nil:
		return preflightCheck{Check: label, Status: "unknown", Detail: "could not read the list to confirm"}
	case known[want]:
		return preflightCheck{Check: label, Status: "ok", Detail: "exists"}
	default:
		return preflightCheck{Check: label, Status: "fail", Detail: "not found on this account"}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func boardsKeys(m map[string]map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}
