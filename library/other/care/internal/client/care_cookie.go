package client

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/other/care/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/other/care/internal/config"
)

// CareCookieCachePath is where `care-pp-cli auth profile-login/refresh` caches
// the session cookie header extracted from the persistent Chrome profile. Uses
// the shared cliutil path resolver so `--home`, `CARE_HOME`, and `CARE_DATA_DIR`
// isolate credentials per CLI home.
func CareCookieCachePath() string {
	dir, err := cliutil.DataDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "session-cookie")
}

// ClearSessionCookie removes the custom cached session cookie file written by
// `auth profile-login` / `auth refresh`.
func ClearSessionCookie() error {
	p := CareCookieCachePath()
	if p == "" {
		return nil
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// CustomSessionStatus reports whether the custom care.com session source
// (CARE_SESSION_COOKIE env or the cached `session-cookie` file from
// `auth profile-login`) is configured, along with a human-readable source label
// for `auth status` and `doctor`.
func CustomSessionStatus() (bool, string) {
	if sc := strings.TrimSpace(os.Getenv("CARE_SESSION_COOKIE")); sc != "" {
		return true, "environment (CARE_SESSION_COOKIE)"
	}
	p := CareCookieCachePath()
	if p == "" {
		return false, ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return false, ""
	}
	if strings.TrimSpace(string(b)) == "" {
		return false, ""
	}
	return true, "profile-login (" + p + ")"
}

// SessionCookie exposes the resolved care.com session cookie header to other
// packages (e.g. the messages command, which fetches the Stream token from the
// members page and then talks to Stream Chat directly).
func SessionCookie() string { return ResolveSessionCookie(nil) }

// ResolveSessionCookie resolves the care.com Cookie header across both the
// custom profile-login cache and the generated `auth login --chrome` /
// `auth login --cookies-file` credential stores.
func ResolveSessionCookie(cfg *config.Config) string {
	if sc := careSessionCookie(); sc != "" {
		return sc
	}
	if cfg == nil {
		cfg, _ = config.Load("")
	}
	if cfg != nil {
		if ck := strings.TrimSpace(cfg.CookieCredential()); ck != "" {
			return ck
		}
	}
	if jar := LoadCookieJar(); jar != nil {
		u := &url.URL{Scheme: "https", Host: "www.care.com", Path: "/"}
		if cookies := jar.Cookies(u); len(cookies) > 0 {
			parts := make([]string, 0, len(cookies))
			for _, c := range cookies {
				if c.Name != "" && c.Value != "" {
					parts = append(parts, c.Name+"="+c.Value)
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "; ")
			}
		}
	}
	return ""
}

// careSessionCookie resolves the custom care.com session cookie header.
// Precedence: CARE_SESSION_COOKIE env override, then the cached file.
func careSessionCookie() string {
	if sc := strings.TrimSpace(os.Getenv("CARE_SESSION_COOKIE")); sc != "" {
		return sc
	}
	p := CareCookieCachePath()
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
