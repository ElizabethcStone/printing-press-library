// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
)

type unpinnedPost struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type unpinnedPostsView struct {
	WebsiteURL      string           `json:"website_url"`
	ScannedPosts    int              `json:"scanned_posts"`
	ScheduledPins   int              `json:"scheduled_pins_seen"`
	QueuedPins      int              `json:"queued_pins_seen"`
	LedgeredURLs    int              `json:"ledgered_urls"`
	MatchedByURL    int              `json:"matched_by_url"`
	MatchedByTitle  int              `json:"matched_by_title"`
	MatchedByLedger int              `json:"matched_by_ledger"`
	Unpinned        []unpinnedPost   `json:"unpinned"`
	FetchFailures   []pgFetchFailure `json:"fetch_failures,omitempty"`
	Note            string           `json:"note,omitempty"`
}

func newNovelUnpinnedPostsCmd(flags *rootFlags) *cobra.Command {
	var websiteURL string
	var limit int

	cmd := &cobra.Command{
		Use:   "unpinned-posts",
		Short: "List sitemap posts that have no scheduled pin and no ledgered spend.",
		Long: `Use this command to find sitemap posts that have no scheduled or ledgered pin. Do NOT use it to audit spending; use 'ledger' instead.

Joins find_website_posts with list_scheduled_pins and this CLI's spend ledger.
Pins queued for a timeslot count as pinned too. A post counts as pinned when a pin links to the same URL (host and
path, ignoring query, "www." and trailing slash), or, failing a URL, carries
the same normalized title.`,
		Example:     "  pingenerator-pp-cli unpinned-posts --website-url https://example.com --limit 50 --agent",
		Annotations: map[string]string{"pp:data-source": "live", "mcp:read-only": "true", "pp:happy-args": pgDogfoodHappyArgs("--website-url=example-value", "--website-url="+pgEnvFixture, pgTestWebsiteURL)},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "unpinned-posts")
			}
			websiteURL = pgFixtureValue(websiteURL, pgTestWebsiteURL)
			if websiteURL == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--website-url is required"))
			}
			req := map[string]any{"websiteUrl": websiteURL}
			if limit > 0 {
				req["limit"] = limit
			}
			postsPayload, err := pgCall(cmd, flags, "find_website_posts", req)
			if err != nil {
				return err
			}
			view := unpinnedPostsView{WebsiteURL: websiteURL, Unpinned: make([]unpinnedPost, 0)}

			pinnedURL := map[string]bool{}
			pinnedTitle := map[string]bool{}
			pinsPayload, err := pgCall(cmd, flags, "list_scheduled_pins", nil)
			if err != nil {
				// Without the queue every post would look unpinned; refuse to guess.
				return fmt.Errorf("reading scheduled pins: %w", err)
			}
			scheduled, queued := scheduledAndQueued(pinsPayload)
			view.ScheduledPins, view.QueuedPins = len(scheduled), len(queued)
			// Queued pins already exist; counting them prevents re-pinning a
			// post whose pin is only waiting for a timeslot.
			for _, pin := range append(scheduled, queued...) {
				// Title is only a fallback for pins with no URL: a pin that links
				// elsewhere must not mark a same-titled post as pinned.
				if u := pgmcp.NormalizeURL(pgmcp.Str(pin, "url", "link", "destinationUrl", "destinationURL", "outboundUrl", "pin/url")); u != "" {
					pinnedURL[u] = true
				} else if t := pgmcp.NormalizeTitle(pgmcp.Str(pin, "title", "pinTitle", "name", "pin/title")); t != "" {
					pinnedTitle[t] = true
				}
			}
			ledgered := ledgeredURLs(cmd)
			view.LedgeredURLs = len(ledgered)

			for _, post := range pgmcp.List(postsPayload, "posts", "items", "urls", "data") {
				view.ScannedPosts++
				u := pgmcp.Str(post, "url", "loc", "link", "postUrl")
				title := pgmcp.Str(post, "title", "name")
				nu := pgmcp.NormalizeURL(u)
				switch {
				case nu != "" && pinnedURL[nu]:
					view.MatchedByURL++
				case nu != "" && ledgered[nu]:
					view.MatchedByLedger++
				case title != "" && pinnedTitle[pgmcp.NormalizeTitle(title)]:
					view.MatchedByTitle++
				default:
					view.Unpinned = append(view.Unpinned, unpinnedPost{URL: u, Title: title})
				}
			}
			if view.ScannedPosts == 0 {
				view.Note = "the sitemap returned no posts; check --website-url"
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			w := cmd.OutOrStdout()
			if len(view.Unpinned) == 0 {
				fmt.Fprintf(w, "All %d posts already have a pin.\n", view.ScannedPosts)
				return nil
			}
			for _, p := range view.Unpinned {
				fmt.Fprintf(w, "%s\t%s\n", p.URL, strings.TrimSpace(p.Title))
			}
			fmt.Fprintf(w, "\n%d of %d posts have no pin (%d scheduled pins checked)\n", len(view.Unpinned), view.ScannedPosts, view.ScheduledPins)
			return nil
		},
	}
	cmd.Flags().StringVar(&websiteURL, "website-url", "", "Website whose sitemap to check")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum sitemap posts to read (0 = server default)")
	return cmd
}
