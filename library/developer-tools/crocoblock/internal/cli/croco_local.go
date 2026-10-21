package cli

// pp:data-source local

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
	"os"
	"strings"
	"time"
)

func crocoLocalCommand(f *rootFlags, cmd *cobra.Command, run func(context.Context, *cobra.Command, []string) (any, error)) *cobra.Command {
	cmd.Annotations = map[string]string{"pp:data-source": "local", "mcp:read-only": "true"}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if dryRunOK(f) {
			return writeDryRun(cmd.OutOrStdout(), f, cmd.CommandPath())
		}
		if f.dataSource == "live" {
			return usageErr(fmt.Errorf("this analysis reads local snapshots; --data-source live is not supported"))
		}
		ctx, cancel := boundCtx(cmd.Context(), f)
		defer cancel()
		v, e := run(ctx, cmd, args)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(crocoSafe(v))
		if e != nil {
			return e
		}
		return printOutputWithFlagsMeta(cmd.OutOrStdout(), raw, f, map[string]any{"source": "computed", "transport": "local"})
	}
	return cmd
}
func crocoLoadCapture(ctx context.Context, path, resource string) (store.CrocoCapture, []map[string]any, error) {
	var cap store.CrocoCapture
	if path == "" {
		path = crocoDBPath()
	}
	s, e := store.OpenReadOnlyContext(ctx, path)
	if e != nil {
		return cap, nil, configErr(fmt.Errorf("cannot read snapshot %s: %w; run sync first", path, e))
	}
	defer s.Close()
	cap, raw, e := s.CrocoSnapshot(ctx, resource)
	if e != nil {
		return cap, nil, configErr(fmt.Errorf("coverage unknown for %s: capture missing or unreadable: %w", resource, e))
	}
	rows := make([]map[string]any, 0, len(raw))
	for _, b := range raw {
		var m map[string]any
		d := json.NewDecoder(strings.NewReader(string(b)))
		d.UseNumber()
		if e = d.Decode(&m); e != nil || m == nil {
			return cap, nil, configErr(fmt.Errorf("invalid snapshot row in %s", resource))
		}
		rows = append(rows, m)
	}
	if at, e := time.Parse(time.RFC3339Nano, cap.CapturedAt); e == nil && time.Since(at) > 24*time.Hour {
		fmt.Fprintf(os.Stderr, "warning: %s snapshot was captured at %s; this analysis does not refresh it\n", resource, cap.CapturedAt)
	}
	return cap, rows, nil
}
func crocoRequireComplete(cap store.CrocoCapture) error {
	if !cap.Complete {
		return configErr(fmt.Errorf("coverage unknown for %s: capture is incomplete (%s)", cap.Resource, cap.Note))
	}
	if cap.Site == "" || cap.CapturedAt == "" {
		return configErr(fmt.Errorf("capture provenance missing for %s", cap.Resource))
	}
	return nil
}
func crocoRequireVersion(cap store.CrocoCapture, plugin string) error {
	if cap.Versions[plugin] == "" {
		return configErr(fmt.Errorf("capture has no %s version; schema compatibility is unknown", plugin))
	}
	return nil
}
