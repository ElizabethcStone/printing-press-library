// Copyright 2026 Allen Lew and contributors. Licensed under Apache-2.0. See LICENSE.
// PATCH(amend-2026-10-18: auth login saved an already-expired session)

package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/productivity/concur/internal/cliutil"
)

// testJWT builds an unsigned JWT whose payload carries only an exp claim.
func testJWT(exp time.Time) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]string{"alg": "none"}) + "." + enc(map[string]int64{"exp": exp.Unix()}) + ".sig"
}

func TestJWTExpiry(t *testing.T) {
	exp := time.Unix(1_800_000_000, 0)
	payloadWithoutExp := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x"}`))
	padded := base64.URLEncoding.EncodeToString([]byte(`{"exp":1800000000,"x":"y"}`))
	if !strings.HasSuffix(padded, "=") {
		t.Fatal("setup: expected a padded payload")
	}
	tests := []struct {
		name   string
		token  string
		want   time.Time
		wantOK bool
	}{
		{"valid token", testJWT(exp), exp, true},
		{"padded payload", "e30." + padded + ".sig", exp, true},
		{"no exp claim", "e30." + payloadWithoutExp + ".sig", time.Time{}, false},
		{"two segments", "abc.def", time.Time{}, false},
		{"payload is not base64", "a.!!!.c", time.Time{}, false},
		{"payload is not JSON", "a." + base64.RawURLEncoding.EncodeToString([]byte("plain text")) + ".c", time.Time{}, false},
		{"not a token at all", "csrf-value", time.Time{}, false},
		{"empty", "", time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := jwtExpiry(tt.token)
			if ok != tt.wantOK || !got.Equal(tt.want) {
				t.Fatalf("jwtExpiry = (%v, %v), want (%v, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestExpiredSessionError(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		token   string
		wantErr string // substring; empty means no error
	}{
		{"expires in an hour", testJWT(now.Add(time.Hour)), ""},
		{"expired 30 minutes ago", testJWT(now.Add(-30 * time.Minute)), "expired 30 minutes ago"},
		{"expired 5 hours ago", testJWT(now.Add(-5 * time.Hour)), "expired 5 hours ago"},
		{"expired days ago", testJWT(now.Add(-56 * time.Hour)), "expired 2 days ago"},
		{"unreadable expiry is left for the server", "not-a-jwt", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := expiredSessionError(tt.token, now)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestAuthLogin_RefusesAnExpiredSessionAndKeepsTheStoredOne(t *testing.T) {
	t.Setenv("CONCUR_HOME", t.TempDir())
	dataDir, err := cliutil.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	credentials := filepath.Join(dataDir, "credentials.toml")

	login := func(jwt string) (string, error) {
		t.Helper()
		file := filepath.Join(t.TempDir(), "cookies.txt")
		if err := os.WriteFile(file, []byte("JWT="+jwt+"; _csrf=csrf-value"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := RootCmd()
		cmd.SetArgs([]string{"auth", "login", "--cookies-file", file})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		return out.String(), err
	}

	t.Run("nothing is stored for an expired session", func(t *testing.T) {
		out, err := login(testJWT(time.Now().Add(-56 * time.Hour)))
		if err == nil {
			t.Fatalf("expected the expired session to be refused; output:\n%s", out)
		}
		if got := ExitCode(err); got != 4 {
			t.Errorf("exit code = %d, want 4 (authentication)", got)
		}
		if !strings.Contains(out, "expired 2 days ago") {
			t.Errorf("output should say how stale the session is, got:\n%s", out)
		}
		// Signing in again cannot refresh a file exported earlier, so the advice
		// must be to re-export it, not to retry.
		if !strings.Contains(out, "holds an old session") || !strings.Contains(out, "export fresh cookies") {
			t.Errorf("a saved cookies file needs advice to re-export it, got:\n%s", out)
		}
		if strings.Contains(out, "browser you are importing from") {
			t.Errorf("the advice to sign in and retry cannot fix a saved file:\n%s", out)
		}
		if _, statErr := os.Stat(credentials); !os.IsNotExist(statErr) {
			t.Errorf("an expired session was saved to %s (stat err: %v)", credentials, statErr)
		}
	})

	t.Run("a working stored session survives an expired import", func(t *testing.T) {
		if out, err := login(testJWT(time.Now().Add(time.Hour))); err != nil {
			t.Fatalf("a fresh session must still be accepted: %v\n%s", err, out)
		}
		stored, err := os.ReadFile(credentials)
		if err != nil {
			t.Fatalf("a fresh session was not saved: %v", err)
		}

		if _, err := login(testJWT(time.Now().Add(-time.Hour))); err == nil {
			t.Fatal("expected the expired session to be refused")
		}
		after, err := os.ReadFile(credentials)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(stored, after) {
			t.Error("a refused login overwrote the working stored session")
		}
	})
}
