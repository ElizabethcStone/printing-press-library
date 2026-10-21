package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func crocoWPPage(ctx context.Context, c *client.Client, route string, page, limit int, query map[string]string) ([]any, int64, error) {
	q := map[string]string{"page": strconv.Itoa(page), "per_page": strconv.Itoa(limit), "context": "edit", "orderby": "id", "order": "asc", "_envelope": "1"}
	for k, v := range query {
		q[k] = v
	}
	v, e := crocoRequest(ctx, c, "GET", route, q, nil, true)
	if e != nil {
		return nil, 0, e
	}
	m, e := crocoMap(v)
	if e != nil {
		return nil, 0, e
	}
	status, e := crocoInt(m["status"])
	if e != nil || status != 200 {
		return nil, 0, apiErr(fmt.Errorf("WordPress collection returned status %v", m["status"]))
	}
	a, e := crocoArray(m["body"])
	if e != nil || m["body"] == nil {
		return nil, 0, apiErr(fmt.Errorf("WordPress collection body missing"))
	}
	headers, e := crocoMap(m["headers"])
	if e != nil {
		return nil, 0, e
	}
	var total int64 = -1
	for k, v := range headers {
		if strings.EqualFold(k, "X-WP-Total") {
			total, e = crocoInt(v)
			if e != nil {
				return nil, 0, e
			}
		}
	}
	if total < 0 {
		return nil, 0, apiErr(fmt.Errorf("WordPress collection missing total header"))
	}
	return a, total, nil
}
func crocoSyncRows(items []any, idKeys ...string) ([]store.CrocoRow, error) {
	rows := make([]store.CrocoRow, 0, len(items))
	for i, v := range items {
		m, e := crocoMap(v)
		if e != nil {
			return nil, e
		}
		id := ""
		for _, k := range idKeys {
			if m[k] != nil {
				id = crocoString(m[k])
				break
			}
		}
		if id == "" {
			return nil, apiErr(fmt.Errorf("collection row %d has no stable ID", i))
		}
		b, e := json.Marshal(crocoSafe(m))
		if e != nil {
			return nil, e
		}
		rows = append(rows, store.CrocoRow{ID: id, Data: b})
	}
	return rows, nil
}
func crocoCaptureRows(ctx context.Context, s *store.Store, c *client.Client, versions map[string]string, resource string, items []any, complete bool, fields []string, scope map[string]any, note string, idKeys ...string) error {
	rows, e := crocoSyncRows(items, idKeys...)
	if e != nil {
		return e
	}
	return s.CrocoReplace(ctx, store.CrocoCapture{Site: c.BaseURL, Resource: resource, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano), Complete: complete, Fields: fields, Versions: versions, Scope: scope, Note: note}, rows)
}
func crocoPageAll(maxPages int, fetch func(int) ([]any, int64, error)) ([]any, bool, int, error) {
	out := []any{}
	var expected int64 = -1
	for page := 1; page <= maxPages; page++ {
		a, total, e := fetch(page)
		if e != nil {
			return out, false, page, e
		}
		if total < 0 {
			return out, false, page, apiErr(fmt.Errorf("collection total is unknown"))
		}
		if expected >= 0 && expected != total {
			return append(out, a...), false, page, apiErr(fmt.Errorf("collection changed during pagination; capture is incomplete"))
		}
		expected = total
		out = append(out, a...)
		if int64(len(out)) >= total {
			if int64(len(out)) != total {
				return out, false, page, apiErr(fmt.Errorf("collection count exceeds reported total"))
			}
			return out, true, page, nil
		}
		if len(a) == 0 {
			return out, false, page, apiErr(fmt.Errorf("empty page before reported total; capture is incomplete"))
		}
	}
	return out, false, maxPages, nil
}
func newSyncCmd(f *rootFlags) *cobra.Command {
	var resources, fields, objectTypes []string
	var dbPath, cct string
	var maxPages, recordForm int
	var full, strict bool
	cmd := crocoDeclaredCommand(f, &cobra.Command{Use: "sync", Short: "Capture selected JetPlugin resources with site, schema and coverage provenance"}, false, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
		if !full {
			return nil, usageErr(fmt.Errorf("--full=false is not supported: sync always captures full paginated collections; use --max-pages to cap work and record incomplete coverage"))
		}
		if maxPages < 1 || maxPages > 1000 {
			return nil, usageErr(fmt.Errorf("--max-pages must be 1..1000"))
		}
		if cliutil.IsDogfoodEnv() && !cmd.Flags().Changed("max-pages") {
			maxPages = 1
		}
		if recordForm < 0 {
			return nil, usageErr(fmt.Errorf("--record-form must be positive"))
		}
		if cct != "" {
			if _, e := crocoSlug(cct); e != nil {
				return nil, e
			}
			if len(fields) == 0 {
				return nil, usageErr(fmt.Errorf("--cct requires --fields: choose the data fields to capture"))
			}
		}
		allowed := map[string]bool{"schema": true, "forms": true, "filters": true, "bookings": true, "appointments": true, "relations": true, "cct": true, "records": true, "objects": true}
		for _, resource := range resources {
			if !allowed[resource] {
				return nil, usageErr(fmt.Errorf("unknown resource %s", resource))
			}
			if resource == "cct" && cct == "" {
				return nil, usageErr(fmt.Errorf("resource cct requires --cct and --fields"))
			}
			if resource == "records" && recordForm == 0 {
				return nil, usageErr(fmt.Errorf("resource records requires --record-form; values are not captured"))
			}
			if resource == "objects" && len(objectTypes) == 0 {
				return nil, usageErr(fmt.Errorf("resource objects requires --object-types"))
			}
		}
		var cctFields []string
		for _, resource := range resources {
			if resource == "cct" {
				var err error
				cctFields, err = crocoSyncCCTFields(ctx, c, cct, fields)
				if err != nil {
					return nil, err
				}
				break
			}
		}
		versions, e := crocoPluginVersions(ctx, c)
		if e != nil {
			return nil, e
		}
		s, e := crocoOpenStore(ctx, dbPath)
		if e != nil {
			return nil, e
		}
		defer s.Close()
		results := []any{}
		failures := []any{}
		save := func(resource string, items []any, complete bool, selected []string, scope map[string]any, note string, ids ...string) error {
			e := crocoCaptureRows(ctx, s, c, versions, resource, items, complete, selected, scope, note, ids...)
			if e == nil {
				results = append(results, map[string]any{"resource": resource, "count": len(items), "complete": complete, "scope": scope})
			}
			return e
		}
		for _, resource := range resources {
			var err error
			switch resource {
			case "schema":
				var config map[string]any
				config, err = crocoConfiguration(ctx, c)
				if err == nil {
					config["id"] = "configuration"
					err = save("engine:configuration", []any{config}, true, []string{"schema"}, map[string]any{"parts": "all"}, "Native configuration; omitted secrets are redacted", "id")
				}
				if err == nil {
					var v any
					v, err = crocoRequest(ctx, c, "GET", "/jet-engine/v2/get-content-types", nil, nil, true)
					if err == nil {
						var m map[string]any
						m, err = crocoMap(v)
						if err == nil {
							var a []any
							a, err = crocoRelationCollection(m["data"])
							if m["data"] == nil && err == nil {
								err = apiErr(fmt.Errorf("content type definitions missing data"))
							}
							if err == nil {
								err = save("engine:cct", a, true, []string{"schema"}, map[string]any{}, "", "id", "ID")
							}
						}
					}
				}
				if err == nil {
					for _, part := range []string{"post-types", "taxonomies", "meta-boxes", "queries"} {
						var v any
						v, err = crocoRequest(ctx, c, "GET", "/jet-engine/v2/get-"+part, nil, nil, true)
						if err != nil {
							break
						}
						var m map[string]any
						m, err = crocoMap(v)
						if err != nil {
							break
						}
						var a []any
						a, err = crocoDeclaredRows(m["data"])
						if err != nil {
							break
						}
						err = save("engine:"+part, a, true, []string{"schema"}, map[string]any{}, "Definition metadata with credentials redacted", "id", "ID")
						if err != nil {
							break
						}
					}
				}

			case "forms":
				var a []any
				var complete bool
				a, complete, _, err = crocoPageAll(maxPages, func(page int) ([]any, int64, error) {
					return crocoWPPage(ctx, c, "/wp/v2/jet-form-builder", page, 100, map[string]string{"status": "any"})
				})
				if err == nil {
					err = save("forms", a, complete, []string{"id", "status", "content", "meta"}, map[string]any{"status": "any"}, "Action settings redacted; submitted values excluded", "id")
				}
				if err == nil {
					for _, x := range a {
						m, _ := crocoMap(x)
						id := crocoString(m["id"])
						var fs []any
						var source string
						fs, source, err = crocoFormFields(ctx, c, id)
						if err != nil {
							break
						}
						err = save("forms:fields:"+id, fs, true, []string{"name", "type", "label"}, map[string]any{"form_id": id, "source": source}, "Declared fields only", "name")
						if err != nil {
							break
						}
					}
				}
			case "filters":
				all := []any{}
				complete := true
				for _, status := range []string{"publish", "draft", "trash"} {
					var a []any
					var done bool
					a, done, _, err = crocoPageAll(maxPages, func(page int) ([]any, int64, error) {
						m, e := crocoFilterPage(ctx, c, 100, page, status, "")
						if e != nil {
							return nil, 0, e
						}
						a, e := crocoArray(m["filters"])
						if e != nil {
							return nil, 0, e
						}
						n, e := crocoInt(m["count"])
						return a, n, e
					})
					if err != nil {
						break
					}
					complete = complete && done
					for _, x := range a {
						m, e := crocoMap(x)
						if e != nil {
							err = e
							break
						}
						id := crocoString(m["ID"])
						if id == "" {
							id = crocoString(m["id"])
						}
						var detail map[string]any
						detail, err = crocoFilterGet(ctx, c, id)
						if err != nil {
							break
						}
						detail["status"] = status
						all = append(all, detail)
					}
					if err != nil {
						break
					}
				}
				if err == nil {
					err = save("filters", all, complete, []string{"dependencies", "status"}, map[string]any{"statuses": []string{"publish", "draft", "trash"}}, "Configured dependencies; unknown providers remain unknown", "ID")
				}
			case "bookings", "appointments":
				var a []any
				var complete bool
				a, complete, _, err = crocoPageAll(maxPages, func(page int) ([]any, int64, error) {
					var m map[string]any
					var e error
					if resource == "bookings" {
						m, e = crocoBookingPage(ctx, c, 100, (page-1)*100, "{}", "all")
					} else {
						m, e = crocoAppointmentPage(ctx, c, 100, (page-1)*100, "{}", "all")
					}
					if e != nil {
						return nil, 0, e
					}
					a, e := crocoArray(m["data"])
					if e != nil {
						return nil, 0, e
					}
					n, e := crocoInt(m["total"])
					return a, n, e
				})
				if err == nil {
					safe := []any{}
					for _, x := range a {
						m, _ := crocoMap(x)
						row := map[string]any{}
						for _, k := range []string{"booking_id", "ID", "status", "apartment_id", "apartment_unit", "service", "provider", "check_in_date_timestamp", "check_out_date_timestamp", "date_timestamp", "slot_timestamp", "slot_end_timestamp", "group_ID", "type"} {
							if v, ok := m[k]; ok {
								row[k] = v
							}
						}
						safe = append(safe, row)
					}
					err = save(resource, safe, complete, []string{"id", "resource", "status", "interval"}, map[string]any{"all_statuses": true}, "Customer details excluded; interval validity is assessed by the analysis policy", "booking_id", "ID")
				}
			case "relations":
				var defs []any
				defs, err = crocoRelationDefinitions(ctx, c)
				if err == nil {
					err = save("engine:relations", defs, true, []string{"schema"}, map[string]any{}, "", "id", "ID")
				}
				if err == nil {
					for _, x := range defs {
						m, _ := crocoMap(x)
						id := crocoString(m["id"])
						if id == "" {
							id = crocoString(m["ID"])
						}
						var edges []any
						edges, err = crocoRelationEdges(ctx, c, id)
						if err != nil {
							break
						}
						for i, x := range edges {
							edge, _ := crocoMap(x)
							edge["id"] = strconv.Itoa(i)
							delete(edge, "meta")
						}
						err = save("relations:"+id, edges, true, []string{"parent_object_id", "child_object_id"}, map[string]any{"relation_id": id}, "Edge metadata excluded; endpoint inventory is captured separately", "id")
						if err != nil {
							break
						}
					}
				}
			case "cct":
				selected := cctFields
				all := []any{}
				complete := false
				for page := 1; page <= maxPages; page++ {
					var a []any
					a, err = crocoCCTPage(ctx, c, cct, 100, (page-1)*100, nil)
					if err != nil {
						break
					}
					for _, x := range a {
						m, _ := crocoMap(x)
						row := map[string]any{}
						for _, k := range selected {
							v, ok := m[k]
							if !ok {
								err = apiErr(fmt.Errorf("CCT row omitted selected field %q; coverage is incomplete", k))
								break
							}
							row[k] = v
						}
						if err != nil {
							break
						}
						all = append(all, row)
					}
					if err != nil {
						break
					}
					if len(a) < 100 {
						complete = true
						break
					}
				}
				if err == nil {
					sort.Strings(selected)
					err = save("cct:"+cct, all, complete, selected, map[string]any{"type": cct}, "Only explicitly selected fields captured", "_ID")
				}
			case "records":
				var a []any
				var complete bool
				a, complete, _, err = crocoPageAll(maxPages, func(page int) ([]any, int64, error) { return crocoRecordsPage(ctx, c, page, 100, recordForm, "ASC") })
				if err == nil {
					for _, x := range a {
						m, _ := crocoMap(x)
						id := crocoString(m["record_id"])
						var fs []any
						var done bool
						fs, _, done, err = crocoRecordFields(ctx, c, id, 1000, nil, false)
						if err != nil {
							break
						}
						m["fields"] = fs
						complete = complete && done
					}
					if err == nil {
						err = save("records:"+strconv.Itoa(recordForm), a, complete, []string{"record_id", "form_id", "fields.name", "fields.present"}, map[string]any{"form_id": recordForm, "values": false}, "Submitted values excluded; only field presence captured", "record_id")
					}
				}
			case "objects":
				var v any
				v, err = crocoRequest(ctx, c, "GET", "/wp/v2/types", map[string]string{"context": "edit"}, nil, true)
				if err != nil {
					break
				}
				var types map[string]any
				types, err = crocoMap(v)
				if err != nil {
					break
				}
				for _, typ := range objectTypes {
					var def map[string]any
					def, err = crocoMap(types[typ])
					if err != nil {
						break
					}
					base := crocoString(def["rest_base"])
					ns := crocoString(def["rest_namespace"])
					if ns == "" {
						ns = "wp/v2"
					}
					if base == "" {
						err = apiErr(fmt.Errorf("post type %s is not exposed through REST", typ))
						break
					}
					var a []any
					var done bool
					a, done, _, err = crocoPageAll(maxPages, func(page int) ([]any, int64, error) {
						return crocoWPPage(ctx, c, "/"+ns+"/"+base, page, 100, map[string]string{"status": "any", "_fields": "id,type,status"})
					})
					if err != nil {
						break
					}
					err = save("objects:"+typ, a, done, []string{"id", "type", "status"}, map[string]any{"type": typ, "status": "any"}, "Accessible non-trash objects only; restricted or trashed objects remain outside this scope", "id")
					if err != nil {
						break
					}
				}
			}
			if err != nil {
				failures = append(failures, map[string]any{"resource": resource, "error": err.Error()}) // Invalidate stale coverage without claiming failed data is empty.
				captures, ce := s.CrocoCaptures(ctx)
				if ce != nil {
					return nil, ce
				}
				for _, cap := range captures {
					match := cap.Resource == resource || strings.HasPrefix(cap.Resource, resource+":")
					if resource == "schema" {
						match = strings.HasPrefix(cap.Resource, "engine:")
					}
					if resource == "relations" {
						match = match || cap.Resource == "engine:relations"
					}
					if resource == "cct" {
						match = cap.Resource == "cct:"+cct
					}
					if resource == "records" {
						match = cap.Resource == "records:"+strconv.Itoa(recordForm)
					}
					if match {
						if e := s.CrocoInvalidate(ctx, cap.Resource, "Latest fetch failed; retained rows are prior observations, coverage is unknown"); e != nil {
							return nil, e
						}
					}
				}
			}
		}
		result := map[string]any{"site": c.BaseURL, "captures": results, "fetch_failures": failures, "max_pages": maxPages}
		if len(failures) > 0 && strict {
			_ = f.printJSON(cmd, result)
			return nil, apiErr(fmt.Errorf("%d resources failed; captures explicitly report incomplete coverage", len(failures)))
		}
		return result, nil
	})
	cmd.Flags().BoolVar(&full, "full", true, "Capture full selected collections; this adapter does not use incremental cursors")
	cmd.Flags().BoolVar(&strict, "strict", true, "Exit nonzero if any selected resource fails to capture")
	cmd.Flags().StringSliceVar(&resources, "resources", []string{"schema", "forms", "filters", "bookings", "appointments", "relations"}, "Resources to capture: schema,forms,filters,bookings,appointments,relations,cct,records,objects")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite capture file; defaults to CROCOBLOCK_DB or the CLI data directory")
	cmd.Flags().IntVar(&maxPages, "max-pages", 10, "Maximum pages per selected collection; capped captures remain incomplete")
	cmd.Flags().StringVar(&cct, "cct", "", "Custom content type slug required for a CCT data capture")
	cmd.Flags().StringSliceVar(&fields, "fields", nil, "Explicit CCT data fields to capture; credentials are rejected")
	cmd.Flags().IntVar(&recordForm, "record-form", 0, "Form ID for submitted record field-presence capture, without values")
	cmd.Flags().StringSliceVar(&objectTypes, "object-types", nil, "WordPress post type slugs whose accessible IDs should be captured")
	cmd.Annotations["mcp:write-flags"] = "db"
	cmd.Example = "  crocoblock-pp-cli sync --resources schema,forms,filters --db candidate.db --agent\n  crocoblock-pp-cli sync --resources cct --cct rooms --fields external_id,title --db candidate.db --agent"
	return cmd
}

// The admin schema exposes actual CCT fields even when the item collection is
// empty. Service columns below are declared by JetEngine's factory/manager.
func crocoSyncCCTFields(ctx context.Context, c *client.Client, slug string, fields []string) ([]string, error) {
	if _, e := crocoSlug(slug); e != nil {
		return nil, e
	}
	if len(fields) == 0 {
		return nil, usageErr(fmt.Errorf("CCT sync requires --fields naming declared fields"))
	}
	selected := []string{"_ID", "cct_status"}
	seen := map[string]bool{"_ID": true, "cct_status": true}
	for _, name := range fields {
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(name) {
			return nil, usageErr(fmt.Errorf("invalid --fields name %q", name))
		}
		if crocoSensitive(name) {
			return nil, usageErr(fmt.Errorf("sensitive field %q cannot be cached", name))
		}
		if !seen[name] {
			selected = append(selected, name)
			seen[name] = true
		}
	}
	v, e := crocoRequest(ctx, c, "GET", "/jet-engine/v2/get-content-types", nil, nil, true)
	if e != nil {
		return nil, e
	}
	m, e := crocoMap(v)
	if e != nil {
		return nil, e
	}
	if m["success"] != true {
		return nil, apiErr(fmt.Errorf("CCT schema discovery did not return success"))
	}
	definitions, e := crocoRelationCollection(m["data"])
	if e != nil {
		return nil, e
	}
	var definition map[string]any
	for _, raw := range definitions {
		d, e := crocoMap(raw)
		if e != nil {
			return nil, e
		}
		args, _ := d["args"].(map[string]any)
		name := crocoString(d["slug"])
		if name == "" {
			name = crocoString(args["slug"])
		}
		if name == slug {
			if definition != nil {
				return nil, apiErr(fmt.Errorf("CCT schema repeats type %s", slug))
			}
			definition = d
		}
	}
	if definition == nil {
		return nil, usageErr(fmt.Errorf("CCT type %q does not exist in the engine schema", slug))
	}
	declared := map[string]bool{"_ID": true, "cct_status": true, "cct_author_id": true, "cct_created": true, "cct_modified": true}
	args, _ := definition["args"].(map[string]any)
	if args["has_single"] == true || args["has_single"] == "true" || args["has_single"] == "1" {
		declared["cct_single_post_id"] = true
	}
	schema, ok := definition["meta_fields"].([]any)
	if !ok {
		return nil, apiErr(fmt.Errorf("CCT %s field schema is missing or malformed", slug))
	}
	for _, raw := range schema {
		field, e := crocoMap(raw)
		if e != nil {
			return nil, e
		}
		name, typ, objectType := crocoString(field["name"]), crocoString(field["type"]), crocoString(field["object_type"])
		if typ != "" && typ != "html" && (objectType == "" || objectType == "field" || objectType == "service_field") {
			declared[name] = true
		}
	}
	for _, name := range selected {
		if !declared[name] {
			return nil, usageErr(fmt.Errorf("--fields contains %q, which is not a declared data field of CCT %s", name, slug))
		}
	}
	sort.Strings(selected)
	return selected, nil
}

func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) { crocoAttach(root, "", newSyncCmd(f)) })
}
