// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/cliutil"
)

// Synthetic stand-ins; the real values only ever live in the operator's env.
const (
	fxPost    = "https://example.com/fixture-post"
	fxSite    = "https://example.com"
	fxProfile = "fixture-profile-name"
	fxStore   = "fixture-store-id"
)

var fxCommands = []string{"pin-post", "pin-products", "unpinned-posts", "list-boards", "get-post-content", "list-store-products"}

func setFixtureEnv(t *testing.T) {
	t.Helper()
	t.Setenv(pgTestPostURL, fxPost)
	t.Setenv(pgTestWebsiteURL, fxSite)
	t.Setenv(pgTestProfile, fxProfile)
	t.Setenv(pgTestStoreID, fxStore)
}

func happyArgsOf(t *testing.T, root *cobra.Command, name string) string {
	t.Helper()
	c, _, err := root.Find([]string{name})
	if err != nil || c.Name() != name {
		t.Fatalf("%s not found", name)
	}
	return c.Annotations["pp:happy-args"]
}

func TestDogfoodFixturesInactiveOutsideDogfood(t *testing.T) {
	newMockMCP(t, map[string]string{})
	setFixtureEnv(t)
	t.Setenv(cliutil.DogfoodEnvVar, "")
	root := RootCmd()
	for _, name := range fxCommands {
		if ha := happyArgsOf(t, root, name); !strings.Contains(ha, "example-value") || strings.Contains(ha, pgEnvFixture) {
			t.Errorf("%s outside dogfood: %q", name, ha)
		}
	}
	if got := pgFixtureValue(pgEnvFixture, pgTestProfile); got != pgEnvFixture {
		t.Fatalf("sentinel resolved outside dogfood: %q", got)
	}
}

func TestDogfoodFixturesNeedEveryEnvVar(t *testing.T) {
	newMockMCP(t, map[string]string{})
	t.Setenv(cliutil.DogfoodEnvVar, "1")
	t.Setenv(pgTestProfile, fxProfile) // post URL deliberately missing
	if ha := happyArgsOf(t, RootCmd(), "pin-post"); !strings.Contains(ha, "example-value") {
		t.Fatalf("partial env must keep the blocked fixture: %q", ha)
	}
}

func TestDogfoodFixturesActiveWithoutLeakingValues(t *testing.T) {
	newMockMCP(t, map[string]string{})
	setFixtureEnv(t)
	t.Setenv(cliutil.DogfoodEnvVar, "1")
	root := RootCmd()
	for _, name := range fxCommands {
		ha := happyArgsOf(t, root, name)
		if !strings.Contains(ha, pgEnvFixture) || strings.Contains(ha, "example-value") {
			t.Errorf("%s under dogfood: %q", name, ha)
		}
		for _, v := range []string{fxPost, fxProfile, fxStore, "example.com"} {
			if strings.Contains(ha, v) {
				t.Errorf("%s annotation leaks a fixture value: %q", name, ha)
			}
		}
	}
	if ha := happyArgsOf(t, root, "get-automation-batch-status"); !strings.Contains(ha, "example-value") {
		t.Errorf("batch status must stay blocked: %q", ha)
	}
	out, _, code := runPG(t, "agent-context")
	if code != 0 {
		t.Fatalf("agent-context exit %d", code)
	}
	for _, v := range []string{fxPost, fxProfile, fxStore} {
		if strings.Contains(out, v) {
			t.Fatalf("agent-context leaks fixture value %q", v)
		}
	}
}

func TestNoHappyArgsIntroduceConfirmFlags(t *testing.T) {
	for _, dogfood := range []string{"", "1"} {
		newMockMCP(t, map[string]string{})
		setFixtureEnv(t)
		t.Setenv(cliutil.DogfoodEnvVar, dogfood)
		var walk func(c *cobra.Command)
		walk = func(c *cobra.Command) {
			ha := c.Annotations["pp:happy-args"]
			for _, bad := range []string{"--execute", "--yes", "--confirm", "--force", "--allow-unknown-tool"} {
				if strings.Contains(ha, bad) {
					t.Errorf("dogfood=%q %s happy-args contains %s: %q", dogfood, c.CommandPath(), bad, ha)
				}
			}
			for _, child := range c.Commands() {
				walk(child)
			}
		}
		walk(RootCmd())
	}
}

func TestDogfoodFixturePreviewsUseEnvAndNeverSpend(t *testing.T) {
	m := newMockMCP(t, map[string]string{
		"get_post_content":    `{"title":"Fixture Post","images":["https://example.com/cdn/f.png"]}`,
		"list_store_products": `{"products":[{"id":"1","title":"Fixture Product","url":"https://example.com/shop/p/1"}]}`,
		"find_website_posts":  `{"posts":[{"url":"https://example.com/fixture-post","title":"Fixture Post"}]}`,
		"list_scheduled_pins": `{"scheduledPins":[],"queuedPinsAwaitingTimeslot":[]}`,
		"list_boards":         `{"boards":[]}`,
	})
	setFixtureEnv(t)
	t.Setenv(cliutil.DogfoodEnvVar, "1")
	run := func(args ...string) string {
		t.Helper()
		out, stderr, code := runPG(t, append(args, "--json")...)
		if code != 0 {
			t.Fatalf("%v exit %d: %s", args, code, stderr)
		}
		return out
	}
	// Exactly the invocations the runner builds from the sentinel happy-args.
	out := run("pin-post", pgEnvFixture, "--pinterest-profile", pgEnvFixture, "--use-ai-board-selection")
	var pv pinPostView
	if err := json.Unmarshal([]byte(out), &pv); err != nil || pv.Executed || pv.PostURL != fxPost || pv.Request.Params.Arguments["pinterestProfile"] != fxProfile {
		t.Fatalf("pin-post preview: %v %s", err, out)
	}
	run("pin-products", "--store-id", pgEnvFixture, "--pinterest-profile", pgEnvFixture, "--use-ai-board-selection")
	run("unpinned-posts", "--website-url", pgEnvFixture)
	run("list-boards", "--pinterest-profile", pgEnvFixture)
	run("get-post-content", "--post-url", pgEnvFixture)
	run("list-store-products", "--store-id", pgEnvFixture)

	want := map[string][2]string{
		"get_post_content":    {"postUrl", fxPost},
		"list_store_products": {"storeId", fxStore},
		"find_website_posts":  {"websiteUrl", fxSite},
		"list_boards":         {"pinterestProfile", fxProfile},
	}
	for _, c := range m.toolCalls("") {
		switch c.Req.Params.Name {
		case "generate_and_schedule_pin", "generate_and_schedule_ai_pin", "schedule_pin", "generate_pin_image", "generate_ai_pin_image", "run_automation_batch", "create_automation":
			t.Fatalf("a preview reached spending tool %s", c.Req.Params.Name)
		}
		if w, ok := want[c.Req.Params.Name]; ok && c.Req.Params.Arguments[w[0]] != w[1] {
			t.Errorf("%s got %s=%v, want the env value", c.Req.Params.Name, w[0], c.Req.Params.Arguments[w[0]])
		}
		for _, v := range c.Req.Params.Arguments {
			if v == pgEnvFixture {
				t.Errorf("%s sent the unresolved sentinel", c.Req.Params.Name)
			}
		}
	}
}
