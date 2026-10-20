// Copyright 2026 elizabethcstone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

// pp:data-source live

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mvanhorn/printing-press-library/library/productivity/airtable/internal/store"

	"github.com/spf13/cobra"
)

// newRecordsSyncTableCmd fetches every record for one base+table and mirrors
// it into the local store under resource_type "records", tagged with
// base_id/table so search/analytics/diff can scope to it. The framework's
// generic `sync` command cannot auto-discover Airtable tables: it supports
// one level of parent->child dependency, but records require baseId AND
// table, and there is no endpoint that lists tables as children of bases in
// the shape the generic sync engine expects (get-base-schema returns a full
// nested schema blob, not a flat listable child resource).
func newRecordsSyncTableCmd(flags *rootFlags) *cobra.Command {
	var flagBase string
	var flagTable string
	var flagView string

	cmd := &cobra.Command{
		Use:         "sync-table",
		Short:       "Mirror all records of one base/table into the local store for search, analytics, and diff",
		Example:     `  airtable-pp-cli records sync-table --base appLkNDICXNqxSDhG --table Shipments`,
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "records sync-table")
			}
			if flagBase == "" || flagTable == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--base and --table are required"))
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			path := replacePathParam("/v0/{base_id}/{table}", "base_id", formatCLIParamValue(flagBase))
			path = replacePathParam(path, "table", formatCLIParamValue(flagTable))

			// Airtable's own pagination shape (an opaque "offset" present
			// only when more pages remain) doesn't match the framework's
			// generic --all cursor detection, so this hand-written command
			// paginates manually rather than through resolvePaginatedReadWithStrategy.
			// Prune only rows last written before this run started, so an overlapping
			// newer sync's rows are never deleted by an older run's stale ID set.
			runStart := time.Now().UTC().Format(time.RFC3339)
			var rawRecords []json.RawMessage
			offset := ""
			for {
				params := map[string]string{"pageSize": "100"}
				if flagView != "" {
					params["view"] = flagView
				}
				if offset != "" {
					params["offset"] = offset
				}
				data, err := c.GetNoCache(cmd.Context(), path, params)
				if err != nil {
					return classifyAPIError(cmd.OutOrStdout(), err, flags)
				}
				var page struct {
					Records []json.RawMessage `json:"records"`
					Offset  string            `json:"offset"`
				}
				if err := json.Unmarshal(data, &page); err != nil {
					return fmt.Errorf("parsing records response: %w", err)
				}
				rawRecords = append(rawRecords, page.Records...)
				if page.Offset == "" {
					break
				}
				offset = page.Offset
			}
			items := make([]json.RawMessage, 0, len(rawRecords))
			for _, raw := range rawRecords {
				var obj map[string]any
				if err := json.Unmarshal(raw, &obj); err != nil {
					continue
				}
				obj["base_id"] = flagBase
				obj["table"] = flagTable
				merged, err := json.Marshal(obj)
				if err != nil {
					continue
				}
				items = append(items, merged)
			}
			dbPath := defaultDBPath("airtable-pp-cli")
			db, err := store.OpenWithContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()
			stored, failures, err := db.UpsertBatch("records", items)
			if err != nil {
				return fmt.Errorf("storing records: %w", err)
			}
			pruned := 0
			if flagView == "" && failures == 0 && len(items) == len(rawRecords) {
				seenIDs := make([]string, 0, len(items))
				for _, item := range items {
					obj, _ := store.DecodeJSONObject(item)
					seenIDs = append(seenIDs, store.ExtractResourceID("records", obj))
				}
				pruned, err = db.ReconcileAirtableRecords(flagBase, flagTable, seenIDs, runStart)
				if err != nil {
					return fmt.Errorf("pruning stale records: %w", err)
				}
			}
			view := map[string]any{
				"base":             flagBase,
				"table":            flagTable,
				"records_fetched":  len(rawRecords),
				"records_synced":   stored,
				"extract_failures": failures,
				"records_pruned":   pruned,
			}
			return printJSONFiltered(cmd.OutOrStdout(), view, flags)
		},
	}
	cmd.Flags().StringVar(&flagBase, "base", "", "Base ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table name or ID")
	cmd.Flags().StringVar(&flagView, "view", "", "Optional view to scope which records are synced")
	return cmd
}
