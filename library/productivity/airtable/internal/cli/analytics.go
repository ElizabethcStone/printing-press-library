// Copyright 2026 elizabethcstone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

// pp:data-source local

import (
	"fmt"
	"os"

	"github.com/mvanhorn/printing-press-library/library/productivity/airtable/internal/store"

	"github.com/spf13/cobra"
)

type analyticsCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

func newNovelAnalyticsCmd(flags *rootFlags) *cobra.Command {
	var flagType string
	var flagGroupBy string
	var flagDb string
	var flagBase string
	var flagTable string

	cmd := &cobra.Command{
		Use:         "analytics",
		Short:       "Count records by distinct value of any field, offline, across a synced table.",
		Example:     "  airtable-pp-cli analytics --type records --group-by fields.Status",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "analytics")
			}
			if flagType == "" || flagGroupBy == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--type and --group-by are required"))
			}
			dbPath := flagDb
			if dbPath == "" {
				dbPath = defaultDBPath("airtable-pp-cli")
			}
			if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
				fmt.Fprintf(cmd.ErrOrStderr(), "no local mirror at %s\nrun: airtable-pp-cli records sync-table --base <id> --table <name>\n", dbPath)
				if flags.asJSON || flags.agent {
					fmt.Fprintln(cmd.OutOrStdout(), "[]")
				}
				return nil
			}
			db, err := store.OpenReadOnlyContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			jsonPath := "$." + flagGroupBy
			query := `SELECT COALESCE(json_extract(data, ?), '') AS val, COUNT(*) AS n
				 FROM resources
				 WHERE resource_type = ?`
			queryArgs := []any{jsonPath, flagType}
			if flagBase != "" {
				query += ` AND json_extract(data, '$.base_id') = ?`
				queryArgs = append(queryArgs, flagBase)
			}
			if flagTable != "" {
				query += ` AND json_extract(data, '$.table') = ?`
				queryArgs = append(queryArgs, flagTable)
			}
			query += ` GROUP BY val ORDER BY n DESC`
			rows, err := db.DB().QueryContext(cmd.Context(), query, queryArgs...)
			if err != nil {
				return fmt.Errorf("query: %w", err)
			}
			counts := make([]analyticsCount, 0)
			for rows.Next() {
				var c analyticsCount
				if err := rows.Scan(&c.Value, &c.Count); err != nil {
					_ = rows.Close()
					return fmt.Errorf("scan row: %w", err)
				}
				counts = append(counts, c)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return fmt.Errorf("iterate rows: %w", err)
			}
			if err := rows.Close(); err != nil {
				return fmt.Errorf("close rows: %w", err)
			}
			total := 0
			for _, c := range counts {
				total += c.Count
			}
			view := map[string]any{
				"type":          flagType,
				"group_by":      flagGroupBy,
				"counts":        counts,
				"total_records": total,
			}
			return printJSONFiltered(cmd.OutOrStdout(), view, flags)
		},
	}
	cmd.Flags().StringVar(&flagType, "type", "", "Local resource_type to count (e.g. records)")
	cmd.Flags().StringVar(&flagGroupBy, "group-by", "", "Dot-path field to group by, e.g. fields.Status")
	cmd.Flags().StringVar(&flagDb, "db", "", "SQLite database file path (default: resolved data directory data.db)")
	cmd.Flags().StringVar(&flagBase, "base", "", "Filter records by synced base ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Filter records by synced table name or ID")
	return cmd
}
