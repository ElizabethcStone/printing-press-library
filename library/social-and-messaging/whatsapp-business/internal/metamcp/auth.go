package metamcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/config"
)

const Endpoint = "https://mcp.facebook.com/whatsapp_business_tools"

type AuthError struct{ Message string }

func (e *AuthError) Error() string { return e.Message }

// ResolveAuth uses the generated config seam first. Native Codex reuse is
// explicit, endpoint-bound and read-only: it never copies or refreshes secrets.
func ResolveAuth(ctx context.Context, cfg *config.Config, useCodex bool) (string, error) {
	if h := cfg.AuthHeader(); h != "" {
		return h, nil
	}
	if !useCodex {
		return "", &AuthError{"no MCP credential; use --codex-auth for the existing native session or auth set-token for a resource-specific bearer"}
	}
	if strings.TrimRight(cfg.BaseURL, "/") != "https://mcp.facebook.com" {
		return "", &AuthError{"native Codex OAuth is bound to the official Meta endpoint and cannot follow --base-url overrides"}
	}
	if runtime.GOOS != "darwin" {
		return "", &AuthError{"native Codex credential reuse currently supports macOS Keychain; use a configured resource-specific bearer on this platform"}
	}
	// Codex's public store-key contract hashes these three fields. Builds with
	// preserve_order and canonical ordering are both supported, with exact target
	// lookups only; no other credential entries are enumerated.
	payloads := []string{`{"type":"http","url":"` + Endpoint + `","headers":{}}`, `{"headers":{},"type":"http","url":"` + Endpoint + `"}`}
	for _, payload := range payloads {
		h := sha256.Sum256([]byte(payload))
		key := "whatsapp_business_tools|" + hex.EncodeToString(h[:])[:16]
		// #nosec G204 -- Fixed macOS executable and exact resource-derived Keychain account; no shell or user-supplied argv.
		raw, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", "Codex MCP Credentials", "-a", key, "-w").Output()
		if err != nil {
			continue
		}
		var saved struct {
			URL       string `json:"url"`
			ExpiresAt int64  `json:"expires_at"`
			Token     struct {
				Access string `json:"access_token"`
			} `json:"token_response"`
		}
		if json.Unmarshal(raw, &saved) != nil || saved.URL != Endpoint || saved.Token.Access == "" {
			continue
		}
		if saved.ExpiresAt > 0 && time.Now().UnixMilli() >= saved.ExpiresAt-30000 {
			return "", &AuthError{"native Meta OAuth session expired; run auth codex --launch, then retry --codex-auth"}
		}
		return "Bearer " + saved.Token.Access, nil
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("native credential lookup: %w", ctx.Err())
	}
	return "", &AuthError{"native Meta OAuth credential unavailable; unlock this Mac's Keychain if locked, or run auth codex --launch to sign in"}
}
