package cli

import (
	"strings"
	"testing"
)

// The approved domain surface supersedes the generator's internal transport names.
var crocoApprovedBasePaths = []string{"discover", "sync", "engine configuration", "engine website", "engine macros", "engine modules", "engine create-cct", "engine create-cpt", "engine create-taxonomy", "engine create-meta-box", "engine create-query", "engine create-listing", "engine create-glossary", "cct list", "cct get", "cct create", "cct update", "cct delete", "relations definitions", "relations list", "relations parents", "relations children", "relations update", "forms list", "forms get", "forms create", "forms update", "forms trash", "forms fields", "records list", "records fields", "bookings list", "bookings config", "bookings create", "bookings update", "bookings delete", "appointments list", "appointments get", "appointments create", "appointments update", "appointments delete", "appointments slots", "appointments providers", "filters list", "filters get", "filters create", "filters update", "filters duplicate", "filters trash", "filters restore", "filters reindex", "fields list", "fields get", "fields update", "fields restore"}

func TestCrocoApprovedPathsAreRealLeaves(t *testing.T) {
	root := RootCmd()
	for _, path := range crocoApprovedBasePaths {
		cmd, remaining, e := root.Find(strings.Fields(path))
		if e != nil || len(remaining) > 0 || cmd == nil || cmd.RunE == nil || isNovelScaffoldCommand(cmd) {
			t.Fatalf("unimplemented approved path %s: %v, remaining=%v", path, e, remaining)
		}
	}
}

func TestCrocoApprovedAnalysesAreRealLeaves(t *testing.T) {
	root := RootCmd()
	for _, path := range []string{"schema diff", "forms field-usage", "reservations overlaps", "filters impact", "cct reconcile", "relations audit"} {
		cmd, remaining, e := root.Find(strings.Fields(path))
		if e != nil || len(remaining) > 0 || cmd == nil || cmd.RunE == nil || isNovelScaffoldCommand(cmd) {
			t.Fatalf("unimplemented approved analysis %s: %v", path, e)
		}
		if cmd.Annotations["mcp:read-only"] != "true" || cmd.Annotations["pp:data-source"] != "local" {
			t.Fatalf("wrong safety/provenance annotations for %s", path)
		}
	}
}
