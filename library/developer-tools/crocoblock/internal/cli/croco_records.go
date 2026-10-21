package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		var limit, page, form int
		var sortOrder string
		list := crocoDeclaredCommand(f, &cobra.Command{Use: "list", Short: "List submission metadata without personal fields"}, true, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
			if len(args) > 0 {
				return nil, usageErr(fmt.Errorf("records list accepts no positional arguments"))
			}
			if limit < 1 || limit > 100 || page < 1 || form < 0 {
				return nil, usageErr(fmt.Errorf("--limit must be 1–100; --page positive; --form nonnegative"))
			}
			if sortOrder != "ASC" && sortOrder != "DESC" {
				return nil, usageErr(fmt.Errorf("--sort must be ASC or DESC"))
			}
			rows, total, e := crocoRecordsPage(ctx, c, page, limit, form, sortOrder)
			if e != nil {
				return nil, e
			}
			return map[string]any{"items": rows, "total": total, "page": page, "limit": limit, "complete": page == 1 && int64(len(rows)) == total, "scope": map[string]any{"form_id": form}}, nil
		})
		list.Flags().IntVar(&limit, "limit", 50, "Items per page (1–100)")
		list.Flags().IntVar(&page, "page", 1, "One-based page")
		list.Flags().IntVar(&form, "form", 0, "Filter to this form ID; enables a verified form association")
		list.Flags().StringVar(&sortOrder, "sort", "ASC", "ID sort order: ASC or DESC")
		list.Example = "  crocoblock-pp-cli records list --form 42 --limit 50 --agent"
		crocoAttach(root, "records", list)
		var selected string
		var include bool
		var scanLimit int
		fields := crocoDeclaredCommand(f, &cobra.Command{Use: "fields <id>", Short: "Inspect saved field names and presence; values require explicit selection"}, true, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
			id, e := crocoOneID(args)
			if e != nil {
				return nil, e
			}
			names, e := crocoSelectedNames(selected)
			if e != nil {
				return nil, e
			}
			if include && len(names) == 0 {
				return nil, usageErr(fmt.Errorf("--include-values requires an explicit --fields selection"))
			}
			if scanLimit < 1 || scanLimit > 10000 {
				return nil, usageErr(fmt.Errorf("--limit must be 1–10000"))
			}
			items, total, complete, e := crocoRecordFields(ctx, c, id, scanLimit, names, include)
			if e != nil {
				return nil, e
			}
			return map[string]any{"record_id": id, "fields": items, "total": total, "complete": complete, "values_included": include, "selected_fields": names}, nil
		})
		fields.Flags().StringVar(&selected, "fields", "", "Comma-separated field names to select")
		fields.Flags().BoolVar(&include, "include-values", false, "Include values only for explicitly selected fields (secrets remain redacted)")
		fields.Flags().IntVar(&scanLimit, "limit", 1000, "Maximum fields to scan (1–10000); incomplete coverage is reported")
		fields.Example = "  crocoblock-pp-cli records fields 7 --agent\n  crocoblock-pp-cli records fields 7 --fields reference --include-values --agent"
		crocoAttach(root, "records", fields)
	})
}

func crocoSelectedNames(s string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	for _, x := range strings.Split(s, ",") {
		x = strings.TrimSpace(x)
		if _, e := crocoSlug(x); e != nil {
			return nil, e
		}
		if seen[x] {
			return nil, usageErr(fmt.Errorf("duplicate field %s", x))
		}
		seen[x] = true
		out = append(out, x)
	}
	return out, nil
}
func crocoColumn(row map[string]any, key string) any {
	v := row[key]
	if m, ok := v.(map[string]any); ok {
		if value, exists := m["value"]; exists {
			return value
		}
	}
	return v
}
func crocoRecordPage(v any) ([]any, int64, error) {
	m, e := crocoMap(v)
	if e != nil {
		return nil, 0, e
	}
	if _, ok := m["list"]; !ok {
		return nil, 0, apiErr(fmt.Errorf("records response missing list; coverage unknown"))
	}
	if _, ok := m["list"].([]any); !ok {
		return nil, 0, apiErr(fmt.Errorf("records list is not an array; coverage unknown"))
	}
	a, e := crocoArray(m["list"])
	if e != nil {
		return nil, 0, e
	}
	n, e := crocoInt(m["total"])
	if e != nil || n < 0 {
		return nil, 0, apiErr(fmt.Errorf("records response missing valid total; coverage unknown"))
	}
	return a, n, nil
}
func crocoRecordsPage(ctx context.Context, c *client.Client, page, limit, form int, sortOrder string) ([]any, int64, error) {
	q := map[string]string{"limit": strconv.Itoa(limit), "page": strconv.Itoa(page), "sort": sortOrder}
	if form > 0 {
		q["filters[form]"] = strconv.Itoa(form)
	}
	v, e := crocoRequest(ctx, c, "GET", "/jet-form-builder/v1/records/fetch-page", q, nil, true)
	if e != nil {
		return nil, 0, e
	}
	rows, total, e := crocoRecordPage(v)
	if e != nil {
		return nil, 0, e
	}
	out := []any{}
	for _, v := range rows {
		m, e := crocoMap(v)
		if e != nil {
			return nil, 0, e
		}
		id := crocoColumn(m, "choose")
		if id == nil {
			id = m["id"]
		}
		n, e := crocoInt(id)
		if e != nil || n < 1 {
			return nil, 0, apiErr(fmt.Errorf("record list row missing numeric ID; coverage unknown"))
		}
		var formID any
		if form > 0 {
			formID = form
		}
		out = append(out, map[string]any{"record_id": n, "form_id": formID, "created_at": crocoColumn(m, "created_at"), "status": crocoColumn(m, "status")})
	}
	if len(out) > limit {
		return nil, 0, apiErr(fmt.Errorf("record response exceeded requested page size"))
	}
	if int64(len(out)) > total {
		return nil, 0, apiErr(fmt.Errorf("record page exceeds reported total; coverage unknown"))
	}
	return out, total, nil
}
func crocoRecordFields(ctx context.Context, c *client.Client, id string, cap int, selected []string, include bool) ([]any, int64, bool, error) {
	out := []any{}
	count := 0
	total := int64(-1)
	wanted := map[string]bool{}
	for _, k := range selected {
		wanted[k] = true
	}
	found := map[string]bool{}
	for page := 1; count < cap; page++ {
		limit := 100
		if cap-count < limit {
			limit = cap - count
		}
		// Keep page size stable. A shortened final request would change the offset.
		q := map[string]string{"page": strconv.Itoa(page), "limit": "100", "sort": "ASC"}
		v, e := crocoRequest(ctx, c, "GET", "/jet-form-builder/v1/records/fields-box/"+id, q, nil, true)
		if e != nil {
			return nil, 0, false, e
		}
		rows, n, e := crocoRecordPage(v)
		if e != nil {
			return nil, 0, false, e
		}
		if len(rows) > 100 {
			return nil, 0, false, apiErr(fmt.Errorf("record fields response exceeded requested page size"))
		}
		if total >= 0 && n != total {
			return nil, 0, false, apiErr(fmt.Errorf("record fields changed during pagination; coverage unknown"))
		}
		total = n
		if len(rows) == 0 && int64(count) < total {
			return nil, total, false, apiErr(fmt.Errorf("record field page empty before reported total; coverage incomplete"))
		}
		for i, v := range rows {
			if i >= limit {
				break
			}
			count++
			m, e := crocoMap(v)
			if e != nil {
				return nil, 0, false, e
			}
			nv := crocoColumn(m, "name")
			name := ""
			label := any(nil)
			if nm, ok := nv.(map[string]any); ok {
				name = crocoString(nm["name"])
				label = nm["label"]
			} else {
				name = crocoString(nv)
			}
			if name == "" {
				return nil, 0, false, apiErr(fmt.Errorf("record field missing name; coverage unknown"))
			}
			found[name] = true
			if len(wanted) > 0 && !wanted[name] {
				continue
			}
			value := crocoColumn(m, "value")
			present := value != nil && crocoString(value) != ""
			item := map[string]any{"name": name, "label": label, "present": present, "type": crocoColumn(m, "field_type")}
			if include {
				if crocoSensitive(name) {
					item["value"] = "[redacted]"
				} else {
					item["value"] = value
				}
			}
			out = append(out, item)
		}
		if int64(count) >= total {
			for k := range wanted {
				if !found[k] {
					return nil, total, true, notFoundErr(fmt.Errorf("record does not contain selected field %s", k))
				}
			}
			return out, total, true, nil
		}
	}
	return out, total, false, nil
}
