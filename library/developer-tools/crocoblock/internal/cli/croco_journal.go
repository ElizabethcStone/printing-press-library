package cli

import (
	"context"
	"fmt"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"os"
	"path/filepath"
)

func crocoDBPath() string {
	if v := os.Getenv("CROCOBLOCK_DB"); v != "" {
		return v
	}
	return defaultDBPath("crocoblock-pp-cli")
}
func crocoOpenStore(ctx context.Context, path string) (*store.Store, error) {
	if path == "" {
		path = crocoDBPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	s, e := store.OpenWithContext(ctx, path)
	if e != nil {
		return nil, e
	}
	if e = os.Chmod(path, 0600); e != nil {
		s.Close()
		return nil, e
	}
	return s, nil
}
func crocoJournal(f *rootFlags, ctx context.Context, c *client.Client, kind, resource string, fields []string, detail map[string]any) error {
	s, e := crocoOpenStore(ctx, "")
	if e != nil {
		return apiErr(fmt.Errorf("mutation_applied: local journal unavailable; do not retry write: %w", e))
	}
	defer s.Close()
	_, e = s.CrocoEvent(ctx, c.BaseURL, kind, resource, fields, detail)
	if e != nil {
		return apiErr(fmt.Errorf("mutation_applied: journal failed; verify upstream before any retry: %w", e))
	}
	return nil
}

// Persist intent before transport so ambiguous commits and failed readbacks remain
// visible to impact analysis. Only operation metadata and field names are stored.
func crocoFieldWriteIntent(ctx context.Context, c *client.Client, id, operation string, fields []string) (int64, error) {
	s, err := crocoOpenStore(ctx, "")
	if err != nil {
		return 0, apiErr(fmt.Errorf("write_not_sent: prepare field journal: %w", err))
	}
	defer s.Close()
	event, err := s.CrocoEvent(ctx, c.BaseURL, "post_fields_attempt", "fields:"+id, fields, map[string]any{"id": id, "operation": operation, "verified": false})
	if err != nil {
		return 0, apiErr(fmt.Errorf("write_not_sent: persist field intent: %w", err))
	}
	return event, nil
}
