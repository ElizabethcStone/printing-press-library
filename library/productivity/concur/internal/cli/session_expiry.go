// Copyright 2026 Allen Lew and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PATCH(amend-2026-10-18: auth login saved an already-expired session) -- the
// cookie sources hand back whatever they have on file, and a Concur JWT lasts
// about an hour, so a stale browser profile or an old press-auth capture was
// reported as "OK Found 2 cookies / Session saved" while every later call got
// HTTP 401. Reading the token's own deadline lets login fail where the cause
// is, instead of one network round trip later in a different command.

// jwtExpiry reads the exp claim of a JWT. The signature is not verified: the
// deadline is only used to avoid saving a session the server would reject.
func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(claims.Exp), 0), true
}

// expiredSessionError reports a captured JWT that has already expired. A token
// whose expiry cannot be read is let through for the server to judge.
func expiredSessionError(token string, now time.Time) error {
	exp, ok := jwtExpiry(token)
	if !ok || exp.After(now) {
		return nil
	}
	return fmt.Errorf("the Concur session you captured expired %s ago (%s)", humanAge(now.Sub(exp)), exp.Local().Format("2006-01-02 15:04 MST"))
}

func humanAge(d time.Duration) string {
	switch {
	case d < 2*time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}
