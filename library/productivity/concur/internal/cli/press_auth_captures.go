package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/productivity/concur/internal/config"
)

// PATCH(amend-2026-10-15: press-auth lookups were apex-only) -- Concur sets
// _csrf host-only on the regional web host (e.g. us2.concursolutions.com)
// while JWT is scoped to the apex. press-auth keeps only the cookies whose
// domain attribute matches the capture key, so a capture keyed on the apex
// can never hold _csrf, and a capture keyed on the regional host was never
// consulted. Result: `auth login --chrome` failed with "_csrf not found"
// for a user who was signed in and had captured a perfectly good session.

// pressAuthCapture is one entry of `press-auth list --json`.
type pressAuthCapture struct {
	Domain     string `json:"domain"`
	CapturedAt string `json:"captured_at"`
}

// pressAuthCaptureKeys returns the press-auth capture keys that can hold
// cookies for domain -- the apex itself and any captured subdomain -- newest
// capture first. When the list output is unusable or has no related capture
// it returns just domain, so a user with no capture still gets press-auth's
// own "not captured" message instead of a silent skip.
func pressAuthCaptureKeys(listJSON []byte, domain string) []string {
	var captures []pressAuthCapture
	if err := json.Unmarshal(listJSON, &captures); err != nil {
		return []string{domain}
	}
	related := make([]pressAuthCapture, 0, len(captures))
	for _, c := range captures {
		if c.Domain == domain || strings.HasSuffix(c.Domain, "."+domain) {
			related = append(related, c)
		}
	}
	if len(related) == 0 {
		return []string{domain}
	}
	capturedAt := func(c pressAuthCapture) time.Time {
		t, _ := time.Parse(time.RFC3339, c.CapturedAt)
		return t
	}
	sort.SliceStable(related, func(i, j int) bool {
		return capturedAt(related[i]).After(capturedAt(related[j]))
	})
	keys := make([]string, 0, len(related))
	for _, c := range related {
		keys = append(keys, c.Domain)
	}
	return keys
}

// pickCookieHeader chooses one Cookie header from headers (newest first).
// It returns the first that carries every required cookie, never a blend of
// several: a _csrf value is minted per sign-in, so pairing it with a JWT from
// a different capture would fail at the API instead of here. When no header
// is complete it returns the newest non-empty one so the caller's
// missing-cookie error names what is actually absent.
func pickCookieHeader(headers []string, required []string) string {
	newest := ""
	for _, h := range headers {
		if h == "" {
			continue
		}
		if newest == "" {
			newest = h
		}
		have := parseCookieString(h)
		complete := true
		for _, name := range required {
			if _, ok := have[name]; !ok {
				complete = false
				break
			}
		}
		if complete {
			return h
		}
	}
	return newest
}

// loadPressAuthCookies asks press-auth for every capture relevant to domain
// and returns the Cookie header chosen by pickCookieHeader. It errors only
// when no capture produced cookies, returning the first failure so callers
// keep press-auth's own recovery hint.
func loadPressAuthCookies(pressAuthPath, domain string, required []string) (string, error) {
	keys := []string{domain}
	if out, err := exec.Command(pressAuthPath, "list", "--json").Output(); err == nil {
		keys = pressAuthCaptureKeys(out, domain)
	}
	var headers []string
	var firstErr error
	for _, key := range keys {
		header, err := tryPressAuth(pressAuthPath, key)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		headers = append(headers, header)
	}
	picked := pickCookieHeader(headers, required)
	if picked == "" && firstErr != nil {
		return "", firstErr
	}
	return picked, nil
}

// configuredBaseURL returns the API base URL this CLI will use, or "" when
// the config cannot be loaded; callers treat "" as "region unknown".
func configuredBaseURL(flags *rootFlags) string {
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		return ""
	}
	return cfg.BaseURL
}

// pressAuthLoginCommand is the capture command that yields a session this
// CLI can use. It is keyed on the regional web host rather than the apex so
// the host-only _csrf cookie survives press-auth's domain filter.
func pressAuthLoginCommand(apex, apiBaseURL string) string {
	host, err := resolveReportsUIHost(apiBaseURL)
	if err != nil {
		host = "<region>." + apex
	}
	return fmt.Sprintf("press-auth login %s --login-url https://www.%s", host, apex)
}

// pressAuthShimHint explains the mise failure that otherwise reads as an
// opaque press-auth crash: the shim exists but the Go toolchain that built
// press-auth is no longer the active one.
const pressAuthShimHint = `The press-auth mise shim has no version for the active Go toolchain. Reinstall it with the Go you use now:

  go install github.com/mvanhorn/cli-printing-press/v4/cmd/press-auth@latest
`
