// Copyright 2026 elizabethcstone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

// pp:data-source live

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/mvanhorn/printing-press-library/library/productivity/airtable/internal/client"
	"github.com/spf13/cobra"
)

type recordsDiffProposed struct {
	ID     string         `json:"id"`
	Fields map[string]any `json:"fields"`
}

type recordsDiffFieldChange struct {
	Field    string `json:"field"`
	Current  any    `json:"current,omitempty"`
	Proposed any    `json:"proposed"`
}

type recordsDiffResult struct {
	ID              string                   `json:"id"`
	Found           bool                     `json:"found"`
	Changes         []recordsDiffFieldChange `json:"changes"`
	UnchangedFields int                      `json:"unchanged_fields"`
}

func newNovelRecordsDiffCmd(flags *rootFlags) *cobra.Command {
	var flagBase string
	var flagTable string
	var flagStdin bool
	var flagChanges string

	cmd := &cobra.Command{
		Use:         "diff",
		Short:       "See exactly which fields on which existing records would change before a bulk update or upsert actually runs.",
		Example:     "  airtable-pp-cli records diff --base appLkNDICXNqxSDhG --table Shipments --stdin < changes.json",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "records diff")
			}
			if flagBase == "" || flagTable == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--base and --table are required"))
			}
			changesSet := cmd.Flags().Changed("changes")
			if flagStdin == changesSet {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("exactly one of --stdin or --changes is required"))
			}
			raw := []byte(flagChanges)
			inputName := "--changes"
			if flagStdin {
				inputName = "stdin"
				var err error
				raw, err = io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
			}
			var proposed []recordsDiffProposed
			// Empty stdin is a valid degenerate case (nothing to diff), not a
			// parse error - only non-empty, malformed input is a real error.
			if len(bytes.TrimSpace(raw)) > 0 {
				if err := json.Unmarshal(raw, &proposed); err != nil {
					return fmt.Errorf("parsing %s JSON array: %w", inputName, err)
				}
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			results := make([]recordsDiffResult, 0, len(proposed))
			for _, p := range proposed {
				path := replacePathParam("/v0/{base_id}/{table}/{record_id}", "base_id", formatCLIParamValue(flagBase))
				path = replacePathParam(path, "table", formatCLIParamValue(flagTable))
				path = replacePathParam(path, "record_id", p.ID)
				data, err := c.GetNoCache(cmd.Context(), path, nil)
				if err != nil {
					var apiErr *client.APIError
					if errors.As(err, &apiErr) && (apiErr.StatusCode == 404 || apiErrorNotFound(apiErr)) {
						results = append(results, recordsDiffResult{ID: p.ID, Found: false, Changes: []recordsDiffFieldChange{}})
						continue
					}
					return classifyAPIError(cmd.OutOrStdout(), err, flags)
				}
				var current struct {
					Fields map[string]any `json:"fields"`
				}
				if err := json.Unmarshal(data, &current); err != nil {
					return fmt.Errorf("parsing record %s response: %w", p.ID, err)
				}
				result := recordsDiffResult{ID: p.ID, Found: true, Changes: []recordsDiffFieldChange{}}
				for field, proposedVal := range p.Fields {
					currentVal, existed := current.Fields[field]
					if !existed || !reflect.DeepEqual(currentVal, proposedVal) {
						result.Changes = append(result.Changes, recordsDiffFieldChange{
							Field:    field,
							Current:  currentVal,
							Proposed: proposedVal,
						})
					} else {
						result.UnchangedFields++
					}
				}
				results = append(results, result)
			}
			return printJSONFiltered(cmd.OutOrStdout(), results, flags)
		},
	}
	cmd.Flags().StringVar(&flagBase, "base", "", "Base ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table name or ID")
	cmd.Flags().BoolVar(&flagStdin, "stdin", false, "Read proposed changes as a JSON array of {\"id\":...,\"fields\":{...}} from stdin")
	cmd.Flags().StringVar(&flagChanges, "changes", "", "Proposed changes as a JSON array of {\"id\":...,\"fields\":{...}}")
	return cmd
}

func apiErrorNotFound(err *client.APIError) bool {
	var body struct {
		Error struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"error"`
	}
	return json.Unmarshal([]byte(err.Body), &body) == nil && (body.Error.Type == "NOT_FOUND" || body.Error.Code == "NOT_FOUND")
}
