// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live
// Rendered from internal/pgmcp.Tools by TestPGStaticToolsUpToDate; do not edit by hand.
// Refresh with: PG_UPDATE_STATIC=1 go test ./internal/cli -run TestPGStaticToolsUpToDate

package cli

import "github.com/spf13/cobra"

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newPGToolGetAccountStatusCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolListPinterestProfilesCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolListBoardsCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolListScheduledPinsCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolFindWebsitePostsCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolGetPostContentCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolListConnectedStoresCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolListStoreProductsCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolListTemplatesCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolGeneratePinImageCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolGenerateAndSchedulePinCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolSchedulePinCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolListDesignProfilesCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolSetupDesignProfileCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolUpdateDesignProfileCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolGenerateAiPinImageCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolGenerateAndScheduleAiPinCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolListAutomationsCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolCreateAutomationCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolRunAutomationBatchCmd(flags))
		addNovelCommandIfAbsent(root, newPGToolGetAutomationBatchStatusCmd(flags))
	})
}

func newPGToolGetAccountStatusCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "get-account-status"}
	return pgWireToolCmd(flags, "get_account_status", cmd)
}

func newPGToolListPinterestProfilesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "list-pinterest-profiles"}
	return pgWireToolCmd(flags, "list_pinterest_profiles", cmd)
}

func newPGToolListBoardsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "list-boards"}
	var flagPinterestProfile string
	cmd.Flags().StringVar(&flagPinterestProfile, "pinterest-profile", "", "(required) Pinterest username (see list-pinterest-profiles).")
	return pgWireToolCmd(flags, "list_boards", cmd)
}

func newPGToolListScheduledPinsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "list-scheduled-pins"}
	return pgWireToolCmd(flags, "list_scheduled_pins", cmd)
}

func newPGToolFindWebsitePostsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "find-website-posts"}
	var flagWebsiteUrl string
	cmd.Flags().StringVar(&flagWebsiteUrl, "website-url", "", "(required) Website URL to read the sitemap of.")
	var flagLimit int
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Maximum posts to return.")
	return pgWireToolCmd(flags, "find_website_posts", cmd)
}

func newPGToolGetPostContentCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "get-post-content"}
	var flagPostUrl string
	cmd.Flags().StringVar(&flagPostUrl, "post-url", "", "(required) URL of the post to read.")
	return pgWireToolCmd(flags, "get_post_content", cmd)
}

func newPGToolListConnectedStoresCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "list-connected-stores"}
	return pgWireToolCmd(flags, "list_connected_stores", cmd)
}

func newPGToolListStoreProductsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "list-store-products"}
	var flagStoreId string
	cmd.Flags().StringVar(&flagStoreId, "store-id", "", "(required) Store ID (see list-connected-stores).")
	var flagSearch string
	cmd.Flags().StringVar(&flagSearch, "search", "", "Filter products by search term.")
	var flagPage int
	cmd.Flags().IntVar(&flagPage, "page", 0, "Page number for pagination.")
	return pgWireToolCmd(flags, "list_store_products", cmd)
}

func newPGToolListTemplatesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "list-templates"}
	return pgWireToolCmd(flags, "list_templates", cmd)
}

func newPGToolGeneratePinImageCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "generate-pin-image"}
	var flagTemplate string
	cmd.Flags().StringVar(&flagTemplate, "template", "", "(required) Template ID, built-in name (see list-templates), or \"RandomTemplate\".")
	var flagTitle string
	cmd.Flags().StringVar(&flagTitle, "title", "", "(required) Title text displayed on the pin.")
	var flagImageUrl string
	cmd.Flags().StringVar(&flagImageUrl, "image-url", "", "Public image URL for the first layer (ignored if --image is given).")
	var flagImage []string
	cmd.Flags().StringArrayVar(&flagImage, "image", nil, "Image layer URL, repeatable; maps in order to image1..image10.")
	var flagUrl string
	cmd.Flags().StringVar(&flagUrl, "url", "", "Destination URL the pin links to.")
	var flagUrlText string
	cmd.Flags().StringVar(&flagUrlText, "url-text", "", "Display text for the URL shown on the pin.")
	var flagColor string
	cmd.Flags().StringVar(&flagColor, "color", "", "Hex color for the pin (with or without #).")
	var flagDescription string
	cmd.Flags().StringVar(&flagDescription, "description", "", "Description text for the pin.")
	var execute bool
	cmd.Flags().BoolVar(&execute, "execute", false, "Actually run the call and spend credits (1 credit)")
	return pgWireToolCmd(flags, "generate_pin_image", cmd)
}

func newPGToolGenerateAndSchedulePinCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "generate-and-schedule-pin"}
	var flagTemplate string
	cmd.Flags().StringVar(&flagTemplate, "template", "", "(required) Template ID, built-in name (see list-templates), or \"RandomTemplate\".")
	var flagTitle string
	cmd.Flags().StringVar(&flagTitle, "title", "", "(required) Title text displayed on the pin.")
	var flagImageUrl string
	cmd.Flags().StringVar(&flagImageUrl, "image-url", "", "Public image URL for the first layer (ignored if --image is given).")
	var flagImage []string
	cmd.Flags().StringArrayVar(&flagImage, "image", nil, "Image layer URL, repeatable; maps in order to image1..image10.")
	var flagUrl string
	cmd.Flags().StringVar(&flagUrl, "url", "", "Destination URL the pin links to.")
	var flagUrlText string
	cmd.Flags().StringVar(&flagUrlText, "url-text", "", "Display text for the URL shown on the pin.")
	var flagColor string
	cmd.Flags().StringVar(&flagColor, "color", "", "Hex color for the pin (with or without #).")
	var flagDescription string
	cmd.Flags().StringVar(&flagDescription, "description", "", "Description text for the pin.")
	var flagPinterestProfile string
	cmd.Flags().StringVar(&flagPinterestProfile, "pinterest-profile", "", "(required) Connected Pinterest username (see list-pinterest-profiles).")
	var flagBoardId string
	cmd.Flags().StringVar(&flagBoardId, "board-id", "", "Board ID (see list-boards). Required unless --use-ai-board-selection.")
	var flagBoardSectionId string
	cmd.Flags().StringVar(&flagBoardSectionId, "board-section-id", "", "Board section ID to pin into a specific section.")
	var flagAltText string
	cmd.Flags().StringVar(&flagAltText, "alt-text", "", "Alt text for the image (up to 500 chars).")
	var flagScheduledTime string
	cmd.Flags().StringVar(&flagScheduledTime, "scheduled-time", "", "ISO 8601 time to publish; omit to add to the scheduling queue.")
	var flagUseAiBoardSelection bool
	cmd.Flags().BoolVar(&flagUseAiBoardSelection, "use-ai-board-selection", false, "Let AI choose or create the best board (boardId then optional).")
	var flagAiLanguage string
	cmd.Flags().StringVar(&flagAiLanguage, "ai-language", "", "Language for AI board names/descriptions (default \"English\").")
	var execute bool
	cmd.Flags().BoolVar(&execute, "execute", false, "Actually run the call and spend credits (1 credit)")
	return pgWireToolCmd(flags, "generate_and_schedule_pin", cmd)
}

func newPGToolSchedulePinCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "schedule-pin"}
	var flagMediaUrl string
	cmd.Flags().StringVar(&flagMediaUrl, "media-url", "", "(required) URL of the image to schedule (from generate-pin-image or any public URL).")
	var flagTitle string
	cmd.Flags().StringVar(&flagTitle, "title", "", "Title for the pin on Pinterest.")
	var flagDescription string
	cmd.Flags().StringVar(&flagDescription, "description", "", "Description for the pin on Pinterest.")
	var flagUrl string
	cmd.Flags().StringVar(&flagUrl, "url", "", "Destination URL the pin links to.")
	var flagPinterestProfile string
	cmd.Flags().StringVar(&flagPinterestProfile, "pinterest-profile", "", "(required) Connected Pinterest username (see list-pinterest-profiles).")
	var flagBoardId string
	cmd.Flags().StringVar(&flagBoardId, "board-id", "", "Board ID (see list-boards). Required unless --use-ai-board-selection.")
	var flagBoardSectionId string
	cmd.Flags().StringVar(&flagBoardSectionId, "board-section-id", "", "Board section ID to pin into a specific section.")
	var flagAltText string
	cmd.Flags().StringVar(&flagAltText, "alt-text", "", "Alt text for the image (up to 500 chars).")
	var flagScheduledTime string
	cmd.Flags().StringVar(&flagScheduledTime, "scheduled-time", "", "ISO 8601 time to publish; omit to add to the scheduling queue.")
	var flagUseAiBoardSelection bool
	cmd.Flags().BoolVar(&flagUseAiBoardSelection, "use-ai-board-selection", false, "Let AI choose or create the best board (boardId then optional).")
	var flagAiLanguage string
	cmd.Flags().StringVar(&flagAiLanguage, "ai-language", "", "Language for AI board names/descriptions (default \"English\").")
	var execute bool
	cmd.Flags().BoolVar(&execute, "execute", false, "Actually run the call and spend credits (1 credit)")
	return pgWireToolCmd(flags, "schedule_pin", cmd)
}

func newPGToolListDesignProfilesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "list-design-profiles"}
	return pgWireToolCmd(flags, "list_design_profiles", cmd)
}

func newPGToolSetupDesignProfileCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "setup-design-profile"}
	var flagWebsiteUrl string
	cmd.Flags().StringVar(&flagWebsiteUrl, "website-url", "", "Website to analyze for brand style.")
	var flagReferenceImageUrls []string
	cmd.Flags().StringArrayVar(&flagReferenceImageUrls, "reference-image-urls", nil, "Reference image URL, repeatable.")
	var flagBrandDescription string
	cmd.Flags().StringVar(&flagBrandDescription, "brand-description", "", "Plain-language description of the brand.")
	var flagName string
	cmd.Flags().StringVar(&flagName, "name", "", "Name for the design profile.")
	return pgWireToolCmd(flags, "setup_design_profile", cmd)
}

func newPGToolUpdateDesignProfileCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "update-design-profile"}
	var flagName string
	cmd.Flags().StringVar(&flagName, "name", "", "Name of the design profile to update.")
	var flagBrandDescription string
	cmd.Flags().StringVar(&flagBrandDescription, "brand-description", "", "New brand description.")
	var flagStyleKeywords []string
	cmd.Flags().StringArrayVar(&flagStyleKeywords, "style-keywords", nil, "Style keyword, repeatable.")
	var flagMood string
	cmd.Flags().StringVar(&flagMood, "mood", "", "Overall mood.")
	var flagColorPalette []string
	cmd.Flags().StringArrayVar(&flagColorPalette, "color-palette", nil, "Hex color, repeatable.")
	var flagFontFeel string
	cmd.Flags().StringVar(&flagFontFeel, "font-feel", "", "Typography feel.")
	return pgWireToolCmd(flags, "update_design_profile", cmd)
}

func newPGToolGenerateAiPinImageCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "generate-ai-pin-image"}
	var flagTitle string
	cmd.Flags().StringVar(&flagTitle, "title", "", "(required) Title / prompt for the AI pin.")
	var flagDescription string
	cmd.Flags().StringVar(&flagDescription, "description", "", "Description for the pin.")
	var flagUrl string
	cmd.Flags().StringVar(&flagUrl, "url", "", "Destination URL the pin links to.")
	var flagDesignProfileName string
	cmd.Flags().StringVar(&flagDesignProfileName, "design-profile-name", "", "Design profile to style the pin (see list-design-profiles).")
	var flagAiTextMode string
	cmd.Flags().StringVar(&flagAiTextMode, "ai-text-mode", "", "How much text the AI renders. One of: text, noText, balanced.")
	var flagAiImageModel string
	cmd.Flags().StringVar(&flagAiImageModel, "ai-image-model", "", "Image model. One of: balanced, highQuality.")
	var flagReferenceImageUrls []string
	cmd.Flags().StringArrayVar(&flagReferenceImageUrls, "reference-image-urls", nil, "Reference image URL, repeatable.")
	var flagAiLanguage string
	cmd.Flags().StringVar(&flagAiLanguage, "ai-language", "", "Language for AI text (default \"English\").")
	var execute bool
	cmd.Flags().BoolVar(&execute, "execute", false, "Actually run the call and spend credits (12-24 credits)")
	return pgWireToolCmd(flags, "generate_ai_pin_image", cmd)
}

func newPGToolGenerateAndScheduleAiPinCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "generate-and-schedule-ai-pin"}
	var flagTitle string
	cmd.Flags().StringVar(&flagTitle, "title", "", "(required) Title / prompt for the AI pin.")
	var flagDescription string
	cmd.Flags().StringVar(&flagDescription, "description", "", "Description for the pin.")
	var flagUrl string
	cmd.Flags().StringVar(&flagUrl, "url", "", "Destination URL the pin links to.")
	var flagDesignProfileName string
	cmd.Flags().StringVar(&flagDesignProfileName, "design-profile-name", "", "Design profile to style the pin (see list-design-profiles).")
	var flagAiTextMode string
	cmd.Flags().StringVar(&flagAiTextMode, "ai-text-mode", "", "How much text the AI renders. One of: text, noText, balanced.")
	var flagAiImageModel string
	cmd.Flags().StringVar(&flagAiImageModel, "ai-image-model", "", "Image model. One of: balanced, highQuality.")
	var flagReferenceImageUrls []string
	cmd.Flags().StringArrayVar(&flagReferenceImageUrls, "reference-image-urls", nil, "Reference image URL, repeatable.")
	var flagAiLanguage string
	cmd.Flags().StringVar(&flagAiLanguage, "ai-language", "", "Language for AI text (default \"English\").")
	var flagAltText string
	cmd.Flags().StringVar(&flagAltText, "alt-text", "", "Alt text for the image (up to 500 chars).")
	var flagPinterestProfile string
	cmd.Flags().StringVar(&flagPinterestProfile, "pinterest-profile", "", "(required) Connected Pinterest username.")
	var flagBoardId string
	cmd.Flags().StringVar(&flagBoardId, "board-id", "", "Board ID. Required unless --use-ai-board-selection.")
	var flagBoardSectionId string
	cmd.Flags().StringVar(&flagBoardSectionId, "board-section-id", "", "Board section ID.")
	var flagScheduledTime string
	cmd.Flags().StringVar(&flagScheduledTime, "scheduled-time", "", "ISO 8601 time to publish; omit to queue.")
	var flagUseAiBoardSelection bool
	cmd.Flags().BoolVar(&flagUseAiBoardSelection, "use-ai-board-selection", false, "Let AI choose or create the best board.")
	var execute bool
	cmd.Flags().BoolVar(&execute, "execute", false, "Actually run the call and spend credits (13-25 credits)")
	return pgWireToolCmd(flags, "generate_and_schedule_ai_pin", cmd)
}

func newPGToolListAutomationsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "list-automations"}
	return pgWireToolCmd(flags, "list_automations", cmd)
}

func newPGToolCreateAutomationCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "create-automation"}
	var flagName string
	cmd.Flags().StringVar(&flagName, "name", "", "(required) Automation name.")
	var flagSourceType string
	cmd.Flags().StringVar(&flagSourceType, "source-type", "", "(required) Where pins come from. One of: manual, website.")
	var flagPinterestProfile string
	cmd.Flags().StringVar(&flagPinterestProfile, "pinterest-profile", "", "(required) Connected Pinterest username.")
	var flagPinsPerDay int
	cmd.Flags().IntVar(&flagPinsPerDay, "pins-per-day", 0, "Pins to generate per day.")
	var flagBatchFrequencyDays int
	cmd.Flags().IntVar(&flagBatchFrequencyDays, "batch-frequency-days", 0, "Batch cadence in days. One of: 7, 28.")
	var flagPinDescription string
	cmd.Flags().StringVar(&flagPinDescription, "pin-description", "", "Topic/description for AI to pin about (manual source).")
	var flagOutboundUrls []string
	cmd.Flags().StringArrayVar(&flagOutboundUrls, "outbound-urls", nil, "Outbound URL, repeatable.")
	var flagPostUrls []string
	cmd.Flags().StringArrayVar(&flagPostUrls, "post-urls", nil, "Post URL, repeatable (website source).")
	var flagSitemapUrl string
	cmd.Flags().StringVar(&flagSitemapUrl, "sitemap-url", "", "Sitemap URL to pull posts from.")
	var flagIncludeNewPostsFromSitemap bool
	cmd.Flags().BoolVar(&flagIncludeNewPostsFromSitemap, "include-new-posts-from-sitemap", false, "Keep pulling new posts from the sitemap.")
	var flagBoards string
	cmd.Flags().StringVar(&flagBoards, "boards", "", "Boards as JSON array of objects, e.g. '[{\"id\":\"...\",\"name\":\"...\"}]'.")
	var flagAiBoardsOnlyExisting bool
	cmd.Flags().BoolVar(&flagAiBoardsOnlyExisting, "ai-boards-only-existing", false, "Only use existing boards for AI selection.")
	var flagDesignProfileName string
	cmd.Flags().StringVar(&flagDesignProfileName, "design-profile-name", "", "Design profile to style pins.")
	var flagAiImageModel string
	cmd.Flags().StringVar(&flagAiImageModel, "ai-image-model", "", "Image model. One of: balanced, highQuality.")
	var flagAiTextMode string
	cmd.Flags().StringVar(&flagAiTextMode, "ai-text-mode", "", "How much text the AI renders. One of: text, noText, balanced.")
	var flagAiLanguage string
	cmd.Flags().StringVar(&flagAiLanguage, "ai-language", "", "Language for AI text.")
	var flagAiTone string
	cmd.Flags().StringVar(&flagAiTone, "ai-tone", "", "Tone for AI copy.")
	var flagAiKeywords string
	cmd.Flags().StringVar(&flagAiKeywords, "ai-keywords", "", "Keywords to steer AI copy.")
	var flagFirstBatch string
	cmd.Flags().StringVar(&flagFirstBatch, "first-batch", "", "When the first batch runs. One of: now, nextCycle.")
	var execute bool
	cmd.Flags().BoolVar(&execute, "execute", false, "Actually run the call and spend credits (credits per pin)")
	return pgWireToolCmd(flags, "create_automation", cmd)
}

func newPGToolRunAutomationBatchCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "run-automation-batch"}
	var flagAutomationId string
	cmd.Flags().StringVar(&flagAutomationId, "automation-id", "", "(required) Automation ID (see list-automations).")
	var execute bool
	cmd.Flags().BoolVar(&execute, "execute", false, "Actually run the call and spend credits (credits per pin)")
	return pgWireToolCmd(flags, "run_automation_batch", cmd)
}

func newPGToolGetAutomationBatchStatusCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "get-automation-batch-status"}
	var flagBatchId string
	cmd.Flags().StringVar(&flagBatchId, "batch-id", "", "(required) Batch ID (returned by run-automation-batch).")
	return pgWireToolCmd(flags, "get_automation_batch_status", cmd)
}
