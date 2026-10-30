// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live
// Hand-authored helpers shared by the Pin Generator novel commands.

package cli

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
)

// pgFetchFailure records one failed read inside a multi-read command so the
// aggregate is never computed over silent zeros.
type pgFetchFailure struct {
	Tool  string `json:"tool"`
	Error string `json:"error"`
}

func mustTool(name string) pgmcp.Tool {
	t, ok := pgmcp.Lookup(name)
	if !ok {
		panic("pingenerator: unknown tool " + name)
	}
	return t
}

// creditsRemaining reads the remaining balance from get_account_status.
// The live response (confirmed 2026-10-29) uses remainingPinCredits,
// pinCreditLimit and usedPinCredits; older spellings stay as fallbacks. A
// zero balance is a real value, not a missing one.
func creditsRemaining(acct map[string]any) (float64, bool) {
	if v, ok := pgmcp.Num(acct, "remainingPinCredits", "creditsRemaining", "remainingCredits", "credits_remaining", "credits/remaining", "availableCredits", "credits/available"); ok {
		return v, true
	}
	limit, okL := pgmcp.Num(acct, "pinCreditLimit", "creditLimit", "creditsLimit", "credits/limit", "monthlyCredits", "credits/total", "totalCredits")
	used, okU := pgmcp.Num(acct, "usedPinCredits", "creditsUsed", "usedCredits", "credits/used", "creditUsage", "credits_used")
	if okL && okU {
		return math.Max(0, limit-used), true
	}
	if v, ok := pgmcp.Num(acct, "credits"); ok {
		return v, true
	}
	return 0, false
}

// scheduledAndQueued splits a list_scheduled_pins reply. The live shape
// (confirmed 2026-10-29) is {"scheduledPins":[...],
// "queuedPinsAwaitingTimeslot":[...]}; queued pins exist but have no time
// yet. Other shapes fall back to the first recognizable list as scheduled.
func scheduledAndQueued(payload json.RawMessage) (scheduled, queued []map[string]any) {
	scheduled = pgmcp.ListAt(payload, "scheduledPins")
	queued = pgmcp.ListAt(payload, "queuedPinsAwaitingTimeslot")
	if scheduled == nil && queued == nil {
		scheduled = pgmcp.List(payload, "pins", "items", "data")
	}
	return scheduled, queued
}

// firstImage pulls a usable image URL from post or product content.
func firstImage(m map[string]any) string {
	if s := pgmcp.Str(m, "image", "imageUrl", "imageURL", "featuredImage", "photo", "thumbnail", "image/url", "image/src"); s != "" {
		return s
	}
	for _, key := range []string{"images", "photos", "imageUrls"} {
		arr, ok := m[key].([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		switch v := arr[0].(type) {
		case string:
			return v
		case map[string]any:
			if s := pgmcp.Str(v, "url", "src", "imageUrl"); s != "" {
				return s
			}
		}
	}
	return ""
}

// boardTarget validates the board choice shared by pin-post and pin-products.
func boardTarget(boardID string, aiBoard bool) error {
	if boardID == "" && !aiBoard {
		return fmt.Errorf("--board-id is required unless --use-ai-board-selection is set")
	}
	return nil
}

// scheduleArgs builds the tool arguments for one pin from extracted content.
func scheduleArgs(useAI bool, template, title, desc, image, link, profile, boardID string, aiBoard bool, designProfile string) (pgmcp.Tool, map[string]any) {
	args := map[string]any{"title": title, "pinterestProfile": profile}
	if desc != "" {
		args["description"] = desc
	}
	if link != "" {
		args["url"] = link
	}
	if boardID != "" {
		args["boardId"] = boardID
	}
	if aiBoard {
		args["useAIBoardSelection"] = true
	}
	if useAI {
		if designProfile != "" {
			args["designProfileName"] = designProfile
		}
		if image != "" {
			args["referenceImageUrls"] = []string{image}
		}
		return mustTool("generate_and_schedule_ai_pin"), args
	}
	args["template"] = template
	if image != "" {
		args["imageURL"] = image
	}
	return mustTool("generate_and_schedule_pin"), args
}

// ledgeredURLs loads normalized URLs already spent on; ledger trouble is a
// warning, not a failure, because skipping is an optimization.
func ledgeredURLs(cmd *cobra.Command) map[string]bool {
	path, err := pgLedgerPath()
	if err != nil {
		return map[string]bool{}
	}
	entries, _, err := pgmcp.ReadLedger(path)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not read ledger %s: %v\n", path, err)
		return map[string]bool{}
	}
	return pgmcp.LedgeredURLs(entries)
}
