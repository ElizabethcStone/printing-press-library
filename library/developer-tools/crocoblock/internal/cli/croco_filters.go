package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
	"math/big"
	"reflect"
	"sort"
	"strconv"
)

var crocoFilterKeys = map[string]bool{"_ability_deselect_radio": true, "_active_label": true, "_add_all_option": true, "_all_option_label": true, "_alphabet_behavior": true, "_alphabet_options": true, "_alphabet_radio_deselect": true, "_color_image_ability_deselect_radio": true, "_color_image_add_all_option": true, "_color_image_add_all_option_color": true, "_color_image_add_all_option_image": true, "_color_image_add_all_option_lael": true, "_color_image_behavior": true, "_color_image_type": true, "_custom_field_source_plugin": true, "_custom_query_var": true, "_data_exclude_include": true, "_data_source": true, "_date_available_range": true, "_date_available_range_custom_info": true, "_date_available_range_custom_max": true, "_date_available_range_custom_min": true, "_date_format": true, "_date_from_placeholder": true, "_date_period_date_formats_info": true, "_date_period_datepicker_button_text": true, "_date_period_end_format": true, "_date_period_format": true, "_date_period_separator": true, "_date_period_start_end_enabled": true, "_date_period_start_format": true, "_date_period_type": true, "_date_source": true, "_date_to_placeholder": true, "_date_type": true, "_default_filter_value": true, "_default_filter_value_info": true, "_default_filter_value_info_date": true, "_default_filter_value_info_hierarchical": true, "_default_filter_value_info_multiple": true, "_default_filter_value_info_range": true, "_filter_label": true, "_filter_type": true, "_group_by_parent": true, "_group_collapsible": true, "_ih_source_map": true, "_ih_terms_order": true, "_ih_terms_orderby": true, "_ih_terms_orderby_meta_value": true, "_is_custom_checkbox": true, "_is_custom_query_var": true, "_is_default_filter_value": true, "_is_hierarchical": true, "_only_child": true, "_placeholder": true, "_query_compare": true, "_query_var": true, "_range_inputs_enabled": true, "_range_inputs_separators_enabled": true, "_rating_compare_operand": true, "_rating_options": true, "_s_by": true, "_s_placeholder": true, "_show_empty_terms": true, "_source_callback": true, "_source_color_image_input": true, "_source_custom_field": true, "_source_get_from_field_data": true, "_source_manual_input": true, "_source_manual_input_range": true, "_source_max": true, "_source_min": true, "_source_post_type": true, "_source_step": true, "_source_taxonomy": true, "_terms_order": true, "_terms_orderby": true, "_terms_orderby_meta_value": true, "_terms_relational_operator": true, "_use_exclude_include": true, "_values_decimal_num": true, "_values_decimal_sep": true, "_values_prefix": true, "_values_suffix": true, "_values_thousand_sep": true, "date": true, "title": true}

func crocoFilterPage(ctx context.Context, c *client.Client, limit, page int, status, search string) (map[string]any, error) {
	if limit < 1 || limit > 100 || page < 1 {
		return nil, usageErr(fmt.Errorf("--limit must be 1..100 and --page at least 1"))
	}
	if status != "publish" && status != "trash" && status != "draft" {
		return nil, usageErr(fmt.Errorf("--status must be publish, draft or trash"))
	}
	v, e := crocoRequest(ctx, c, "POST", "/jet-smart-filters-api/v1/filters", nil, map[string]any{"per_page": limit, "page": page, "status": status, "search": search, "orderby": "date", "order": "asc", "language": "all"}, true)
	if e != nil {
		return nil, e
	}
	m, e := crocoMap(v)
	if e != nil {
		return nil, e
	}
	if _, e = crocoArray(m["filters"]); e != nil {
		return nil, e
	}
	if m["filters"] == nil {
		return nil, apiErr(fmt.Errorf("filter response missing filters collection"))
	}
	return m, nil
}
func crocoFilterGet(ctx context.Context, c *client.Client, id string) (map[string]any, error) {
	v, e := crocoRequest(ctx, c, "POST", "/jet-smart-filters-api/v1/filter", nil, map[string]any{"id": id}, true)
	if e != nil {
		return nil, e
	}
	if v == false || v == nil {
		return nil, notFoundErr(fmt.Errorf("filter %s not found", id))
	}
	m, e := crocoMap(v)
	if e != nil {
		return nil, e
	}
	if crocoString(m["ID"]) != id {
		return nil, apiErr(fmt.Errorf("filter response ID mismatch"))
	}
	return m, nil
}
func crocoFilterData(body map[string]any) error {
	if e := crocoRequireBody(body); e != nil {
		return e
	}
	for k, value := range body {
		if !crocoFilterKeys[k] {
			return usageErr(fmt.Errorf("unknown JetSmartFilters setting %q; inspect filters get first", k))
		}
		if k == "title" || k == "date" {
			if _, ok := value.(string); !ok {
				return usageErr(fmt.Errorf("filter %s must be a string", k))
			}
			continue
		}
		if crocoFilterSerialized[k] {
			switch value.(type) {
			case []any, map[string]any:
			default:
				return usageErr(fmt.Errorf("filter %s requires a JSON array or object", k))
			}
			continue
		}
		if _, e := crocoFilterScalar(value); e != nil {
			return usageErr(fmt.Errorf("filter %s must be a scalar setting", k))
		}
	}
	return nil
}
func crocoFilterKeysOf(m map[string]any) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}
func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		var limit, page int
		var status, search string
		list := crocoCommand(f, "list", "List filters with explicit publication status and bounded pagination", true, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
			return crocoFilterPage(ctx, c, limit, page, status, search)
		})
		list.Flags().IntVar(&limit, "limit", 25, "Maximum filters returned per page, from 1 to 100")
		list.Flags().IntVar(&page, "page", 1, "One-based page number within the selected status")
		list.Flags().StringVar(&status, "status", "publish", "Filter publication status: publish, draft or trash")
		list.Flags().StringVar(&search, "search", "", "Search filter titles using the plugin query")
		list.Example = "  crocoblock-pp-cli filters list --limit 25 --agent"
		crocoAttach(root, "filters", list)
		get := crocoCommand(f, "get <id>", "Read one filter and its configured dependencies", true, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
			id, e := crocoID(args, 0)
			if e != nil {
				return nil, e
			}
			return crocoFilterGet(ctx, c, id)
		})
		get.Example = "  crocoblock-pp-cli filters get 12 --agent"
		crocoAttach(root, "filters", get)
		for _, action := range []string{"create", "update", "duplicate", "trash", "restore"} {
			action := action
			var data, input string
			use := action + " <id>"
			if action == "create" {
				use = action
			}
			cmd := crocoCommand(f, use, "Apply and verify filter "+action+" through the plugin service", false, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
				id := "new"
				var e error
				if action != "create" {
					id, e = crocoID(args, 0)
					if e != nil {
						return nil, e
					}
				}
				body := map[string]any{"id": id}
				fields := []string{}
				if action == "create" || action == "update" {
					payload, e := crocoBody(data, input)
					if e != nil {
						return nil, e
					}
					if e = crocoFilterData(payload); e != nil {
						return nil, e
					}
					if action == "create" {
						for _, k := range []string{"title", "_filter_type", "_data_source"} {
							if crocoString(payload[k]) == "" {
								return nil, usageErr(fmt.Errorf("create requires %s", k))
							}
						}
					}
					body["data"] = payload
					fields = crocoFilterKeysOf(payload)
				}
				path := "/jet-smart-filters-api/v1/filter"
				if action == "duplicate" {
					body["duplicate"] = true
				}
				if action == "trash" || action == "restore" {
					if _, e = crocoFilterGet(ctx, c, id); e != nil {
						return nil, e
					}
					path = "/jet-smart-filters-api/v1/filters"
					key := "move_to_trash"
					if action == "restore" {
						key = "restore"
					}
					body = map[string]any{key: []string{id}, "per_page": 1}
					fields = []string{"status"}
				}
				v, e := crocoRequest(ctx, c, "POST", path, nil, body, false)
				if e != nil {
					return nil, e
				}
				if action != "trash" && action != "restore" {
					id, e = crocoFilterMutationID(action, id, v)
					if e != nil {
						return nil, e
					}
				}
				if e = crocoJournal(f, ctx, c, action, "filters", fields, map[string]any{"id": id, "operation": action}); e != nil {
					return nil, e
				}
				if action == "trash" || action == "restore" {
					want := "trash"
					if action == "restore" {
						want = "publish"
					}
					found := false
					scanned := 0
					for p := 1; p <= 10; p++ {
						m, e := crocoFilterPage(ctx, c, 100, p, want, "")
						if e != nil {
							return nil, apiErr(fmt.Errorf("mutation_applied: status verification failed: %w", e))
						}
						a, e := crocoArray(m["filters"])
						if e != nil {
							return nil, e
						}
						scanned += len(a)
						for _, x := range a {
							q, _ := crocoMap(x)
							if crocoString(q["ID"]) == id || crocoString(q["id"]) == id {
								found = true
							}
						}
						if found || len(a) < 100 {
							break
						}
					}
					if !found {
						return nil, apiErr(fmt.Errorf("mutation_applied: filter %s not observed in %s after scanning %d records; verify upstream before retry", id, want, scanned))
					}
					return map[string]any{"id": id, "status": want, "verified": true}, nil
				}
				payload, _ := body["data"].(map[string]any)
				got, e := crocoVerifyFilterReadback(ctx, c, id, payload)
				if e != nil {
					return nil, apiErr(fmt.Errorf("mutation_applied: readback failed: %w", e))
				}
				return map[string]any{"id": id, "verified": true, "filter": got}, nil
			})
			if action == "create" || action == "update" {
				crocoDataFlags(cmd, &data, &input)
			}
			cmd.Example = "  crocoblock-pp-cli filters " + use + " --dry-run --agent"
			crocoAttach(root, "filters", cmd)
		}
		crocoAttach(root, "filters", crocoReindexCmd(f))
	})
}

// The official service unserializes only these five settings. Other post-meta
// scalar values are returned as database strings; title/date are raw post fields.
var crocoFilterSerialized = map[string]bool{"_source_manual_input": true, "_source_color_image_input": true, "_source_manual_input_range": true, "_ih_source_map": true, "_data_exclude_include": true}

func crocoFilterScalar(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case bool:
		if v {
			return "1", nil
		}
		return "", nil
	case json.Number:
		return v.String(), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("unsupported scalar type %T", value)
	}
}
func crocoFilterJSONEqual(want, got any) bool {
	// PHP json_encode drops .0, but values remain numbers, not strings.
	if a, ok := want.(json.Number); ok {
		b, ok := got.(json.Number)
		if !ok {
			return false
		}
		x, ok := new(big.Rat).SetString(a.String())
		if !ok {
			return false
		}
		y, ok := new(big.Rat).SetString(b.String())
		return ok && x.Cmp(y) == 0
	}
	switch a := want.(type) {
	case map[string]any:
		if len(a) == 0 {
			if b, ok := got.([]any); ok && len(b) == 0 {
				return true
			}
		}
		b, ok := got.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for k, v := range a {
			other, exists := b[k]
			if !exists || !crocoFilterJSONEqual(v, other) {
				return false
			}
		}
		return true
	case []any:
		b, ok := got.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for i, v := range a {
			if !crocoFilterJSONEqual(v, b[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(want, got)
	}
}
func crocoVerifyFilterValues(got, payload map[string]any) error {
	for key, want := range payload {
		actual, exists := got[key]
		if !exists {
			return apiErr(fmt.Errorf("mutation_applied_or_unknown: filter readback is missing requested field %s; inspect before retry", key))
		}
		equal := false
		if crocoFilterSerialized[key] {
			equal = crocoFilterJSONEqual(want, actual)
		} else if key == "title" || key == "date" {
			equal = reflect.DeepEqual(want, actual)
		} else {
			expected, e := crocoFilterScalar(want)
			actualString, ok := actual.(string)
			equal = e == nil && ok && expected == actualString
			if e == nil && ok {
				if _, numeric := want.(json.Number); numeric {
					a, aok := new(big.Rat).SetString(expected)
					b, bok := new(big.Rat).SetString(actualString)
					equal = aok && bok && a.Cmp(b) == 0
				}
			}
		}
		if !equal {
			return apiErr(fmt.Errorf("mutation_applied_or_unknown: filter field %s did not persist as requested; inspect before retry", key))
		}
	}
	return nil
}
func crocoVerifyFilterReadback(ctx context.Context, c *client.Client, id string, payload map[string]any) (map[string]any, error) {
	got, e := crocoFilterGet(ctx, c, id)
	if e != nil {
		return nil, e
	}
	if e = crocoVerifyFilterValues(got, payload); e != nil {
		return nil, e
	}
	return got, nil
}
func crocoFilterMutationID(action, requested string, value any) (string, error) {
	n, e := crocoInt(value)
	if e != nil || n <= 0 {
		return "", apiErr(fmt.Errorf("mutation response contains no valid filter ID; verify before retrying"))
	}
	id := strconv.FormatInt(n, 10)
	if action == "update" && id != requested {
		return "", apiErr(fmt.Errorf("mutation_applied_or_unknown: update returned filter ID %s instead of %s; inspect before retry", id, requested))
	}
	if action == "duplicate" && id == requested {
		return "", apiErr(fmt.Errorf("mutation_applied_or_unknown: duplicate returned the original filter ID; inspect before retry"))
	}
	return id, nil
}
