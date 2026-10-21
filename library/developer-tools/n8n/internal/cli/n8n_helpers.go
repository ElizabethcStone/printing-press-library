// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/client"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/config"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/n8nwf"
)

// isNotFound reports an upstream HTTP 404 from the generated client.
func isNotFound(err error) bool {
	var ae *client.APIError
	return errors.As(err, &ae) && ae.StatusCode == 404
}

// clientFromConfigFile builds a client for a second n8n instance described
// only by the given config file. The process-wide N8N_* overrides are
// suspended while loading so they cannot redirect the comparison back to the
// primary instance.
func clientFromConfigFile(flags *rootFlags, path string) (*client.Client, error) {
	overrides := []string{"N8N_API_KEY", "N8N_BASE_URL", "N8N_URL"}
	saved := map[string]string{}
	for _, k := range overrides {
		if v, ok := os.LookupEnv(k); ok {
			saved[k] = v
			_ = os.Unsetenv(k)
		}
	}
	cfg, err := config.Load(path)
	for k, v := range saved {
		_ = os.Setenv(k, v)
	}
	if err != nil {
		return nil, configErr(fmt.Errorf("load comparison config %s: %w", path, err))
	}
	if cfg.CredentialRouteBlocked() {
		return nil, configErr(fmt.Errorf("comparison config %s is group/world-readable, so its base_url and credentials are not trusted; run: chmod 600 %s", path, path))
	}
	c := client.New(cfg, flags.timeout, flags.rateLimit)
	c.DryRun = flags.dryRun
	c.NoCache = true
	if err := ApplyClientHooks(c); err != nil {
		return nil, err
	}
	return c, nil
}

// sameInstance reports whether two API base URLs point at the same instance.
func sameInstance(a, b string) bool {
	norm := func(s string) string { return strings.TrimRight(strings.ToLower(strings.TrimSpace(s)), "/") }
	return norm(a) == norm(b)
}

// writeDiffLines renders a workflow diff for humans; JSON callers get the struct.
func writeDiffLines(w io.Writer, d n8nwf.DiffResult, baseActive, targetActive bool) {
	val := func(v any) string {
		b, _ := json.Marshal(v)
		return clip(string(b), 120)
	}
	change := func(c n8nwf.ParamChange) {
		label := c.Path
		if c.Node != "" {
			label = c.Node + "." + c.Path
		}
		fmt.Fprintf(w, "~ %s: %s -> %s\n", label, val(c.Before), val(c.After))
	}
	if d.NameChange != nil {
		change(*d.NameChange)
	}
	for _, x := range d.NodesAdded {
		fmt.Fprintf(w, "+ node %s\n", x)
	}
	for _, x := range d.NodesRemoved {
		fmt.Fprintf(w, "- node %s\n", x)
	}
	for _, x := range d.NodesRenamed {
		fmt.Fprintf(w, "renamed %s -> %s\n", x.From, x.To)
	}
	for _, x := range d.TypeChanges {
		change(x)
	}
	for _, x := range d.ParamChanges {
		change(x)
	}
	for _, x := range d.CredentialChanges {
		change(x)
	}
	for _, x := range d.DisabledChanges {
		change(x)
	}
	for _, x := range d.EdgesAdded {
		fmt.Fprintf(w, "+ edge %s -> %s%s\n", x.From, x.To, edgeInput(x))
	}
	for _, x := range d.EdgesRemoved {
		fmt.Fprintf(w, "- edge %s -> %s%s\n", x.From, x.To, edgeInput(x))
	}
	for _, x := range d.SettingsChanges {
		change(x)
	}
	if d.ErrorWorkflowDelta != nil {
		change(*d.ErrorWorkflowDelta)
	}
	if d.ActiveChanged {
		fmt.Fprintf(w, "active: %t -> %t\n", baseActive, targetActive)
	}
}

// clip shortens s to n runes, cutting on rune boundaries so multi-byte text is not split.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-3]) + "..."
	}
	return s
}

// savedVersionID returns the versionId from a workflow PUT/POST response.
func savedVersionID(raw []byte) (any, bool) {
	var saved map[string]any
	if json.Unmarshal(raw, &saved) != nil {
		return nil, false
	}
	v, ok := saved["versionId"]
	return v, ok
}

// edgeInput labels a non-default target input so Merge-node rewiring is visible.
func edgeInput(e n8nwf.Edge) string {
	if e.Input == 0 {
		return ""
	}
	return fmt.Sprintf(" (input %d)", e.Input)
}
