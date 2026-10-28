// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/substack"
)

const (
	testS3A = "https://substack-post-media.s3.amazonaws.com/public/images/aaa_10x10.png"
	testS3B = "https://substack-post-media.s3.amazonaws.com/public/images/bbb_20x20.jpg"
	testCDN = "https://substackcdn.com/image/fetch/$s_!x!,f_auto/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2F"
)

func testPost(markdown string) savedPost {
	return savedPost{
		Slug: "my-post", Title: "My <post>", Subtitle: "sub", Source: "https://pub.substack.com/p/my-post",
		PostDate: "2026-01-02T00:00:00.000Z", Access: "entitled", Wordcount: 12,
		BodyHTML: "<p>raw <b>html</b></p>", Markdown: markdown,
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func noImages(context.Context, string) ([]byte, error) {
	return nil, errors.New("no images expected")
}

// TestSavePostWritesNoteAndMasterCopy: --save writes <slug>.md (properties,
// then the Markdown) and <slug>.html (the body exactly as Substack sent it, in a
// minimal page so a browser reads it as UTF-8).
func TestSavePostWritesNoteAndMasterCopy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "notes", "uxmentor") // does not exist yet
	got, err := savePost(context.Background(), dir, testPost("# Hi\n"), false, noImages)
	if err != nil {
		t.Fatalf("savePost: %v", err)
	}
	if got.Markdown != filepath.Join(dir, "my-post.md") || got.HTML != filepath.Join(dir, "my-post.html") {
		t.Errorf("paths = %q, %q", got.Markdown, got.HTML)
	}

	md := readFile(t, got.Markdown)
	wantProps := substack.Properties(substack.PostProperties{
		Title: "My <post>", Subtitle: "sub", Source: "https://pub.substack.com/p/my-post",
		Published: "2026-01-02T00:00:00.000Z", Access: "entitled", Wordcount: 12,
	})
	if md != wantProps+"# Hi\n" {
		t.Errorf("note =\n%q\nwant\n%q", md, wantProps+"# Hi\n")
	}

	page := readFile(t, got.HTML)
	for _, want := range []string{`<meta charset="utf-8">`, "<title>My &lt;post&gt;</title>", "<p>raw <b>html</b></p>"} {
		if !strings.Contains(page, want) {
			t.Errorf("html page missing %q:\n%s", want, page)
		}
	}
}

// TestSavePostImagesAreSavedBesideTheNote: with --images every distinct image is
// downloaded from its ORIGINAL address (not the CDN resize URL), saved as
// <slug>--images/NN.ext, and the note links it by a path relative to the note.
func TestSavePostImagesAreSavedBesideTheNote(t *testing.T) {
	cdnA := testCDN + "aaa_10x10.png"
	md := "![one](" + cdnA + ")\n\n" +
		"![again](" + cdnA + ")\n\n" +
		"[![linked](" + testCDN + "bbb_20x20.jpg)](https://pub.substack.com/perks/p1)\n\n" +
		"![YouTube](https://www.youtube.com/watch?v=abcdefghijk)\n"
	var asked []string
	fetch := func(_ context.Context, u string) ([]byte, error) {
		asked = append(asked, u)
		return []byte("bytes of " + u), nil
	}

	dir := t.TempDir()
	got, err := savePost(context.Background(), dir, testPost(md), true, fetch)
	if err != nil {
		t.Fatalf("savePost: %v", err)
	}
	if got.Images != 2 || got.ImageFailures != 0 {
		t.Errorf("Images=%d ImageFailures=%d, want 2 and 0", got.Images, got.ImageFailures)
	}
	if strings.Join(asked, ",") != testS3A+","+testS3B {
		t.Errorf("downloads = %v, want each original once, in order", asked)
	}
	if b := readFile(t, filepath.Join(dir, "my-post--images", "01.png")); b != "bytes of "+testS3A {
		t.Errorf("01.png = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "my-post--images", "02.jpg")); err != nil {
		t.Errorf("02.jpg missing: %v", err)
	}

	note := readFile(t, got.Markdown)
	for _, want := range []string{
		"![one](my-post--images/01.png)",
		"![again](my-post--images/01.png)",
		"[![linked](my-post--images/02.jpg)](https://pub.substack.com/perks/p1)",
		"![YouTube](https://www.youtube.com/watch?v=abcdefghijk)",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "substackcdn.com") {
		t.Errorf("note still points at the CDN:\n%s", note)
	}
}

// TestSavePostImageFailureKeepsTheRemoteLink: one image that cannot be fetched
// must not lose the post or the other images.
func TestSavePostImageFailureKeepsTheRemoteLink(t *testing.T) {
	md := "![bad](" + testCDN + "aaa_10x10.png)\n\n![good](" + testCDN + "bbb_20x20.jpg)\n"
	fetch := func(_ context.Context, u string) ([]byte, error) {
		if u == testS3A {
			return nil, errors.New("HTTP 403")
		}
		return []byte("ok"), nil
	}
	got, err := savePost(context.Background(), t.TempDir(), testPost(md), true, fetch)
	if err != nil {
		t.Fatalf("savePost: %v", err)
	}
	if got.Images != 1 || got.ImageFailures != 1 {
		t.Errorf("Images=%d ImageFailures=%d, want 1 and 1", got.Images, got.ImageFailures)
	}
	note := readFile(t, got.Markdown)
	if !strings.Contains(note, "![bad]("+testCDN+"aaa_10x10.png)") {
		t.Errorf("failed image lost its remote link:\n%s", note)
	}
	if !strings.Contains(note, "![good](my-post--images/01.jpg)") {
		t.Errorf("good image not localised (numbering counts saved files only):\n%s", note)
	}
}

// TestSavePostNamesFilesAfterAnExtensionlessImage: an address with no file
// extension is typed from the bytes.
func TestSavePostNamesFilesFromTheBytesWhenTheURLHasNoExtension(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}
	fetch := func(context.Context, string) ([]byte, error) { return png, nil }
	dir := t.TempDir()
	if _, err := savePost(context.Background(), dir, testPost("![x](https://img.example.com/blob)\n"), true, fetch); err != nil {
		t.Fatalf("savePost: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "my-post--images", "01.png")); err != nil {
		t.Errorf("expected 01.png from the PNG signature: %v", err)
	}
}

// TestSavePostCannotEscapeTheFolder: the slug comes from the network; a hostile
// one must not write outside --save.
func TestSavePostCannotEscapeTheFolder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "notes")
	p := testPost("x\n")
	p.Slug = "../../evil"
	got, err := savePost(context.Background(), dir, p, false, noImages)
	if err != nil {
		t.Fatalf("savePost: %v", err)
	}
	if filepath.Dir(got.Markdown) != dir || filepath.Dir(got.HTML) != dir {
		t.Errorf("files escaped %s: %s, %s", dir, got.Markdown, got.HTML)
	}
	if _, err := os.Stat(filepath.Join(root, "evil.md")); err == nil {
		t.Error("wrote outside the save folder")
	}
}
