// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

// Package pgmcp speaks the Pin Generator MCP server's JSON-RPC surface.
//
// Pin Generator exposes its full tool set (reads, template and AI pin
// generation, scheduling, design profiles, automations) only through one MCP
// endpoint, POST /mcp, authorized by the account access key as a Bearer token.
// The generated REST client cannot express tools/call, so this package builds
// the JSON-RPC envelopes and unwraps the results while the generated
// internal/client still owns transport, auth, retries and rate limiting.
package pgmcp

import (
	"strconv"
	"strings"
)

// ParamType controls how a flag is declared and how its value becomes a
// tools/call argument.
type ParamType int

const (
	String      ParamType = iota // string flag -> JSON string
	Bool                         // bool flag -> JSON bool
	Int                          // int flag -> JSON number
	Array                        // repeatable string flag -> JSON array of strings
	RawJSON                      // string flag whose value is parsed as raw JSON
	ImageLayers                  // repeatable --image -> image1..imageN arguments
)

// Param describes one MCP tool argument and the CLI flag that supplies it.
type Param struct {
	API      string // MCP argument name, e.g. "pinterestProfile"
	Type     ParamType
	Required bool
	Desc     string
	Enum     []string
}

// Flag returns the kebab-case CLI flag name for the argument.
func (p Param) Flag() string {
	if p.Type == ImageLayers {
		return "image"
	}
	return FlagName(p.API)
}

// Cost is a tool's credit price. PerPin marks tools whose cost scales with
// pins produced (automation batches), so Min/Max are per pin.
type Cost struct {
	Min, Max int
	PerPin   bool
}

// Spends reports whether the tool costs credits at all.
func (c Cost) Spends() bool { return c.Max > 0 || c.PerPin }

// Note is the human label shown in help and previews.
func (c Cost) Note() string {
	switch {
	case !c.Spends():
		return "free"
	case c.PerPin:
		return "credits per pin"
	case c.Min == c.Max && c.Min == 1:
		return "1 credit"
	case c.Min == c.Max:
		return strconv.Itoa(c.Min) + " credits"
	default:
		return strconv.Itoa(c.Min) + "-" + strconv.Itoa(c.Max) + " credits"
	}
}

// Tool is one MCP tool exposed as a CLI command.
type Tool struct {
	Name    string // MCP tool name, e.g. "generate_pin_image"
	Cmd     string // CLI command, e.g. "generate-pin-image"
	Summary string
	Example string
	Params  []Param
	// Mutating tools change server state; they honor --dry-run.
	Mutating bool
	// Cost > 0 tools default to a dry run and need --execute to fire.
	Cost Cost
	// HappyArgs is the live-dogfood fixture (pp:happy-args). Account-specific
	// inputs use "example-value" so the live gate reports a blocked fixture
	// instead of calling the API with an invented ID.
	HappyArgs string
}

var commonPinFields = []Param{
	{API: "template", Type: String, Required: true, Desc: `Template ID, built-in name (see list-templates), or "RandomTemplate".`},
	{API: "title", Type: String, Required: true, Desc: "Title text displayed on the pin."},
	{API: "imageURL", Type: String, Desc: "Public image URL for the first layer (ignored if --image is given)."},
	{API: "image", Type: ImageLayers, Desc: "Image layer URL, repeatable; maps in order to image1..image10."},
	{API: "url", Type: String, Desc: "Destination URL the pin links to."},
	{API: "urlText", Type: String, Desc: "Display text for the URL shown on the pin."},
	{API: "color", Type: String, Desc: "Hex color for the pin (with or without #)."},
	{API: "description", Type: String, Desc: "Description text for the pin."},
}

var schedulingFields = []Param{
	{API: "pinterestProfile", Type: String, Required: true, Desc: "Connected Pinterest username (see list-pinterest-profiles)."},
	{API: "boardId", Type: String, Desc: "Board ID (see list-boards). Required unless --use-ai-board-selection."},
	{API: "boardSectionId", Type: String, Desc: "Board section ID to pin into a specific section."},
	{API: "altText", Type: String, Desc: "Alt text for the image (up to 500 chars)."},
	{API: "scheduledTime", Type: String, Desc: "ISO 8601 time to publish; omit to add to the scheduling queue."},
	{API: "useAIBoardSelection", Type: Bool, Desc: "Let AI choose or create the best board (boardId then optional)."},
	{API: "aiLanguage", Type: String, Desc: `Language for AI board names/descriptions (default "English").`},
}

var aiTextMode = []string{"text", "noText", "balanced"}
var aiImageModel = []string{"balanced", "highQuality"}

var aiPinFields = []Param{
	{API: "title", Type: String, Required: true, Desc: "Title / prompt for the AI pin."},
	{API: "description", Type: String, Desc: "Description for the pin."},
	{API: "url", Type: String, Desc: "Destination URL the pin links to."},
	{API: "designProfileName", Type: String, Desc: "Design profile to style the pin (see list-design-profiles)."},
	{API: "aiTextMode", Type: String, Enum: aiTextMode, Desc: "How much text the AI renders."},
	{API: "aiImageModel", Type: String, Enum: aiImageModel, Desc: "Image model."},
	{API: "referenceImageUrls", Type: Array, Desc: "Reference image URL, repeatable."},
	{API: "aiLanguage", Type: String, Desc: `Language for AI text (default "English").`},
}

func concat(groups ...[]Param) []Param {
	var out []Param
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// Tools is the Pin Generator MCP tool surface. Order groups related commands.
var Tools = []Tool{
	// Discover (free reads)
	{Name: "get_account_status", Cmd: "get-account-status", Summary: "Account email, credit usage and connected-profile count. Call first to confirm the connection.",
		Example: "  pingenerator-pp-cli get-account-status --agent"},
	{Name: "list_pinterest_profiles", Cmd: "list-pinterest-profiles", Summary: "List Pinterest profiles connected to this account (use username as --pinterest-profile).",
		Example: "  pingenerator-pp-cli list-pinterest-profiles --agent"},
	{Name: "list_boards", Cmd: "list-boards", Summary: "List a connected profile's boards, with IDs for scheduling.",
		Example: "  pingenerator-pp-cli list-boards --pinterest-profile myprofile --agent", HappyArgs: "--pinterest-profile=example-value", Params: []Param{
			{API: "pinterestProfile", Type: String, Required: true, Desc: "Pinterest username (see list-pinterest-profiles)."},
		}},
	{Name: "list_scheduled_pins", Cmd: "list-scheduled-pins", Summary: "Review everything queued or scheduled: titles, boards and times.",
		Example: "  pingenerator-pp-cli list-scheduled-pins --agent"},

	// Content sources (free reads)
	{Name: "find_website_posts", Cmd: "find-website-posts", Summary: "Read a site's sitemap to find real blog posts/pages, newest first.",
		Example: "  pingenerator-pp-cli find-website-posts --website-url https://example.com --limit 10", Params: []Param{
			{API: "websiteUrl", Type: String, Required: true, Desc: "Website URL to read the sitemap of."},
			{API: "limit", Type: Int, Desc: "Maximum posts to return."},
		}},
	{Name: "get_post_content", Cmd: "get-post-content", Summary: "Pull the title, description and images from a post URL.",
		Example: "  pingenerator-pp-cli get-post-content --post-url https://example.com/best-beaches", HappyArgs: "--post-url=example-value", Params: []Param{
			{API: "postUrl", Type: String, Required: true, Desc: "URL of the post to read."},
		}},
	{Name: "list_connected_stores", Cmd: "list-connected-stores", Summary: "List connected Shopify, Etsy and WooCommerce stores.",
		Example: "  pingenerator-pp-cli list-connected-stores --agent"},
	{Name: "list_store_products", Cmd: "list-store-products", Summary: "Browse a store's products: titles, prices, links and photos.",
		Example: "  pingenerator-pp-cli list-store-products --store-id store_123 --page 1", HappyArgs: "--store-id=example-value", Params: []Param{
			{API: "storeId", Type: String, Required: true, Desc: "Store ID (see list-connected-stores)."},
			{API: "search", Type: String, Desc: "Filter products by search term."},
			{API: "page", Type: Int, Desc: "Page number for pagination."},
		}},

	// Template engine
	{Name: "list_templates", Cmd: "list-templates", Summary: "Browse the built-in pin templates and their styles (free).",
		Example: "  pingenerator-pp-cli list-templates --agent --select templates.name,templates.style"},
	{Name: "generate_pin_image", Cmd: "generate-pin-image", Summary: "Render a template pin image and return its hosted URL.",
		Example: `  pingenerator-pp-cli generate-pin-image --template Standard --title "10 Best Beaches" --image-url https://example.com/beach.jpg`,
		Params:  commonPinFields, Mutating: true, Cost: Cost{Min: 1, Max: 1}},
	{Name: "generate_and_schedule_pin", Cmd: "generate-and-schedule-pin", Summary: "Render a template pin and schedule it to Pinterest.",
		Example: `  pingenerator-pp-cli generate-and-schedule-pin --template RandomTemplate --title "Best Beaches" --pinterest-profile myprofile --use-ai-board-selection`,
		Params:  concat(commonPinFields, schedulingFields), Mutating: true, Cost: Cost{Min: 1, Max: 1}},

	// Schedule an existing image
	{Name: "schedule_pin", Cmd: "schedule-pin", Summary: "Schedule an already-hosted image to Pinterest.",
		Example:  "  pingenerator-pp-cli schedule-pin --media-url https://example.com/pin.jpg --pinterest-profile myprofile --board-id 992269799088633298",
		Mutating: true, Cost: Cost{Min: 1, Max: 1}, Params: concat([]Param{
			{API: "mediaURL", Type: String, Required: true, Desc: "URL of the image to schedule (from generate-pin-image or any public URL)."},
			{API: "title", Type: String, Desc: "Title for the pin on Pinterest."},
			{API: "description", Type: String, Desc: "Description for the pin on Pinterest."},
			{API: "url", Type: String, Desc: "Destination URL the pin links to."},
		}, schedulingFields)},

	// AI design system
	{Name: "list_design_profiles", Cmd: "list-design-profiles", Summary: "List saved AI design profiles.",
		Example: "  pingenerator-pp-cli list-design-profiles --agent"},
	{Name: "setup_design_profile", Cmd: "setup-design-profile", Summary: "Build a reusable brand design profile from a site, images or description.",
		Example:  `  pingenerator-pp-cli setup-design-profile --website-url https://example.com --name "Example brand" --dry-run`,
		Mutating: true, Params: []Param{
			{API: "websiteUrl", Type: String, Desc: "Website to analyze for brand style."},
			{API: "referenceImageUrls", Type: Array, Desc: "Reference image URL, repeatable."},
			{API: "brandDescription", Type: String, Desc: "Plain-language description of the brand."},
			{API: "name", Type: String, Desc: "Name for the design profile."},
		}},
	{Name: "update_design_profile", Cmd: "update-design-profile", Summary: "Refine a design profile's colors, mood, style or description.",
		Example:  `  pingenerator-pp-cli update-design-profile --name "Example brand" --mood calm --color-palette "#1a2b3c" --dry-run`,
		Mutating: true, Params: []Param{
			{API: "name", Type: String, Desc: "Name of the design profile to update."},
			{API: "brandDescription", Type: String, Desc: "New brand description."},
			{API: "styleKeywords", Type: Array, Desc: "Style keyword, repeatable."},
			{API: "mood", Type: String, Desc: "Overall mood."},
			{API: "colorPalette", Type: Array, Desc: "Hex color, repeatable."},
			{API: "fontFeel", Type: String, Desc: "Typography feel."},
		}},
	{Name: "generate_ai_pin_image", Cmd: "generate-ai-pin-image", Summary: "Generate a fully AI, on-brand pin image (unique every time).",
		Example: `  pingenerator-pp-cli generate-ai-pin-image --title "10 Best Beaches" --design-profile-name "Example brand"`,
		Params:  aiPinFields, Mutating: true, Cost: Cost{Min: 12, Max: 24}},
	{Name: "generate_and_schedule_ai_pin", Cmd: "generate-and-schedule-ai-pin", Summary: "Generate a fully AI pin and schedule it to Pinterest.",
		Example: `  pingenerator-pp-cli generate-and-schedule-ai-pin --title "10 Best Beaches" --pinterest-profile myprofile --board-id 992269799088633298 --url https://example.com/beaches`,
		Params: concat(aiPinFields, []Param{
			{API: "altText", Type: String, Desc: "Alt text for the image (up to 500 chars)."},
			{API: "pinterestProfile", Type: String, Required: true, Desc: "Connected Pinterest username."},
			{API: "boardId", Type: String, Desc: "Board ID. Required unless --use-ai-board-selection."},
			{API: "boardSectionId", Type: String, Desc: "Board section ID."},
			{API: "scheduledTime", Type: String, Desc: "ISO 8601 time to publish; omit to queue."},
			{API: "useAIBoardSelection", Type: Bool, Desc: "Let AI choose or create the best board."},
		}), Mutating: true, Cost: Cost{Min: 13, Max: 25}},

	// Automations
	{Name: "list_automations", Cmd: "list-automations", Summary: "List automations, their cadence, status and next batch.",
		Example: "  pingenerator-pp-cli list-automations --agent"},
	{Name: "create_automation", Cmd: "create-automation", Summary: "Create a recurring automation that pins from posts or a topic; it spends credits per pin as batches run.",
		Example: `  pingenerator-pp-cli create-automation --name "Weekly beach pins" --source-type website --pinterest-profile myprofile --sitemap-url https://example.com/sitemap.xml`,
		// An automation bills credits per pin on the server's schedule (firstBatch
		// "now" starts at once), so creating one is gated like any spend.
		Mutating: true, Cost: Cost{PerPin: true}, Params: []Param{
			{API: "name", Type: String, Required: true, Desc: "Automation name."},
			{API: "sourceType", Type: String, Required: true, Enum: []string{"manual", "website"}, Desc: "Where pins come from."},
			{API: "pinterestProfile", Type: String, Required: true, Desc: "Connected Pinterest username."},
			{API: "pinsPerDay", Type: Int, Desc: "Pins to generate per day."},
			{API: "batchFrequencyDays", Type: Int, Enum: []string{"7", "28"}, Desc: "Batch cadence in days."},
			{API: "pinDescription", Type: String, Desc: "Topic/description for AI to pin about (manual source)."},
			{API: "outboundUrls", Type: Array, Desc: "Outbound URL, repeatable."},
			{API: "postUrls", Type: Array, Desc: "Post URL, repeatable (website source)."},
			{API: "sitemapUrl", Type: String, Desc: "Sitemap URL to pull posts from."},
			{API: "includeNewPostsFromSitemap", Type: Bool, Desc: "Keep pulling new posts from the sitemap."},
			{API: "boards", Type: RawJSON, Desc: `Boards as JSON array of objects, e.g. '[{"id":"...","name":"..."}]'.`},
			{API: "aiBoardsOnlyExisting", Type: Bool, Desc: "Only use existing boards for AI selection."},
			{API: "designProfileName", Type: String, Desc: "Design profile to style pins."},
			{API: "aiImageModel", Type: String, Enum: aiImageModel, Desc: "Image model."},
			{API: "aiTextMode", Type: String, Enum: aiTextMode, Desc: "How much text the AI renders."},
			{API: "aiLanguage", Type: String, Desc: "Language for AI text."},
			{API: "aiTone", Type: String, Desc: "Tone for AI copy."},
			{API: "aiKeywords", Type: String, Desc: "Keywords to steer AI copy."},
			{API: "firstBatch", Type: String, Enum: []string{"now", "nextCycle"}, Desc: "When the first batch runs."},
		}},
	{Name: "run_automation_batch", Cmd: "run-automation-batch", Summary: "Kick off a fresh batch of pins for an automation.",
		Example:  "  pingenerator-pp-cli run-automation-batch --automation-id automation_123",
		Mutating: true, Cost: Cost{PerPin: true}, Params: []Param{
			{API: "automationId", Type: String, Required: true, Desc: "Automation ID (see list-automations)."},
		}},
	{Name: "get_automation_batch_status", Cmd: "get-automation-batch-status", Summary: "Follow a running batch: phase, progress and pin count.",
		Example: "  pingenerator-pp-cli get-automation-batch-status --batch-id batch_123", HappyArgs: "--batch-id=example-value", Params: []Param{
			{API: "batchId", Type: String, Required: true, Desc: "Batch ID (returned by run-automation-batch)."},
		}},
}

// Lookup finds a tool by MCP name or CLI command.
func Lookup(nameOrCmd string) (Tool, bool) {
	for _, t := range Tools {
		if t.Name == nameOrCmd || t.Cmd == nameOrCmd {
			return t, true
		}
	}
	return Tool{}, false
}

// FlagName converts an MCP argument name to a kebab-case flag, handling
// acronym runs (imageURL -> image-url, useAIBoardSelection -> use-ai-board-selection).
func FlagName(api string) string {
	var b strings.Builder
	rs := []rune(api)
	for i, r := range rs {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				prev := rs[i-1]
				lowerOrDigit := (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9')
				nextLower := i+1 < len(rs) && rs[i+1] >= 'a' && rs[i+1] <= 'z'
				if lowerOrDigit || (prev >= 'A' && prev <= 'Z' && nextLower) {
					b.WriteByte('-')
				}
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
