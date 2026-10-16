package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPressAuthCaptureKeys(t *testing.T) {
	tests := []struct {
		name string
		list string
		want []string
	}{
		{
			name: "apex and regional host, newest first",
			list: `[{"domain":"concursolutions.com","captured_at":"2026-10-15T14:59:22Z"},{"domain":"us2.concursolutions.com","captured_at":"2026-10-15T15:03:40Z"}]`,
			want: []string{"us2.concursolutions.com", "concursolutions.com"},
		},
		{
			name: "older regional capture sorts after a newer apex capture",
			list: `[{"domain":"us2.concursolutions.com","captured_at":"2026-10-14T09:00:00Z"},{"domain":"concursolutions.com","captured_at":"2026-10-15T09:00:00Z"}]`,
			want: []string{"concursolutions.com", "us2.concursolutions.com"},
		},
		{
			name: "unrelated and lookalike domains are never consulted",
			list: `[{"domain":"example.com","captured_at":"2026-10-15T10:00:00Z"},{"domain":"evilconcursolutions.com","captured_at":"2026-10-15T11:00:00Z"},{"domain":"concursolutions.com.evil.example","captured_at":"2026-10-15T12:00:00Z"},{"domain":"us2.concursolutions.com","captured_at":"2026-10-15T09:00:00Z"}]`,
			want: []string{"us2.concursolutions.com"},
		},
		{
			name: "no related capture falls back to the apex so press-auth reports not captured",
			list: `[{"domain":"example.com","captured_at":"2026-10-15T10:00:00Z"}]`,
			want: []string{"concursolutions.com"},
		},
		{
			name: "empty list falls back to the apex",
			list: `[]`,
			want: []string{"concursolutions.com"},
		},
		{
			name: "unparsable list output falls back to the apex",
			list: `no domains captured yet`,
			want: []string{"concursolutions.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pressAuthCaptureKeys([]byte(tt.list), "concursolutions.com")
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pressAuthCaptureKeys() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPickCookieHeader(t *testing.T) {
	required := []string{"JWT", "_csrf"}
	tests := []struct {
		name    string
		headers []string
		want    string
	}{
		{
			name:    "newest complete header wins",
			headers: []string{"JWT=new; _csrf=newcsrf", "JWT=old; _csrf=oldcsrf"},
			want:    "JWT=new; _csrf=newcsrf",
		},
		{
			name:    "skips a newer incomplete header rather than blending it with an older one",
			headers: []string{"JWT=new; origin_dc=us2", "JWT=old; _csrf=oldcsrf"},
			want:    "JWT=old; _csrf=oldcsrf",
		},
		{
			name:    "no complete header returns the newest so the missing cookie is named",
			headers: []string{"JWT=new", "JWT=old; origin_dc=us2"},
			want:    "JWT=new",
		},
		{
			name:    "empty headers are ignored",
			headers: []string{"", "JWT=a; _csrf=b"},
			want:    "JWT=a; _csrf=b",
		},
		{
			name:    "nothing at all",
			headers: nil,
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pickCookieHeader(tt.headers, required); got != tt.want {
				t.Errorf("pickCookieHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

// writeMockPressAuth installs a press-auth stand-in whose `list --json` and
// `cookies <key>` output is driven by the given script body.
func writeMockPressAuth(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "press-auth")
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+body), 0o755); err != nil {
		t.Fatalf("writing mock press-auth: %v", err)
	}
	return path
}

func TestLoadPressAuthCookies(t *testing.T) {
	required := []string{"JWT", "_csrf"}

	t.Run("regional capture supplies the host-only cookie the apex capture lacks", func(t *testing.T) {
		mock := writeMockPressAuth(t, `
case "$1" in
  list) echo '[{"domain":"concursolutions.com","captured_at":"2026-10-15T14:59:22Z"},{"domain":"us2.concursolutions.com","captured_at":"2026-10-15T15:03:40Z"},{"domain":"evilconcursolutions.com","captured_at":"2026-10-15T16:00:00Z"}]' ;;
  cookies)
    case "$2" in
      concursolutions.com) echo "JWT=apexjwt; origin_dc=us2" ;;
      us2.concursolutions.com) echo "JWT=subjwt; _csrf=subcsrf" ;;
      *) echo "unexpected key $2" >&2; exit 9 ;;
    esac ;;
esac
`)
		got, err := loadPressAuthCookies(mock, "concursolutions.com", required)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "JWT=subjwt; _csrf=subcsrf"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("regional-only capture works with no apex capture at all", func(t *testing.T) {
		mock := writeMockPressAuth(t, `
case "$1" in
  list) echo '[{"domain":"us2.concursolutions.com","captured_at":"2026-10-15T15:03:40Z"}]' ;;
  cookies) [ "$2" = "us2.concursolutions.com" ] && echo "JWT=subjwt; _csrf=subcsrf" || { echo "not captured" >&2; exit 1; } ;;
esac
`)
		got, err := loadPressAuthCookies(mock, "concursolutions.com", required)
		if err != nil || got != "JWT=subjwt; _csrf=subcsrf" {
			t.Errorf("got (%q, %v), want the regional header", got, err)
		}
	})

	t.Run("unusable list output still consults the apex like before", func(t *testing.T) {
		mock := writeMockPressAuth(t, `
case "$1" in
  list) exit 2 ;;
  cookies) echo "JWT=apexjwt; origin_dc=us2" ;;
esac
`)
		got, err := loadPressAuthCookies(mock, "concursolutions.com", required)
		if err != nil || got != "JWT=apexjwt; origin_dc=us2" {
			t.Errorf("got (%q, %v), want the apex header", got, err)
		}
	})

	t.Run("no capture surfaces press-auth's own message", func(t *testing.T) {
		mock := writeMockPressAuth(t, `
case "$1" in
  list) echo '[]' ;;
  cookies) echo "concursolutions.com: not captured" >&2; exit 1 ;;
esac
`)
		_, err := loadPressAuthCookies(mock, "concursolutions.com", required)
		if err == nil || !strings.Contains(err.Error(), "not captured") {
			t.Errorf("err = %v, want press-auth's \"not captured\" message", err)
		}
	})
}

func TestPressAuthLoginCommand(t *testing.T) {
	tests := []struct {
		name       string
		apiBaseURL string
		want       string
	}{
		{
			name:       "keys the capture on the regional host derived from the API base URL",
			apiBaseURL: "https://www-us2.api.concursolutions.com",
			want:       "press-auth login us2.concursolutions.com --login-url https://www.concursolutions.com",
		},
		{
			name:       "other regions are derived, not hardcoded to us2",
			apiBaseURL: "https://www-eu1.api.concursolutions.com",
			want:       "press-auth login eu1.concursolutions.com --login-url https://www.concursolutions.com",
		},
		{
			name:       "an unknown region is a placeholder, never a guess",
			apiBaseURL: "https://my-custom-proxy.example.com",
			want:       "press-auth login <region>.concursolutions.com --login-url https://www.concursolutions.com",
		},
		{
			name:       "an unloadable config is also a placeholder",
			apiBaseURL: "",
			want:       "press-auth login <region>.concursolutions.com --login-url https://www.concursolutions.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CONCUR_UI_BASE_URL", "")
			if got := pressAuthLoginCommand("concursolutions.com", tt.apiBaseURL); got != tt.want {
				t.Errorf("pressAuthLoginCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}
