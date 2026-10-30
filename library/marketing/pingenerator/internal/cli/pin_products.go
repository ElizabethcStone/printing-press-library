// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
)

type pinProductPlan struct {
	Product string          `json:"product"`
	URL     string          `json:"url,omitempty"`
	Image   string          `json:"image,omitempty"`
	Skipped string          `json:"skipped,omitempty"`
	Request *pgmcp.Request  `json:"request,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type pinProductsView struct {
	StoreID         string           `json:"store_id"`
	Tool            string           `json:"tool"`
	CreditsEach     string           `json:"credits_each"`
	CreditsMin      int              `json:"credits_min_total"`
	CreditsMax      int              `json:"credits_max_total"`
	ScannedProducts int              `json:"scanned_products"`
	ScannedPages    int              `json:"scanned_pages"`
	Planned         int              `json:"planned"`
	Executed        int              `json:"executed"`
	Items           []pinProductPlan `json:"items"`
	FetchFailures   []pgFetchFailure `json:"fetch_failures,omitempty"`
	Note            string           `json:"note,omitempty"`
}

func newNovelPinProductsCmd(flags *rootFlags) *cobra.Command {
	var storeID, search, profile, boardID, template, designProfile string
	var aiBoard, useAI, execute, force bool
	var limit, maxScanPages, startPage int

	cmd := &cobra.Command{
		Use:   "pin-products",
		Short: "Turn a connected store's products into a batch of scheduled pins, skipping products already pinned.",
		Long: `Use this command to pin products from a connected store. Do NOT use it for blog posts; use 'pin-post' instead.

Pages through list_store_products (free) and plans one scheduled pin per
product from its title, first photo and link. Without --execute it prints the
plan and the total credit cost and spends nothing. Products whose link is
already in the spend ledger are skipped unless --force. With --execute it
stops at the first out-of-credits or auth error.`,
		Example: strings.Trim(`
  pingenerator-pp-cli pin-products --store-id store_123 --search linen --pinterest-profile myprofile --board-id 992269799088633298 --agent
  pingenerator-pp-cli pin-products --store-id store_123 --pinterest-profile myprofile --use-ai-board-selection --limit 5 --execute`, "\n"),
		Annotations: map[string]string{"pp:data-source": "live", "pp:preview-happy-path": "true", "pp:happy-args": pgDogfoodHappyArgs(
			"--store-id=example-value;--pinterest-profile=example-value;--use-ai-board-selection",
			"--store-id="+pgEnvFixture+";--pinterest-profile="+pgEnvFixture+";--use-ai-board-selection",
			pgTestStoreID, pgTestProfile)},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "pin-products")
			}
			storeID, profile = pgFixtureValue(storeID, pgTestStoreID), pgFixtureValue(profile, pgTestProfile)
			if storeID == "" || profile == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--store-id and --pinterest-profile are required"))
			}
			if err := boardTarget(boardID, aiBoard); err != nil {
				_ = cmd.Usage()
				return usageErr(err)
			}
			if limit < 1 || maxScanPages < 1 {
				return usageErr(fmt.Errorf("--limit and --max-scan-pages must be at least 1"))
			}
			if cliutil.IsDogfoodEnv() && maxScanPages > 1 {
				maxScanPages = 1
			}

			done := map[string]bool{}
			if !force {
				done = ledgeredURLs(cmd)
			}
			seen := map[string]bool{}
			var readErr error
			sample, _ := scheduleArgs(useAI, template, "", "", "", "", profile, boardID, aiBoard, designProfile)
			view := pinProductsView{StoreID: storeID, Tool: sample.Name, CreditsEach: sample.Cost.Note(), Items: make([]pinProductPlan, 0)}

			for page := startPage; page < startPage+maxScanPages && view.Planned < limit; page++ {
				req := map[string]any{"storeId": storeID, "page": page}
				if search != "" {
					req["search"] = search
				}
				payload, err := pgCall(cmd, flags, "list_store_products", req)
				if err != nil {
					if view.ScannedPages == 0 {
						return err
					}
					// A later page failed: keep what was planned, but report it
					// and exit non-zero so an incomplete scan is never silent.
					view.FetchFailures = append(view.FetchFailures, pgFetchFailure{Tool: "list_store_products", Error: err.Error()})
					readErr = err
					break
				}
				view.ScannedPages++
				products := pgmcp.List(payload, "products", "items", "data", "results")
				fresh := 0
				for _, p := range products {
					key := pgmcp.Str(p, "id", "productId") + "|" + pgmcp.Str(p, "title", "name") + "|" + pgmcp.Str(p, "url", "link", "productUrl", "onlineStoreUrl", "permalink")
					if !seen[key] {
						seen[key] = true
						fresh++
					}
				}
				// An empty page, or a server that repeats the last page for
				// out-of-range page numbers, ends the scan.
				if fresh == 0 {
					break
				}
				for _, p := range products {
					view.ScannedProducts++
					title := pgmcp.Str(p, "title", "name", "productTitle")
					link := pgmcp.Str(p, "url", "link", "productUrl", "onlineStoreUrl", "permalink")
					nu := pgmcp.NormalizeURL(link)
					item := pinProductPlan{Product: title, URL: link, Image: firstImage(p)}
					if title == "" {
						item.Skipped = "product has no title"
					} else if nu == "" {
						// Without a link there is nothing to dedupe on, so a
						// re-run would pay again; skip instead.
						item.Skipped = "product has no link; pin it with generate-and-schedule-pin"
					} else if done[nu] {
						item.Skipped = "already pinned by this CLI (see 'ledger') or planned earlier in this run; pass --force to repeat ledgered products"
					} else if view.Planned >= limit {
						break
					} else {
						_, toolArgs := scheduleArgs(useAI, template, title, pgmcp.Str(p, "description", "body"), item.Image, link, profile, boardID, aiBoard, designProfile)
						r := pgmcp.BuildRequest(sample.Name, toolArgs)
						item.Request = &r
						done[nu] = true
						view.Planned++
						view.CreditsMin += sample.Cost.Min
						view.CreditsMax += sample.Cost.Max
					}
					view.Items = append(view.Items, item)
				}
			}
			if view.Planned == 0 {
				view.Note = fmt.Sprintf("no new products to pin after scanning %d products across %d pages; raise --max-scan-pages or pass --force", view.ScannedProducts, view.ScannedPages)
			}

			// runErr carries the first failure so the exit code reflects it
			// (4 auth, 5 API/no credits, 7 rate limit) after the report prints.
			runErr := readErr
			if execute && view.Planned > 0 {
				for i := range view.Items {
					it := &view.Items[i]
					if it.Request == nil {
						continue
					}
					payload, previewed, skipped, err := pgSpendLocked(cmd, flags, sample, it.Request.Params.Arguments, it.URL, force)
					if previewed {
						return err
					}
					if skipped {
						it.Skipped, it.Request = "another run of this CLI pinned this product first (see 'ledger')", nil
						continue
					}
					if err != nil {
						it.Error = err.Error()
						if runErr == nil {
							runErr = err
						}
						var ce *cliError
						if errors.As(err, &ce) && (ce.code == 4 || ce.code == 7 || strings.Contains(err.Error(), "no credits")) {
							break
						}
						continue
					}
					it.Result = payload
					view.Executed++
				}
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				if err := printJSONFiltered(cmd.OutOrStdout(), view, flags); err != nil {
					return err
				}
				return runErr
			}
			w := cmd.OutOrStdout()
			for _, it := range view.Items {
				switch {
				case it.Skipped != "":
					fmt.Fprintf(w, "skip  %s (%s)\n", it.Product, it.Skipped)
				case it.Error != "":
					fmt.Fprintf(w, "FAIL  %s: %s\n", it.Product, it.Error)
				case it.Result != nil:
					fmt.Fprintf(w, "done  %s\n", it.Product)
				default:
					fmt.Fprintf(w, "plan  %s\n", it.Product)
				}
			}
			fmt.Fprintf(w, "\n%d planned, %d executed, credits %d-%d (%s each)\n", view.Planned, view.Executed, view.CreditsMin, view.CreditsMax, view.CreditsEach)
			for _, f := range view.FetchFailures {
				fmt.Fprintf(w, "INCOMPLETE: reading more products failed after %d pages: %s\n", view.ScannedPages, f.Error)
			}
			if !execute && view.Planned > 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "dry run: nothing was spent; re-run with --execute to schedule these pins")
			}
			return runErr
		},
	}
	cmd.Flags().StringVar(&storeID, "store-id", "", "Store ID (see list-connected-stores)")
	cmd.Flags().StringVar(&search, "search", "", "Only products matching this search term")
	cmd.Flags().StringVar(&profile, "pinterest-profile", "", "Connected Pinterest username (see list-pinterest-profiles)")
	cmd.Flags().StringVar(&boardID, "board-id", "", "Board ID to pin into (see list-boards); required unless --use-ai-board-selection")
	cmd.Flags().BoolVar(&aiBoard, "use-ai-board-selection", false, "Let Pin Generator choose or create the best board")
	cmd.Flags().StringVar(&template, "template", "RandomTemplate", "Template for template pins (see list-templates)")
	cmd.Flags().BoolVar(&useAI, "ai", false, "Generate fully AI pins instead of template pins (13-25 credits each)")
	cmd.Flags().StringVar(&designProfile, "design-profile-name", "", "Design profile for --ai pins")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum pins to plan")
	cmd.Flags().IntVar(&maxScanPages, "max-scan-pages", 5, "Maximum product pages to read")
	cmd.Flags().IntVar(&startPage, "page", 1, "First product page to read")
	cmd.Flags().BoolVar(&execute, "execute", false, "Actually spend credits and schedule the planned pins")
	cmd.Flags().BoolVar(&force, "force", false, "Include products the ledger shows were already pinned")
	return cmd
}
