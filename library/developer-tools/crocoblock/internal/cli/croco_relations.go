package cli

import (
	"context"
	"fmt"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		for _, action := range []string{"definitions", "list", "parents", "children", "update"} {
			crocoAttach(root, "relations", newCrocoRelationsCommand(f, action))
		}
	})
}

func newCrocoRelationsCommand(f *rootFlags, action string) *cobra.Command {
	var data, input, updateContext, mode string
	limit, offset := 50, 0
	use := action
	if action != "definitions" {
		use += " <relation-id>"
	}
	if action == "parents" || action == "children" {
		use += " <object-id>"
	}
	cmd := crocoCommand(f, use, "Read typed relation definitions and explicitly manage relation edges", action != "update", func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
		count := 1
		if action == "definitions" {
			count = 0
		}
		if action == "parents" || action == "children" {
			count = 2
		}
		if len(args) != count {
			return nil, usageErr(fmt.Errorf("relations %s requires %d positional arguments", action, count))
		}
		id := ""
		var err error
		if count > 0 {
			id, err = crocoID(args, 0)
			if err != nil {
				return nil, err
			}
		}
		if action != "update" {
			if err := crocoPageBounds(limit, offset); err != nil {
				return nil, err
			}
			var rows []any
			switch action {
			case "definitions":
				rows, err = crocoRelationDefinitions(ctx, c)
			case "list":
				rows, err = crocoRelationEdges(ctx, c, id)
			default:
				objectID, e := crocoID(args, 1)
				if e != nil {
					return nil, e
				}
				rows, err = crocoRelationTraversal(ctx, c, id, action, objectID)
			}
			if err != nil {
				return nil, err
			}
			if offset >= len(rows) {
				return []any{}, nil
			}
			end := offset + limit
			if end > len(rows) {
				end = len(rows)
			}
			return rows[offset:end], nil
		}
		body, err := crocoBody(data, input)
		if err != nil {
			return nil, err
		}
		if err := crocoRequireBody(body); err != nil {
			return nil, err
		}
		if err := crocoOnly(body, "parent_id", "child_id", "context", "store_items_type", "meta"); err != nil {
			return nil, err
		}
		if cmd.Flags().Changed("context") {
			if _, ok := body["context"]; ok {
				return nil, usageErr(fmt.Errorf("use --context or data.context, not both"))
			}
			body["context"] = updateContext
		}
		if cmd.Flags().Changed("mode") {
			if _, ok := body["store_items_type"]; ok {
				return nil, usageErr(fmt.Errorf("use --mode or data.store_items_type, not both"))
			}
			body["store_items_type"] = mode
		}
		parents, children, err := crocoValidateRelationUpdate(body)
		if err != nil {
			return nil, err
		}
		definitions, err := crocoRelationDefinitions(ctx, c)
		if err != nil {
			return nil, err
		}
		var definition map[string]any
		for _, v := range definitions {
			m, _ := v.(map[string]any)
			if crocoString(m["id"]) == id {
				definition = m
				break
			}
		}
		if definition == nil {
			return nil, notFoundErr(fmt.Errorf("relation %s is not an accessible definition", id))
		}
		if meta, ok := body["meta"].(map[string]any); ok && len(meta) > 0 {
			allowed := map[string]bool{}
			if fields, ok := definition["meta_fields"].([]any); ok {
				for _, v := range fields {
					m, _ := v.(map[string]any)
					if name := crocoString(m["name"]); name != "" {
						allowed[name] = true
					}
				}
			}
			for key := range meta {
				if !allowed[key] {
					return nil, usageErr(fmt.Errorf("relation metadata field %q is not declared", key))
				}
			}
		}
		v, err := crocoRequest(ctx, c, "PUT", "/jet-rel/"+id, nil, body, false)
		if err != nil {
			return nil, err
		}
		result, err := crocoMap(v)
		if err != nil {
			return nil, err
		}
		if result["success"] != true {
			return nil, apiErr(fmt.Errorf("relation update returned no successful result"))
		}
		if err := crocoJournal(f, ctx, c, "update", "relations:"+id, crocoKeys(body), map[string]any{"id": id, "operation": "update", "count": len(parents) * len(children)}); err != nil {
			return nil, err
		}
		if err := crocoVerifyRelationUpdate(ctx, c, id, body, parents, children); err != nil {
			return nil, apiErr(fmt.Errorf("mutation_applied: relation readback failed: %w; do not retry automatically", err))
		}
		return map[string]any{"success": true, "relation_id": id, "context": body["context"], "mode": body["store_items_type"], "verified": true}, nil
	})
	cmd.Example = "  crocoblock-pp-cli relations " + action
	if action != "definitions" {
		cmd.Example += " 2"
	}
	if action == "parents" || action == "children" {
		cmd.Example += " 13"
	}
	if action == "update" {
		crocoDataFlags(cmd, &data, &input)
		cmd.Flags().StringVar(&updateContext, "context", "", "Required update direction: child replaces children of parents; parent replaces parents of children")
		cmd.Flags().StringVar(&mode, "mode", "", "Required edge operation: append, replace or disconnect")
		cmd.Example += " --context child --mode append --data '{\"parent_id\":13,\"child_id\":[14]}' --dry-run"
	} else {
		cmd.Flags().IntVar(&limit, "limit", 50, "Maximum rows to display from 1 to 500; upstream relation reads are not paginated")
		cmd.Flags().IntVar(&offset, "offset", 0, "Number of relation rows to skip in the returned collection")
	}
	cmd.Example += " --agent"
	return cmd
}

// Definitions may be keyed objects: PHP array_filter preserves original keys.
func crocoRelationDefinitions(ctx context.Context, c *client.Client) ([]any, error) {
	v, err := crocoRequest(ctx, c, "GET", "/jet-engine/v2/get-relations", nil, nil, true)
	if err != nil {
		return nil, err
	}
	m, err := crocoMap(v)
	if err != nil {
		return nil, err
	}
	if m["success"] != true {
		return nil, apiErr(fmt.Errorf("relation definitions returned no success"))
	}
	data, present := m["data"]
	if !present {
		return nil, apiErr(fmt.Errorf("relation definitions omitted data; coverage unknown"))
	}
	a, err := crocoRelationCollection(data)
	if err != nil {
		return nil, err
	}
	for _, v := range a {
		m, e := crocoMap(v)
		if e != nil {
			return nil, e
		}
		if _, e := crocoID([]string{crocoString(m["id"])}, 0); e != nil {
			return nil, apiErr(fmt.Errorf("relation definition lacks a numeric ID"))
		}
	}
	return a, nil
}
func crocoRelationCollection(v any) ([]any, error) {
	if a, ok := v.([]any); ok {
		return a, nil
	}
	if m, ok := v.(map[string]any); ok {
		keys := crocoKeys(m)
		a := make([]any, 0, len(keys))
		for _, k := range keys {
			a = append(a, m[k])
		}
		return a, nil
	}
	return nil, apiErr(fmt.Errorf("relation collection missing or malformed; coverage unknown"))
}
func crocoRelationEdges(ctx context.Context, c *client.Client, id string) ([]any, error) {
	if _, err := crocoID([]string{id}, 0); err != nil {
		return nil, err
	}
	v, err := crocoRequest(ctx, c, "GET", "/jet-rel/"+id, nil, nil, true)
	if err != nil {
		return nil, err
	}
	edges := []any{}
	if a, ok := v.([]any); ok && len(a) == 0 {
		return edges, nil
	}
	parents, ok := v.(map[string]any)
	if !ok {
		return nil, apiErr(fmt.Errorf("relation edge map missing or malformed; coverage unknown"))
	}
	for _, parent := range crocoKeys(parents) {
		if _, err := crocoID([]string{parent}, 0); err != nil {
			return nil, apiErr(fmt.Errorf("relation edge contains invalid parent ID"))
		}
		children, ok := parents[parent].([]any)
		if !ok {
			return nil, apiErr(fmt.Errorf("relation child collection malformed"))
		}
		for _, entry := range children {
			child, err := crocoMap(entry)
			if err != nil {
				return nil, err
			}
			if _, err := crocoID([]string{crocoString(child["child_object_id"])}, 0); err != nil {
				return nil, apiErr(fmt.Errorf("relation edge contains invalid child ID"))
			}
			edge := map[string]any{"parent_object_id": parent, "child_object_id": child["child_object_id"]}
			if meta, ok := child["meta"]; ok {
				edge["meta"] = meta
			}
			edges = append(edges, edge)
		}
	}
	return edges, nil
}
func crocoRelationTraversal(ctx context.Context, c *client.Client, id, direction, objectID string) ([]any, error) {
	if _, err := crocoID([]string{id}, 0); err != nil {
		return nil, err
	}
	if _, err := crocoID([]string{objectID}, 0); err != nil {
		return nil, err
	}
	if direction != "parents" && direction != "children" {
		return nil, usageErr(fmt.Errorf("relation traversal must be parents or children"))
	}
	v, err := crocoRequest(ctx, c, "GET", "/jet-rel/"+id+"/"+direction+"/"+objectID, nil, nil, true)
	if err != nil {
		return nil, err
	}
	a, ok := v.([]any)
	if !ok {
		return nil, apiErr(fmt.Errorf("relation traversal collection missing or malformed"))
	}
	key := "child_object_id"
	if direction == "parents" {
		key = "parent_object_id"
	}
	for _, v := range a {
		m, err := crocoMap(v)
		if err != nil {
			return nil, err
		}
		if _, err := crocoID([]string{crocoString(m[key])}, 0); err != nil {
			return nil, apiErr(fmt.Errorf("relation traversal contains invalid object ID"))
		}
	}
	return a, nil
}
func crocoRelationIDs(v any) ([]string, error) {
	values, ok := v.([]any)
	if !ok {
		values = []any{v}
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, x := range values {
		id, err := crocoID([]string{crocoString(x)}, 0)
		if err != nil {
			return nil, usageErr(fmt.Errorf("parent_id/child_id must contain positive integer IDs"))
		}
		if seen[id] {
			return nil, usageErr(fmt.Errorf("duplicate parent_id/child_id"))
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) > 500 {
		return nil, usageErr(fmt.Errorf("at most 500 object IDs may be supplied"))
	}
	return ids, nil
}
func crocoValidateRelationUpdate(body map[string]any) ([]string, []string, error) {
	context := crocoString(body["context"])
	mode := crocoString(body["store_items_type"])
	if context != "child" && context != "parent" {
		return nil, nil, usageErr(fmt.Errorf("explicit --context child|parent or data.context is required"))
	}
	if mode != "append" && mode != "replace" && mode != "disconnect" {
		return nil, nil, usageErr(fmt.Errorf("explicit --mode append|replace|disconnect or data.store_items_type is required"))
	}
	parents, err := crocoRelationIDs(body["parent_id"])
	if err != nil {
		return nil, nil, err
	}
	children, err := crocoRelationIDs(body["child_id"])
	if err != nil {
		return nil, nil, err
	}
	if len(parents) == 0 && (mode != "replace" || context != "parent") || len(children) == 0 && (mode != "replace" || context != "child") {
		return nil, nil, usageErr(fmt.Errorf("both sides require IDs, except an explicit replace may clear the selected side"))
	}
	if len(parents)*len(children) > 1000 {
		return nil, nil, usageErr(fmt.Errorf("one relation update may address at most 1000 edges"))
	}
	if meta, exists := body["meta"]; exists {
		m, ok := meta.(map[string]any)
		if !ok {
			return nil, nil, usageErr(fmt.Errorf("meta must be a JSON object"))
		}
		if len(m) > 0 && (len(parents) != 1 || len(children) != 1 || mode == "disconnect") {
			return nil, nil, usageErr(fmt.Errorf("metadata may only be written for one connected parent/child pair"))
		}
	}
	return parents, children, nil
}
func crocoVerifyRelationUpdate(ctx context.Context, c *client.Client, id string, body map[string]any, parents, children []string) error {
	direction, anchors, targets, key := "children", parents, children, "child_object_id"
	if body["context"] == "parent" {
		direction, anchors, targets, key = "parents", children, parents, "parent_object_id"
	}
	for _, anchor := range anchors {
		rows, err := crocoRelationTraversal(ctx, c, id, direction, anchor)
		if err != nil {
			return err
		}
		observed := map[string]map[string]any{}
		for _, v := range rows {
			m := v.(map[string]any)
			observed[crocoString(m[key])] = m
		}
		if body["store_items_type"] == "replace" && len(observed) != len(targets) {
			return fmt.Errorf("replacement membership differs")
		}
		for _, target := range targets {
			record, exists := observed[target]
			if exists != (body["store_items_type"] != "disconnect") {
				return fmt.Errorf("edge membership differs")
			}
			if meta, ok := body["meta"].(map[string]any); ok && len(meta) > 0 {
				got, _ := record["meta"].(map[string]any)
				for k, want := range meta {
					if !crocoValuesEqual(got[k], want) {
						return fmt.Errorf("edge metadata differs for %s", k)
					}
				}
			}
		}
	}
	return nil
}
