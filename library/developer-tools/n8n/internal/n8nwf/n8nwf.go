// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.

// Package n8nwf parses n8n workflow and execution JSON into the shapes the
// cross-workflow commands query: flattened node references, connection
// edges, semantic diffs, and execution failure details.
package n8nwf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// CredRef is one credential a node references.
type CredRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Node is a flattened n8n node with the references the usage queries need.
type Node struct {
	ID            string          `json:"id,omitempty"`
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	TypeVersion   float64         `json:"typeVersion,omitempty"`
	Disabled      bool            `json:"disabled,omitempty"`
	Credentials   []CredRef       `json:"credentials,omitempty"`
	WebhookPath   string          `json:"webhook_path,omitempty"`
	SubworkflowID string          `json:"subworkflow_id,omitempty"`
	VarRefs       []string        `json:"var_refs,omitempty"`
	Parameters    json.RawMessage `json:"-"`
	Flags         map[string]any  `json:"-"` // node-level behavior settings (onError, retryOnFail, notes, ...)
}

// nodeFlagKeys are node-level fields outside parameters that change behavior.
var nodeFlagKeys = []string{"onError", "continueOnFail", "retryOnFail", "maxTries", "waitBetweenTries", "executeOnce", "alwaysOutputData", "notes", "notesInFlow"}

// Edge is one connection between two nodes.
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Output int    `json:"output"`
	Input  int    `json:"input"`
}

// Workflow is the parsed subset of an n8n workflow body.
type Workflow struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Active        bool           `json:"active"`
	Archived      bool           `json:"archived"`
	ErrorWorkflow string         `json:"error_workflow,omitempty"`
	Settings      map[string]any `json:"-"`
	Nodes         []Node         `json:"nodes"`
	Edges         []Edge         `json:"edges"`
}

type rawNode struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Type        string                     `json:"type"`
	TypeVersion float64                    `json:"typeVersion"`
	Disabled    bool                       `json:"disabled"`
	Credentials map[string]json.RawMessage `json:"credentials"`
	Parameters  json.RawMessage            `json:"parameters"`
}

type rawWorkflow struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Active      bool              `json:"active"`
	IsArchived  bool              `json:"isArchived"`
	Nodes       []json.RawMessage `json:"nodes"`
	Connections map[string]map[string][][]struct {
		Node  string `json:"node"`
		Index int    `json:"index"`
	} `json:"connections"`
	Settings map[string]any `json:"settings"`
}

var varRefRe = regexp.MustCompile(`\$vars\.([A-Za-z_][A-Za-z0-9_]*)`)

// ParseWorkflow parses an n8n workflow JSON body.
func ParseWorkflow(data []byte) (Workflow, error) {
	var rw rawWorkflow
	if err := json.Unmarshal(data, &rw); err != nil {
		return Workflow{}, fmt.Errorf("parse workflow: %w", err)
	}
	wf := Workflow{
		ID:       rw.ID,
		Name:     rw.Name,
		Active:   rw.Active,
		Archived: rw.IsArchived,
		Settings: rw.Settings,
		Nodes:    make([]Node, 0, len(rw.Nodes)),
		Edges:    make([]Edge, 0),
	}
	if ew, ok := rw.Settings["errorWorkflow"].(string); ok {
		wf.ErrorWorkflow = ew
	}
	for _, raw := range rw.Nodes {
		var rn rawNode
		if err := json.Unmarshal(raw, &rn); err != nil {
			return Workflow{}, fmt.Errorf("parse node: %w", err)
		}
		wf.Nodes = append(wf.Nodes, parseNode(rn, raw))
	}
	for from, kinds := range rw.Connections {
		for kind, outputs := range kinds {
			for idx, targets := range outputs {
				for _, t := range targets {
					wf.Edges = append(wf.Edges, Edge{From: from, To: t.Node, Kind: kind, Output: idx, Input: t.Index})
				}
			}
		}
	}
	sort.Slice(wf.Edges, func(i, j int) bool { return edgeKey(wf.Edges[i]) < edgeKey(wf.Edges[j]) })
	return wf, nil
}

func edgeKey(e Edge) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%d", e.From, e.Kind, e.Output, e.To, e.Input)
}

func parseNode(rn rawNode, raw json.RawMessage) Node {
	n := Node{ID: rn.ID, Name: rn.Name, Type: rn.Type, TypeVersion: rn.TypeVersion, Disabled: rn.Disabled, Parameters: rn.Parameters}
	n.Flags = map[string]any{}
	var all map[string]any
	_ = json.Unmarshal(raw, &all)
	for _, k := range nodeFlagKeys {
		if v, ok := all[k]; ok && v != nil {
			n.Flags[k] = v
		}
	}
	credTypes := make([]string, 0, len(rn.Credentials))
	for t := range rn.Credentials {
		credTypes = append(credTypes, t)
	}
	sort.Strings(credTypes)
	for _, t := range credTypes {
		var ref struct {
			ID   *string `json:"id"`
			Name string  `json:"name"`
		}
		if err := json.Unmarshal(rn.Credentials[t], &ref); err != nil {
			continue
		}
		c := CredRef{Type: t, Name: ref.Name}
		if ref.ID != nil {
			c.ID = *ref.ID
		}
		n.Credentials = append(n.Credentials, c)
	}
	// Decode parameters only for the node types whose fields we index.
	isWebhook := strings.HasSuffix(rn.Type, ".webhook") || strings.HasSuffix(rn.Type, ".formTrigger")
	isSub := strings.HasSuffix(rn.Type, ".executeWorkflow") || strings.HasSuffix(rn.Type, ".toolWorkflow")
	if isWebhook || isSub {
		var params map[string]any
		_ = json.Unmarshal(rn.Parameters, &params)
		if p, ok := params["path"].(string); ok && isWebhook {
			n.WebhookPath = strings.Trim(p, "/")
		}
		if isSub {
			n.SubworkflowID = resourceLocatorValue(params["workflowId"])
		}
	}
	if !bytes.Contains(rn.Parameters, []byte("$vars.")) {
		return n
	}
	seen := map[string]bool{}
	for _, m := range varRefRe.FindAllStringSubmatch(string(rn.Parameters), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			n.VarRefs = append(n.VarRefs, m[1])
		}
	}
	sort.Strings(n.VarRefs)
	return n
}

// resourceLocatorValue reads either a plain string or an n8n resource-locator
// object ({"__rl": true, "value": "...", "mode": "id"}).
func resourceLocatorValue(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimPrefix(t, "=")
	case map[string]any:
		if s, ok := t["value"].(string); ok {
			return strings.TrimPrefix(s, "=")
		}
	}
	return ""
}

// UsageQuery selects nodes by one exact reference. Empty fields are ignored;
// a node matches when it satisfies every non-empty field.
type UsageQuery struct {
	Credential  string // credential id or name (case-insensitive name match)
	NodeType    string // full type or short suffix such as "slack" or "httpRequest"
	Webhook     string // webhook path, leading/trailing slashes ignored
	Variable    string // variable key referenced as $vars.<key>
	Subworkflow string // workflow id called via Execute Workflow
}

// Empty reports whether no selector is set.
func (q UsageQuery) Empty() bool {
	return q.Credential == "" && q.NodeType == "" && q.Webhook == "" && q.Variable == "" && q.Subworkflow == ""
}

// MatchedFields names the selectors n satisfies, in a fixed order.
func (q UsageQuery) MatchedFields(n Node) []string {
	out := []string{}
	if q.Credential != "" && matchCred(n.Credentials, q.Credential) {
		out = append(out, "credential")
	}
	if q.NodeType != "" && MatchNodeType(n.Type, q.NodeType) {
		out = append(out, "node-type")
	}
	if q.Webhook != "" && strings.EqualFold(n.WebhookPath, strings.Trim(q.Webhook, "/")) {
		out = append(out, "webhook")
	}
	if q.Variable != "" && slices.Contains(n.VarRefs, strings.TrimPrefix(q.Variable, "$vars.")) {
		out = append(out, "variable")
	}
	if q.Subworkflow != "" && n.SubworkflowID == q.Subworkflow {
		out = append(out, "subworkflow")
	}
	return out
}

// Matches reports whether n satisfies every non-empty selector.
func (q UsageQuery) Matches(n Node) bool {
	if q.Credential != "" && !matchCred(n.Credentials, q.Credential) {
		return false
	}
	if q.NodeType != "" && !MatchNodeType(n.Type, q.NodeType) {
		return false
	}
	if q.Webhook != "" && !strings.EqualFold(n.WebhookPath, strings.Trim(q.Webhook, "/")) {
		return false
	}
	if q.Variable != "" && !slices.Contains(n.VarRefs, strings.TrimPrefix(q.Variable, "$vars.")) {
		return false
	}
	if q.Subworkflow != "" && n.SubworkflowID != q.Subworkflow {
		return false
	}
	return !q.Empty()
}

func matchCred(refs []CredRef, want string) bool {
	for _, c := range refs {
		if c.ID == want || strings.EqualFold(c.Name, want) {
			return true
		}
	}
	return false
}

// MatchNodeType matches a full node type ("n8n-nodes-base.slack") or the
// short name after the last dot ("slack"), case-insensitively.
func MatchNodeType(full, want string) bool {
	if strings.EqualFold(full, want) {
		return true
	}
	short := full
	if i := strings.LastIndex(full, "."); i >= 0 {
		short = full[i+1:]
	}
	return strings.EqualFold(short, want)
}

// Failure is the failing node and message extracted from an execution.
type Failure struct {
	Node    string `json:"node"`
	Message string `json:"message"`
}

// ExecutionFailure extracts the failing node and error message from an
// execution fetched with includeData=true. It accepts both the bare
// execution object and a {"data": {...}} wrapper.
func ExecutionFailure(data []byte) Failure {
	var ex struct {
		Data struct {
			ResultData struct {
				LastNodeExecuted string `json:"lastNodeExecuted"`
				Error            *struct {
					Message string `json:"message"`
					Node    *struct {
						Name string `json:"name"`
					} `json:"node"`
				} `json:"error"`
			} `json:"resultData"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &ex); err != nil {
		return Failure{}
	}
	rd := ex.Data.ResultData
	f := Failure{Node: rd.LastNodeExecuted}
	if rd.Error != nil {
		f.Message = rd.Error.Message
		if rd.Error.Node != nil && rd.Error.Node.Name != "" {
			f.Node = rd.Error.Node.Name
		}
	}
	return f
}

// ParamChange is one changed parameter path on a node present in both sides.
type ParamChange struct {
	Node   string `json:"node"`
	Path   string `json:"path"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// Rename records a node whose id is stable but whose name changed.
type Rename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DiffResult is a semantic workflow diff. Positions, timestamps, ids and
// pinned data are ignored by construction because Workflow never carries them.
type DiffResult struct {
	NodesAdded         []string      `json:"nodes_added"`
	NodesRemoved       []string      `json:"nodes_removed"`
	NodesRenamed       []Rename      `json:"nodes_renamed"`
	TypeChanges        []ParamChange `json:"type_changes"`
	ParamChanges       []ParamChange `json:"param_changes"`
	CredentialChanges  []ParamChange `json:"credential_changes"`
	DisabledChanges    []ParamChange `json:"disabled_changes"`
	EdgesAdded         []Edge        `json:"edges_added"`
	EdgesRemoved       []Edge        `json:"edges_removed"`
	SettingsChanges    []ParamChange `json:"settings_changes"`
	NameChange         *ParamChange  `json:"name_change,omitempty"`
	ActiveChanged      bool          `json:"active_changed"`
	ErrorWorkflowDelta *ParamChange  `json:"error_workflow_change,omitempty"`
}

// Changed reports whether the diff has any difference, including active state.
func (d DiffResult) Changed() bool { return d.ContentChanged() || d.ActiveChanged }

// ContentChanged reports differences a workflow update (PUT) can apply.
// Active state is excluded: it changes through publish/activate, not PUT.
func (d DiffResult) ContentChanged() bool {
	return len(d.NodesAdded)+len(d.NodesRemoved)+len(d.NodesRenamed)+len(d.TypeChanges)+
		len(d.ParamChanges)+len(d.CredentialChanges)+len(d.DisabledChanges)+
		len(d.EdgesAdded)+len(d.EdgesRemoved)+len(d.SettingsChanges) > 0 ||
		d.ErrorWorkflowDelta != nil || d.NameChange != nil
}

// Diff compares base (a) to target (b). Nodes match by name; a node whose id
// is shared across sides under a different name is reported as a rename and
// compared as the same node.
func Diff(a, b Workflow) DiffResult {
	d := DiffResult{
		NodesAdded: []string{}, NodesRemoved: []string{}, NodesRenamed: []Rename{},
		TypeChanges: []ParamChange{}, ParamChanges: []ParamChange{}, CredentialChanges: []ParamChange{},
		DisabledChanges: []ParamChange{}, EdgesAdded: []Edge{}, EdgesRemoved: []Edge{}, SettingsChanges: []ParamChange{},
	}
	aByName, bByName := byName(a.Nodes), byName(b.Nodes)
	bByID := map[string]Node{}
	for _, n := range b.Nodes {
		if n.ID != "" {
			bByID[n.ID] = n
		}
	}
	renamedTo := map[string]string{} // a name -> b name
	for _, n := range a.Nodes {
		if _, ok := bByName[n.Name]; ok {
			continue
		}
		if m, ok := bByID[n.ID]; ok && n.ID != "" {
			if _, clash := aByName[m.Name]; !clash {
				renamedTo[n.Name] = m.Name
				d.NodesRenamed = append(d.NodesRenamed, Rename{From: n.Name, To: m.Name})
			}
		}
	}
	renamedFrom := map[string]bool{}
	for _, to := range renamedTo {
		renamedFrom[to] = true
	}
	for _, n := range a.Nodes {
		target, ok := bByName[n.Name]
		if !ok {
			to, renamed := renamedTo[n.Name]
			if !renamed {
				d.NodesRemoved = append(d.NodesRemoved, n.Name)
				continue
			}
			target = bByName[to]
		}
		label := target.Name
		if n.Type != target.Type {
			d.TypeChanges = append(d.TypeChanges, ParamChange{Node: label, Path: "type", Before: n.Type, After: target.Type})
		}
		if n.TypeVersion != target.TypeVersion {
			d.TypeChanges = append(d.TypeChanges, ParamChange{Node: label, Path: "typeVersion", Before: n.TypeVersion, After: target.TypeVersion})
		}
		for _, k := range nodeFlagKeys {
			if !reflect.DeepEqual(n.Flags[k], target.Flags[k]) {
				d.ParamChanges = append(d.ParamChanges, ParamChange{Node: label, Path: k, Before: n.Flags[k], After: target.Flags[k]})
			}
		}
		if n.Disabled != target.Disabled {
			d.DisabledChanges = append(d.DisabledChanges, ParamChange{Node: label, Path: "disabled", Before: n.Disabled, After: target.Disabled})
		}
		if !reflect.DeepEqual(credMap(n.Credentials), credMap(target.Credentials)) {
			d.CredentialChanges = append(d.CredentialChanges, ParamChange{Node: label, Path: "credentials", Before: credMap(n.Credentials), After: credMap(target.Credentials)})
		}
		d.ParamChanges = append(d.ParamChanges, paramDiff(label, decode(n.Parameters), decode(target.Parameters))...)
	}
	for _, n := range b.Nodes {
		if _, ok := aByName[n.Name]; !ok && !renamedFrom[n.Name] {
			d.NodesAdded = append(d.NodesAdded, n.Name)
		}
	}
	rename := func(e Edge) Edge {
		if to, ok := renamedTo[e.From]; ok {
			e.From = to
		}
		if to, ok := renamedTo[e.To]; ok {
			e.To = to
		}
		return e
	}
	aEdges := map[string]Edge{}
	for _, e := range a.Edges {
		e = rename(e)
		aEdges[edgeKey(e)] = e
	}
	bEdges := map[string]Edge{}
	for _, e := range b.Edges {
		bEdges[edgeKey(e)] = e
	}
	for k, e := range bEdges {
		if _, ok := aEdges[k]; !ok {
			d.EdgesAdded = append(d.EdgesAdded, e)
		}
	}
	for k, e := range aEdges {
		if _, ok := bEdges[k]; !ok {
			d.EdgesRemoved = append(d.EdgesRemoved, e)
		}
	}
	sort.Slice(d.EdgesAdded, func(i, j int) bool { return edgeKey(d.EdgesAdded[i]) < edgeKey(d.EdgesAdded[j]) })
	sort.Slice(d.EdgesRemoved, func(i, j int) bool { return edgeKey(d.EdgesRemoved[i]) < edgeKey(d.EdgesRemoved[j]) })
	sort.Strings(d.NodesAdded)
	sort.Strings(d.NodesRemoved)
	d.ActiveChanged = a.Active != b.Active
	if a.Name != b.Name && a.Name != "" && b.Name != "" {
		d.NameChange = &ParamChange{Path: "name", Before: a.Name, After: b.Name}
	}
	// Saved-version payloads may omit settings; compare only when both sides carry them.
	if a.Settings != nil && b.Settings != nil {
		as, bs := withoutKey(a.Settings, "errorWorkflow"), withoutKey(b.Settings, "errorWorkflow")
		walkDiff("", "settings", as, bs, &d.SettingsChanges)
	}
	sort.Slice(d.SettingsChanges, func(i, j int) bool { return d.SettingsChanges[i].Path < d.SettingsChanges[j].Path })
	if a.Settings != nil && b.Settings != nil && a.ErrorWorkflow != b.ErrorWorkflow {
		d.ErrorWorkflowDelta = &ParamChange{Path: "errorWorkflow", Before: a.ErrorWorkflow, After: b.ErrorWorkflow}
	}
	return d
}

func withoutKey(m map[string]any, key string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}

func byName(nodes []Node) map[string]Node {
	m := make(map[string]Node, len(nodes))
	for _, n := range nodes {
		m[n.Name] = n
	}
	return m
}

func credMap(refs []CredRef) map[string]string {
	m := map[string]string{}
	for _, c := range refs {
		m[c.Type] = c.ID
	}
	return m
}

func decode(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return map[string]any{}
	}
	return v
}

func paramDiff(node string, a, b any) []ParamChange {
	out := []ParamChange{}
	walkDiff(node, "parameters", a, b, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func walkDiff(node, path string, a, b any, out *[]ParamChange) {
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		keys := map[string]bool{}
		for k := range am {
			keys[k] = true
		}
		for k := range bm {
			keys[k] = true
		}
		for k := range keys {
			walkDiff(node, path+"."+k, am[k], bm[k], out)
		}
		return
	}
	if !reflect.DeepEqual(a, b) {
		*out = append(*out, ParamChange{Node: node, Path: path, Before: a, After: b})
	}
}

// SetNodeParam sets a dotted parameter path on the named node of a raw
// workflow body and returns the previous value. Intermediate objects are
// created as needed. The returned body keeps every other field untouched.
func SetNodeParam(body map[string]any, nodeName, path string, value any) (any, error) {
	node, err := findNode(body, nodeName)
	if err != nil {
		return nil, err
	}
	params, _ := node["parameters"].(map[string]any)
	if params == nil {
		params = map[string]any{}
		node["parameters"] = params
	}
	parts := strings.Split(strings.TrimPrefix(path, "parameters."), ".")
	cur := params
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			if cur[p] != nil {
				return nil, fmt.Errorf("parameter %q on node %q is not an object", p, nodeName)
			}
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	last := parts[len(parts)-1]
	prev := cur[last]
	cur[last] = value
	return prev, nil
}

// SetNodeDisabled toggles a node's disabled flag and returns the previous value.
func SetNodeDisabled(body map[string]any, nodeName string, disabled bool) (bool, error) {
	node, err := findNode(body, nodeName)
	if err != nil {
		return false, err
	}
	prev, _ := node["disabled"].(bool)
	if disabled {
		node["disabled"] = true
	} else {
		delete(node, "disabled")
	}
	return prev, nil
}

// RenameNode renames a node and rewrites every connection that references it.
func RenameNode(body map[string]any, from, to string) error {
	if _, err := findNode(body, to); err == nil {
		return fmt.Errorf("a node named %q already exists", to)
	}
	node, err := findNode(body, from)
	if err != nil {
		return err
	}
	node["name"] = to
	rewriteNodeRefs(body, from, to)
	conns, _ := body["connections"].(map[string]any)
	if conns == nil {
		return nil
	}
	if v, ok := conns[from]; ok {
		conns[to] = v
		delete(conns, from)
	}
	for _, kinds := range conns {
		km, _ := kinds.(map[string]any)
		for _, outputs := range km {
			outs, _ := outputs.([]any)
			for _, targets := range outs {
				ts, _ := targets.([]any)
				for _, t := range ts {
					if tm, ok := t.(map[string]any); ok && tm["node"] == from {
						tm["node"] = to
					}
				}
			}
		}
	}
	return nil
}

// rewriteNodeRefs updates expression references to a renamed node in every
// node's parameters: $('Name'), $("Name"), $node["Name"] and $node['Name'].
func rewriteNodeRefs(body map[string]any, from, to string) {
	// Names are JS string literals inside expressions, so escape per quote style.
	sq := func(n string) string { return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(n) }
	dq := func(n string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(n) }
	r := strings.NewReplacer(
		"$('"+sq(from)+"')", "$('"+sq(to)+"')", `$("`+dq(from)+`")`, `$("`+dq(to)+`")`,
		`$node["`+dq(from)+`"]`, `$node["`+dq(to)+`"]`, "$node['"+sq(from)+"']", "$node['"+sq(to)+"']",
	)
	var walk func(v any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case string:
			return r.Replace(t)
		case map[string]any:
			for k, x := range t {
				t[k] = walk(x)
			}
		case []any:
			for i, x := range t {
				t[i] = walk(x)
			}
		}
		return v
	}
	nodes, _ := body["nodes"].([]any)
	for _, n := range nodes {
		if m, ok := n.(map[string]any); ok {
			if p, ok := m["parameters"]; ok {
				m["parameters"] = walk(p)
			}
		}
	}
	if pd, ok := body["pinData"].(map[string]any); ok {
		if v, ok := pd[from]; ok {
			pd[to] = v
			delete(pd, from)
		}
	}
}

func findNode(body map[string]any, name string) (map[string]any, error) {
	nodes, _ := body["nodes"].([]any)
	for _, n := range nodes {
		if m, ok := n.(map[string]any); ok && m["name"] == name {
			return m, nil
		}
	}
	return nil, fmt.Errorf("node %q not found", name)
}

// UpdatableBody returns the subset of a workflow body accepted by
// PUT /workflows/{id}: name, nodes, connections, settings, staticData.
// The public API rejects read-only fields such as id, active, and tags.
func UpdatableBody(body map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"name", "nodes", "connections", "settings", "staticData"} {
		if v, ok := body[k]; ok && v != nil {
			out[k] = v
		}
	}
	if s, ok := out["settings"].(map[string]any); ok {
		out["settings"] = allowedSettings(s)
	}
	return out
}

// writableSettings mirrors components.schemas.workflowSettings in the n8n
// public API spec, which sets additionalProperties: false; a PUT carrying any
// other key is rejected, so unknown keys must be dropped (and reported).
var writableSettings = map[string]bool{
	"availableInMCP": true, "binaryMode": true, "callerIds": true, "callerPolicy": true,
	"credentialResolverId": true, "customTelemetryTags": true, "errorWorkflow": true,
	"executionOrder": true, "executionTimeout": true, "redactionPolicy": true,
	"saveDataErrorExecution": true, "saveDataSuccessExecution": true, "saveExecutionProgress": true,
	"saveManualExecutions": true, "timeSavedMode": true, "timeSavedPerExecution": true, "timezone": true,
}

func allowedSettings(s map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range s {
		if writableSettings[k] {
			out[k] = v
		}
	}
	return out
}

// derivedSettings are server-derived per the spec ("not intended to be set
// programmatically"); the server fills them in, so they never signal a user change.
var derivedSettings = []string{"binaryMode", "credentialResolverId"}

func comparableBody(body map[string]any) map[string]any {
	out := UpdatableBody(body)
	if s, ok := out["settings"].(map[string]any); ok {
		for _, k := range derivedSettings {
			s = withoutKey(s, k)
		}
		out["settings"] = s
	}
	return out
}

// DroppedSettings lists settings keys in body that UpdatableBody cannot send.
func DroppedSettings(body map[string]any) []string {
	s, _ := body["settings"].(map[string]any)
	out := []string{}
	for k := range s {
		if !writableSettings[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// BodiesDiffer reports whether two workflow bodies differ in any field a PUT
// would write, including fields Diff ignores as layout noise (positions,
// webhookId, credential names, staticData).
func BodiesDiffer(a, b map[string]any) bool {
	na, _ := json.Marshal(comparableBody(a))
	nb, _ := json.Marshal(comparableBody(b))
	var x, y any
	_ = json.Unmarshal(na, &x)
	_ = json.Unmarshal(nb, &y)
	return !reflect.DeepEqual(x, y)
}
