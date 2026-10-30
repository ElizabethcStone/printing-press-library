// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"fmt"
	"math"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
)

type runwayAutomation struct {
	ID         string  `json:"id,omitempty"`
	Name       string  `json:"name"`
	Active     bool    `json:"active"`
	PinsPerDay float64 `json:"pins_per_day"`
	PinKind    string  `json:"pin_kind"` // ai | template | unknown
	WeeklyMin  float64 `json:"weekly_credits_min"`
	WeeklyMax  float64 `json:"weekly_credits_max"`
	Note       string  `json:"note,omitempty"`
}

type creditRunwayView struct {
	CreditsRemaining *float64           `json:"credits_remaining"`
	ActiveCount      int                `json:"active_automations"`
	WeeklyMin        float64            `json:"weekly_credits_min"`
	WeeklyMax        float64            `json:"weekly_credits_max"`
	DaysLeftBest     *float64           `json:"days_left_best_case"`
	DaysLeftWorst    *float64           `json:"days_left_worst_case"`
	Automations      []runwayAutomation `json:"automations"`
	FetchFailures    []pgFetchFailure   `json:"fetch_failures,omitempty"`
	Note             string             `json:"note,omitempty"`
}

// Per-pin credit prices from the tool cost table: template pins cost 1, AI
// pins 12-24 for the image (13-25 when scheduled through the AI tool).
var (
	templatePinCost = mustTool("generate_and_schedule_pin").Cost
	aiPinCost       = mustTool("generate_and_schedule_ai_pin").Cost
)

// runwayFor turns one automation object into its weekly burn estimate.
func runwayFor(a map[string]any) runwayAutomation {
	r := runwayAutomation{
		ID:   pgmcp.Str(a, "id", "automationId", "_id"),
		Name: pgmcp.Str(a, "name", "title"),
	}
	status := strings.ToLower(pgmcp.Str(a, "status", "state"))
	r.Active = status == "" || status == "active" || status == "running" || status == "enabled"
	if v, ok := a["active"].(bool); ok {
		r.Active = v
	}
	if v, ok := a["paused"].(bool); ok && v {
		r.Active = false
	}
	if ppd, ok := pgmcp.Num(a, "pinsPerDay", "pins_per_day", "settings/pinsPerDay"); ok {
		r.PinsPerDay = ppd
	} else {
		r.Note = "no pins-per-day field; excluded from burn"
	}
	kind := strings.ToLower(pgmcp.Str(a, "pinType", "generationType", "mode", "type"))
	// Whole tokens only: "daily" or "detail" must not read as "ai".
	tokens := map[string]bool{}
	for _, tok := range strings.FieldsFunc(kind, func(r rune) bool { return !(r >= 'a' && r <= 'z') }) {
		tokens[tok] = true
	}
	switch {
	case tokens["ai"] || pgmcp.Str(a, "designProfileName", "aiImageModel", "designProfile") != "":
		r.PinKind = "ai"
	case tokens["template"] || pgmcp.Str(a, "template", "templateId") != "":
		r.PinKind = "template"
	default:
		r.PinKind = "unknown"
	}
	if !r.Active || r.PinsPerDay <= 0 {
		return r
	}
	week := r.PinsPerDay * 7
	switch r.PinKind {
	case "ai":
		r.WeeklyMin, r.WeeklyMax = week*float64(aiPinCost.Min), week*float64(aiPinCost.Max)
	case "template":
		r.WeeklyMin, r.WeeklyMax = week*float64(templatePinCost.Min), week*float64(templatePinCost.Max)
	default:
		r.WeeklyMin, r.WeeklyMax = week*float64(templatePinCost.Min), week*float64(aiPinCost.Max)
		r.Note = "pin type not recognized; range spans template to AI pricing"
	}
	return r
}

func runway(automations []map[string]any, remaining *float64) creditRunwayView {
	view := creditRunwayView{CreditsRemaining: remaining, Automations: make([]runwayAutomation, 0, len(automations))}
	for _, a := range automations {
		r := runwayFor(a)
		if r.Active {
			view.ActiveCount++
		}
		view.WeeklyMin += r.WeeklyMin
		view.WeeklyMax += r.WeeklyMax
		view.Automations = append(view.Automations, r)
	}
	if remaining != nil && view.WeeklyMax > 0 {
		worst := round1(*remaining / (view.WeeklyMax / 7))
		view.DaysLeftWorst = &worst
		if view.WeeklyMin > 0 {
			best := round1(*remaining / (view.WeeklyMin / 7))
			view.DaysLeftBest = &best
		}
	}
	if view.ActiveCount == 0 {
		view.Note = "no active automations, so no recurring burn"
	}
	return view
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func newNovelCreditRunwayCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credit-runway",
		Short: "Estimate weekly credit burn from active automations and how many days of credit remain.",
		Long: `Use this command for the recurring automation burn forecast. Do NOT use it to price a one-off plan; use 'preflight' instead.

Joins list_automations (pins per day, pin type) with the balance from
get_account_status. Template pins cost 1 credit; AI pins cost 13-25, so burn
and days left are a best-case/worst-case range.`,
		Example:     "  pingenerator-pp-cli credit-runway --agent",
		Annotations: map[string]string{"pp:data-source": "live", "mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "credit-runway")
			}
			autos, err := pgCall(cmd, flags, "list_automations", nil)
			if err != nil {
				return err
			}
			var remaining *float64
			var failures []pgFetchFailure
			if acct, err := pgCall(cmd, flags, "get_account_status", nil); err != nil {
				failures = append(failures, pgFetchFailure{Tool: "get_account_status", Error: err.Error()})
			} else if rem, ok := creditsRemaining(pgmcp.Object(acct)); ok {
				remaining = &rem
			}
			view := runway(pgmcp.List(autos, "automations", "items", "data"), remaining)
			view.FetchFailures = failures
			if remaining == nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: credit balance unavailable; days left not computed")
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			w := cmd.OutOrStdout()
			for _, a := range view.Automations {
				state := "paused"
				if a.Active {
					state = "active"
				}
				fmt.Fprintf(w, "%-30s %-6s %4.0f pins/day  %-8s %.0f-%.0f credits/week\n", a.Name, state, a.PinsPerDay, a.PinKind, a.WeeklyMin, a.WeeklyMax)
			}
			fmt.Fprintf(w, "\nweekly burn: %.0f-%.0f credits\n", view.WeeklyMin, view.WeeklyMax)
			if view.DaysLeftWorst != nil {
				best := "n/a"
				if view.DaysLeftBest != nil {
					best = fmt.Sprintf("%.1f", *view.DaysLeftBest)
				}
				fmt.Fprintf(w, "days left: %.1f (worst case) to %s (best case)\n", *view.DaysLeftWorst, best)
			}
			return nil
		},
	}
	return cmd
}
