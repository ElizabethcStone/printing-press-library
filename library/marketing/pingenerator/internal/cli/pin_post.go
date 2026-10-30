// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
)

type pinPostView struct {
	PostURL     string          `json:"post_url"`
	Tool        string          `json:"tool"`
	Credits     string          `json:"credits"`
	Executed    bool            `json:"executed"`
	Skipped     bool            `json:"skipped,omitempty"`
	SkipReason  string          `json:"skip_reason,omitempty"`
	Request     pgmcp.Request   `json:"request"`
	Result      json.RawMessage `json:"result,omitempty"`
	Would       string          `json:"would,omitempty"`
	MissingData []string        `json:"missing_data,omitempty"`
}

func newNovelPinPostCmd(flags *rootFlags) *cobra.Command {
	var postURL, profile, boardID, template, designProfile string
	var aiBoard, useAI, execute, force bool

	cmd := &cobra.Command{
		Use:   "pin-post [post-url]",
		Short: "Turn a blog post URL into a scheduled Pinterest pin in one command, previewed for free before any credit is spent.",
		Long: `Use this command to turn blog or website post URLs into scheduled pins. Do NOT use it for store products; use 'pin-products' instead.

Reads the post (free), maps its title, description, first image and URL into
generate_and_schedule_pin (template, 1 credit) or, with --ai,
generate_and_schedule_ai_pin (13-25 credits). Without --execute it prints the
resolved request and spends nothing. Posts already in the spend ledger are
skipped unless --force.`,
		Example: strings.Trim(`
  pingenerator-pp-cli pin-post https://example.com/best-beaches --pinterest-profile myprofile --board-id 992269799088633298 --agent
  pingenerator-pp-cli pin-post https://example.com/best-beaches --pinterest-profile myprofile --use-ai-board-selection --ai --execute`, "\n"),
		Annotations: map[string]string{"pp:data-source": "live", "pp:preview-happy-path": "true", "pp:happy-args": pgDogfoodHappyArgs(
			"post-url=example-value;--pinterest-profile=example-value;--board-id=example-value",
			"post-url="+pgEnvFixture+";--pinterest-profile="+pgEnvFixture+";--use-ai-board-selection",
			pgTestPostURL, pgTestProfile)},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "pin-post")
			}
			if len(args) > 1 {
				return usageErr(fmt.Errorf("pin-post takes one post URL"))
			}
			if len(args) == 1 {
				if postURL != "" && postURL != args[0] {
					return usageErr(fmt.Errorf("give the post URL once: positional or --post-url"))
				}
				postURL = args[0]
			}
			postURL, profile = pgFixtureValue(postURL, pgTestPostURL), pgFixtureValue(profile, pgTestProfile)
			if postURL == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a post URL is required (positional or --post-url)"))
			}
			if profile == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--pinterest-profile is required"))
			}
			if err := boardTarget(boardID, aiBoard); err != nil {
				_ = cmd.Usage()
				return usageErr(err)
			}

			content, err := pgCall(cmd, flags, "get_post_content", map[string]any{"postUrl": postURL})
			if err != nil {
				return err
			}
			post := pgmcp.Object(content)
			if post == nil {
				post = map[string]any{}
			}
			title := pgmcp.Str(post, "title", "postTitle", "name", "post/title")
			desc := pgmcp.Str(post, "description", "excerpt", "metaDescription", "post/description")
			image := firstImage(post)
			link := pgmcp.Str(post, "url", "postUrl", "link", "canonicalUrl")
			if link == "" {
				link = postURL
			}
			view := pinPostView{PostURL: postURL}
			if title == "" {
				view.MissingData = append(view.MissingData, "title")
				title = postURL
			}
			if image == "" && !useAI {
				view.MissingData = append(view.MissingData, "image")
			}

			tool, toolArgs := scheduleArgs(useAI, template, title, desc, image, link, profile, boardID, aiBoard, designProfile)
			view.Tool, view.Credits, view.Request = tool.Name, tool.Cost.Note(), pgmcp.BuildRequest(tool.Name, toolArgs)

			if !force && ledgeredURLs(cmd)[pgmcp.NormalizeURL(link)] {
				view.Skipped, view.SkipReason = true, "post already pinned by this CLI (see 'ledger'); pass --force to pin again"
				return printPinPost(cmd, flags, view)
			}
			if !execute {
				view.Would = "spend " + tool.Cost.Note() + " and schedule this pin; re-run with --execute"
				return printPinPost(cmd, flags, view)
			}
			payload, previewed, skipped, err := pgSpendLocked(cmd, flags, tool, toolArgs, link, force)
			if err != nil || previewed {
				return err
			}
			if skipped {
				view.Skipped, view.SkipReason = true, "another run of this CLI pinned this post first (see 'ledger'); pass --force to pin again"
				return printPinPost(cmd, flags, view)
			}
			view.Executed, view.Result = true, payload
			return printPinPost(cmd, flags, view)
		},
	}
	cmd.Flags().StringVar(&postURL, "post-url", "", "Post URL to pin (alternative to the positional argument)")
	cmd.Flags().StringVar(&profile, "pinterest-profile", "", "Connected Pinterest username (see list-pinterest-profiles)")
	cmd.Flags().StringVar(&boardID, "board-id", "", "Board ID to pin into (see list-boards); required unless --use-ai-board-selection")
	cmd.Flags().BoolVar(&aiBoard, "use-ai-board-selection", false, "Let Pin Generator choose or create the best board")
	cmd.Flags().StringVar(&template, "template", "RandomTemplate", "Template for template pins (see list-templates)")
	cmd.Flags().BoolVar(&useAI, "ai", false, "Generate a fully AI pin instead of a template pin (13-25 credits)")
	cmd.Flags().StringVar(&designProfile, "design-profile-name", "", "Design profile for --ai pins (see list-design-profiles)")
	cmd.Flags().BoolVar(&execute, "execute", false, "Actually spend credits and schedule the pin")
	cmd.Flags().BoolVar(&force, "force", false, "Pin even if the ledger shows this post was already pinned")
	return cmd
}

func printPinPost(cmd *cobra.Command, flags *rootFlags, v pinPostView) error {
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return printJSONFiltered(cmd.OutOrStdout(), v, flags)
	}
	w := cmd.OutOrStdout()
	switch {
	case v.Skipped:
		fmt.Fprintf(w, "skipped %s: %s\n", v.PostURL, v.SkipReason)
	case v.Executed:
		fmt.Fprintf(w, "scheduled %s via %s (%s)\n%s\n", v.PostURL, v.Tool, v.Credits, string(v.Result))
	default:
		b, _ := json.MarshalIndent(v.Request, "", "  ")
		fmt.Fprintf(w, "%s\n", b)
		fmt.Fprintf(cmd.ErrOrStderr(), "\ndry run: would %s.\n", v.Would)
	}
	if len(v.MissingData) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "note: post content had no %s\n", strings.Join(v.MissingData, ", "))
	}
	return nil
}
