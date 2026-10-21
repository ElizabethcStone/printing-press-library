package cli

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		for _, action := range []string{"list", "get", "create", "update", "delete"} {
			crocoAttach(root, "cct", newCrocoCCTCommand(f, action))
		}
	})
}

func newCrocoCCTCommand(f *rootFlags, action string) *cobra.Command {
	var data, input, search, searchBy, orderBy, order string
	var where []string
	limit, offset := 50, 0
	use := action + " <type>"
	if action == "get" || action == "update" || action == "delete" {
		use += " <id>"
	}
	cmd := crocoCommand(f, use, "Operate verified custom content type records with explicit pagination", action == "list" || action == "get", func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
		count := 1
		if action == "get" || action == "update" || action == "delete" {
			count = 2
		}
		if len(args) != count {
			return nil, usageErr(fmt.Errorf("cct %s requires %d positional arguments; see --help", action, count))
		}
		slug, err := crocoSlug(args[0])
		if err != nil {
			return nil, err
		}
		id := ""
		if count == 2 {
			id, err = crocoID(args, 1)
			if err != nil {
				return nil, err
			}
		}
		if action == "get" {
			return crocoCCTItem(ctx, c, slug, id)
		}
		if action == "list" {
			if err := crocoPageBounds(limit, offset); err != nil {
				return nil, err
			}
			if order != "asc" && order != "desc" {
				return nil, usageErr(fmt.Errorf("--order must be asc or desc"))
			}
			query := map[string]string{}
			for _, w := range where {
				k, v, ok := strings.Cut(w, "=")
				if !ok || k == "" || strings.HasPrefix(k, "_") {
					return nil, usageErr(fmt.Errorf("--where requires a declared field=value; reserved query keys are not allowed"))
				}
				query[k] = v
			}
			schema, err := crocoCCTSchema(ctx, c, slug, "GET")
			if err != nil {
				return nil, err
			}
			for k := range query {
				if _, ok := schema[k]; !ok {
					return nil, usageErr(fmt.Errorf("--where field %q is not exposed for this CCT", k))
				}
			}
			if search != "" {
				query["_cct_search"] = search
			}
			if searchBy != "" {
				for _, k := range strings.Split(searchBy, ",") {
					if _, ok := schema[k]; !ok || strings.HasPrefix(k, "_") {
						return nil, usageErr(fmt.Errorf("--search-by contains an unknown field"))
					}
				}
				query["_cct_search_by"] = searchBy
			}
			if orderBy != "" {
				if _, ok := schema[orderBy]; !ok {
					return nil, usageErr(fmt.Errorf("--order-by is not a declared field"))
				}
				query["_orderby"] = orderBy
				query["_order"] = order
			}
			return crocoCCTPage(ctx, c, slug, limit, offset, query)
		}
		method := map[string]string{"create": "POST", "update": "PUT", "delete": "DELETE"}[action]
		body := map[string]any{}
		if action != "delete" {
			body, err = crocoBody(data, input)
			if err != nil {
				return nil, err
			}
			if err := crocoRequireBody(body); err != nil {
				return nil, err
			}
			if _, ok := body["_ID"]; ok {
				return nil, usageErr(fmt.Errorf("_ID belongs in the positional record ID, not the request body"))
			}
		}
		schema, err := crocoCCTSchema(ctx, c, slug, method)
		if err != nil {
			return nil, err
		}
		if action != "delete" {
			if err := crocoValidateSchema(body, map[string]any{"type": "object", "properties": schema}, "data"); err != nil {
				return nil, err
			}
			for key := range body {
				if _, ok := schema[key]; !ok {
					return nil, usageErr(fmt.Errorf("field %q is not writable in this CCT", key))
				}
			}
		}
		if action != "create" {
			if _, err := crocoCCTItem(ctx, c, slug, id); err != nil {
				return nil, err
			}
		}
		path := "/jet-cct/" + slug
		if id != "" {
			path += "/" + id
		}
		var payload any = body
		if action == "delete" {
			payload = nil
		}
		v, err := crocoRequest(ctx, c, method, path, nil, payload, false)
		if err != nil {
			return nil, err
		}
		result, err := crocoMap(v)
		if err != nil {
			return nil, err
		}
		if result["success"] != true {
			return nil, apiErr(fmt.Errorf("CCT mutation did not return success"))
		}
		if action != "delete" {
			returned, err := crocoID([]string{crocoString(result["item_id"])}, 0)
			if err != nil {
				return nil, apiErr(fmt.Errorf("mutation_applied_unknown: no CCT item ID returned; do not retry automatically"))
			}
			if id != "" && id != returned {
				return nil, apiErr(fmt.Errorf("mutation_applied_unknown: CCT response ID differs; do not retry automatically"))
			}
			id = returned
		}
		if err := crocoJournal(f, ctx, c, action, "cct:"+slug, crocoKeys(body), map[string]any{"id": id, "operation": action}); err != nil {
			return nil, err
		}
		after, err := crocoCCTItem(ctx, c, slug, id)
		if action == "delete" {
			var ce *cliError
			if errors.As(err, &ce) && ce.code == 3 {
				return map[string]any{"success": true, "item_id": id, "deleted": true, "verified": true}, nil
			}
			if err != nil {
				return nil, apiErr(fmt.Errorf("mutation_applied: delete readback failed: %w", err))
			}
			return nil, apiErr(fmt.Errorf("mutation_applied: deleted item is still readable; do not retry automatically"))
		}
		if err != nil {
			return nil, apiErr(fmt.Errorf("mutation_applied: CCT readback failed: %w; do not retry mutation", err))
		}
		for key, want := range body {
			if got, exists := after[key]; !exists || !crocoValuesEqual(got, want) {
				return nil, apiErr(fmt.Errorf("mutation_applied: CCT readback differs for field %q; do not retry automatically", key))
			}
		}
		return map[string]any{"success": true, "item_id": id, "verified": true, "item": after}, nil
	})
	cmd.Example = "  crocoblock-pp-cli cct " + action + " rooms"
	if strings.Contains(use, "<id>") {
		cmd.Example += " 12"
	}
	if action == "create" || action == "update" {
		cmd.Example += " --data '{\"room_name\":\"Suite\"}' --dry-run"
	}
	cmd.Example += " --agent"
	if action == "create" || action == "update" {
		crocoDataFlags(cmd, &data, &input)
	}
	if action == "list" {
		cmd.Flags().IntVar(&limit, "limit", 50, "Maximum records in this page, from 1 to 500")
		cmd.Flags().IntVar(&offset, "offset", 0, "Number of records to skip before this page")
		cmd.Flags().StringArrayVar(&where, "where", nil, "Filter a declared CCT field using field=value; repeat for multiple fields")
		cmd.Flags().StringVar(&search, "search", "", "Search the CCT using the server's documented search parameter")
		cmd.Flags().StringVar(&searchBy, "search-by", "", "Comma-separated declared fields to restrict the text search")
		cmd.Flags().StringVar(&orderBy, "order-by", "", "Declared field used by the server to order this page")
		cmd.Flags().StringVar(&order, "order", "asc", "Sort direction for --order-by: asc or desc")
	}
	return cmd
}

func crocoPageBounds(limit, offset int) error {
	if limit < 1 || limit > 500 || offset < 0 {
		return usageErr(fmt.Errorf("--limit must be 1..500 and --offset must be nonnegative"))
	}
	return nil
}

// crocoCCTSchema reads the actual site's advertised arguments and method access.
func crocoCCTSchema(ctx context.Context, c *client.Client, slug, method string) (map[string]any, error) {
	if _, err := crocoSlug(slug); err != nil {
		return nil, err
	}
	v, err := crocoRequest(ctx, c, "GET", "/", nil, nil, true)
	if err != nil {
		return nil, err
	}
	index, err := crocoMap(v)
	if err != nil {
		return nil, err
	}
	routes, ok := index["routes"].(map[string]any)
	if !ok {
		return nil, apiErr(fmt.Errorf("WordPress index contains no route map"))
	}
	path := "/jet-cct/" + slug
	if method == "PUT" || method == "DELETE" {
		path += "/(?P<_ID>[\\d]+)"
	}
	route, ok := routes[path].(map[string]any)
	if !ok {
		return nil, notFoundErr(fmt.Errorf("CCT %s does not expose %s at this site; enable its REST endpoint", slug, method))
	}
	endpoints, ok := route["endpoints"].([]any)
	if !ok {
		return nil, apiErr(fmt.Errorf("CCT route contains no endpoint schema"))
	}
	for _, x := range endpoints {
		endpoint, err := crocoMap(x)
		if err != nil {
			return nil, err
		}
		methods, ok := endpoint["methods"].([]any)
		if !ok {
			return nil, apiErr(fmt.Errorf("CCT route methods are malformed"))
		}
		for _, m := range methods {
			if m == method {
				args, ok := endpoint["args"].(map[string]any)
				if !ok {
					if empty, isArray := endpoint["args"].([]any); isArray && len(empty) == 0 {
						return map[string]any{}, nil
					}
					return nil, apiErr(fmt.Errorf("CCT endpoint arguments unavailable"))
				}
				return args, nil
			}
		}
	}
	return nil, notFoundErr(fmt.Errorf("CCT %s does not expose method %s", slug, method))
}

func crocoCCTPage(ctx context.Context, c *client.Client, slug string, limit, offset int, query map[string]string) ([]any, error) {
	if _, err := crocoSlug(slug); err != nil {
		return nil, err
	}
	if err := crocoPageBounds(limit, offset); err != nil {
		return nil, err
	}
	params := map[string]string{}
	for k, v := range query {
		params[k] = v
	}
	params["_limit"] = strconv.Itoa(limit)
	params["_offset"] = strconv.Itoa(offset)
	v, err := crocoRequest(ctx, c, "GET", "/jet-cct/"+slug, params, nil, true)
	if err != nil {
		return nil, err
	}
	a, ok := v.([]any)
	if !ok {
		return nil, apiErr(fmt.Errorf("CCT page is missing or malformed; coverage is unknown"))
	}
	if len(a) > limit {
		return nil, apiErr(fmt.Errorf("CCT server exceeded requested page limit; coverage is unknown"))
	}
	for _, x := range a {
		m, err := crocoMap(x)
		if err != nil {
			return nil, err
		}
		if _, err := crocoID([]string{crocoString(m["_ID"])}, 0); err != nil {
			return nil, apiErr(fmt.Errorf("CCT page contains a row without a valid _ID"))
		}
	}
	return a, nil
}
func crocoCCTItem(ctx context.Context, c *client.Client, slug, id string) (map[string]any, error) {
	if _, err := crocoSlug(slug); err != nil {
		return nil, err
	}
	if _, err := crocoID([]string{id}, 0); err != nil {
		return nil, err
	}
	v, err := crocoRequest(ctx, c, "GET", "/jet-cct/"+slug+"/"+id, nil, nil, true)
	if err != nil {
		return nil, err
	}
	if v == nil || v == false {
		return nil, notFoundErr(fmt.Errorf("CCT item %s not found", id))
	}
	m, err := crocoMap(v)
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, notFoundErr(fmt.Errorf("CCT item %s not found", id))
	}
	if crocoString(m["_ID"]) != id {
		return nil, apiErr(fmt.Errorf("CCT item response ID differs"))
	}
	return m, nil
}
func crocoValuesEqual(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	switch a.(type) {
	case string:
		switch b.(type) {
		case string:
			return false
		case nil, map[string]any, []any, bool:
			return false
		default:
			return crocoString(a) == crocoString(b)
		}
	}
	return false
}
