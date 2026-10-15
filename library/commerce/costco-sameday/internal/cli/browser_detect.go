// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored (costco-sameday-browser-login): finds an installed
// Chromium-based browser for `auth login` (Chrome, Chromium, Brave, Edge,
// Vivaldi, Arc), including per-user ~/Applications on macOS, and names the
// browser it found in messages.

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type browserCandidate struct {
	Name string
	Path string
}

// macBrowserApps is the preference order on macOS: app bundle, executable,
// display name. Arc is last: it is Chromium-based but manages profiles its
// own way.
var macBrowserApps = []struct{ App, Exe, Name string }{
	{"Google Chrome.app", "Google Chrome", "Google Chrome"},
	{"Chromium.app", "Chromium", "Chromium"},
	{"Google Chrome Beta.app", "Google Chrome Beta", "Google Chrome Beta"},
	{"Brave Browser.app", "Brave Browser", "Brave"},
	{"Microsoft Edge.app", "Microsoft Edge", "Microsoft Edge"},
	{"Vivaldi.app", "Vivaldi", "Vivaldi"},
	{"Arc.app", "Arc", "Arc"},
}

var windowsBrowserPaths = []struct{ Rel, Name string }{
	{`Google\Chrome\Application\chrome.exe`, "Google Chrome"},
	{`BraveSoftware\Brave-Browser\Application\brave.exe`, "Brave"},
	{`Microsoft\Edge\Application\msedge.exe`, "Microsoft Edge"},
	{`Vivaldi\Application\vivaldi.exe`, "Vivaldi"},
}

var pathBrowserNames = []struct{ Bin, Name string }{
	{"google-chrome", "Google Chrome"},
	{"google-chrome-stable", "Google Chrome"},
	{"chromium", "Chromium"},
	{"chromium-browser", "Chromium"},
	{"chrome", "Google Chrome"},
	{"brave-browser", "Brave"},
	{"brave", "Brave"},
	{"microsoft-edge", "Microsoft Edge"},
	{"microsoft-edge-stable", "Microsoft Edge"},
	{"vivaldi", "Vivaldi"},
	{"vivaldi-stable", "Vivaldi"},
}

// browserCandidates lists absolute install locations to probe for goos.
func browserCandidates(goos, home string, getenv func(string) string) []browserCandidate {
	var out []browserCandidate
	switch goos {
	case "darwin":
		roots := []string{"/Applications"}
		if home != "" {
			roots = append(roots, filepath.Join(home, "Applications"))
		}
		for _, app := range macBrowserApps {
			for _, root := range roots {
				out = append(out, browserCandidate{Name: app.Name, Path: root + "/" + app.App + "/Contents/MacOS/" + app.Exe})
			}
		}
	case "windows":
		for _, b := range windowsBrowserPaths {
			for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
				if base := getenv(env); base != "" {
					out = append(out, browserCandidate{Name: b.Name, Path: base + `\` + b.Rel})
				}
			}
		}
	}
	return out
}

// detectInstalledBrowser returns the first installed browser: known install
// locations first, then PATH.
func detectInstalledBrowser() (browserCandidate, bool) {
	home, _ := os.UserHomeDir()
	for _, c := range browserCandidates(runtime.GOOS, home, os.Getenv) {
		if _, err := os.Stat(c.Path); err == nil {
			return c, true
		}
	}
	for _, b := range pathBrowserNames {
		if p, err := exec.LookPath(b.Bin); err == nil {
			return browserCandidate{Name: b.Name, Path: p}, true
		}
	}
	return browserCandidate{}, false
}

// browserNameForPath names the browser behind an executable path, for
// messages ("A Brave window opened ..."). Unknown paths say "browser".
func browserNameForPath(p string) string {
	l := strings.ToLower(filepath.Base(strings.ReplaceAll(p, `\`, "/")))
	full := strings.ToLower(p)
	switch {
	case strings.Contains(l, "brave"):
		return "Brave"
	case strings.Contains(l, "msedge") || strings.Contains(l, "microsoft-edge") || strings.Contains(l, "microsoft edge"):
		return "Microsoft Edge"
	case strings.Contains(l, "vivaldi"):
		return "Vivaldi"
	case l == "arc" || strings.Contains(full, "/arc.app/"):
		return "Arc"
	case strings.Contains(l, "chromium"):
		return "Chromium"
	case strings.Contains(l, "chrome"):
		if strings.Contains(l, "beta") {
			return "Google Chrome Beta"
		}
		return "Google Chrome"
	}
	return "browser"
}
