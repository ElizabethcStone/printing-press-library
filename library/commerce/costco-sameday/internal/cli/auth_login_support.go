// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored glue for the one-step browser `auth login` and the
// actionable auth error hints (expired session, Costco sign-in 429,
// keychain denial). Preserved across regenerate via .printing-press-patches.

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/commerce/costco-sameday/internal/client"
	"github.com/mvanhorn/printing-press-library/library/commerce/costco-sameday/internal/config"
	"github.com/spf13/cobra"
)

// Hint text shared by auth login, auth status, doctor and API errors.
const (
	hintRunAuthLogin    = "run 'costco-sameday-pp-cli auth login' to sign in again (opens a browser window)"
	hintSigninRateLimit = "Costco is rate-limiting sign-in; wait about 15-30 minutes, then run 'costco-sameday-pp-cli auth login' once"
	hintKeychainDenied  = "reading your main Chrome profile needs the \"Chrome Safe Storage\" keychain entry, and access was denied. Use the browser login instead: 'costco-sameday-pp-cli auth login' (no keychain needed), or import a DevTools cURL: 'pbpaste | costco-sameday-pp-cli auth login --cookies-file -'"
)

type browserLoginFlags struct {
	browserPath  string
	loginURL     string
	profileDir   string
	timeout      time.Duration
	headless     bool
	resetProfile bool
}

func (b *browserLoginFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&b.browserPath, "browser-path", "", "Chromium-based browser (Chrome, Chromium, Brave, Edge, Vivaldi, Arc) for the login window (default: auto-detect, or $"+browserLoginEnvBinary+")")
	cmd.Flags().StringVar(&b.loginURL, "login-url", sameDayOrigin, "Page the login window opens")
	cmd.Flags().DurationVar(&b.timeout, "timeout", 10*time.Minute, "How long to wait for you to finish signing in")
	cmd.Flags().BoolVar(&b.resetProfile, "reset-profile", false, "Delete the CLI's login-window profile first (fresh browser, signs you out of it; only a profile auth login created)")
	cmd.Flags().StringVar(&b.profileDir, "login-profile-dir", "", "Login-window profile directory (default: <data dir>/"+browserLoginProfile+")")
	cmd.Flags().BoolVar(&b.headless, "login-headless", false, "Run the login window headless (testing only; you cannot sign in headless)")
	_ = cmd.Flags().MarkHidden("login-headless")
	_ = cmd.Flags().MarkHidden("login-profile-dir")
}

func runBrowserLoginFromFlags(cmd *cobra.Command, flags *rootFlags, b browserLoginFlags) (browserLoginResult, error) {
	dir := b.profileDir
	if dir == "" {
		d, err := defaultBrowserProfileDir()
		if err != nil {
			return browserLoginResult{}, err
		}
		dir = d
	}
	if b.resetProfile {
		if err := resetLoginProfileDir(dir); err != nil {
			return browserLoginResult{}, err
		}
	}
	return runBrowserLogin(cmd.Context(), browserLoginOptions{
		StartURL:    b.loginURL,
		CookieURL:   sameDayCookieURL(b.loginURL),
		ProfileDir:  dir,
		BrowserPath: b.browserPath,
		Headless:    b.headless,
		Timeout:     b.timeout,
		Probe:       productionCookieProbe(flags),
		Out:         cmd.ErrOrStderr(),
	})
}

// loginProfileMarker is written inside a login profile directory the CLI
// created. --reset-profile deletes only directories that carry it.
const (
	loginProfileMarker        = ".costco-sameday-pp-cli-login-profile"
	loginProfileMarkerContent = "Created by costco-sameday-pp-cli auth login. --reset-profile may delete this directory.\n"
)

// prepareLoginProfileDir makes sure the login profile exists and is private.
// A missing or empty directory is created 0700 and marked as CLI-owned. An
// existing marked directory is re-tightened to 0700. An existing unmarked,
// non-empty directory (only reachable via the hidden --login-profile-dir) is
// used as-is and never marked, so --reset-profile will refuse it.
func prepareLoginProfileDir(dir string) error {
	fi, err := os.Lstat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating login profile dir: %w", err)
		}
		return writeLoginProfileMarker(dir)
	case err != nil:
		return fmt.Errorf("checking login profile dir: %w", err)
	case !fi.IsDir():
		return fmt.Errorf("login profile path %s is not a directory", dir)
	}
	if hasLoginProfileMarker(dir) {
		return os.Chmod(dir, 0o700)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading login profile dir: %w", err)
	}
	if len(entries) == 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
		return writeLoginProfileMarker(dir)
	}
	return nil
}

func writeLoginProfileMarker(dir string) error {
	if err := os.WriteFile(filepath.Join(dir, loginProfileMarker), []byte(loginProfileMarkerContent), 0o600); err != nil {
		return fmt.Errorf("marking login profile dir: %w", err)
	}
	return nil
}

func hasLoginProfileMarker(dir string) bool {
	fi, err := os.Lstat(filepath.Join(dir, loginProfileMarker))
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, loginProfileMarker)) // #nosec G304 -- fixed name inside the profile dir.
	return err == nil && string(data) == loginProfileMarkerContent
}

// resetLoginProfileDir deletes the login profile only when it is a real
// directory (not a symlink) carrying the CLI's marker file. Anything else,
// including a custom --login-profile-dir or its parent, is refused.
func resetLoginProfileDir(dir string) error {
	clean := filepath.Clean(dir)
	fi, err := os.Lstat(clean)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking login profile dir: %w", err)
	}
	home, _ := os.UserHomeDir()
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("refusing to reset %s: it is a symlink", clean)
	case !fi.IsDir():
		return fmt.Errorf("refusing to reset %s: not a directory", clean)
	case clean == string(filepath.Separator) || clean == filepath.VolumeName(clean)+string(filepath.Separator) || (home != "" && clean == filepath.Clean(home)):
		return fmt.Errorf("refusing to reset %s", clean)
	case !hasLoginProfileMarker(clean):
		return fmt.Errorf("refusing to reset %s: it was not created by 'auth login' (no %s marker); delete it yourself if you are sure", clean, loginProfileMarker)
	}
	if err := os.RemoveAll(clean); err != nil {
		return fmt.Errorf("resetting login profile: %w", err)
	}
	return nil
}

// sameDayCookieURL is the origin whose cookies are captured: Same-Day, unless
// the login URL points elsewhere (tests use a local page).
func sameDayCookieURL(loginURL string) string {
	if strings.Contains(loginURL, "costco.com") {
		return sameDayOrigin
	}
	return loginURL
}

// productionCookieProbe classifies captured cookies with the read-only
// guest/member probe, using an in-memory credential (nothing is saved).
func productionCookieProbe(flags *rootFlags) cookieProbe {
	return func(ctx context.Context, header string) accountTypeResult {
		cfg, err := config.Load(flags.configPath)
		if err != nil {
			return accountTypeResult{Type: accountTypeUnknown}
		}
		cfg.AccessToken = header
		cfg.CredentialDomain = "sameday.costco.com"
		c := client.New(cfg, flags.timeout, flags.rateLimit)
		c.NoCache = true
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		return probeAccountType(pctx, c)
	}
}

// browserLoginHint turns browser-login failures into next steps.
func browserLoginHint(err error) error {
	var te *browserLoginTimeoutError
	switch {
	case errors.Is(err, errSigninRateLimited):
		return fmt.Errorf("%w\nhint: %s", err, hintSigninRateLimit)
	case errors.Is(err, errBrowserNotFound):
		return err
	case errors.As(err, &te):
		return fmt.Errorf("%w\nhint: run 'costco-sameday-pp-cli auth login --timeout 20m' for more time, or import a DevTools cURL: 'pbpaste | costco-sameday-pp-cli auth login --cookies-file -'", err)
	default:
		return fmt.Errorf("browser login: %w\nhint: fallbacks are 'costco-sameday-pp-cli auth login --chrome' or 'pbpaste | costco-sameday-pp-cli auth login --cookies-file -'", err)
	}
}

// keychainDenied spots macOS keychain / Linux keyring refusals from cookie
// tools (pycookiecheat, cookies, cookie-scoop).
func keychainDenied(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range []string{"keychain", "safe storage", "seckeychain", "errsecauthfailed", "user interaction is not allowed", "user canceled", "user cancelled", "-25293", "-128", "secretstorage", "keyring", "libsecret", "kwallet"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

func keychainHint(err error) error {
	if err == nil || !keychainDenied(err.Error()) {
		return err
	}
	return fmt.Errorf("%w\nhint: %s", err, hintKeychainDenied)
}

// sessionAuthHint is appended to 401/403 and GraphQL auth failures.
func sessionAuthHint(msg string) string {
	if strings.Contains(strings.ToLower(msg), "signin.costco.com") && strings.Contains(msg, "429") {
		return hintSigninRateLimit
	}
	return "the Same-Day session is missing or expired; " + hintRunAuthLogin
}
