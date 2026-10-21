package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
)

// Curated adapters replace raw transport commands, which lack plugin-specific
// permissions, redaction and response-body validation.
func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		for _, c := range root.Commands() {
			// Import has no inline payload alternative; its required input path
			// is intentionally unavailable to MCP callers.
			if c.Name() == "import" {
				if c.Annotations == nil {
					c.Annotations = map[string]string{}
				}
				c.Annotations["mcp:hidden"] = "true"
			}
			if strings.HasSuffix(c.Name(), "-api") || c.Name() == "transport" {
				root.RemoveCommand(c)
			}
		}
	})
}

func crocoGroup(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Name() == name {
			if c.Annotations == nil {
				c.Annotations = map[string]string{}
			}
			c.Annotations["pp:parent-group"] = "true"
			return c
		}
	}
	c := &cobra.Command{Use: name, Short: "Manage Crocoblock " + name, Annotations: map[string]string{"pp:parent-group": "true"}}
	root.AddCommand(c)
	return c
}
func crocoAttach(root *cobra.Command, group string, command *cobra.Command) {
	// These CLI workflows require operator-selected local files. The MCP
	// walker rejects read/write path arguments by design, so advertising the
	// commands there would leave tools that cannot receive required inputs.
	switch group + " " + command.Name() {
	case "schema diff", "cct reconcile", "reservations overlaps", "fields update", "fields restore":
		if command.Annotations == nil {
			command.Annotations = map[string]string{}
		}
		command.Annotations["mcp:hidden"] = "true"
	}
	parent := root
	if group != "" {
		parent = crocoGroup(root, group)
	}
	for _, c := range parent.Commands() {
		if c.Name() == command.Name() {
			parent.RemoveCommand(c)
		}
	}
	parent.AddCommand(command)
}

// crocoCommand guarantees dry runs reach neither files nor network, and every
// live request shares the command deadline and the generated adaptive limiter.
func crocoCommand(f *rootFlags, use, short string, readOnly bool, run func(context.Context, *client.Client, *cobra.Command, []string) (any, error)) *cobra.Command {
	a := map[string]string{"pp:data-source": "live"}
	if readOnly {
		a["mcp:read-only"] = "true"
	}
	cmd := &cobra.Command{Use: use, Short: short, Annotations: a, Example: "  crocoblock-pp-cli " + strings.Fields(use)[0] + " --agent", RunE: func(cmd *cobra.Command, args []string) error {
		if dryRunOK(f) {
			return writeDryRun(cmd.OutOrStdout(), f, cmd.CommandPath())
		}
		ctx, cancel := boundCtx(cmd.Context(), f)
		defer cancel()
		c, err := f.newClient()
		if err != nil {
			return err
		}
		c.NoCache = true
		result, err := run(ctx, c, cmd, args)
		if err != nil {
			return err
		}
		return f.printJSON(cmd, crocoSafe(result))
	}}
	return cmd
}

// crocoDeclaredCommand keeps the Cobra shape explicit for source-based tooling.
func crocoDeclaredCommand(f *rootFlags, shape *cobra.Command, readOnly bool, run func(context.Context, *client.Client, *cobra.Command, []string) (any, error)) *cobra.Command {
	return crocoCommand(f, shape.Use, shape.Short, readOnly, run)
}

func crocoRequest(ctx context.Context, c *client.Client, method, path string, query map[string]string, body any, readOnly bool) (any, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "wordpress.example" {
		return nil, configErr(fmt.Errorf("set CROCOBLOCK_BASE_URL to your authorized HTTPS WordPress /wp-json URL"))
	}
	if c.Config.AuthHeader() == "" {
		return nil, authErr(fmt.Errorf("configure CROCOBLOCK_USERNAME and CROCOBLOCK_APPLICATION_PASSWORD for this WordPress site"))
	}
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "://") {
		return nil, usageErr(fmt.Errorf("invalid relative WordPress route"))
	}
	if !readOnly {
		db, e := crocoOpenStore(ctx, "")
		if e != nil {
			return nil, configErr(fmt.Errorf("prepare write journal: %w", e))
		}
		e = db.CrocoCheckSite(ctx, c.BaseURL)
		db.Close()
		if e != nil {
			return nil, configErr(fmt.Errorf("write journal belongs to another site; select a separate CROCOBLOCK_DB: %w", e))
		}
	}
	var raw json.RawMessage
	// WordPress/JetPlugins do not promise idempotency-key deduplication.
	// Clear generic configured keys so they cannot override this write's
	// explicit no-replay policy in the generated client.
	headers := map[string]string{
		client.ReplaySafeHeader: "false",
		"Idempotency-Key":       "",
		"X-Idempotency-Key":     "",
	}
	switch method {
	case "GET":
		raw, err = c.Get(ctx, path, query)
	case "POST":
		if readOnly {
			raw, _, err = c.PostQueryWithParams(ctx, path, query, body)
		} else {
			raw, _, err = c.PostWithParamsAndHeaders(ctx, path, query, body, headers)
		}
	case "PUT":
		raw, _, err = c.PutWithParamsAndHeaders(ctx, path, query, body, headers)
	case "DELETE":
		raw, _, err = c.DeleteWithParamsAndBodyAndHeaders(ctx, path, query, body, headers)
	default:
		return nil, usageErr(fmt.Errorf("unsupported HTTP method %s", method))
	}
	if err != nil {
		return nil, classifyAPIErrorOnly(err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var v any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if err = d.Decode(&v); err != nil {
		return nil, apiErr(fmt.Errorf("invalid JSON from %s: %w", path, err))
	}
	if obj, ok := v.(map[string]any); ok {
		if success, exists := obj["success"].(bool); exists && !success {
			return nil, apiErr(fmt.Errorf("plugin rejected %s: %v", path, crocoSafe(obj)))
		}
		if e, exists := obj["error"]; exists && e != nil {
			return nil, apiErr(fmt.Errorf("upstream RPC error: %v", crocoSafe(e)))
		}
	}
	return v, nil
}

func crocoMap(v any) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, apiErr(fmt.Errorf("expected object, received %T", v))
	}
	return m, nil
}
func crocoArray(v any) ([]any, error) {
	if v == nil {
		return []any{}, nil
	}
	a, ok := v.([]any)
	if !ok {
		return nil, apiErr(fmt.Errorf("expected collection, received %T", v))
	}
	return a, nil
}
func crocoString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}
func crocoInt(v any) (int64, error) {
	s := crocoString(v)
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil {
		return 0, apiErr(fmt.Errorf("invalid integer %q", s))
	}
	return n, nil
}
func crocoID(args []string, i int) (string, error) {
	if len(args) <= i {
		return "", usageErr(fmt.Errorf("a numeric record ID is required; see --help"))
	}
	n, e := strconv.ParseInt(args[i], 10, 64)
	if e != nil || n <= 0 {
		return "", usageErr(fmt.Errorf("record ID must be a positive integer"))
	}
	return strconv.FormatInt(n, 10), nil
}

var crocoSlugRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

func crocoSlug(s string) (string, error) {
	if !crocoSlugRE.MatchString(s) {
		return "", usageErr(fmt.Errorf("invalid slug %q; use letters, digits, underscores or hyphens", s))
	}
	return s, nil
}

var crocoSensitiveRE = regexp.MustCompile(`(?i)(password|passwd|secret|token|authorization|cookie|nonce|license|api.?key|_jf_actions|user_email|user_name|ip_address)`)

func crocoSensitive(k string) bool { return crocoSensitiveRE.MatchString(k) }
func crocoSafe(v any) any {
	switch x := v.(type) {
	case map[string]any:
		r := map[string]any{}
		for k, val := range x {
			if crocoSensitive(k) {
				r[k] = "[redacted]"
			} else {
				r[k] = crocoSafe(val)
			}
		}
		return r
	case []any:
		r := make([]any, 0, len(x))
		for _, val := range x {
			r = append(r, crocoSafe(val))
		}
		return r
	default:
		if v != nil {
			k := reflect.TypeOf(v).Kind()
			if k == reflect.Map || k == reflect.Slice || k == reflect.Array || k == reflect.Struct {
				b, e := json.Marshal(v)
				if e != nil {
					return "[unserializable]"
				}
				var normalized any
				d := json.NewDecoder(strings.NewReader(string(b)))
				d.UseNumber()
				if d.Decode(&normalized) != nil {
					return "[invalid JSON]"
				}
				return crocoSafe(normalized)
			}
		}
		return v
	}
}
func crocoBody(data, input string) (map[string]any, error) {
	if data != "" && input != "" {
		return nil, usageErr(fmt.Errorf("use exactly one of --data or --input"))
	}
	if input != "" {
		var reader io.Reader
		var file *os.File
		if input == "-" {
			reader = os.Stdin
		} else {
			f, e := os.Open(input)
			if e != nil {
				return nil, usageErr(e)
			}
			file = f
			defer file.Close()
			reader = f
		}
		b, e := io.ReadAll(io.LimitReader(reader, 4*1024*1024+1))
		if e != nil {
			return nil, e
		}
		if len(b) > 4*1024*1024 {
			return nil, usageErr(fmt.Errorf("input exceeds 4 MiB"))
		}
		data = string(b)
	}
	if strings.TrimSpace(data) == "" {
		return map[string]any{}, nil
	}
	d := json.NewDecoder(strings.NewReader(data))
	d.UseNumber()
	var body map[string]any
	if e := d.Decode(&body); e != nil || body == nil {
		return nil, usageErr(fmt.Errorf("--data/--input must contain a JSON object"))
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return nil, usageErr(fmt.Errorf("input must contain exactly one JSON object"))
	}
	return body, nil
}
func crocoDataFlags(cmd *cobra.Command, data, input *string) {
	cmd.Flags().StringVar(data, "data", "", "JSON object containing the selected plugin fields to write")
	cmd.Flags().StringVar(input, "input", "", "Read the JSON object from this private file, or - for stdin")
	cmd.Annotations["mcp:read-flags"] = "input"
}
func crocoRequireBody(body map[string]any) error {
	if len(body) == 0 {
		return usageErr(fmt.Errorf("provide a nonempty JSON object with --data or --input"))
	}
	return nil
}
func crocoOnly(body map[string]any, allowed ...string) error {
	set := map[string]bool{}
	for _, k := range allowed {
		set[k] = true
	}
	for k := range body {
		if !set[k] {
			return usageErr(fmt.Errorf("unsupported field %q; allowed: %s", k, strings.Join(allowed, ", ")))
		}
	}
	return nil
}

func crocoMCP(ctx context.Context, c *client.Client, name string, args map[string]any) (map[string]any, error) {
	read := strings.HasPrefix(name, "resource-") || (name == "tool-manage-modules" && args["operation"] == "list")
	v, e := crocoRequest(ctx, c, "POST", "/jet-engine/v1/mcp", nil, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, read)
	if e != nil {
		return nil, e
	}
	envelope, e := crocoMap(v)
	if e != nil {
		return nil, e
	}
	result, e := crocoMap(envelope["result"])
	if e != nil {
		return nil, e
	}
	if result["isError"] == true {
		return nil, apiErr(fmt.Errorf("native MCP %s failed", name))
	}
	content, e := crocoArray(result["content"])
	if e != nil {
		return nil, e
	}
	for _, entry := range content {
		m, e := crocoMap(entry)
		if e != nil {
			return nil, e
		}
		if m["type"] == "text" {
			d := json.NewDecoder(strings.NewReader(crocoString(m["text"])))
			d.UseNumber()
			var response map[string]any
			if e = d.Decode(&response); e != nil {
				return nil, apiErr(fmt.Errorf("native MCP returned non-JSON text"))
			}
			if response["success"] == false {
				return nil, apiErr(fmt.Errorf("native MCP rejected %s: %v", name, crocoSafe(response)))
			}
			return response, nil
		}
	}
	return nil, apiErr(fmt.Errorf("native MCP returned no JSON text content"))
}
func crocoConfiguration(ctx context.Context, c *client.Client) (map[string]any, error) {
	parts := map[string]any{}
	for _, part := range []string{"post_types", "taxonomies", "meta_boxes", "options_pages", "queries", "relations", "custom_content_types", "rest_api_endpoints", "glossaries"} {
		parts[part] = true
	}
	return crocoMCP(ctx, c, "resource-get-configuration", map[string]any{"parts": parts})
}
func crocoPluginVersions(ctx context.Context, c *client.Client) (map[string]string, error) {
	v, e := crocoRequest(ctx, c, "GET", "/wp/v2/plugins", nil, nil, true)
	if e != nil {
		return nil, e
	}
	a, e := crocoArray(v)
	if e != nil {
		return nil, e
	}
	versions := map[string]string{}
	for _, x := range a {
		m, e := crocoMap(x)
		if e != nil {
			return nil, e
		}
		slug := strings.Split(crocoString(m["plugin"]), "/")[0]
		if (strings.HasPrefix(slug, "jet-") || strings.HasPrefix(slug, "jetformbuilder")) && m["status"] == "active" {
			versions[slug] = crocoString(m["version"])
		}
	}
	return versions, nil
}
