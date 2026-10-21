package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
)

type crocoPostFields struct {
	Route    string
	Declared map[string]map[string]any
	Exposed  map[string]map[string]any
}
type crocoFieldSnapshot struct {
	Version   int            `json:"version"`
	Site      string         `json:"site"`
	PostType  string         `json:"post_type"`
	ID        string         `json:"id"`
	CreatedAt string         `json:"created_at"`
	Verified  bool           `json:"verified"`
	Before    map[string]any `json:"before"`
	After     map[string]any `json:"after"`
}

func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		for _, op := range []string{"list", "get", "update", "restore"} {
			operation := op
			var postType, selected, data, input, snapshot string
			use := op
			if op == "get" || op == "update" {
				use += " <id>"
			}
			cmd := crocoCommand(f, use, "Inspect or update declared JetEngine fields exposed by WordPress REST", op == "list" || op == "get", func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
				id := ""
				var e error
				var snap *crocoFieldSnapshot
				if operation == "get" || operation == "update" {
					id, e = crocoOneID(args)
				} else if len(args) > 0 {
					e = usageErr(fmt.Errorf("fields %s accepts no positional arguments", operation))
				}
				if e != nil {
					return nil, e
				}
				if operation == "restore" {
					snap, e = crocoReadFieldSnapshot(snapshot)
					if e != nil {
						return nil, e
					}
					if strings.TrimRight(snap.Site, "/") != strings.TrimRight(c.BaseURL, "/") {
						return nil, usageErr(fmt.Errorf("snapshot belongs to another site"))
					}
					postType = snap.PostType
					id = snap.ID
				}
				if _, e = crocoSlug(postType); e != nil {
					return nil, e
				}
				patch := map[string]any{}
				if operation == "update" {
					patch, e = crocoBody(data, input)
					if e != nil {
						return nil, e
					}
					if e = crocoRequireBody(patch); e != nil {
						return nil, e
					}
					if snapshot == "" {
						return nil, usageErr(fmt.Errorf("--snapshot is required before editing fields"))
					}
				}
				schema, e := crocoDiscoverPostFields(ctx, c, postType)
				if e != nil {
					return nil, e
				}
				if operation == "list" {
					out := []any{}
					names := make([]string, 0, len(schema.Declared))
					for name := range schema.Declared {
						names = append(names, name)
					}
					sort.Strings(names)
					for _, name := range names {
						def := schema.Declared[name]
						rs, exposed := schema.Exposed[name]
						out = append(out, map[string]any{"name": name, "type": def["type"], "rest_exposed": exposed, "rest_schema": rs})
					}
					return map[string]any{"post_type": postType, "route": schema.Route, "fields": out, "complete": true}, nil
				}
				current, e := crocoPostMeta(ctx, c, schema.Route, id)
				if e != nil {
					return nil, e
				}
				if operation == "get" {
					names, e := crocoSelectedNames(selected)
					if e != nil {
						return nil, e
					}
					if len(names) == 0 {
						return nil, usageErr(fmt.Errorf("--fields is required to select metadata explicitly"))
					}
					values, e := crocoSelectPostMeta(schema, current, names)
					if e != nil {
						return nil, e
					}
					return map[string]any{"post_type": postType, "id": id, "fields": values}, nil
				}
				if operation == "restore" {
					if e = crocoRestoreUnchanged(current, snap); e != nil {
						return nil, e
					}
					patch = snap.Before
				}
				keys := make([]string, 0, len(patch))
				for k, v := range patch {
					if e = crocoValidatePostField(schema, k, v); e != nil {
						return nil, e
					}
					keys = append(keys, k)
				}
				sort.Strings(keys)
				before, e := crocoSelectPostMeta(schema, current, keys)
				if e != nil {
					return nil, e
				}
				var file *os.File
				if operation == "update" {
					snap = &crocoFieldSnapshot{Version: 1, Site: strings.TrimRight(c.BaseURL, "/"), PostType: postType, ID: id, CreatedAt: time.Now().UTC().Format(time.RFC3339), Before: before, After: patch}
					file, e = crocoCreateFieldSnapshot(snapshot, snap)
					if e != nil {
						return nil, e
					}
					defer file.Close()
				}
				attempt, e := crocoFieldWriteIntent(ctx, c, id, operation, keys)
				if e != nil {
					return nil, e
				}
				_, e = crocoRequest(ctx, c, "POST", schema.Route+"/"+id, nil, map[string]any{"meta": patch}, false)
				if e != nil {
					return nil, e
				}
				after, e := crocoPostMeta(ctx, c, schema.Route, id)
				if e != nil {
					return nil, apiErr(fmt.Errorf("mutation_applied: verification failed; do not retry write: %w", e))
				}
				if e = crocoVerifyMeta(current, after, patch); e != nil {
					return nil, apiErr(fmt.Errorf("mutation_applied: %w; inspect before retry", e))
				}
				if operation == "update" {
					snap.After = map[string]any{}
					for _, k := range keys {
						snap.After[k] = after[k]
					}
					snap.Verified = true
					if e = crocoWriteFieldSnapshot(file, snap); e != nil {
						return nil, apiErr(fmt.Errorf("mutation_applied: private snapshot finalization failed: %w", e))
					}
				}
				if e = crocoJournal(f, ctx, c, "post_fields_write", "fields:"+id, keys, map[string]any{"id": id, "operation": operation, "verified": true, "start_event_id": attempt}); e != nil {
					return nil, e
				}
				return map[string]any{"post_type": postType, "id": id, "changed_fields": keys, "verified": true, "snapshot": snapshot, "operation": operation}, nil
			})
			if op != "restore" {
				cmd.Flags().StringVar(&postType, "post-type", "post", "WordPress post type slug; REST route is discovered")
			}
			if op == "get" {
				cmd.Flags().StringVar(&selected, "fields", "", "Required comma-separated field names")
			}
			if op == "update" {
				crocoDataFlags(cmd, &data, &input)
			}
			if op == "update" || op == "restore" {
				cmd.Flags().StringVar(&snapshot, "snapshot", "", "Private snapshot file: create exclusively for update; read for restore")
				if op == "restore" {
					cmd.Annotations["mcp:read-flags"] = "snapshot"
				} else {
					cmd.Annotations["mcp:write-flags"] = "snapshot"
				}
			}
			switch op {
			case "list":
				cmd.Example = "  crocoblock-pp-cli fields list --post-type post --agent"
			case "get":
				cmd.Example = "  crocoblock-pp-cli fields get 13 --fields pp_keep --agent"
			case "update":
				cmd.Example = "  crocoblock-pp-cli fields update 13 --input fields.json --snapshot private/change.json --agent"
			case "restore":
				cmd.Example = "  crocoblock-pp-cli fields restore --snapshot private/change.json --agent"
			}
			crocoAttach(root, "fields", cmd)
		}
	})
}

func crocoDiscoverPostFields(ctx context.Context, c *client.Client, postType string) (crocoPostFields, error) {
	result := crocoPostFields{Declared: map[string]map[string]any{}, Exposed: map[string]map[string]any{}}
	v, e := crocoRequest(ctx, c, "GET", "/wp/v2/types/"+postType, map[string]string{"context": "edit"}, nil, true)
	if e != nil {
		return result, e
	}
	typ, e := crocoMap(v)
	if e != nil {
		return result, e
	}
	base := crocoString(typ["rest_base"])
	namespace := crocoString(typ["rest_namespace"])
	if namespace == "" {
		namespace = "wp/v2"
	}
	if _, e = crocoSlug(base); e != nil {
		return result, apiErr(fmt.Errorf("post type has no supported REST base"))
	}
	if namespace != "wp/v2" {
		return result, usageErr(fmt.Errorf("post type REST namespace %s is outside the supported WordPress controller", namespace))
	}
	result.Route = "/" + namespace + "/" + base
	isCustom := false
	for _, kind := range []string{"get-post-types", "get-meta-boxes"} {
		v, e = crocoRequest(ctx, c, "GET", "/jet-engine/v2/"+kind, nil, nil, true)
		if e != nil {
			return result, e
		}
		m, e := crocoMap(v)
		if e != nil {
			return result, e
		}
		if _, present := m["data"]; !present {
			return result, apiErr(fmt.Errorf("JetEngine definition response missing data; coverage unknown"))
		}
		rows, e := crocoDeclaredRows(m["data"])
		if e != nil {
			return result, e
		}
		for _, v := range rows {
			row, e := crocoMap(v)
			if e != nil {
				return result, e
			}
			applies := false
			if kind == "get-post-types" {
				applies = crocoString(row["slug"]) == postType
				if applies {
					isCustom = true
				}
			} else {
				a, _ := row["args"].(map[string]any)
				if objectType := crocoString(a["object_type"]); objectType == "post" || objectType == "" {
					applies = crocoContainsType(a["allowed_post_type"], postType)
				}
			}
			if applies {
				if e = crocoAddDeclaredFields(result.Declared, row["meta_fields"]); e != nil {
					return result, e
				}
			}
		}
	}
	if !isCustom {
		v, e = crocoRequest(ctx, c, "GET", "/jet-engine/v2/get-built-in-post-type/"+postType, nil, nil, true)
		if e != nil {
			return result, e
		}
		m, e := crocoMap(v)
		if e != nil {
			return result, e
		}
		row, e := crocoMap(m["data"])
		if e != nil {
			return result, e
		}
		if e = crocoAddDeclaredFields(result.Declared, row["meta_fields"]); e != nil {
			return result, e
		}
	}
	// WP_REST_Server supports _method=OPTIONS; this read-only request retains
	// authentication, request deadlines and the shared adaptive limiter.
	v, e = crocoRequest(ctx, c, "GET", result.Route, map[string]string{"_method": "OPTIONS"}, nil, true)
	if e != nil {
		return result, e
	}
	m, e := crocoMap(v)
	if e != nil {
		return result, e
	}
	schema, ok := m["schema"].(map[string]any)
	if !ok {
		return result, apiErr(fmt.Errorf("REST schema unavailable; field exposure unknown"))
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return result, apiErr(fmt.Errorf("REST properties unavailable; field exposure unknown"))
	}
	meta, ok := props["meta"].(map[string]any)
	if !ok {
		return result, nil
	}
	mp, ok := meta["properties"].(map[string]any)
	if !ok {
		return result, apiErr(fmt.Errorf("REST meta schema missing properties; field exposure unknown"))
	}
	for name, v := range mp {
		if _, declared := result.Declared[name]; declared {
			rs, ok := v.(map[string]any)
			if !ok {
				return result, apiErr(fmt.Errorf("invalid REST schema for field %s", name))
			}
			result.Exposed[name] = rs
		}
	}
	return result, nil
}
func crocoContainsType(v any, t string) bool {
	if s, ok := v.(string); ok {
		return s == t
	}
	if a, ok := v.([]any); ok {
		for _, x := range a {
			if crocoString(x) == t {
				return true
			}
		}
	}
	return false
}
func crocoAddDeclaredFields(out map[string]map[string]any, v any) error {
	if v == nil {
		return nil
	}
	rows, e := crocoArray(v)
	if e != nil {
		return e
	}
	for _, v := range rows {
		m, e := crocoMap(v)
		if e != nil {
			return e
		}
		name := crocoString(m["name"])
		if name == "" {
			continue
		}
		if object := crocoString(m["object_type"]); object != "" && object != "field" {
			continue
		}
		if old, exists := out[name]; exists && old["type"] != m["type"] {
			return apiErr(fmt.Errorf("conflicting JetEngine declarations for %s", name))
		}
		out[name] = m
	}
	return nil
}
func crocoPostMeta(ctx context.Context, c *client.Client, route, id string) (map[string]any, error) {
	v, e := crocoRequest(ctx, c, "GET", route+"/"+id, map[string]string{"context": "edit", "_fields": "id,meta"}, nil, true)
	if e != nil {
		return nil, e
	}
	m, e := crocoMap(v)
	if e != nil {
		return nil, e
	}
	meta, ok := m["meta"].(map[string]any)
	if !ok {
		return nil, apiErr(fmt.Errorf("REST post meta unavailable; no fields can be verified"))
	}
	return meta, nil
}
func crocoSelectPostMeta(s crocoPostFields, values map[string]any, names []string) (map[string]any, error) {
	out := map[string]any{}
	for _, k := range names {
		if _, ok := s.Declared[k]; !ok {
			return nil, usageErr(fmt.Errorf("field %s is not declared by JetEngine for this post type", k))
		}
		if _, ok := s.Exposed[k]; !ok {
			return nil, usageErr(fmt.Errorf("field %s is not exposed through WordPress REST", k))
		}
		if crocoSensitive(k) {
			return nil, usageErr(fmt.Errorf("sensitive field %s is excluded", k))
		}
		v, ok := values[k]
		if !ok {
			return nil, apiErr(fmt.Errorf("declared field %s absent from REST read; coverage unknown", k))
		}
		if crocoNestedSensitive(v) {
			return nil, usageErr(fmt.Errorf("field %s contains sensitive keys and is excluded from snapshots", k))
		}
		out[k] = v
	}
	return out, nil
}
func crocoValidatePostField(s crocoPostFields, k string, v any) error {
	d, ok := s.Declared[k]
	if !ok {
		return usageErr(fmt.Errorf("field %s is not declared by JetEngine", k))
	}
	schema, ok := s.Exposed[k]
	if !ok {
		return usageErr(fmt.Errorf("field %s is not exposed through WordPress REST", k))
	}
	if crocoSensitive(k) {
		return usageErr(fmt.Errorf("sensitive field %s is excluded", k))
	}
	if schema["readonly"] == true {
		return usageErr(fmt.Errorf("field %s is read-only in REST", k))
	}
	if d["type"] == "html" {
		return usageErr(fmt.Errorf("HTML layout field %s cannot store metadata", k))
	}
	if crocoNestedSensitive(v) {
		return usageErr(fmt.Errorf("field %s contains sensitive keys and is excluded", k))
	}
	if e := crocoValidateRESTValue(schema, v); e != nil {
		return usageErr(fmt.Errorf("field %s: %w", k, e))
	}
	return nil
}
func crocoValidateRESTValue(schema map[string]any, v any) error {
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, x := range enum {
			if reflect.DeepEqual(x, v) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("value is not in the declared enum")
		}
	}
	types := []string{}
	switch t := schema["type"].(type) {
	case string:
		types = append(types, t)
	case []any:
		for _, x := range t {
			types = append(types, crocoString(x))
		}
	}
	if len(types) == 0 {
		return fmt.Errorf("REST type is unspecified")
	}
	valid := false
	for _, t := range types {
		switch t {
		case "null":
			valid = valid || v == nil
		case "string":
			_, ok := v.(string)
			valid = valid || ok
		case "boolean":
			_, ok := v.(bool)
			valid = valid || ok
		case "integer":
			if n, ok := v.(json.Number); ok {
				_, e := n.Int64()
				valid = valid || e == nil
			}
		case "number":
			if n, ok := v.(json.Number); ok {
				_, e := n.Float64()
				valid = valid || e == nil
			}
		case "object":
			_, ok := v.(map[string]any)
			valid = valid || ok
		case "array":
			_, ok := v.([]any)
			valid = valid || ok
		}
	}
	if !valid {
		return fmt.Errorf("value does not match REST type %v", types)
	}
	if a, ok := v.([]any); ok {
		if item, ok := schema["items"].(map[string]any); ok {
			for _, x := range a {
				if e := crocoValidateRESTValue(item, x); e != nil {
					return e
				}
			}
		}
	}
	if obj, ok := v.(map[string]any); ok {
		props, _ := schema["properties"].(map[string]any)
		for k, x := range obj {
			if prop, ok := props[k].(map[string]any); ok {
				if e := crocoValidateRESTValue(prop, x); e != nil {
					return fmt.Errorf("%s: %w", k, e)
				}
			} else if schema["additionalProperties"] == false {
				return fmt.Errorf("unknown object property %s", k)
			}
		}
	}
	return nil
}
func crocoVerifyMeta(before, after, patch map[string]any) error {
	for k, want := range patch {
		if !reflect.DeepEqual(want, after[k]) {
			return fmt.Errorf("field %s readback differs", k)
		}
	}
	for k, want := range before {
		if _, changed := patch[k]; !changed && !reflect.DeepEqual(want, after[k]) {
			return fmt.Errorf("unrelated field %s changed", k)
		}
	}
	return nil
}
func crocoRestoreUnchanged(current map[string]any, snap *crocoFieldSnapshot) error {
	if !snap.Verified {
		return usageErr(fmt.Errorf("snapshot was not verified after its original write; restore refused"))
	}
	for k, v := range snap.After {
		if got, ok := current[k]; !ok || !reflect.DeepEqual(got, v) {
			return usageErr(fmt.Errorf("restore conflict: field %s changed after the saved write", k))
		}
	}
	return nil
}
func crocoCreateFieldSnapshot(path string, snap *crocoFieldSnapshot) (*os.File, error) {
	if path == "" {
		return nil, usageErr(fmt.Errorf("--snapshot path is required"))
	}
	path, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	file, e := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return nil, usageErr(fmt.Errorf("create private snapshot without overwriting: %w", e))
	}
	if e = crocoWriteInitialFieldSnapshot(file, snap); e != nil {
		file.Close()
		return nil, e
	}
	return file, nil
}

// The initial backup is written through the exclusively created file before
// any mutation. Finalization must never truncate this only copy of Before.
func crocoWriteInitialFieldSnapshot(file *os.File, snap *crocoFieldSnapshot) error {
	b, e := json.MarshalIndent(snap, "", "  ")
	if e != nil {
		return e
	}
	return crocoWriteSnapshotBytes(file, append(b, '\n'))
}
func crocoWriteSnapshotBytes(file *os.File, contents []byte) error {
	n, e := file.Write(contents)
	if e != nil {
		return e
	}
	if n != len(contents) {
		return io.ErrShortWrite
	}
	return file.Sync()
}
func crocoWriteFieldSnapshot(file *os.File, snap *crocoFieldSnapshot) error {
	b, e := json.MarshalIndent(snap, "", "  ")
	if e != nil {
		return e
	}
	return crocoFinalizeFieldSnapshot(file, append(b, '\n'), crocoWriteSnapshotBytes)
}

// crocoFinalizeFieldSnapshot leaves the initial snapshot untouched until the
// private replacement is written, synced and closed. The writer is passed in
// so failure-path tests can exercise partial writes without filling a disk.
func crocoFinalizeFieldSnapshot(file *os.File, contents []byte, write func(*os.File, []byte) error) error {
	original, e := file.Stat()
	if e != nil {
		return e
	}
	path := file.Name()
	unchanged := func() error {
		current, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if !original.Mode().IsRegular() || !current.Mode().IsRegular() || !os.SameFile(original, current) {
			return fmt.Errorf("snapshot path changed before finalization; original backup retained")
		}
		return nil
	}
	if e = unchanged(); e != nil {
		return e
	}
	temporary, e := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if e != nil {
		return e
	}
	defer func() {
		temporary.Close()
		os.Remove(temporary.Name())
	}()
	if e = temporary.Chmod(0600); e != nil {
		return e
	}
	if e = write(temporary, contents); e != nil {
		return e
	}
	if e = temporary.Close(); e != nil {
		return e
	}
	// Close the original before replacing it for platforms that disallow
	// renaming over an open file. The caller only defers another Close.
	if e = file.Close(); e != nil {
		return e
	}
	if e = unchanged(); e != nil {
		return e
	}
	// Rename replaces the directory entry itself; it never follows a target
	// symlink. A replacement detected above must not be overwritten.
	return os.Rename(temporary.Name(), path)
}
func crocoReadFieldSnapshot(path string) (*crocoFieldSnapshot, error) {
	if path == "" {
		return nil, usageErr(fmt.Errorf("--snapshot path is required"))
	}
	before, e := os.Lstat(path)
	if e != nil {
		return nil, usageErr(e)
	}
	if !before.Mode().IsRegular() {
		return nil, usageErr(fmt.Errorf("snapshot must be a regular file, not a symlink"))
	}
	file, e := os.Open(path)
	if e != nil {
		return nil, usageErr(e)
	}
	defer file.Close()
	st, e := file.Stat()
	if e != nil {
		return nil, e
	}
	if !os.SameFile(before, st) {
		return nil, usageErr(fmt.Errorf("snapshot changed while opening"))
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return nil, usageErr(fmt.Errorf("snapshot must be a private regular file (chmod 600)"))
	}
	if st.Size() > 4*1024*1024 {
		return nil, usageErr(fmt.Errorf("snapshot exceeds 4 MiB"))
	}
	d := json.NewDecoder(io.LimitReader(file, 4*1024*1024+1))
	d.UseNumber()
	d.DisallowUnknownFields()
	var snap crocoFieldSnapshot
	if e = d.Decode(&snap); e != nil {
		return nil, usageErr(e)
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, usageErr(fmt.Errorf("snapshot contains trailing data"))
	}
	if snap.Version != 1 || snap.Site == "" || snap.PostType == "" || !snap.Verified || len(snap.Before) == 0 || len(snap.Before) != len(snap.After) {
		return nil, usageErr(fmt.Errorf("snapshot is incomplete or unverified"))
	}
	if _, e = crocoOneID([]string{snap.ID}); e != nil {
		return nil, e
	}
	for k := range snap.Before {
		if _, ok := snap.After[k]; !ok {
			return nil, usageErr(fmt.Errorf("snapshot field sets differ"))
		}
	}
	return &snap, nil
}

// JetEngine meta-box inventories preserve keys such as "meta-1" in PHP,
// producing a JSON object when nonempty and [] when empty.
func crocoDeclaredRows(v any) ([]any, error) {
	if a, ok := v.([]any); ok {
		return a, nil
	}
	if m, ok := v.(map[string]any); ok {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		a := make([]any, 0, len(keys))
		for _, k := range keys {
			row, ok := m[k].(map[string]any)
			if !ok {
				return nil, apiErr(fmt.Errorf("invalid JetEngine definition row; coverage unknown"))
			}
			a = append(a, row)
		}
		return a, nil
	}
	return nil, apiErr(fmt.Errorf("JetEngine definition collection missing or malformed; coverage unknown"))
}

func crocoNestedSensitive(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if crocoSensitive(k) || crocoNestedSensitive(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if crocoNestedSensitive(v) {
				return true
			}
		}
	}
	return false
}
