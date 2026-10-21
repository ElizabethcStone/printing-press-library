// Copyright 2026 elizabethcstone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

// pp:data-source live

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mvanhorn/printing-press-library/library/productivity/airtable/internal/store"

	"github.com/spf13/cobra"
)

type schemaField struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type schemaTable struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Fields []schemaField `json:"fields"`
}

type baseSchema struct {
	Tables []schemaTable `json:"tables"`
}

func newNovelBasesSchemaDiffCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "schema-diff <base_id>",
		Short:       "Show exactly what changed in a base's schema since this command last ran for that base.",
		Example:     "  airtable-pp-cli bases schema-diff appLkNDICXNqxSDhG",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "bases schema-diff")
			}
			if len(args) < 1 || args[0] == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("base_id is required"))
			}
			baseID := args[0]

			c, err := flags.newClient()
			if err != nil {
				return err
			}
			path := replacePathParam("/v0/meta/bases/{base_id}/tables", "base_id", formatCLIParamValue(baseID))
			data, err := c.GetNoCache(cmd.Context(), path, nil)
			if err != nil {
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			var live baseSchema
			if err := json.Unmarshal(data, &live); err != nil {
				return fmt.Errorf("parsing live schema: %w", err)
			}

			dbPath := defaultDBPath("airtable-pp-cli")
			db, err := store.OpenWithContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			// Lazy migration: this table is owned by this novel command, not
			// the generator's core schema.
			if _, err := db.DB().ExecContext(cmd.Context(), `CREATE TABLE IF NOT EXISTS airtable_schema_snapshots (
				base_id TEXT PRIMARY KEY,
				schema_json TEXT NOT NULL,
				captured_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)`); err != nil {
				return fmt.Errorf("creating snapshot table: %w", err)
			}

			var priorJSON string
			var capturedAt sql.NullString
			row := db.DB().QueryRowContext(cmd.Context(),
				`SELECT schema_json, captured_at FROM airtable_schema_snapshots WHERE base_id = ?`, baseID)
			hasPrior := true
			if err := row.Scan(&priorJSON, &capturedAt); err != nil {
				if err != sql.ErrNoRows {
					return fmt.Errorf("reading prior snapshot: %w", err)
				}
				hasPrior = false
			}

			liveJSON, err := json.Marshal(live)
			if err != nil {
				return fmt.Errorf("marshaling live schema: %w", err)
			}
			if _, err := db.DB().ExecContext(cmd.Context(),
				`INSERT INTO airtable_schema_snapshots (base_id, schema_json, captured_at) VALUES (?, ?, CURRENT_TIMESTAMP)
				 ON CONFLICT(base_id) DO UPDATE SET schema_json = excluded.schema_json, captured_at = excluded.captured_at`,
				baseID, string(liveJSON)); err != nil {
				return fmt.Errorf("storing snapshot: %w", err)
			}

			if !hasPrior {
				view := map[string]any{
					"base_id": baseID,
					"note":    "no prior snapshot; this live schema is now the baseline for future diffs",
					"tables":  len(live.Tables),
				}
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}

			var prior baseSchema
			if err := json.Unmarshal([]byte(priorJSON), &prior); err != nil {
				return fmt.Errorf("parsing prior snapshot: %w", err)
			}

			priorTables := make(map[string]schemaTable, len(prior.Tables))
			for _, t := range prior.Tables {
				priorTables[t.ID] = t
			}
			liveTables := make(map[string]schemaTable, len(live.Tables))
			for _, t := range live.Tables {
				liveTables[t.ID] = t
			}

			addedTables := []string{}
			removedTables := []string{}
			renamedTables := []map[string]string{}
			tableChanges := []map[string]any{}

			for id, t := range liveTables {
				if priorTable, ok := priorTables[id]; !ok {
					addedTables = append(addedTables, t.Name)
				} else if priorTable.Name != t.Name {
					renamedTables = append(renamedTables, map[string]string{"id": id, "from": priorTable.Name, "to": t.Name})
				}
			}
			for id, t := range priorTables {
				if _, ok := liveTables[id]; !ok {
					removedTables = append(removedTables, t.Name)
				}
			}
			for id, liveTable := range liveTables {
				priorTable, ok := priorTables[id]
				if !ok {
					continue
				}
				priorFields := make(map[string]schemaField, len(priorTable.Fields))
				for _, f := range priorTable.Fields {
					priorFields[f.ID] = f
				}
				liveFields := make(map[string]schemaField, len(liveTable.Fields))
				for _, f := range liveTable.Fields {
					liveFields[f.ID] = f
				}
				addedFields := []string{}
				removedFields := []string{}
				retypedFields := []map[string]string{}
				renamedFields := []map[string]string{}
				for fid, f := range liveFields {
					if pf, ok := priorFields[fid]; !ok {
						addedFields = append(addedFields, f.Name)
					} else {
						if pf.Name != f.Name {
							renamedFields = append(renamedFields, map[string]string{"id": fid, "from": pf.Name, "to": f.Name})
						}
						if pf.Type != f.Type {
							retypedFields = append(retypedFields, map[string]string{
								"field": f.Name, "from": pf.Type, "to": f.Type,
							})
						}
					}
				}
				for fid, f := range priorFields {
					if _, ok := liveFields[fid]; !ok {
						removedFields = append(removedFields, f.Name)
					}
				}
				if len(addedFields) > 0 || len(removedFields) > 0 || len(retypedFields) > 0 || len(renamedFields) > 0 {
					tableChanges = append(tableChanges, map[string]any{
						"table":          liveTable.Name,
						"added_fields":   addedFields,
						"removed_fields": removedFields,
						"retyped_fields": retypedFields,
						"renamed_fields": renamedFields,
					})
				}
			}

			view := map[string]any{
				"base_id":             baseID,
				"prior_captured_at":   capturedAt.String,
				"added_tables":        addedTables,
				"removed_tables":      removedTables,
				"renamed_tables":      renamedTables,
				"changed_tables":      tableChanges,
				"no_changes_detected": len(addedTables) == 0 && len(removedTables) == 0 && len(renamedTables) == 0 && len(tableChanges) == 0,
			}
			return printJSONFiltered(cmd.OutOrStdout(), view, flags)
		},
	}
	return cmd
}
