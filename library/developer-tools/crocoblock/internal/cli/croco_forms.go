package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
)

const crocoFormsRoute = "/wp/v2/jet-form-builder"

func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		var page, limit int
		var status string
		list := crocoDeclaredCommand(f, &cobra.Command{Use: "list", Short: "List a bounded page of JetFormBuilder forms"}, true, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
			if len(args) != 0 {
				return nil, usageErr(fmt.Errorf("forms list accepts no positional arguments"))
			}
			if page < 1 || limit < 1 || limit > 100 {
				return nil, usageErr(fmt.Errorf("--page must be positive and --limit between 1 and 100"))
			}
			if !crocoFormStatus(status, true) {
				return nil, usageErr(fmt.Errorf("invalid --status"))
			}
			v, e := crocoRequest(ctx, c, "GET", crocoFormsRoute, map[string]string{"context": "edit", "page": strconv.Itoa(page), "per_page": strconv.Itoa(limit), "status": status, "orderby": "id", "order": "asc"}, nil, true)
			if e != nil {
				return nil, e
			}
			if v == nil {
				return nil, apiErr(fmt.Errorf("form collection returned no content; coverage unknown"))
			}
			items, e := crocoArray(v)
			if e != nil {
				return nil, e
			}
			return map[string]any{"items": items, "page": page, "limit": limit, "complete": page == 1 && len(items) < limit, "scope": map[string]any{"status": status}}, nil
		})
		list.Flags().IntVar(&page, "page", 1, "One-based page")
		list.Flags().IntVar(&limit, "limit", 50, "Items per page (1–100)")
		list.Flags().StringVar(&status, "status", "any", "WordPress status, including any or trash")
		list.Example = "  crocoblock-pp-cli forms list --status draft --limit 50 --agent"
		crocoAttach(root, "forms", list)
		get := crocoDeclaredCommand(f, &cobra.Command{Use: "get <id>", Short: "Read a form with raw Gutenberg blocks and redacted actions"}, true, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
			id, e := crocoOneID(args)
			if e != nil {
				return nil, e
			}
			return crocoFormGet(ctx, c, id)
		})
		get.Example = "  crocoblock-pp-cli forms get 42 --agent"
		crocoAttach(root, "forms", get)
		fields := crocoDeclaredCommand(f, &cobra.Command{Use: "fields <id>", Short: "Inspect declared form fields, including drafts without publishing"}, true, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
			id, e := crocoOneID(args)
			if e != nil {
				return nil, e
			}
			items, source, e := crocoFormFields(ctx, c, id)
			if e != nil {
				return nil, e
			}
			return map[string]any{"form_id": id, "source": source, "fields": items, "complete": true}, nil
		})
		fields.Example = "  crocoblock-pp-cli forms fields 42 --agent --select fields"
		crocoAttach(root, "forms", fields)
		for _, op := range []string{"create", "update"} {
			operation := op
			var data, input string
			use := operation
			if operation == "update" {
				use += " <id>"
			}
			cmd := crocoCommand(f, use, "Write selected form properties and verify persistence", false, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
				id := ""
				var e error
				if operation == "update" {
					id, e = crocoOneID(args)
				} else if len(args) != 0 {
					e = usageErr(fmt.Errorf("forms create accepts no positional arguments"))
				}
				if e != nil {
					return nil, e
				}
				body, e := crocoBody(data, input)
				if e != nil {
					return nil, e
				}
				if e = crocoRequireBody(body); e != nil {
					return nil, e
				}
				if e = crocoOnly(body, "title", "content", "status", "slug", "excerpt", "meta"); e != nil {
					return nil, e
				}
				if operation == "create" {
					if _, ok := body["status"]; !ok {
						body["status"] = "draft"
					}
				}
				if s, ok := body["status"]; ok && !crocoFormStatus(crocoString(s), false) {
					return nil, usageErr(fmt.Errorf("invalid form status"))
				}
				after, id, e := crocoFormWrite(ctx, c, operation, id, body)
				if e != nil {
					return nil, e
				}
				keys := make([]string, 0, len(body))
				for k := range body {
					keys = append(keys, k)
				}
				if e = crocoJournal(f, ctx, c, "form_write", "forms", keys, map[string]any{"id": id, "operation": operation, "verified": true}); e != nil {
					return nil, e
				}
				return after, nil
			})
			crocoDataFlags(cmd, &data, &input)
			cmd.Example = "  crocoblock-pp-cli forms " + operation + " "
			if operation == "update" {
				cmd.Example += "42 "
			}
			cmd.Example += "--input form.json --agent"
			crocoAttach(root, "forms", cmd)
		}
		trash := crocoDeclaredCommand(f, &cobra.Command{Use: "trash <id>", Short: "Move a form to WordPress trash and verify its status"}, false, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
			id, e := crocoOneID(args)
			if e != nil {
				return nil, e
			}
			if _, e = crocoRequest(ctx, c, "DELETE", crocoFormsRoute+"/"+id, map[string]string{"force": "false"}, nil, false); e != nil {
				return nil, e
			}
			after, e := crocoFormGet(ctx, c, id)
			if e != nil {
				return nil, apiErr(fmt.Errorf("mutation_applied: trash verification failed: %w", e))
			}
			if after["status"] != "trash" {
				return nil, apiErr(fmt.Errorf("mutation_applied_unknown: expected trash status; inspect before retry"))
			}
			if e = crocoJournal(f, ctx, c, "form_write", "forms", []string{"status"}, map[string]any{"id": id, "operation": "trash", "verified": true}); e != nil {
				return nil, e
			}
			return after, nil
		})
		trash.Example = "  crocoblock-pp-cli forms trash 42 --agent"
		crocoAttach(root, "forms", trash)
	})
}

func crocoOneID(args []string) (string, error) {
	if len(args) != 1 {
		return "", usageErr(fmt.Errorf("exactly one positive record ID is required"))
	}
	return crocoID(args, 0)
}
func crocoFormStatus(s string, list bool) bool {
	switch s {
	case "draft", "pending", "private", "publish", "future":
		return true
	case "any", "trash":
		return list
	}
	return false
}
func crocoFormGet(ctx context.Context, c *client.Client, id string) (map[string]any, error) {
	v, e := crocoRequest(ctx, c, "GET", crocoFormsRoute+"/"+id, map[string]string{"context": "edit"}, nil, true)
	if e != nil {
		return nil, e
	}
	return crocoMap(v)
}
func crocoFormRaw(form map[string]any) (string, error) {
	c, ok := form["content"].(map[string]any)
	if !ok {
		return "", apiErr(fmt.Errorf("form content missing; raw edit permission is required"))
	}
	s, ok := c["raw"].(string)
	if !ok {
		return "", apiErr(fmt.Errorf("raw form content unavailable; field coverage is unknown"))
	}
	return s, nil
}

var crocoFormBlockRE = regexp.MustCompile(`(?s)<!--\s+wp:jet-forms/([a-zA-Z0-9_-]+)(.*?)-->`)

func crocoParseFormFields(raw string) ([]any, error) {
	out := []any{}
	matches := crocoFormBlockRE.FindAllStringSubmatch(raw, -1)
	if strings.Count(raw, "<!-- wp:jet-forms/") > len(matches) {
		return nil, apiErr(fmt.Errorf("unterminated JetFormBuilder block; field coverage unknown"))
	}
	for _, m := range crocoFormBlockRE.FindAllStringSubmatch(raw, -1) {
		attrs := strings.TrimSpace(m[2])
		attrs = strings.TrimSpace(strings.TrimSuffix(attrs, "/"))
		if attrs == "" {
			continue
		}
		var a map[string]any
		d := json.NewDecoder(strings.NewReader(attrs))
		d.UseNumber()
		if e := d.Decode(&a); e != nil {
			return nil, apiErr(fmt.Errorf("invalid Gutenberg attributes for %s: field coverage unknown", m[1]))
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return nil, apiErr(fmt.Errorf("trailing Gutenberg attribute data; field coverage unknown"))
		}
		name, _ := a["name"].(string)
		if name == "" {
			continue
		}
		field := map[string]any{"name": name, "type": m[1], "label": a["label"], "required": false}
		if m[1] == "text-field" {
			field["field_type"] = "text"
		}
		if e := crocoFormSchemaAttributes(field, a); e != nil {
			return nil, e
		}
		out = append(out, field)
	}
	return out, nil
}

// These attributes come from JetFormBuilder's block.json definitions. Only
// structural switches and literal validation limits are retained: defaults,
// submitted values, option values, messages and action settings are excluded.
func crocoFormSchemaAttributes(field, attrs map[string]any) error {
	for _, key := range []string{"required", "multiple", "enable_input_mask", "clear_on_submit", "is_timestamp"} {
		if value, exists := attrs[key]; exists {
			v, ok := value.(bool)
			if !ok {
				return apiErr(fmt.Errorf("form attribute %s must be boolean; schema coverage unknown", key))
			}
			field[key] = v
		}
	}
	if value, exists := attrs["field_type"]; exists && field["type"] == "text-field" {
		switch value {
		case "text", "email", "url", "tel", "password":
			field["field_type"] = value
		default:
			return apiErr(fmt.Errorf("unsupported form field_type; schema coverage unknown"))
		}
	}
	for _, key := range []string{"minlength", "maxlength", "min", "max", "step", "max_files", "max_size", "multiple_size"} {
		value, exists := attrs[key]
		if !exists || value == "" {
			continue
		}
		switch v := value.(type) {
		case json.Number:
			field[key] = v
		case string:
			// Rich/dynamic constraints can embed private presets; this
			// projection includes only literal numeric constraints.
			if crocoFormNumberRE.MatchString(v) {
				field[key] = json.Number(v)
			}
		default:
			return apiErr(fmt.Errorf("form attribute %s is not a literal limit; schema coverage unknown", key))
		}
	}
	return nil
}

var crocoFormNumberRE = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

func crocoFormFields(ctx context.Context, c *client.Client, id string) ([]any, string, error) {
	form, e := crocoFormGet(ctx, c, id)
	if e != nil {
		return nil, "", e
	}
	raw, e := crocoFormRaw(form)
	if e != nil {
		return nil, "", e
	}
	declarations, e := crocoParseFormFields(raw)
	if e != nil {
		return nil, "", e
	}
	// The native endpoint skips drafts (HTTP 204). Raw blocks are the same
	// declarations WordPress stores, so never publish a draft merely to inspect it.
	if form["status"] != "publish" {
		return declarations, "gutenberg_raw", nil
	}
	v, e := crocoRequest(ctx, c, "GET", "/jet-form-builder/v1/"+id+"/fields", nil, nil, true)
	if e != nil {
		return nil, "", e
	}
	if v == nil {
		return nil, "", apiErr(fmt.Errorf("published form fields returned no content; coverage unknown"))
	}
	// Get_Form_Fields wraps the collection in {fields:[...]}; do not let an
	// absent/null member become a successful empty field inventory.
	if envelope, ok := v.(map[string]any); ok {
		v = envelope["fields"]
	}
	items, ok := v.([]any)
	if !ok {
		return nil, "", apiErr(fmt.Errorf("published form fields collection missing or malformed; coverage unknown"))
	}
	for _, v := range items {
		m, e := crocoMap(v)
		if e != nil {
			return nil, "", e
		}
		name := crocoString(m["value"])
		if name == "" {
			return nil, "", apiErr(fmt.Errorf("field response missing name; coverage unknown"))
		}
		if typ, ok := m["type"].(string); !ok || typ == "" {
			return nil, "", apiErr(fmt.Errorf("field response missing type; coverage unknown"))
		}
	}
	// The native response omits validation attributes and may represent types
	// differently. Validate that route, then use the same raw declarations as
	// drafts so publishing alone cannot manufacture a field-schema change.
	return declarations, "gutenberg_raw_native_verified", nil
}
func crocoFormProperty(m map[string]any, k string) any {
	v := m[k]
	if k == "content" || k == "title" || k == "excerpt" {
		if x, ok := v.(map[string]any); ok {
			return x["raw"]
		}
	}
	return v
}
func crocoVerifyForm(before, after, patch map[string]any) error {
	for k, want := range patch {
		if k == "meta" {
			w, ok := want.(map[string]any)
			if !ok {
				return fmt.Errorf("meta must be an object")
			}
			got, ok := after[k].(map[string]any)
			if !ok {
				return fmt.Errorf("readback meta unavailable")
			}
			for mk, mv := range w {
				if !reflect.DeepEqual(mv, got[mk]) {
					return fmt.Errorf("form meta %s readback differs", mk)
				}
			}
			continue
		}
		if !reflect.DeepEqual(want, crocoFormProperty(after, k)) {
			return fmt.Errorf("form %s readback differs (WordPress may have normalized the value)", k)
		}
	}
	if _, changed := patch["content"]; !changed && len(before) > 0 {
		if !reflect.DeepEqual(crocoFormProperty(before, "content"), crocoFormProperty(after, "content")) {
			return fmt.Errorf("unrelated Gutenberg blocks changed")
		}
	}
	if bm, ok := before["meta"].(map[string]any); ok {
		am, _ := after["meta"].(map[string]any)
		pm, _ := patch["meta"].(map[string]any)
		for k, v := range bm {
			if _, changed := pm[k]; !changed && !reflect.DeepEqual(v, am[k]) {
				return fmt.Errorf("unrelated form meta %s changed", k)
			}
		}
	}
	return nil
}

func crocoFormWrite(ctx context.Context, c *client.Client, operation, id string, body map[string]any) (map[string]any, string, error) {
	for _, key := range []string{"title", "content", "excerpt", "status", "slug"} {
		if v, exists := body[key]; exists {
			if _, ok := v.(string); !ok {
				return nil, id, usageErr(fmt.Errorf("%s must be a string", key))
			}
		}
	}
	if v, exists := body["meta"]; exists {
		if _, ok := v.(map[string]any); !ok {
			return nil, id, usageErr(fmt.Errorf("meta must be an object"))
		}
	}
	var e error

	before := map[string]any{}
	if operation == "update" {
		before, e = crocoFormGet(ctx, c, id)
		if e != nil {
			return nil, id, e
		}
	}
	route := crocoFormsRoute
	if id != "" {
		route += "/" + id
	}
	v, e := crocoRequest(ctx, c, "POST", route, nil, body, false)
	if e != nil {
		return nil, id, e
	}
	m, e := crocoMap(v)
	if e != nil {
		return nil, id, apiErr(fmt.Errorf("mutation_applied_unknown: response missing form; inspect site before retry"))
	}
	id = crocoString(m["id"])
	if _, e = crocoID([]string{id}, 0); e != nil {
		return nil, id, apiErr(fmt.Errorf("mutation_applied_unknown: response missing ID; inspect site before retry"))
	}
	after, e := crocoFormGet(ctx, c, id)
	if e != nil {
		return nil, id, apiErr(fmt.Errorf("mutation_applied: verification failed; do not retry write: %w", e))
	}
	if e = crocoVerifyForm(before, after, body); e != nil {
		return nil, id, apiErr(fmt.Errorf("mutation_applied: %w; inspect site before retry", e))
	}
	return after, id, nil
}
