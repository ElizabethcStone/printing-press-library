// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/cliutil"
	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if wf, _, err := root.Find([]string{"workflows"}); err == nil && wf != root {
			addNovelCommandIfAbsent(wf, newWorkflowsExportCmd(flags))
		}
	})
}

type workflowExportFile struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	File   string `json:"file"`
	Status string `json:"status"`
}

func newWorkflowsExportCmd(flags *rootFlags) *cobra.Command {
	var dir string
	var activeOnly, includeArchived, stripVolatile bool
	var ids []string
	var maxPages int
	cmd := &cobra.Command{Use: "export", Short: "Write every workflow to a directory as one stable, git-friendly JSON file per workflow.",
		Example: strings.Trim(`
  n8n-pp-cli workflows export --dir ./n8n-workflows
  n8n-pp-cli workflows export --dir ./backup --active-only --agent`, "\n"),
		Annotations: map[string]string{"pp:data-source": "live", "mcp:read-only": "true", "mcp:write-flags": "dir", "pp:happy-args": "--dir=./n8n-export-dogfood"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "workflows export")
			}
			if len(args) != 0 || dir == "" || maxPages < 1 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--dir is required and --max-pages must be positive"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			items := make([]json.RawMessage, 0)
			scanned, capped := 0, false
			if len(ids) > 0 {
				for _, id := range ids {
					b, e := c.GetNoCache(ctx, "/workflows/"+id, nil)
					if e != nil {
						return classifyAPIError(cmd.OutOrStdout(), e, flags)
					}
					items = append(items, b)
				}
			} else {
				if cliutil.IsDogfoodEnv() && maxPages > 1 {
					maxPages = 1
				}
				cursor := ""
				for scanned < maxPages {
					params := map[string]string{"limit": "250"}
					if cursor != "" {
						params["cursor"] = cursor
					}
					if activeOnly {
						params["active"] = "true"
					}
					b, e := c.GetNoCache(ctx, "/workflows", params)
					if e != nil {
						return classifyAPIError(cmd.OutOrStdout(), e, flags)
					}
					var page struct {
						Data []json.RawMessage `json:"data"`
						Next *string           `json:"nextCursor"`
					}
					if e := json.Unmarshal(b, &page); e != nil {
						return fmt.Errorf("decode workflow page: %w", e)
					}
					if page.Data == nil {
						page.Data = make([]json.RawMessage, 0)
					}
					items = append(items, page.Data...)
					scanned++
					if page.Next == nil || *page.Next == "" {
						break
					}
					cursor = *page.Next
					if scanned == maxPages {
						capped = true
					}
				}
			}
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return fmt.Errorf("create export directory: %w", err)
			}
			// Index existing <slug>--<id>.json files once so renamed workflows can be cleaned up without a per-workflow directory scan.
			existingByID := map[string][]string{}
			if entries, err := os.ReadDir(dir); err == nil {
				for _, e := range entries {
					name := e.Name()
					if i := strings.LastIndex(name, "--"); i >= 0 && strings.HasSuffix(name, ".json") && !e.IsDir() {
						fid := strings.TrimSuffix(name[i+2:], ".json")
						existingByID[fid] = append(existingByID[fid], filepath.Join(dir, name))
					}
				}
			}
			files := make([]workflowExportFile, 0)
			written, unchanged, skipped, renamed := 0, 0, 0, 0
			for _, raw := range items {
				var wf map[string]any
				if err := json.Unmarshal(raw, &wf); err != nil {
					return fmt.Errorf("decode workflow: %w", err)
				}
				if archived, _ := wf["isArchived"].(bool); archived && !includeArchived {
					skipped++
					continue
				}
				id, _ := wf["id"].(string)
				name, _ := wf["name"].(string)
				if stripVolatile {
					for _, k := range []string{"updatedAt", "createdAt", "versionId", "activeVersionId", "activeVersion", "triggerCount", "shared", "meta", "staticData"} {
						delete(wf, k)
					}
				}
				if id == "" || id != filepath.Base(id) || strings.ContainsAny(id, "*?[") {
					return fmt.Errorf("workflow has an unusable id %q", id)
				}
				var buf bytes.Buffer
				enc := json.NewEncoder(&buf)
				enc.SetEscapeHTML(false) // keep =>, && and < readable in code and expression parameters
				enc.SetIndent("", "  ")
				if err := enc.Encode(wf); err != nil {
					return err
				}
				data := buf.Bytes()
				file := filepath.Join(dir, workflowSlug(name)+"--"+id+".json")
				old, err := os.ReadFile(file) // #nosec G304 -- path under user-chosen --dir
				status := "written"
				if err == nil && string(old) == string(data) {
					unchanged++
					status = "unchanged"
				} else {
					if err != nil && !os.IsNotExist(err) {
						return fmt.Errorf("read export file: %w", err)
					}
					if err := writeFileAtomic(file, data); err != nil {
						return fmt.Errorf("write export file: %w", err)
					}
					written++
				}
				// A renamed workflow leaves its old <slug>--<id>.json behind; remove it only after the new file is in place.
				for _, o := range existingByID[id] {
					if o != file {
						if err := os.Remove(o); err != nil {
							return fmt.Errorf("remove renamed export file: %w", err)
						}
						renamed++
					}
				}
				files = append(files, workflowExportFile{ID: id, Name: name, File: file, Status: status})
			}
			out := map[string]any{"dir": dir, "written": written, "unchanged": unchanged, "skipped_archived": skipped, "removed_renamed": renamed, "files": files, "scanned_pages": scanned}
			if capped {
				out["note"] = "more workflows may exist; raise --max-pages"
			}
			return printJSONFiltered(cmd.OutOrStdout(), out, flags)
		}}
	cmd.Flags().StringVar(&dir, "dir", "", "Output directory (required)")
	cmd.Flags().BoolVar(&activeOnly, "active-only", false, "Export active workflows only")
	cmd.Flags().BoolVar(&includeArchived, "include-archived", false, "Include archived workflows")
	cmd.Flags().StringArrayVar(&ids, "id", nil, "Export only this workflow id (repeatable)")
	cmd.Flags().IntVar(&maxPages, "max-pages", 20, "Maximum pages of 250 workflows to fetch")
	cmd.Flags().BoolVar(&stripVolatile, "strip-volatile", true, "Drop volatile top-level fields")
	return cmd
}

var workflowSlugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

func workflowSlug(s string) string {
	s = strings.Trim(workflowSlugInvalid.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" {
		return "workflow"
	}
	return s
}

// writeFileAtomic replaces path via a temp file and rename so an interrupted write never truncates the last good export.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
