// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.

// Substack body_html -> Markdown.
//
// HTMLToText (render.go) flattens a post to plain text; it is the input to the
// word count that decides "full or preview", and it keeps no links, headings or
// list markers. This file is the reading copy: a real Markdown document that
// keeps what the post says and links to.
//
// It does what followup/substack_to_md.py did (Substack pre-cleaning, then
// HTML to Markdown), in one pass over the parsed HTML tree instead of regex
// plus pandoc: Substack's own blocks (captioned images, YouTube, PDF
// attachments, embedded-post cards, subscribe widgets, polls, pull-quotes) are
// recognised by class name and turned into their Markdown form; everything
// else is converted by tag. A block this file does not know falls back to its
// plain content, so a change in Substack's class names loses formatting but not
// text.
package substack

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// mdKind tags a rendered block so a list item can tell a nested list (joined
// with a single newline) from a paragraph (joined with a blank line).
type mdKind int

const (
	mdPara mdKind = iota
	mdList
	mdOther
)

type mdBlock struct {
	kind mdKind
	text string
}

// mdConv holds the one piece of state a conversion needs: whether emphasis is
// being suppressed because the caller wraps the whole text in emphasis itself
// (image captions, subscribe captions), where nested marks would break.
type mdConv struct {
	plainEmphasis bool
}

// HTMLToMarkdown converts a Substack body_html into Markdown. It returns an
// empty string for an empty body. Images stay `![alt](src)` pointing at
// Substack's server; saving them locally is the caller's job.
func HTMLToMarkdown(bodyHTML string) string {
	if strings.TrimSpace(bodyHTML) == "" {
		return ""
	}
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(bodyHTML), ctx)
	if err != nil {
		return ""
	}
	c := &mdConv{}
	blocks := c.blocksOf(nodes)
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if strings.TrimSpace(b.text) != "" {
			parts = append(parts, b.text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n") + "\n"
}

// PostProperties are the fields written as the Markdown properties block
// (YAML front matter), which Obsidian shows as a properties panel.
type PostProperties struct {
	Title     string
	Subtitle  string
	Source    string
	Published string // an ISO timestamp or date; only the date part is kept
	Access    string
	Wordcount int
}

// Properties renders the front matter block, ending with a blank line so it can
// be placed directly before the Markdown body. Empty fields are left out.
func Properties(p PostProperties) string {
	var b strings.Builder
	b.WriteString("---\n")
	add := func(key, val string) {
		if val != "" {
			b.WriteString(key + ": " + yamlString(val) + "\n")
		}
	}
	add("title", p.Title)
	add("subtitle", p.Subtitle)
	add("source", p.Source)
	published := p.Published
	if len(published) > 10 {
		published = published[:10]
	}
	add("published", published)
	add("access", p.Access)
	if p.Wordcount > 0 {
		b.WriteString("wordcount: " + strconv.Itoa(p.Wordcount) + "\n")
	}
	b.WriteString("---\n\n")
	return b.String()
}

// yamlString quotes a value the way JSON does, which is also a valid YAML
// double-quoted string, and keeps non-ASCII text readable.
func yamlString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(buf.String(), "\n")
}

var originalImageRE = regexp.MustCompile(`/(https?%3A%2F%2F.+)$`)

// OriginalImageURL turns a Substack image-CDN "fetch" URL, which ends with the
// URL-encoded address of the original upload, into that original address. Any
// other URL comes back unchanged.
func OriginalImageURL(src string) string {
	m := originalImageRE.FindStringSubmatch(src)
	if m == nil {
		return src
	}
	if orig, err := url.PathUnescape(m[1]); err == nil {
		return orig
	}
	return src
}

// ---- block level ----

func children(n *html.Node) []*html.Node {
	var out []*html.Node
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		out = append(out, ch)
	}
	return out
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// hasClassPrefix reports whether any class token of n starts with prefix.
// Substack suffixes its block classes (subscription-widget-wrap-editor), so an
// exact match would miss variants.
func hasClassPrefix(n *html.Node, prefix string) bool {
	for _, tok := range strings.Fields(attr(n, "class")) {
		if strings.HasPrefix(tok, prefix) {
			return true
		}
	}
	return false
}

// descendant returns the first element under n (depth first) that satisfies ok.
func descendant(n *html.Node, ok func(*html.Node) bool) *html.Node {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && ok(ch) {
			return ch
		}
		if d := descendant(ch, ok); d != nil {
			return d
		}
	}
	return nil
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}

func isSkipped(n *html.Node) bool {
	if n.Data == "svg" {
		return true
	}
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Iframe, atom.Noscript, atom.Button, atom.Form,
		atom.Input, atom.Select, atom.Textarea, atom.Video, atom.Audio, atom.Source,
		atom.Template, atom.Object, atom.Embed, atom.Canvas, atom.Head, atom.Title:
		return true
	}
	return false
}

func isBlockLevel(n *html.Node) bool {
	switch n.DataAtom {
	case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Ul, atom.Ol,
		atom.Li, atom.Blockquote, atom.Pre, atom.Hr, atom.Table, atom.Div, atom.Section,
		atom.Article, atom.Figure, atom.Figcaption, atom.Header, atom.Footer, atom.Nav,
		atom.Aside, atom.Main, atom.Details, atom.Summary, atom.Dl, atom.Dt, atom.Dd,
		atom.Address, atom.Fieldset, atom.Center:
		return true
	}
	return false
}

// blocksOf converts sibling nodes into Markdown blocks. Consecutive inline
// nodes (text, links, emphasis) are gathered into one paragraph.
func (c *mdConv) blocksOf(nodes []*html.Node) []mdBlock {
	var out []mdBlock
	var run []*html.Node
	flush := func() {
		if len(run) == 0 {
			return
		}
		if t := c.paragraph(run); t != "" {
			out = append(out, mdBlock{mdPara, t})
		}
		run = nil
	}
	for _, n := range nodes {
		if n.Type == html.ElementNode && isBlockLevel(n) && !isSkipped(n) {
			flush()
			out = append(out, c.blockElement(n)...)
			continue
		}
		run = append(run, n)
	}
	flush()
	return out
}

func (c *mdConv) blockElement(n *html.Node) []mdBlock {
	if n.DataAtom == atom.Div {
		if blocks, handled := c.substackBlock(n); handled {
			return blocks
		}
	}
	switch n.DataAtom {
	case atom.P:
		if t := c.paragraph(children(n)); t != "" {
			return []mdBlock{{mdPara, t}}
		}
		return nil
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		if t := oneLine(c.inlineNodes(children(n))); t != "" {
			return []mdBlock{{mdOther, strings.Repeat("#", level) + " " + t}}
		}
		return nil
	case atom.Ul, atom.Ol:
		if b, ok := c.list(n); ok {
			return []mdBlock{b}
		}
		return nil
	case atom.Blockquote:
		return c.quote(children(n))
	case atom.Pre:
		return []mdBlock{c.codeBlock(n)}
	case atom.Hr:
		return []mdBlock{{mdOther, "---"}}
	case atom.Table:
		if t := c.table(n); t != "" {
			return []mdBlock{{mdOther, t}}
		}
		return nil
	}
	return c.blocksOf(children(n))
}

func (c *mdConv) quote(nodes []*html.Node) []mdBlock {
	inner := c.blocksOf(nodes)
	if len(inner) == 0 {
		return nil
	}
	parts := make([]string, 0, len(inner))
	for _, b := range inner {
		parts = append(parts, b.text)
	}
	lines := strings.Split(strings.Join(parts, "\n\n"), "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	return []mdBlock{{mdOther, strings.Join(lines, "\n")}}
}

func (c *mdConv) codeBlock(pre *html.Node) mdBlock {
	lang := ""
	if code := descendant(pre, func(x *html.Node) bool { return x.DataAtom == atom.Code }); code != nil {
		for _, tok := range strings.Fields(attr(code, "class")) {
			if strings.HasPrefix(tok, "language-") {
				lang = strings.TrimPrefix(tok, "language-")
			}
		}
	}
	text := strings.Trim(textContent(pre), "\n")
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return mdBlock{mdOther, fence + lang + "\n" + text + "\n" + fence}
}

func (c *mdConv) list(n *html.Node) (mdBlock, bool) {
	ordered := n.DataAtom == atom.Ol
	start := 1
	if ordered {
		if v, err := strconv.Atoi(attr(n, "start")); err == nil {
			start = v
		}
	}
	var items []string
	loose := false
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type != html.ElementNode || ch.DataAtom != atom.Li {
			continue
		}
		marker := "-"
		if ordered {
			marker = strconv.Itoa(start+len(items)) + "."
		}
		text := joinItemBlocks(c.blocksOf(children(ch)))
		if strings.Contains(text, "\n\n") {
			loose = true
		}
		items = append(items, indentItem(marker, text))
	}
	if len(items) == 0 {
		return mdBlock{}, false
	}
	sep := "\n"
	if loose {
		sep = "\n\n"
	}
	return mdBlock{mdList, strings.Join(items, sep)}, true
}

func joinItemBlocks(blocks []mdBlock) string {
	var b strings.Builder
	for i, blk := range blocks {
		if i > 0 {
			if blk.kind == mdList && blocks[i-1].kind == mdPara {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}
		b.WriteString(blk.text)
	}
	return b.String()
}

func indentItem(marker, text string) string {
	if text == "" {
		return marker
	}
	pad := strings.Repeat(" ", len(marker)+1)
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		switch {
		case i == 0:
			lines[i] = marker + " " + l
		case l != "":
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}

func (c *mdConv) table(n *html.Node) string {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != html.ElementNode {
				continue
			}
			if ch.DataAtom == atom.Tr {
				var cells []string
				for cell := ch.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type == html.ElementNode && (cell.DataAtom == atom.Td || cell.DataAtom == atom.Th) {
						t := oneLine(c.inlineNodes(children(cell)))
						cells = append(cells, strings.ReplaceAll(t, "|", `\|`))
					}
				}
				rows = append(rows, cells)
				continue
			}
			walk(ch)
		}
	}
	walk(n)
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	if width == 0 {
		return ""
	}
	line := func(cells []string) string {
		padded := make([]string, width)
		copy(padded, cells)
		return "| " + strings.Join(padded, " | ") + " |"
	}
	sep := make([]string, width)
	for i := range sep {
		sep[i] = "---"
	}
	out := []string{line(rows[0]), line(sep)}
	for _, r := range rows[1:] {
		out = append(out, line(r))
	}
	return strings.Join(out, "\n")
}

// ---- Substack's own blocks ----

func (c *mdConv) substackBlock(n *html.Node) ([]mdBlock, bool) {
	switch {
	case hasClassPrefix(n, "captioned-image-container"):
		return c.capturedImage(n), true
	case hasClassPrefix(n, "youtube-wrap"):
		return c.youtube(n), true
	case hasClassPrefix(n, "file-embed-wrapper"):
		return c.fileEmbed(n), true
	case hasClassPrefix(n, "digest-post-embed"):
		return c.embeddedPost(n), true
	case hasClassPrefix(n, "subscription-widget-wrap"):
		return c.subscribeWidget(n), true
	case hasClassPrefix(n, "poll-embed"):
		return []mdBlock{{mdPara, `*\[Poll: not included in the saved post\]*`}}, true
	case hasClassPrefix(n, "pullquote"), hasClassPrefix(n, "callout-block"):
		return c.quote(children(n)), true
	case hasClassPrefix(n, "paywall-jump"):
		return nil, true
	}
	return nil, false
}

func dataAttrs(n *html.Node) map[string]any {
	var m map[string]any
	if raw := attr(n, "data-attrs"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &m)
	}
	return m
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// capturedImage: one image per container. It stays a link when Substack linked
// it somewhere other than its own CDN copy (a perk page, a product), and its
// caption follows as an italic line.
func (c *mdConv) capturedImage(n *html.Node) []mdBlock {
	img := descendant(n, func(x *html.Node) bool { return x.DataAtom == atom.Img })
	if img == nil {
		return nil
	}
	md := c.image(img)
	if md == "" {
		return nil
	}
	if a := descendant(n, func(x *html.Node) bool { return x.DataAtom == atom.A && hasClassPrefix(x, "image-link") }); a != nil {
		if href := attr(a, "href"); href != "" && !strings.Contains(href, "substackcdn.com") {
			md = "[" + md + "](" + linkDest(href) + ")"
		}
	}
	out := []mdBlock{{mdPara, md}}
	if fc := descendant(n, func(x *html.Node) bool { return x.DataAtom == atom.Figcaption }); fc != nil {
		if cap := c.italicLine(children(fc)); cap != "" {
			out = append(out, mdBlock{mdPara, cap})
		}
	}
	return out
}

// italicLine renders nodes as one italic line without nested emphasis marks.
func (c *mdConv) italicLine(nodes []*html.Node) string {
	prev := c.plainEmphasis
	c.plainEmphasis = true
	text := oneLine(c.inlineNodes(nodes))
	c.plainEmphasis = prev
	return wrapMarks("*", text)
}

var youtubeEmbedRE = regexp.MustCompile(`/embed/([\w-]+)`)

func (c *mdConv) youtube(n *html.Node) []mdBlock {
	vid := str(dataAttrs(n), "videoId")
	if vid == "" {
		if f := descendant(n, func(x *html.Node) bool { return x.DataAtom == atom.Iframe }); f != nil {
			if m := youtubeEmbedRE.FindStringSubmatch(attr(f, "src")); m != nil {
				vid = m[1]
			}
		}
	}
	if vid == "" {
		return nil
	}
	return []mdBlock{{mdPara, "![YouTube](https://www.youtube.com/watch?v=" + vid + ")"}}
}

func (c *mdConv) fileEmbed(n *html.Node) []mdBlock {
	byClass := func(class string) *html.Node {
		return descendant(n, func(x *html.Node) bool { return hasClassPrefix(x, class) })
	}
	btn := descendant(n, func(x *html.Node) bool {
		return x.DataAtom == atom.A && hasClassPrefix(x, "file-embed-button") && attr(x, "href") != ""
	})
	if btn == nil {
		return nil
	}
	name, size := "file", ""
	if h1 := byClass("file-embed-details-h1"); h1 != nil {
		if t := collapseSpace(textContent(h1)); strings.TrimSpace(t) != "" {
			name = strings.TrimSpace(t)
		}
	}
	if h2 := byClass("file-embed-details-h2"); h2 != nil {
		size = strings.TrimSpace(collapseSpace(textContent(h2)))
	}
	label := "Attachment: " + name
	if size != "" {
		label += " (" + size + ")"
	}
	return []mdBlock{{mdPara, "[" + escapeText(label) + "](" + linkDest(attr(btn, "href")) + ")"}}
}

func (c *mdConv) embeddedPost(n *html.Node) []mdBlock {
	d := dataAttrs(n)
	title, href := strings.TrimSpace(str(d, "title")), str(d, "canonical_url")
	switch {
	case title == "" && href == "":
		return nil
	case href == "":
		return []mdBlock{{mdPara, "Embedded post: " + escapeText(title)}}
	}
	if title == "" {
		title = href
	}
	return []mdBlock{{mdPara, "Embedded post: [" + escapeText(title) + "](" + linkDest(href) + ")"}}
}

func (c *mdConv) subscribeWidget(n *html.Node) []mdBlock {
	p := descendant(n, func(x *html.Node) bool { return x.DataAtom == atom.P && hasClassPrefix(x, "cta-caption") })
	if p == nil {
		return nil
	}
	if cap := c.italicLine(children(p)); cap != "" {
		return []mdBlock{{mdPara, cap}}
	}
	return nil
}

// ---- inline level ----

// paragraph renders inline nodes as one paragraph, with the characters that
// would turn a line into a heading, quote or list escaped.
func (c *mdConv) paragraph(nodes []*html.Node) string {
	s := c.inlineNodes(nodes)
	s = lineBreakPadRE.ReplaceAllString(s, "\\\n")
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "\\")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return escapeLineStarts(s)
}

var lineBreakPadRE = regexp.MustCompile(` *\\\n *`)

func (c *mdConv) inlineNodes(nodes []*html.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(c.inline(n))
	}
	return b.String()
}

func (c *mdConv) inline(n *html.Node) string {
	switch n.Type {
	case html.TextNode:
		return escapeText(collapseSpace(n.Data))
	case html.ElementNode:
	default:
		return ""
	}
	if isSkipped(n) {
		return ""
	}
	switch n.DataAtom {
	case atom.Br:
		return "\\\n"
	case atom.A:
		return c.link(n)
	case atom.Img:
		return c.image(n)
	case atom.Strong, atom.B:
		if c.plainEmphasis {
			return c.inlineNodes(children(n))
		}
		return wrapMarks("**", c.inlineNodes(children(n)))
	case atom.Em, atom.I:
		if c.plainEmphasis {
			return c.inlineNodes(children(n))
		}
		return wrapMarks("*", c.inlineNodes(children(n)))
	case atom.Del, atom.S, atom.Strike:
		return wrapMarks("~~", c.inlineNodes(children(n)))
	case atom.Code:
		return codeSpan(textContent(n))
	}
	out := c.inlineNodes(children(n))
	if isBlockLevel(n) {
		// A block element inside an inline context (a div inside a link):
		// keep its text apart from what follows.
		out += " "
	}
	return out
}

func (c *mdConv) link(n *html.Node) string {
	inner := strings.ReplaceAll(c.inlineNodes(children(n)), "\\\n", " ")
	href := attr(n, "href")
	if href == "" {
		return inner
	}
	core := strings.TrimSpace(inner)
	lead := inner[:len(inner)-len(strings.TrimLeft(inner, " "))]
	trail := inner[len(strings.TrimRight(inner, " ")):]
	if core == "" {
		lead, trail = "", ""
	}
	return lead + "[" + core + "](" + linkDest(href) + ")" + trail
}

func (c *mdConv) image(n *html.Node) string {
	src := strings.TrimSpace(attr(n, "src"))
	if src == "" {
		return ""
	}
	alt := strings.Join(strings.Fields(attr(n, "alt")), " ")
	alt = strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(alt)
	return "![" + alt + "](" + linkDest(src) + ")"
}

// wrapMarks puts an emphasis marker around text, keeping the spaces at its
// edges outside the marks ("a " in bold is "**a** ", which Markdown requires).
func wrapMarks(marker, text string) string {
	core := strings.TrimSpace(text)
	if core == "" {
		return text
	}
	lead := text[:len(text)-len(strings.TrimLeft(text, " "))]
	trail := text[len(strings.TrimRight(text, " ")):]
	return lead + marker + core + marker + trail
}

func codeSpan(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}

// linkDest makes a URL safe to put between the parentheses of a Markdown link.
func linkDest(u string) string {
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "\n", "").Replace(strings.TrimSpace(u))
}

// oneLine joins a rendered inline run onto a single line (headings, table
// cells, captions, link text cannot hold a line break).
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\\\n", " ")), " ")
}

var spaceRunRE = regexp.MustCompile(`[ \t\r\n\f\x{00A0}]+`)

func collapseSpace(s string) string { return spaceRunRE.ReplaceAllString(s, " ") }

func isAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// escapeText backslash-escapes the characters that would be read as Markdown
// (or, in Obsidian, as math) inside running text.
func escapeText(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		var prev, next rune
		if i > 0 {
			prev = runes[i-1]
		}
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		switch r {
		case '\\', '*', '`', '[', ']', '<', '$':
			b.WriteByte('\\')
		case '_':
			// An underscore inside a word is not emphasis; anywhere else it can be.
			if !(isAlnum(prev) && isAlnum(next)) {
				b.WriteByte('\\')
			}
		case '~':
			if prev == '~' || next == '~' {
				b.WriteByte('\\')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

var (
	headingStartRE = regexp.MustCompile(`^#{1,6}( |$)`)
	bulletStartRE  = regexp.MustCompile(`^[-+]( |$)`)
	orderedStartRE = regexp.MustCompile(`^(\d+)([.)])( |$)`)
	ruleStartRE    = regexp.MustCompile(`^(?:-{3,}|={3,}|_{3,})$`)
)

// escapeLineStarts escapes a line that would otherwise start a heading, quote,
// list or rule. (`*` is already escaped everywhere by escapeText.)
func escapeLineStarts(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, ">"), headingStartRE.MatchString(l), bulletStartRE.MatchString(l), ruleStartRE.MatchString(l):
			lines[i] = `\` + l
		case orderedStartRE.MatchString(l):
			lines[i] = orderedStartRE.ReplaceAllString(l, `$1\$2$3`)
		}
	}
	return strings.Join(lines, "\n")
}
