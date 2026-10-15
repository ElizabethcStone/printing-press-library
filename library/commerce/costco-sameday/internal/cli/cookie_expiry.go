// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored (costco-sameday-cookie-import): Safari cURL hint and the
// session cookie expiry for imported cookies, so `auth status` and `doctor`
// can show when an imported session runs out.

package cli

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// safariCookieHint explains the Safari gap: Safari's Copy as cURL leaves the
// cookies out, so the session cookie has to be copied from Web Inspector.
const safariCookieHint = "Safari's Copy as cURL leaves cookies out. In Safari open Web Inspector > Storage > Cookies, copy the " + sessionCookieName + " value (and its Expires date, optional), then run: echo '" + sessionCookieName + "=<value>; Expires=<date>' | costco-sameday-pp-cli auth login --cookies-file -"

// cookieAttrNames are Set-Cookie attributes that may ride along in a pasted
// cookie line; they are not cookies.
var cookieAttrNames = map[string]bool{
	"expires": true, "max-age": true, "path": true, "domain": true, "secure": true,
	"httponly": true, "samesite": true, "priority": true, "partitioned": true,
}

// stripCookieAttributes drops Set-Cookie attributes from a pasted cookie
// header and returns the expiry they carried (Max-Age wins over Expires, as
// in RFC 6265). now anchors Max-Age.
func stripCookieAttributes(header string, now time.Time) (string, time.Time) {
	var kept []string
	var expires, maxAge time.Time
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		key := strings.ToLower(strings.TrimSpace(name))
		if !cookieAttrNames[key] {
			kept = append(kept, part)
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "expires":
			if t, err := http.ParseTime(value); err == nil {
				expires = t
			} else if t, err := time.Parse(time.RFC3339, value); err == nil {
				expires = t
			}
		case "max-age":
			if n, err := strconv.Atoi(value); err == nil {
				maxAge = now.Add(time.Duration(n) * time.Second)
			}
		}
	}
	if !maxAge.IsZero() {
		expires = maxAge
	}
	return strings.Join(kept, "; "), expires
}

// importedSessionExpiry is the session cookie's expiry, zero when unknown.
func importedSessionExpiry(cookies []*http.Cookie) time.Time {
	for _, c := range cookies {
		if c != nil && c.Name == sessionCookieName && !c.Expires.IsZero() {
			return c.Expires
		}
	}
	return time.Time{}
}
