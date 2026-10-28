// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.

package substack

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The unit fixtures below copy the markup Substack really sends (class names,
// attribute layout, the wrappers around images and embeds) with placeholder
// text, so they can live in git. The real post bodies the converter was built
// against are in testdata/real (not committed) and are used by
// TestHTMLToMarkdownMatchesFormatD.

func TestHTMLToMarkdownBasics(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"heading and inline marks",
			`<h2>Title</h2><p>Hello <strong>bold</strong> and <em>it</em> <a href="https://x.com/a">link</a>.</p>`,
			"## Title\n\nHello **bold** and *it* [link](https://x.com/a).\n"},
		{"emphasis keeps edge spaces outside the marks",
			`<p><strong>a </strong>b</p>`,
			"**a** b\n"},
		{"a paragraph that looks like a list is escaped",
			`<p>1. not a list</p><p>- not a bullet</p><p># not a heading</p>`,
			"1\\. not a list\n\n\\- not a bullet\n\n\\# not a heading\n"},
		{"markdown characters in text are escaped",
			`<p>*star* [br] and snake_case and a_ b</p>`,
			"\\*star\\* \\[br\\] and snake_case and a\\_ b\n"},
		{"line break",
			`<p>a<br>b</p>`,
			"a\\\nb\n"},
		{"bullets keep nesting",
			`<ul><li><p>One</p></li><li><p>Two</p><ul><li><p>Nested</p></li></ul></li></ul>`,
			"- One\n- Two\n  - Nested\n"},
		{"ordered list",
			`<ol><li>A</li><li>B</li></ol>`,
			"1. A\n2. B\n"},
		{"blockquote",
			`<blockquote><p>q</p></blockquote>`,
			"> q\n"},
		{"rule",
			`<p>a</p><hr><p>b</p>`,
			"a\n\n---\n\nb\n"},
		{"code block",
			`<pre><code class="language-go">x := 1</code></pre>`,
			"```go\nx := 1\n```\n"},
		{"inline code",
			`<p>use <code>go test</code></p>`,
			"use `go test`\n"},
		{"table",
			`<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>x|y</td></tr></table>`,
			"| A | B |\n| --- | --- |\n| 1 | x\\|y |\n"},
		{"inline svg icons are dropped",
			`<p>a<svg viewBox="0 0 1 1"><path d="M0"/></svg>b</p>`,
			"ab\n"},
		{"an author's empty link stays an empty link",
			`<p><a href="https://x.com"></a>text</p>`,
			"[](https://x.com)text\n"},
		{"link destinations with spaces and parentheses stay valid",
			`<p><a href="https://x.com/a b(1)">t</a></p>`,
			"[t](https://x.com/a%20b%281%29)\n"},
		{"script and style are not content",
			`<p>a</p><script>alert(1)</script><style>p{}</style><p>b</p>`,
			"a\n\nb\n"},
		{"unknown containers fall back to their content",
			`<div class="mystery"><section><p>inside</p></section></div>`,
			"inside\n"},
		{"text directly in a container becomes a paragraph",
			`<div>loose <b>text</b></div>`,
			"loose **text**\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HTMLToMarkdown(tt.in); got != tt.want {
				t.Errorf("HTMLToMarkdown(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}

const cdnImg = `https://substackcdn.com/image/fetch/$s_!aa!,f_auto/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2Fabc_10x10.png`

func TestHTMLToMarkdownSubstackBlocks(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"captioned image: one image, wrapper link dropped when it points at the image CDN",
			`<div class="captioned-image-container"><figure><a class="image-link image2 is-viewable-img" target="_blank" href="` + cdnImg + `" data-component-name="Image2ToDOM"><div class="image2-inset"><picture><source type="image/webp" srcset="` + cdnImg + ` 424w"><img src="` + cdnImg + `" alt="A chart" width="10" height="10"></picture><div class="image-link-expand"><svg><path d="M"/></svg></div></div></a><figcaption class="image-caption">Fig <em>1</em></figcaption></figure></div>`,
			"![A chart](" + cdnImg + ")\n\n*Fig 1*\n"},
		{"captioned image that links somewhere real stays a link",
			`<div class="captioned-image-container"><figure><a class="image-link image2" href="https://x.substack.com/perks/abc"><div class="image2-inset"><img src="` + cdnImg + `" alt=""></div></a></figure></div>`,
			"[![](" + cdnImg + ")](https://x.substack.com/perks/abc)\n"},
		{"youtube becomes a player line",
			`<div id="youtube2-fXaF0bIthB0" class="youtube-wrap" data-attrs="{&quot;videoId&quot;:&quot;fXaF0bIthB0&quot;,&quot;startTime&quot;:null}" data-component-name="Youtube2ToDOM"><div class="youtube-inner"><iframe src="https://www.youtube-nocookie.com/embed/fXaF0bIthB0?rel=0" width="728"></iframe></div></div>`,
			"![YouTube](https://www.youtube.com/watch?v=fXaF0bIthB0)\n"},
		{"youtube with no attrs falls back to the embed url",
			`<div class="youtube-wrap"><iframe src="https://www.youtube-nocookie.com/embed/abcDEF12345?rel=0"></iframe></div>`,
			"![YouTube](https://www.youtube.com/watch?v=abcDEF12345)\n"},
		{"pdf attachment becomes one named link",
			`<div class="file-embed-wrapper" data-component-name="FileToDOM"><div class="file-embed-container-reader"><div class="file-embed-container-top"><image class="file-embed-thumbnail" src="` + cdnImg + `"></image><div class="file-embed-details"><div class="file-embed-details-h1">Self-Audit</div><div class="file-embed-details-h2">68.5KB ∙ PDF file</div></div><a class="file-embed-button wide" href="https://x.substack.com/api/v1/file/aa-bb.pdf"><span class="file-embed-button-text">Download</span></a></div><a class="file-embed-button narrow" href="https://x.substack.com/api/v1/file/aa-bb.pdf"><span class="file-embed-button-text">Download</span></a></div></div>`,
			"[Attachment: Self-Audit (68.5KB ∙ PDF file)](https://x.substack.com/api/v1/file/aa-bb.pdf)\n"},
		{"embedded post card becomes a link to the post",
			`<div class="digest-post-embed" data-attrs="{&quot;nodeId&quot;:&quot;n&quot;,&quot;title&quot;:&quot;Another post&quot;,&quot;canonical_url&quot;:&quot;https://x.substack.com/p/another&quot;,&quot;publishedBylines&quot;:[{&quot;name&quot;:&quot;A&quot;}]}" data-component-name="DigestPostEmbed"></div>`,
			"Embedded post: [Another post](https://x.substack.com/p/another)\n"},
		{"subscribe widget keeps its caption and drops the form",
			`<div class="subscription-widget-wrap-editor" data-attrs="{}"><div class="subscription-widget show-subscribe"><div class="preamble"><p class="cta-caption">Support my work.</p></div><form class="subscription-widget-subscribe"><input type="email" class="email-input"><input type="submit" class="button primary" value="Subscribe"></form></div></div>`,
			"*Support my work.*\n"},
		{"subscribe widget without a caption disappears",
			`<div class="subscription-widget-wrap"><form><input type="email"></form></div>`,
			""},
		{"poll leaves a placeholder",
			`<div class="poll-embed" data-attrs="{&quot;id&quot;:521391}" data-component-name="PollToDOM"></div>`,
			"*\\[Poll: not included in the saved post\\]*\n"},
		{"pull-quote",
			`<div class="pullquote"><p>Quoted.</p></div>`,
			"> Quoted.\n"},
		{"callout",
			`<div class="callout-block" data-callout="true"><p>Noted <em>well</em>.</p></div>`,
			"> Noted *well*.\n"},
		{"paywall marker is dropped",
			`<p>a</p><div class="paywall-jump" data-component-name="PaywallToDOM"></div><p>b</p>`,
			"a\n\nb\n"},
		{"button keeps its link",
			`<p class="button-wrapper" data-attrs="{}"><a class="button primary" href="https://x.com/buy"><span>Buy now</span></a></p>`,
			"[Buy now](https://x.com/buy)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HTMLToMarkdown(tt.in); got != tt.want {
				t.Errorf("HTMLToMarkdown\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestHTMLToMarkdownEmptyInput(t *testing.T) {
	for _, in := range []string{"", "  \n "} {
		if got := HTMLToMarkdown(in); got != "" {
			t.Errorf("HTMLToMarkdown(%q) = %q, want empty", in, got)
		}
	}
}

func TestOriginalImageURL(t *testing.T) {
	if got, want := OriginalImageURL(cdnImg), "https://substack-post-media.s3.amazonaws.com/public/images/abc_10x10.png"; got != want {
		t.Errorf("OriginalImageURL = %q, want %q", got, want)
	}
	plain := "https://example.com/pic.png"
	if got := OriginalImageURL(plain); got != plain {
		t.Errorf("a plain url must come back unchanged, got %q", got)
	}
}

func TestPropertiesBlock(t *testing.T) {
	got := Properties(PostProperties{
		Title: `A "quoted" title`, Subtitle: "", Source: "https://x.substack.com/p/a",
		Published: "2026-01-02T03:04:05.000Z", Access: "entitled", Wordcount: 1406,
	})
	want := "---\n" +
		"title: \"A \\\"quoted\\\" title\"\n" +
		"source: \"https://x.substack.com/p/a\"\n" +
		"published: \"2026-01-02\"\n" +
		"access: \"entitled\"\n" +
		"wordcount: 1406\n" +
		"---\n\n"
	if got != want {
		t.Errorf("Properties\n got: %q\nwant: %q", got, want)
	}
}

// mdCounts applies the same rules as followup/coverage.py count_md to a
// Markdown text, so the converter can be checked against the audit's counts for
// format D (Substack pre-cleaning, then Markdown).
type mdCounts struct{ Links, Empty, Perk, Headings, List, Images, YouTube, PDF int }

var (
	covIMG     = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	covLINK    = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	covHEAD    = regexp.MustCompile(`(?m)^(?:>\s*)*#{1,6} `)
	covLIST    = regexp.MustCompile(`(?m)^\s*(?:>\s*)*(?:[-*+]|\d+\.)\s+`)
	covYT      = regexp.MustCompile(`(?:youtube\.com/watch\?v=|youtube-nocookie\.com/embed/|youtu\.be/)([\w-]{11})`)
	covPDF     = regexp.MustCompile(`https?://[^\s)"]*/api/v1/file/[\w-]+\.pdf`)
	covFrontRE = regexp.MustCompile(`(?s)\A---\n.*?\n---\n`)
)

func countMarkdown(md string) mdCounts {
	var c mdCounts
	body := covFrontRE.ReplaceAllString(md, "")
	for _, m := range covIMG.FindAllStringSubmatch(body, -1) {
		if strings.Contains(m[1], "youtube.com") || strings.Contains(m[1], "youtu.be") {
			continue
		}
		c.Images++
	}
	flat := covIMG.ReplaceAllString(body, "IMG")
	for _, m := range covLINK.FindAllStringSubmatch(flat, -1) {
		if strings.TrimSpace(m[1]) == "" {
			c.Empty++
			continue
		}
		c.Links++
		if strings.Contains(m[2], "/perks/") {
			c.Perk++
		}
	}
	c.Headings = len(covHEAD.FindAllString(body, -1))
	c.List = len(covLIST.FindAllString(body, -1))
	yt := map[string]bool{}
	for _, m := range covYT.FindAllStringSubmatch(body, -1) {
		yt[m[1]] = true
	}
	c.YouTube = len(yt)
	pdf := map[string]bool{}
	for _, m := range covPDF.FindAllString(body, -1) {
		pdf[m] = true
	}
	c.PDF = len(pdf)
	return c
}

// TestHTMLToMarkdownMatchesFormatD runs the converter on the five audit samples
// and compares its counts with the "D pandoc + clean" rows of
// audits/uxmentor-2026-10-22/followup/evidence/coverage-table.md, produced by
// followup/coverage.py. A count that drops means the converter lost links,
// headings, list items, images, videos or PDFs that the reference kept.
func TestHTMLToMarkdownMatchesFormatD(t *testing.T) {
	want := map[string]mdCounts{
		"01-free":                 {Links: 20, Empty: 0, Perk: 0, Headings: 17, List: 0, Images: 3, YouTube: 2, PDF: 0},
		"02-paid":                 {Links: 12, Empty: 2, Perk: 0, Headings: 16, List: 23, Images: 1, YouTube: 1, PDF: 0},
		"03-paid-template":        {Links: 20, Empty: 0, Perk: 10, Headings: 15, List: 32, Images: 13, YouTube: 0, PDF: 0},
		"04-paid-pdf-attachments": {Links: 8, Empty: 0, Perk: 0, Headings: 18, List: 30, Images: 1, YouTube: 0, PDF: 4},
		"05-page":                 {Links: 41, Empty: 0, Perk: 0, Headings: 41, List: 128, Images: 0, YouTube: 0, PDF: 0},
	}
	ran := 0
	for name, w := range want {
		raw, err := os.ReadFile(filepath.Join("testdata", "real", name+".html"))
		if err != nil {
			t.Logf("skipping %s: %v (see testdata/README.md)", name, err)
			continue
		}
		ran++
		md := HTMLToMarkdown(string(raw))
		if got := countMarkdown(md); got != w {
			t.Errorf("%s counts differ from format D\n got: %+v\nwant: %+v", name, got, w)
		}
		if strings.Contains(md, "<svg") {
			t.Errorf("%s: inline svg left in the Markdown", name)
		}
	}
	if ran == 0 {
		t.Skip("no real fixtures in testdata/real")
	}
}
