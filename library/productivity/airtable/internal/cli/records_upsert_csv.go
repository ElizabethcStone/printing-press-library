// Copyright 2026 elizabethcstone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

// pp:data-source live

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const airtableUpsertBatchSize = 10

func newNovelRecordsUpsertCsvCmd(flags *rootFlags) *cobra.Command {
	var flagBase string
	var flagTable string
	var flagFile string
	var flagMergeOn string

	cmd := &cobra.Command{
		Use:         "upsert-csv",
		Short:       "Take a raw CSV export and merge-upsert it straight into an Airtable table, matched on the field you choose.",
		Example:     "  airtable-pp-cli records upsert-csv --base appLkNDICXNqxSDhG --table Leads --file leads.csv --merge-on Email",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live", "pp:verified-by-proof": "upsert-csv-lifecycle.md", "pp:happy-args": "--base=appLkNDICXNqxSDhG;--table=Leads;--file=testdata/leads-sample.csv;--merge-on=Email"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagBase == "" || flagTable == "" || flagFile == "" || flagMergeOn == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--base, --table, --file, and --merge-on are all required"))
			}
			if dryRunOK(flags) {
				if flags.asJSON {
					return writeDryRun(cmd.OutOrStdout(), flags, "records upsert-csv")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "would read %s and upsert its rows into %s/%s, merging on %q, in batches of %d\n", flagFile, flagBase, flagTable, flagMergeOn, airtableUpsertBatchSize)
				return nil
			}
			f, err := os.Open(flagFile) // #nosec G304 -- flagFile is a user-supplied CLI --file path, the standard local-file-input pattern for this command
			if err != nil {
				return fmt.Errorf("opening --file %s: %w", flagFile, err)
			}
			defer f.Close()
			r := csv.NewReader(f)
			rows, err := r.ReadAll()
			if err != nil {
				return fmt.Errorf("parsing CSV %s: %w", flagFile, err)
			}
			if len(rows) < 1 {
				return fmt.Errorf("%s has no header row", flagFile)
			}
			headers := rows[0]
			if len(headers) > 0 {
				headers[0] = strings.TrimPrefix(headers[0], "\uFEFF")
			}
			seen := make(map[string]struct{}, len(headers))
			found := false
			for _, h := range headers {
				if _, ok := seen[h]; ok {
					return usageErr(fmt.Errorf("duplicate CSV header %q", h))
				}
				seen[h] = struct{}{}
				if h == flagMergeOn {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("--merge-on %q does not match any CSV column header (headers: %v)", flagMergeOn, headers)
			}
			mergeIndex := 0
			for i, h := range headers {
				if h == flagMergeOn {
					mergeIndex = i
					break
				}
			}
			type upsertRecord struct {
				Fields map[string]string `json:"fields"`
			}
			records := make([]upsertRecord, 0, len(rows)-1)
			for line, row := range rows[1:] {
				empty := true
				for _, value := range row {
					if value != "" {
						empty = false
						break
					}
				}
				if empty {
					continue
				}
				if mergeIndex >= len(row) || row[mergeIndex] == "" {
					return usageErr(fmt.Errorf("CSV line %d has an empty --merge-on value for %q", line+2, flagMergeOn))
				}
				fields := make(map[string]string, len(headers))
				for i, h := range headers {
					if i < len(row) && row[i] != "" {
						fields[h] = row[i]
					}
				}
				records = append(records, upsertRecord{Fields: fields})
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			path := replacePathParam("/v0/{base_id}/{table}", "base_id", formatCLIParamValue(flagBase))
			path = replacePathParam(path, "table", formatCLIParamValue(flagTable))
			totalCreated, totalUpdated := 0, 0
			for start := 0; start < len(records); start += airtableUpsertBatchSize {
				end := start + airtableUpsertBatchSize
				if end > len(records) {
					end = len(records)
				}
				body := map[string]any{
					"records": records[start:end],
					"performUpsert": map[string]any{
						"fieldsToMergeOn": []string{flagMergeOn},
					},
				}
				data, _, err := c.Patch(cmd.Context(), path, body)
				if err != nil {
					return classifyAPIError(cmd.OutOrStdout(), fmt.Errorf("upserting rows %d-%d: %w", start, end-1, err), flags)
				}
				var resp struct {
					CreatedRecords []string `json:"createdRecords"`
					UpdatedRecords []string `json:"updatedRecords"`
				}
				if err := json.Unmarshal(data, &resp); err != nil {
					return fmt.Errorf("parsing upsert response for batch %d: %w", start/airtableUpsertBatchSize+1, err)
				}
				totalCreated += len(resp.CreatedRecords)
				totalUpdated += len(resp.UpdatedRecords)
			}
			view := map[string]any{
				"base":            flagBase,
				"table":           flagTable,
				"merge_on":        flagMergeOn,
				"rows_processed":  len(records),
				"records_created": totalCreated,
				"records_updated": totalUpdated,
			}
			return printJSONFiltered(cmd.OutOrStdout(), view, flags)
		},
	}
	cmd.Flags().StringVar(&flagBase, "base", "", "Base ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table name or ID")
	cmd.Flags().StringVar(&flagFile, "file", "", "Path to the CSV file to upsert")
	cmd.Flags().StringVar(&flagMergeOn, "merge-on", "", "CSV column / Airtable field name to match existing records on")
	return cmd
}
