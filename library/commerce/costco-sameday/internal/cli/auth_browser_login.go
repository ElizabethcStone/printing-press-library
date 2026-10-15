// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored one-step `auth login`: opens the installed Chrome/Chromium,
// headed, in a dedicated CLI-owned profile; the user signs in normally; the
// CLI reads only the Same-Day session cookies from THAT browser over CDP
// (--remote-debugging-pipe, so no DevTools TCP port on macOS/Linux), saves
// them, and closes the window. It never reads the user's main Chrome
// cookie store or Chrome Safe Storage. The browser exists only for the
// duration of the login (no resident browser transport); normal commands
// stay plain HTTP.
//
// Detection surface is kept small: the CLI attaches to the browser target
// only (Browser.getVersion, Target.getTargets, Storage.getCookies,
// Browser.close). It never
// attaches to the page, never enables Runtime, never injects script and
// never types or clicks; Chrome runs without --enable-automation.
//
// Preserved across regenerate via .printing-press-patches.

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/commerce/costco-sameday/internal/cliutil"
)

const (
	sameDayOrigin         = "https://sameday.costco.com/"
	browserLoginProfile   = "login-browser"
	browserLoginEnvBinary = "COSTCO_SAMEDAY_BROWSER"
)

// Typed browser-login failures so callers and tests can map them to
// actionable messages.
var (
	errSigninRateLimited = errors.New("Costco is rate-limiting sign-in (HTTP 429 from signin.costco.com). Wait about 15-30 minutes before trying again; retrying sooner extends the block")
	errBrowserNotFound   = errors.New("no Chromium-based browser found (looked for Chrome, Chromium, Brave, Edge, Vivaldi and Arc); install one, pass --browser-path, or set " + browserLoginEnvBinary + ". Fallbacks: 'auth login --chrome' or 'pbpaste | auth login --cookies-file -'")
	errBrowserClosed     = errors.New("the login window was closed before sign-in finished; run 'auth login' again")
	errProfileInUse      = errors.New("the CLI login profile is already open in another browser window (a previous 'auth login' may still be running); close that window and retry")
)

// browserLoginTimeoutError reports a sign-in that did not finish in time.
type browserLoginTimeoutError struct {
	Timeout     time.Duration
	SawSession  bool
	AccountType string
}

func (e *browserLoginTimeoutError) Error() string {
	msg := fmt.Sprintf("timed out after %s waiting for sign-in", e.Timeout)
	if e.AccountType == accountTypeGuest {
		msg += "; the window still had a guest session. Use 'Sign in via Costco.com' in the Same-Day menu, then run 'auth login' again"
	}
	return msg
}

// cookieProbe classifies a candidate Cookie header (in memory only).
type cookieProbe func(ctx context.Context, cookieHeader string) accountTypeResult

type browserLoginOptions struct {
	StartURL     string
	CookieURL    string
	ProfileDir   string
	BrowserPath  string
	Headless     bool
	Timeout      time.Duration
	PollInterval time.Duration
	ExtraFlags   []string
	Probe        cookieProbe
	Out          io.Writer
}

type browserLoginResult struct {
	Cookies     []sessionCookie
	Expires     time.Time
	AccountType accountTypeResult
}

// browserLoginExtraFlags lets tests add flags (e.g. to trust a local TLS
// test server). Empty in production.
var browserLoginExtraFlags []string

func defaultBrowserProfileDir() (string, error) {
	dir, err := cliutil.DataDir()
	if err != nil || dir == "" {
		return "", fmt.Errorf("resolving data dir for the login profile: %w", err)
	}
	return filepath.Join(dir, browserLoginProfile), nil
}

// findBrowserBinary picks an installed Chromium-based browser (browser_detect.go).
func findBrowserBinary(explicit string) (string, error) {
	for _, p := range []string{explicit, os.Getenv(browserLoginEnvBinary)} {
		if p = strings.TrimSpace(p); p != "" {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
			if lp, err := exec.LookPath(p); err == nil {
				return lp, nil
			}
			return "", fmt.Errorf("browser %q not found", p)
		}
	}
	if found, ok := detectInstalledBrowser(); ok {
		return found.Path, nil
	}
	return "", errBrowserNotFound
}

// rateLimitHosts are the host suffixes whose rate-limit pages abort login.
var rateLimitHosts = []string{"costco.com"}

// loginRateLimited spots a rate-limit page on a Costco host from the page
// URL and title (the CLI does not attach to the page to read status codes).
func loginRateLimited(rawURL, title string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	matched := false
	for _, h := range rateLimitHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			matched = true
		}
	}
	if !matched {
		return false
	}
	s := strings.ToLower(rawURL + " " + title)
	for _, marker := range []string{"429", "too many requests", "rate limit", "ratelimit", "too many attempts"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func hostMatchesCookie(cookieDomain, host string) bool {
	d := strings.TrimPrefix(strings.ToLower(cookieDomain), ".")
	host = strings.ToLower(host)
	return d == host || strings.HasSuffix(host, "."+d)
}

// pickSessionCookies keeps the session cookie and the optional companions
// for host, nothing else.
func pickSessionCookies(all []cdpCookie, host string) ([]sessionCookie, *cdpCookie) {
	wanted := map[string]bool{sessionCookieName: true}
	for _, n := range optionalSessionCookies {
		wanted[n] = true
	}
	var out []sessionCookie
	var sid *cdpCookie
	seen := map[string]bool{}
	for i := range all {
		c := &all[i]
		if !wanted[c.Name] || seen[c.Name] || !hostMatchesCookie(c.Domain, host) {
			continue
		}
		seen[c.Name] = true
		if c.Name == sessionCookieName {
			sid = c
			out = append([]sessionCookie{{Name: c.Name, Value: c.Value}}, out...)
			continue
		}
		out = append(out, sessionCookie{Name: c.Name, Value: c.Value})
	}
	return out, sid
}

func cookieHeaderOf(cs []sessionCookie) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// browserClosedGrace is how long polls must keep seeing no page target
// before the login window counts as closed (a var so tests can shorten it).
var browserClosedGrace = 4 * time.Second

// startLoginBrowserFn starts the login browser (replaced in tests).
var startLoginBrowserFn = startLoginBrowser

// redactedPageURL keeps only scheme, host and path of a page URL: no query,
// fragment or user info, which can carry SSO codes or state.
func redactedPageURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
}

// browserClosedErr reports a closed login window with the last page seen.
func browserClosedErr(lastURL string, out io.Writer) error {
	if lastURL == "" {
		return errBrowserClosed
	}
	fmt.Fprintf(out, "Login window closed; last page seen: %s\n", lastURL)
	return fmt.Errorf("%w (last page seen: %s)", errBrowserClosed, lastURL)
}

// runBrowserLogin launches the browser, waits for a signed-in session,
// captures the session cookies and closes the browser.
//
// The browser is driven over --remote-debugging-pipe (see
// auth_browser_cdp.go), so no DevTools TCP port is opened on macOS/Linux;
// the profile dir is private (0700); and the browser lives only until the
// cookies are read. Windows falls back to a 127.0.0.1 random port.
func runBrowserLogin(ctx context.Context, opts browserLoginOptions) (browserLoginResult, error) {
	var res browserLoginResult
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.StartURL == "" {
		opts.StartURL = sameDayOrigin
	}
	if opts.CookieURL == "" {
		opts.CookieURL = opts.StartURL
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Minute
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	cu, err := url.Parse(opts.CookieURL)
	if err != nil || cu.Hostname() == "" {
		return res, fmt.Errorf("invalid cookie URL %q", opts.CookieURL)
	}
	cookieHost := cu.Hostname()
	if opts.ProfileDir == "" {
		if opts.ProfileDir, err = defaultBrowserProfileDir(); err != nil {
			return res, err
		}
	}
	if err := prepareLoginProfileDir(opts.ProfileDir); err != nil {
		return res, err
	}
	bin, err := findBrowserBinary(opts.BrowserPath)
	if err != nil {
		return res, err
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	args := []string{
		"--user-data-dir=" + opts.ProfileDir,
		"--no-first-run",
		"--no-default-browser-check",
	}
	if opts.Headless {
		args = append(args, "--headless=new")
	}
	args = append(args, browserLoginExtraFlags...)
	args = append(args, opts.ExtraFlags...)
	args = append(args, opts.StartURL)
	started := time.Now()
	proc, cdpc, exited, err := startLoginBrowserFn(ctx, bin, args)
	if err != nil {
		return res, err
	}
	reaped := false
	defer func() {
		_ = cdpc.Close()
		if !reaped {
			_ = proc.Process.Kill()
			<-exited
		}
	}()

	// First contact: the browser answers once it is up. A browser that exits
	// right away handed off to another window on the same profile.
	vctx, vcancel := context.WithTimeout(ctx, 30*time.Second)
	verr := cdpc.Call(vctx, "Browser.getVersion", nil, nil)
	vcancel()
	if verr != nil {
		if ctx.Err() != nil {
			// Login deadline passed before the browser answered.
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return res, &browserLoginTimeoutError{Timeout: opts.Timeout}
			}
			return res, ctx.Err()
		}
		select {
		case <-exited:
			reaped = true
			if time.Since(started) < 10*time.Second {
				return res, errProfileInUse
			}
			return res, errBrowserClosed
		case <-time.After(2 * time.Second):
		}
		return res, fmt.Errorf("browser did not answer on the DevTools pipe: %w", verr)
	}

	closeBrowser := func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer ccancel()
		if err := cdpc.Call(cctx, "Browser.close", nil, nil); err != nil && !errors.Is(err, errCDPClosed) {
			return // deferred Kill handles it
		}
		select {
		case <-exited:
			reaped = true
		case <-time.After(5 * time.Second):
		}
	}

	fmt.Fprintf(opts.Out, "A %s window opened with a separate profile for this CLI.\n", browserNameForPath(bin))
	fmt.Fprintln(opts.Out, "Sign in with 'Sign in via Costco.com' as you normally would. This window closes by itself once you're signed in.")

	var (
		sawSignin   bool
		initialSID  string
		lastProbed  string
		lastProbeAt time.Time
		lastAcct    accountTypeResult
		sawSession  bool
		nextNotice  = time.Now().Add(time.Minute)
		// noPagesSince is when polls started seeing no page target;
		// lastPageURL is the last page seen (scheme, host and path only).
		noPagesSince time.Time
		lastPageURL  string
	)
	ticker := time.NewTicker(opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			closeBrowser()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return res, &browserLoginTimeoutError{Timeout: opts.Timeout, SawSession: sawSession, AccountType: lastAcct.Type}
			}
			return res, ctx.Err()
		case <-exited:
			// The browser process ended (window closed): no grace delay,
			// but still name the last page seen.
			reaped = true
			return res, browserClosedErr(lastPageURL, opts.Out)
		case <-ticker.C:
		}

		var targets struct {
			TargetInfos []cdpTargetInfo `json:"targetInfos"`
		}
		if err := cdpc.Call(ctx, "Target.getTargets", nil, &targets); err != nil {
			continue
		}
		pages, onSameDay := 0, false
		for _, ti := range targets.TargetInfos {
			if ti.Type == "page" {
				if u := redactedPageURL(ti.URL); u != "" {
					lastPageURL = u
				}
			}
			if ti.Type != "page" {
				continue
			}
			pages++
			if loginRateLimited(ti.URL, ti.Title) {
				closeBrowser()
				return res, errSigninRateLimited
			}
			u, perr := url.Parse(ti.URL)
			if perr != nil {
				continue
			}
			lp := strings.ToLower(u.Path)
			if (u.Hostname() != "" && u.Hostname() != cookieHost) || strings.Contains(lp, "signin") || strings.Contains(lp, "login") || strings.Contains(lp, "/sso/") {
				sawSignin = true
			}
			if u.Hostname() == cookieHost && !strings.Contains(lp, "/sso/") {
				onSameDay = true
			}
		}
		if pages == 0 {
			// SSO redirects (site hops, renderer swaps, popups) can briefly
			// leave no page target. Only a run of empty polls lasting
			// browserClosedGrace means the user closed the window.
			if noPagesSince.IsZero() {
				noPagesSince = time.Now()
			}
			if time.Since(noPagesSince) < browserClosedGrace {
				continue
			}
			closeBrowser()
			return res, browserClosedErr(lastPageURL, opts.Out)
		}
		noPagesSince = time.Time{}
		var jar struct {
			Cookies []cdpCookie `json:"cookies"`
		}
		if err := cdpc.Call(ctx, "Storage.getCookies", nil, &jar); err != nil {
			continue
		}
		picked, sid := pickSessionCookies(jar.Cookies, cookieHost)
		if sid == nil || sid.Value == "" {
			continue
		}
		sawSession = true
		if initialSID == "" {
			// First session seen. Without a member verdict, completion needs
			// a different session cookie after a sign-in page (the SSO
			// callback issues a new one).
			initialSID = sid.Value
		}
		if !onSameDay {
			continue
		}
		acct := accountTypeResult{Type: accountTypeUnknown, Evidence: "not probed"}
		if opts.Probe != nil {
			if sid.Value != lastProbed || time.Since(lastProbeAt) > 15*time.Second {
				lastAcct = opts.Probe(ctx, cookieHeaderOf(picked))
				lastProbed, lastProbeAt = sid.Value, time.Now()
			}
			acct = lastAcct
		}
		done := acct.Type == accountTypeMember ||
			(acct.Type == accountTypeUnknown && sawSignin && sid.Value != initialSID)
		if !done {
			if time.Now().After(nextNotice) {
				fmt.Fprintln(opts.Out, "Still waiting for sign-in (the window has a guest session so far)...")
				nextNotice = time.Now().Add(time.Minute)
			}
			continue
		}
		res.Cookies = picked
		res.AccountType = acct
		if !sid.Session && sid.Expires > 0 {
			res.Expires = time.Unix(int64(sid.Expires), 0)
		}
		closeBrowser()
		return res, nil
	}
}
