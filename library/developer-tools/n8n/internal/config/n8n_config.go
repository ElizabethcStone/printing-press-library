// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.

package config

import (
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/cliutil"
)

// n8nURLAlias reads N8N_URL, the instance-root variable the official
// @n8n/cli uses, and returns the matching public API base URL. N8N_BASE_URL
// (the full API base, including /api/v1) still wins when both are set.
func n8nURLAlias() string {
	v := strings.TrimRight(strings.TrimSpace(cliutil.EnvOverride("N8N_URL")), "/")
	if v == "" {
		return ""
	}
	if strings.HasSuffix(v, "/api/v1") {
		return v
	}
	return v + "/api/v1"
}
