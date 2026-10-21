package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
)

// pp:data-source live

func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		root.AddCommand(&cobra.Command{Use: "engine", Short: "Operate JetEngine structures and configuration through native MCP"})
		for _, name := range []string{"configuration", "website", "macros", "modules", "create-cct", "create-cpt", "create-taxonomy", "create-meta-box", "create-query", "create-listing", "create-glossary"} {
			crocoAttach(root, "engine", newCrocoEngineCommand(f, name))
		}
	})
}

func newCrocoEngineCommand(f *rootFlags, name string) *cobra.Command {
	var data, input, operation string
	var modules, exclude []string
	read := name == "configuration" || name == "website" || name == "macros"
	shapes := map[string]*cobra.Command{
		"configuration":   {Use: "configuration", Short: "Read JetEngine structures from native MCP configuration"},
		"website":         {Use: "website", Short: "Read installed post types, taxonomies and plugins"},
		"macros":          {Use: "macros", Short: "Read available JetEngine macros and their parameters"},
		"modules":         {Use: "modules", Short: "List, activate or deactivate JetEngine modules"},
		"create-cct":      {Use: "create-cct", Short: "Create a JetEngine custom content type"},
		"create-cpt":      {Use: "create-cpt", Short: "Create a JetEngine custom post type"},
		"create-taxonomy": {Use: "create-taxonomy", Short: "Create a JetEngine taxonomy definition"},
		"create-meta-box": {Use: "create-meta-box", Short: "Create a JetEngine meta box definition"},
		"create-query":    {Use: "create-query", Short: "Create a JetEngine query builder definition"},
		"create-listing":  {Use: "create-listing", Short: "Create a JetEngine listing for an existing query"},
		"create-glossary": {Use: "create-glossary", Short: "Create a JetEngine glossary of manual options"},
	}
	cmd := crocoDeclaredCommand(f, shapes[name], read, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
		if len(args) != 0 {
			return nil, usageErr(fmt.Errorf("engine %s accepts flags, not positional arguments", name))
		}
		body, err := crocoBody(data, input)
		if err != nil {
			return nil, err
		}
		tool := "tool-add-" + strings.TrimPrefix(name, "create-")
		switch name {
		case "configuration", "website":
			tool = "resource-get-configuration"
			parts := []string{"post_types", "taxonomies", "meta_boxes", "options_pages", "queries", "relations", "custom_content_types", "rest_api_endpoints", "glossaries"}
			if name == "website" {
				tool = "resource-get-website-config"
				parts = []string{"post_types", "taxonomies", "active_plugins"}
			}
			if len(body) == 0 {
				p := map[string]any{}
				for _, part := range parts {
					p[part] = true
				}
				body["parts"] = p
			}
		case "macros":
			tool = "resource-get-macros"
			if cmd.Flags().Changed("exclude") {
				if _, exists := body["exclude"]; exists {
					return nil, usageErr(fmt.Errorf("use --exclude or body.exclude, not both"))
				}
				body["exclude"] = crocoStringsAny(exclude)
			}
		case "modules":
			tool = "tool-manage-modules"
			if _, exists := body["operation"]; !exists {
				body["operation"] = operation
			} else if cmd.Flags().Changed("operation") {
				return nil, usageErr(fmt.Errorf("use --operation or body.operation, not both"))
			}
			if cmd.Flags().Changed("modules") {
				if _, exists := body["modules"]; exists {
					return nil, usageErr(fmt.Errorf("use --modules or body.modules, not both"))
				}
				body["modules"] = crocoStringsAny(modules)
			}
		default:
			if err := crocoRequireBody(body); err != nil {
				return nil, err
			}
		}
		if err := crocoValidateEngineInput(tool, body); err != nil {
			return nil, err
		}
		if name == "modules" {
			return crocoEngineModules(ctx, c, f, body)
		}
		response, err := crocoMCP(ctx, c, tool, body)
		if err != nil {
			return nil, err
		}
		if response["success"] != true {
			return nil, apiErr(fmt.Errorf("native MCP %s returned no successful result", tool))
		}
		if read {
			return response, nil
		}
		id, err := crocoEngineCreatedID(name, response["item_id"])
		if err != nil {
			return nil, apiErr(fmt.Errorf("mutation_applied_unknown: native MCP returned no created item ID; do not retry automatically"))
		}
		if err := crocoJournal(f, ctx, c, "create", "engine:"+strings.TrimPrefix(name, "create-"), crocoKeys(body), map[string]any{"id": id, "operation": "create"}); err != nil {
			return nil, err
		}
		if err := crocoVerifyEngineCreate(ctx, c, name, id); err != nil {
			return nil, apiErr(fmt.Errorf("mutation_applied: item %s created but readback failed: %w; do not repeat creation", id, err))
		}
		response["verified"] = true
		return response, nil
	})
	crocoDataFlags(cmd, &data, &input)
	cmd.Example = "  crocoblock-pp-cli engine " + name + " --agent"
	if strings.HasPrefix(name, "create-") {
		cmd.Example = "  crocoblock-pp-cli engine " + name + " --input structure.json --dry-run --agent"
	}
	if name == "modules" {
		cmd.Flags().StringVar(&operation, "operation", "list", "Operation to perform: list, activate or deactivate")
		cmd.Flags().StringSliceVar(&modules, "modules", nil, "Comma-separated JetEngine module slugs to activate or deactivate")
		cmd.Example = "  crocoblock-pp-cli engine modules --operation list --agent\n  crocoblock-pp-cli engine modules --operation activate --modules custom-content-types --dry-run"
	}
	if name == "macros" {
		cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "Comma-separated macro names to exclude from the result")
	}
	return cmd
}

// Meta boxes use option keys (meta-N), while the other native create tools
// return positive database IDs. Validate before using either shape in routes.
func crocoEngineCreatedID(name string, value any) (string, error) {
	id := crocoString(value)
	if name == "create-meta-box" {
		if !strings.HasPrefix(id, "meta-") {
			return "", fmt.Errorf("meta box item_id must use meta-N")
		}
		numeric, err := crocoID([]string{strings.TrimPrefix(id, "meta-")}, 0)
		if err != nil || id != "meta-"+numeric {
			return "", fmt.Errorf("meta box item_id must use meta-N with a positive numeric suffix")
		}
		return id, nil
	}
	return crocoID([]string{id}, 0)
}

func crocoStringsAny(values []string) []any {
	result := make([]any, 0, len(values))
	for _, v := range values {
		result = append(result, v)
	}
	return result
}
func crocoKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func crocoValidateSchema(value any, schema map[string]any, path string) error {
	if choices, ok := schema["enum"].([]any); ok {
		matched := false
		for _, choice := range choices {
			if reflect.DeepEqual(value, choice) {
				matched = true
			}
		}
		if !matched {
			return usageErr(fmt.Errorf("%s is not one of the documented values", path))
		}
	}
	if types, ok := schema["type"].([]any); ok {
		for _, typ := range types {
			copy := map[string]any{}
			for k, v := range schema {
				copy[k] = v
			}
			copy["type"] = typ
			if crocoValidateSchema(value, copy, path) == nil {
				return nil
			}
		}
		return usageErr(fmt.Errorf("%s has an unsupported value type", path))
	}
	switch schema["type"] {
	case "object":
		m, ok := value.(map[string]any)
		if !ok || m == nil {
			return usageErr(fmt.Errorf("%s must be a JSON object", path))
		}
		props, _ := schema["properties"].(map[string]any)
		if required, ok := schema["required"].([]any); ok {
			for _, key := range required {
				if _, found := m[crocoString(key)]; !found {
					return usageErr(fmt.Errorf("%s.%s is required", path, key))
				}
			}
		}
		for key, val := range m {
			child, found := props[key]
			if !found {
				if len(props) > 0 && schema["additionalProperties"] != true {
					return usageErr(fmt.Errorf("unsupported field %s.%s", path, key))
				}
				continue
			}
			nested, ok := child.(map[string]any)
			if !ok {
				return apiErr(fmt.Errorf("invalid documented schema at %s", path))
			}
			if err := crocoValidateSchema(val, nested, path+"."+key); err != nil {
				return err
			}
		}
	case "array":
		a, ok := value.([]any)
		if !ok {
			return usageErr(fmt.Errorf("%s must be a JSON array", path))
		}
		if nested, ok := schema["items"].(map[string]any); ok {
			for i, v := range a {
				if err := crocoValidateSchema(v, nested, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return usageErr(fmt.Errorf("%s must be a string", path))
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return usageErr(fmt.Errorf("%s must be boolean", path))
		}
	case "integer", "number":
		n, ok := value.(json.Number)
		if !ok {
			return usageErr(fmt.Errorf("%s must be a JSON number", path))
		}
		if schema["type"] == "integer" {
			if _, e := n.Int64(); e != nil {
				return usageErr(fmt.Errorf("%s must be an integer", path))
			}
		} else if _, e := n.Float64(); e != nil {
			return usageErr(fmt.Errorf("%s must be numeric", path))
		}
	}
	return nil
}

func crocoValidateEngineInput(tool string, body map[string]any) error {
	var schemas map[string]map[string]any
	if err := json.Unmarshal([]byte(crocoEngineInputSchemas), &schemas); err != nil {
		return apiErr(err)
	}
	schema, ok := schemas[tool]
	if !ok {
		return usageErr(fmt.Errorf("unsupported native MCP tool"))
	}
	if err := crocoValidateSchema(body, schema, "input"); err != nil {
		return err
	}
	requiredText := func(m map[string]any, key string) error {
		if strings.TrimSpace(crocoString(m[key])) == "" {
			return usageErr(fmt.Errorf("%s is required and must not be empty", key))
		}
		return nil
	}
	switch tool {
	case "tool-add-cct", "tool-add-glossary", "tool-add-query":
		if err := requiredText(body, "name"); err != nil {
			return err
		}
	case "tool-add-cpt", "tool-add-taxonomy", "tool-add-meta-box":
		settings, _ := body["general_settings"].(map[string]any)
		if err := requiredText(settings, "name"); err != nil {
			return err
		}
		if tool != "tool-add-meta-box" {
			if _, err := crocoSlug(crocoString(settings["slug"])); err != nil {
				return err
			}
		}
	}
	if tool == "tool-add-cct" {
		if _, err := crocoSlug(crocoString(body["slug"])); err != nil {
			return err
		}
	}
	if tool == "tool-add-listing" {
		if _, err := crocoID([]string{crocoString(body["query_id"])}, 0); err != nil {
			return err
		}
	}
	if tool == "tool-add-query" {
		if err := requiredText(body, "query_type"); err != nil {
			return err
		}
		q, ok := body["query_args"].(map[string]any)
		if !ok || len(q) == 0 {
			return usageErr(fmt.Errorf("query_args must be a nonempty JSON object"))
		}
		if body["query_type"] == "sql" {
			if err := requiredText(q, "sql"); err != nil {
				return err
			}
		}
	}
	if tool == "tool-manage-modules" && body["operation"] != "list" {
		a, _ := body["modules"].([]any)
		if len(a) == 0 {
			return usageErr(fmt.Errorf("activate/deactivate requires --modules or body.modules"))
		}
		for _, v := range a {
			if _, err := crocoSlug(crocoString(v)); err != nil {
				return err
			}
		}
	}
	return nil
}

func crocoEngineModules(ctx context.Context, c *client.Client, f *rootFlags, body map[string]any) (any, error) {
	before, err := crocoMCP(ctx, c, "tool-manage-modules", map[string]any{"operation": "list"})
	if err != nil {
		return nil, err
	}
	if before["success"] != true {
		return nil, apiErr(fmt.Errorf("module list was not successful"))
	}
	if body["operation"] == "list" {
		return before, nil
	}
	available, err := crocoModuleSet(before["all_modules"])
	if err != nil {
		return nil, err
	}
	active, err := crocoModuleSet(before["active_modules"])
	if err != nil {
		return nil, err
	}
	requested := body["modules"].([]any)
	change := []any{}
	for _, v := range requested {
		slug := crocoString(v)
		if !available[slug] {
			return nil, usageErr(fmt.Errorf("unknown JetEngine module %q", slug))
		}
		if active[slug] != (body["operation"] == "activate") {
			change = append(change, slug)
		}
	}
	if len(change) == 0 {
		before["verified"] = true
		before["mutation_applied"] = false
		return before, nil
	}
	body["modules"] = change
	result, err := crocoMCP(ctx, c, "tool-manage-modules", body)
	if err != nil {
		return nil, err
	}
	if result["success"] != true {
		return nil, apiErr(fmt.Errorf("module change was not successful"))
	}
	if err := crocoJournal(f, ctx, c, "update", "engine:modules", []string{"modules"}, map[string]any{"operation": body["operation"], "count": len(change)}); err != nil {
		return nil, err
	}
	after, err := crocoMCP(ctx, c, "tool-manage-modules", map[string]any{"operation": "list"})
	if err != nil {
		return nil, apiErr(fmt.Errorf("mutation_applied: module readback failed: %w", err))
	}
	observed, err := crocoModuleSet(after["active_modules"])
	if err != nil {
		return nil, err
	}
	for _, v := range requested {
		if observed[crocoString(v)] != (body["operation"] == "activate") {
			return nil, apiErr(fmt.Errorf("mutation_applied: module readback differs; do not retry automatically"))
		}
	}
	result["verified"] = true
	return result, nil
}
func crocoModuleSet(v any) (map[string]bool, error) {
	a, ok := v.([]any)
	if !ok {
		return nil, apiErr(fmt.Errorf("native MCP module collection missing or malformed"))
	}
	set := map[string]bool{}
	for _, x := range a {
		m, e := crocoMap(x)
		if e != nil {
			return nil, e
		}
		slug := crocoString(m["slug"])
		if _, e := crocoSlug(slug); e != nil {
			return nil, apiErr(fmt.Errorf("invalid module slug in native MCP response"))
		}
		set[slug] = true
	}
	return set, nil
}

func crocoVerifyEngineCreate(ctx context.Context, c *client.Client, name, id string) error {
	if name == "create-listing" {
		v, e := crocoRequest(ctx, c, "GET", "/wp/v2/jet-engine/"+id, map[string]string{"context": "edit"}, nil, true)
		if e != nil {
			return e
		}
		m, e := crocoMap(v)
		if e != nil {
			return e
		}
		if crocoString(m["id"]) != id {
			return fmt.Errorf("listing ID differs")
		}
		return nil
	}
	part := map[string]string{"create-cct": "custom_content_types", "create-cpt": "post_types", "create-taxonomy": "taxonomies", "create-meta-box": "meta_boxes", "create-query": "queries", "create-glossary": "glossaries"}[name]
	v, e := crocoMCP(ctx, c, "resource-get-configuration", map[string]any{"parts": map[string]any{part: true}})
	if e != nil {
		return e
	}
	if v["success"] != true {
		return fmt.Errorf("configuration readback unsuccessful")
	}
	a, ok := v[part].([]any)
	if !ok {
		return fmt.Errorf("created structure missing from configuration")
	}
	for _, entry := range a {
		m, e := crocoMap(entry)
		if e != nil {
			return e
		}
		if crocoString(m["id"]) == id {
			return nil
		}
	}
	return fmt.Errorf("created item ID absent from configuration")
}

// Input contracts observed from official JetEngine 3.9.0 tools/list; this is validation metadata, not an API response.
const crocoEngineInputSchemas = `{"tool-add-cct":{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"},"fields":{"type":"array","items":{"type":"object","properties":{"field_name":{"type":"string"},"is_key_field":{"type":"string","enum":["yes","no"]},"field_type":{"type":"string","enum":["text","number","date","datetime","media","checkbox","select","radio","textarea"]},"save_as_timestamp":{"type":"boolean"},"value_format":{"type":"string","enum":["id","url","both"]},"options":{"type":"array","items":{"type":"string"}}}}}}},"resource-get-configuration":{"type":"object","properties":{"parts":{"type":"object","properties":{"post_types":{"type":"boolean"},"taxonomies":{"type":"boolean"},"meta_boxes":{"type":"boolean"},"options_pages":{"type":"boolean"},"queries":{"type":"boolean"},"relations":{"type":"boolean"},"custom_content_types":{"type":"boolean"},"rest_api_endpoints":{"type":"boolean"},"glossaries":{"type":"boolean"}}}}},"resource-get-website-config":{"type":"object","properties":{"parts":{"type":"object","properties":{"post_types":{"type":"boolean"},"taxonomies":{"type":"boolean"},"active_plugins":{"type":"boolean"}}}}},"resource-get-macros":{"type":"object","properties":{"exclude":{"type":"array","items":{"type":"string"}}}},"tool-add-glossary":{"type":"object","properties":{"name":{"type":"string"},"source":{"type":"string","enum":["manual"]},"fields":{"type":"array","items":{"type":"object","required":["value","label"],"properties":{"value":{"type":"string"},"label":{"type":"string"},"is_checked":{"type":"boolean"}}}}}},"tool-manage-modules":{"type":"object","properties":{"operation":{"type":"string","enum":["list","activate","deactivate"],"default":"list"},"modules":{"type":"array","items":{"type":"string"}}}},"tool-add-listing":{"type":"object","properties":{"query_id":{"type":"integer"},"view_type":{"type":"string","enum":["auto","elementor","bricks","twig","blocks"]}}},"tool-add-cpt":{"type":"object","properties":{"general_settings":{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"},"custom_storage":{"type":"boolean"},"show_edit_link":{"type":"boolean"},"hide_field_names":{"type":"boolean"},"delete_metadata":{"type":"boolean"}}},"labels":{"type":"object"},"advanced_settings":{"type":"object"},"meta_fields":{"type":"array","items":{"type":"object","properties":{"object_type":{"type":"string"},"id":{"type":"integer"},"name":{"type":"string"},"field_name":{"type":"string"},"label":{"type":"string"},"type":{"type":"string","enum":["text","number","date","datetime","datetime-local","time","media","checkbox","select","radio","textarea","repeater"]},"field_type":{"type":"string"},"placeholder":{"type":"string"},"default":{"type":"string"},"is_required":{"type":"boolean"},"value_format":{"type":"string"},"options":{"type":"array","items":{"type":"string"}},"choices":{"type":"array","items":{"type":"string"}},"fields":{"type":"array","items":{"type":"object"}},"add_to_admin_column":{"type":"boolean"},"admin_column_position":{"type":"integer"},"column_prefix":{"type":"string"},"column_suffix":{"type":"string"},"is_sortable":{"type":"boolean"},"sort_by_field":{"type":"string"}}}},"admin_columns":{"type":"array","items":{"type":"object","properties":{"type":{"type":"string"},"meta_field":{"type":"string"},"taxonomy":{"type":"string"},"callback":{"type":"string"},"position":{"type":"integer"},"prefix":{"type":"string"},"suffix":{"type":"string"},"is_sortable":{"type":"boolean"},"sort_by_field":{"type":"string"},"is_num":{"type":"boolean"}}}},"admin_filters":{"type":"array","items":{"type":"object"}}}},"tool-add-meta-box":{"type":"object","properties":{"general_settings":{"type":"object","properties":{"name":{"type":"string"},"object_type":{"type":"string","enum":["post","taxonomy","user"]},"show_edit_link":{"type":"boolean"},"hide_field_names":{"type":"boolean"},"delete_metadata":{"type":"boolean"},"allowed_post_type":{"type":"array","items":{"type":"string"}},"allowed_tax":{"type":"array","items":{"type":"string"}},"allowed_user_screens":{"type":"string","enum":["edit","edit-profile"]},"active_conditions":{"type":"array","items":{"type":"string"}},"allowed_posts":{"type":"array","items":{"type":"integer"}},"excluded_posts":{"type":"array","items":{"type":"integer"}},"include_roles":{"type":"array","items":{"type":"string"}},"exclude_roles":{"type":"array","items":{"type":"string"}},"post_has_terms__tax":{"type":"string"},"post_has_terms__terms":{"type":"array","items":{"type":"string"}}},"additionalProperties":true},"meta_fields":{"type":"array","items":{"type":"object","properties":{"object_type":{"type":"string"},"id":{"type":"integer"},"name":{"type":"string"},"field_name":{"type":"string"},"label":{"type":"string"},"type":{"type":"string"},"field_type":{"type":"string"},"placeholder":{"type":"string"},"default":{"type":"string"},"is_required":{"type":"boolean"},"quick_editable":{"type":"boolean"},"value_format":{"type":"string"},"taxonomy":{"type":"string"},"appearance":{"type":"string"},"multiple":{"type":"boolean"},"allow_null":{"type":"boolean"},"return_value":{"type":"string"},"load_terms":{"type":"boolean"},"save_terms":{"type":"boolean"},"options":{"type":"array","items":{"type":"object"}},"choices":{"type":"array","items":{"type":"string"}},"fields":{"type":"array","items":{"type":"object"}},"conditions":{"type":"array","items":{"type":"object"}}},"additionalProperties":true}}}},"tool-add-taxonomy":{"type":"object","properties":{"general_settings":{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"},"object_type":{"type":"array","items":{"type":"string"}},"show_edit_link":{"type":"boolean"},"hide_field_names":{"type":"boolean"},"delete_metadata":{"type":"boolean"}},"additionalProperties":true},"labels":{"type":"object","properties":{"singular_name":{"type":"string"},"menu_name":{"type":"string"},"all_items":{"type":"string"},"edit_item":{"type":"string"},"view_item":{"type":"string"},"update_item":{"type":"string"},"add_new_item":{"type":"string"},"new_item_name":{"type":"string"},"parent_item":{"type":"string"},"parent_item_colon":{"type":"string"},"search_items":{"type":"string"},"popular_items":{"type":"string"},"separate_items_with_commas":{"type":"string"},"add_or_remove_items":{"type":"string"},"choose_from_most_used":{"type":"string"},"not_found":{"type":"string"},"back_to_items":{"type":"string"}},"additionalProperties":true},"advanced_settings":{"type":"object","properties":{"public":{"type":"boolean"},"publicly_queryable":{"type":"boolean"},"show_ui":{"type":"boolean"},"show_in_menu":{"type":"boolean"},"show_in_nav_menus":{"type":"boolean"},"show_in_rest":{"type":"boolean"},"query_var":{"type":["boolean","string"]},"rewrite":{"type":["boolean","object"]},"with_front":{"type":"boolean"},"capability_type":{"type":"string"},"hierarchical":{"type":"boolean"},"rewrite_slug":{"type":"string"},"rewrite_hierarchical":{"type":"boolean"}},"additionalProperties":true},"meta_fields":{"type":"array","items":{"type":"object","properties":{"object_type":{"type":"string"},"id":{"type":"integer"},"name":{"type":"string"},"field_name":{"type":"string"},"label":{"type":"string"},"type":{"type":"string"},"field_type":{"type":"string"},"placeholder":{"type":"string"},"default":{"type":"string"},"is_required":{"type":"boolean"},"value_format":{"type":"string"},"options":{"type":"array","items":{"type":["string","object"]}},"choices":{"type":"array","items":{"type":"string"}},"fields":{"type":"array","items":{"type":"object"}}},"additionalProperties":true}}}},"tool-add-query":{"type":"object","properties":{"name":{"type":"string"},"query_type":{"type":"string","enum":["sql","posts","terms","users","comments","repeater","current-wp-query","merged-query","file-query","jet-appointment-query","jet-booking-query","relations-query","jet-form-builder-query"]},"query_args":{"type":"object"}}}}`
