// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.
// read --save: write each post as a Markdown note (with properties), keep the
// raw HTML beside it as the master copy, and optionally save its images.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/substack"
)

// savedPost is everything savePost needs about one post.
type savedPost struct {
	Slug, Title, Subtitle, Source, PostDate, Access string
	Wordcount                                       int
	BodyHTML                                        string // exactly as Substack sent it
	Markdown                                        string // HTMLToMarkdown of BodyHTML, without properties
}

// savedFiles reports what savePost wrote.
type savedFiles struct {
	Markdown, HTML string
	Images         int // images saved next to the note
	ImageFailures  int // distinct images that could not be downloaded (their links stay remote)
}

var unsafeNameRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// safeFileName turns a slug into a file name that cannot leave the save folder.
// The slug comes from the network, so it is treated as untrusted.
func safeFileName(slug string) string {
	name := path.Base(strings.ReplaceAll(slug, `\`, "/"))
	name = strings.Trim(unsafeNameRE.ReplaceAllString(name, "-"), ".-")
	if name == "" {
		return "post"
	}
	return name
}

// savePost writes <slug>.md and <slug>.html into dir (created if missing); see
// saveName for the name when another post already holds that slug. With
// images, every distinct image in the note is fetched through fetch from its
// original address and saved as <slug>--images/NN.ext, and the note links it by
// a path relative to itself. An image that cannot be fetched keeps its remote
// link and is counted in ImageFailures; it never fails the save.
func savePost(ctx context.Context, dir string, p savedPost, images bool, fetch func(ctx context.Context, url string) ([]byte, error)) (savedFiles, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return savedFiles{}, fmt.Errorf("creating %s: %w", dir, err)
	}
	name, err := saveName(dir, p)
	if err != nil {
		return savedFiles{}, err
	}
	out := savedFiles{
		Markdown: filepath.Join(dir, name+".md"),
		HTML:     filepath.Join(dir, name+".html"),
	}

	md := p.Markdown
	if images {
		var err error
		md, out.Images, out.ImageFailures, err = localizeImages(ctx, md, dir, name+"--images", fetch)
		if err != nil {
			return out, err
		}
	}

	props := substack.Properties(substack.PostProperties{
		Title: p.Title, Subtitle: p.Subtitle, Source: p.Source,
		Published: p.PostDate, Access: p.Access, Wordcount: p.Wordcount,
	})
	if err := os.WriteFile(out.Markdown, []byte(props+md), 0o644); err != nil {
		return out, fmt.Errorf("writing %s: %w", out.Markdown, err)
	}
	page := "<!doctype html>\n<meta charset=\"utf-8\">\n<title>" + html.EscapeString(p.Title) + "</title>\n" + p.BodyHTML + "\n"
	if err := os.WriteFile(out.HTML, []byte(page), 0o644); err != nil {
		return out, fmt.Errorf("writing %s: %w", out.HTML, err)
	}
	return out, nil
}

// saveName picks the file name for p in dir: its slug, unless the note of that
// name was saved from another post (two publications can both have a
// "welcome"); then the publication's name goes in front, as
// <publication>--<slug>. A note is recognised by its source property, so saving
// the same post again overwrites its own files and nothing else. When both
// names hold other posts, the save fails instead of overwriting one.
func saveName(dir string, p savedPost) (string, error) {
	slug := safeFileName(p.Slug)
	names := []string{slug}
	if pub := publicationName(p.Source); pub != "" {
		names = append(names, pub+"--"+slug)
	}
	for _, name := range names {
		if noteIsFreeFor(filepath.Join(dir, name+".md"), p.Source) {
			return name, nil
		}
	}
	return "", fmt.Errorf("%s already holds another post", filepath.Join(dir, names[len(names)-1]+".md"))
}

// publicationName is the publication part of a post address, as a file name:
// "astralcodexten" for astralcodexten.substack.com, the host for a custom
// domain, "" when the address has no host.
func publicationName(source string) string {
	u, err := url.Parse(source)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	return safeFileName(strings.TrimSuffix(host, ".substack.com"))
}

// noteIsFreeFor reports whether a note may be written at path for the post at
// source: nothing is there yet, or the note there was saved from that post.
func noteIsFreeFor(path, source string) bool {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return true
	}
	return err == nil && noteSource(string(b)) == source
}

// noteSource reads the source property of a saved note ("" when it has none).
func noteSource(note string) string {
	front, ok := strings.CutPrefix(note, "---\n")
	if !ok {
		return ""
	}
	front, _, _ = strings.Cut(front, "\n---\n")
	for _, line := range strings.Split(front, "\n") {
		v, ok := strings.CutPrefix(line, "source: ")
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal([]byte(v), &s) != nil {
			return ""
		}
		return s
	}
	return ""
}

// markdownImageRE matches ![alt](http...) with the alt text escaping that
// HTMLToMarkdown produces.
var markdownImageRE = regexp.MustCompile(`!\[((?:\\.|[^\]\\])*)\]\((https?://[^)\s]+)\)`)

func isYouTube(u string) bool {
	return strings.Contains(u, "youtube.com/") || strings.Contains(u, "youtu.be/")
}

// localizeImages downloads the images of md into dir/relDir and rewrites their
// links. It returns the new Markdown, how many images were saved and how many
// distinct ones failed.
func localizeImages(ctx context.Context, md, dir, relDir string, fetch func(ctx context.Context, url string) ([]byte, error)) (string, int, int, error) {
	saved := map[string]string{} // remote url -> relative path ("" = failed)
	n, failures := 0, 0
	var firstErr error
	out := markdownImageRE.ReplaceAllStringFunc(md, func(m string) string {
		parts := markdownImageRE.FindStringSubmatch(m)
		alt, src := parts[1], parts[2]
		if isYouTube(src) || firstErr != nil {
			return m
		}
		rel, seen := saved[src]
		if !seen {
			data, err := fetch(ctx, substack.OriginalImageURL(src))
			if err == nil {
				file := fmt.Sprintf("%02d%s", n+1, imageExt(substack.OriginalImageURL(src), data))
				if mkErr := os.MkdirAll(filepath.Join(dir, relDir), 0o755); mkErr != nil {
					firstErr = fmt.Errorf("creating %s: %w", filepath.Join(dir, relDir), mkErr)
					return m
				}
				if wErr := os.WriteFile(filepath.Join(dir, relDir, file), data, 0o644); wErr != nil {
					firstErr = fmt.Errorf("writing image %s: %w", file, wErr)
					return m
				}
				n++
				rel = relDir + "/" + file
			} else {
				failures++
			}
			saved[src] = rel
		}
		if rel == "" {
			return m
		}
		return "![" + alt + "](" + rel + ")"
	})
	return out, n, failures, firstErr
}

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true, ".svg": true, ".bmp": true}

// imageExt picks a file extension: the one in the original address when it is a
// known image type, else one read from the bytes, else ".img".
func imageExt(origURL string, data []byte) string {
	if u, err := url.Parse(origURL); err == nil {
		if ext := strings.ToLower(path.Ext(u.Path)); imageExts[ext] {
			return ext
		}
	}
	switch http.DetectContentType(data) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	}
	return ".img"
}
