// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.

package n8nwf

import (
	"encoding/json"
	"fmt"
	"testing"
)

const sampleWorkflow = `{
  "id": "wf1", "name": "Order intake", "active": true, "isArchived": false,
  "settings": {"errorWorkflow": "errwf"},
  "nodes": [
    {"id": "n1", "name": "Webhook", "type": "n8n-nodes-base.webhook", "typeVersion": 2,
     "parameters": {"path": "/orders/new/", "httpMethod": "POST"}},
    {"id": "n2", "name": "Notify", "type": "n8n-nodes-base.slack", "typeVersion": 2.2,
     "credentials": {"slackApi": {"id": "c1", "name": "Slack Bot"}},
     "parameters": {"text": "={{ $vars.CHANNEL }} {{ $vars.CHANNEL }} {{ $vars.ENV_NAME }}"}},
    {"id": "n3", "name": "Call Sub", "type": "n8n-nodes-base.executeWorkflow", "typeVersion": 1.2,
     "parameters": {"workflowId": {"__rl": true, "value": "sub9", "mode": "id"}}, "disabled": true}
  ],
  "connections": {
    "Webhook": {"main": [[{"node": "Notify", "type": "main", "index": 0}, {"node": "Call Sub", "type": "main", "index": 0}]]}
  }
}`

func mustParse(t *testing.T, s string) Workflow {
	t.Helper()
	wf, err := ParseWorkflow([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return wf
}

func TestParseWorkflow(t *testing.T) {
	wf := mustParse(t, sampleWorkflow)
	if wf.ID != "wf1" || !wf.Active || wf.ErrorWorkflow != "errwf" {
		t.Fatalf("header fields wrong: %+v", wf)
	}
	if len(wf.Nodes) != 3 || len(wf.Edges) != 2 {
		t.Fatalf("got %d nodes %d edges", len(wf.Nodes), len(wf.Edges))
	}
	tests := []struct {
		node  int
		check func(Node) bool
		desc  string
	}{
		{0, func(n Node) bool { return n.WebhookPath == "orders/new" }, "webhook path trimmed"},
		{1, func(n Node) bool {
			return len(n.Credentials) == 1 && n.Credentials[0].ID == "c1" && n.Credentials[0].Type == "slackApi"
		}, "credential ref"},
		{1, func(n Node) bool {
			return len(n.VarRefs) == 2 && n.VarRefs[0] == "CHANNEL" && n.VarRefs[1] == "ENV_NAME"
		}, "var refs deduped and sorted"},
		{2, func(n Node) bool { return n.SubworkflowID == "sub9" && n.Disabled }, "resource locator subworkflow"},
	}
	for _, tc := range tests {
		if !tc.check(wf.Nodes[tc.node]) {
			t.Errorf("%s: %+v", tc.desc, wf.Nodes[tc.node])
		}
	}
}

func TestUsageQuery(t *testing.T) {
	wf := mustParse(t, sampleWorkflow)
	tests := []struct {
		q    UsageQuery
		want []string
	}{
		{UsageQuery{Credential: "c1"}, []string{"Notify"}},
		{UsageQuery{Credential: "slack bot"}, []string{"Notify"}},
		{UsageQuery{NodeType: "slack"}, []string{"Notify"}},
		{UsageQuery{NodeType: "n8n-nodes-base.webhook"}, []string{"Webhook"}},
		{UsageQuery{Webhook: "orders/new"}, []string{"Webhook"}},
		{UsageQuery{Variable: "$vars.ENV_NAME"}, []string{"Notify"}},
		{UsageQuery{Subworkflow: "sub9"}, []string{"Call Sub"}},
		{UsageQuery{NodeType: "slack", Credential: "nope"}, nil},
		{UsageQuery{Credential: "Slack"}, nil}, // name match is exact, not substring
		{UsageQuery{}, nil},
	}
	for _, tc := range tests {
		var got []string
		for _, n := range wf.Nodes {
			if tc.q.Matches(n) {
				got = append(got, n.Name)
			}
		}
		if len(got) != len(tc.want) {
			t.Errorf("%+v: got %v want %v", tc.q, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%+v: got %v want %v", tc.q, got, tc.want)
			}
		}
	}
}

func TestExecutionFailure(t *testing.T) {
	tests := []struct {
		in   string
		want Failure
	}{
		{`{"data":{"resultData":{"lastNodeExecuted":"Boom","error":{"message":"bad thing","node":null}}}}`, Failure{Node: "Boom", Message: "bad thing"}},
		{`{"data":{"resultData":{"lastNodeExecuted":"A","error":{"message":"m","node":{"name":"B"}}}}}`, Failure{Node: "B", Message: "m"}},
		{`{"data":{"resultData":{"lastNodeExecuted":"Done"}}}`, Failure{Node: "Done"}},
		{`not json`, Failure{}},
	}
	for _, tc := range tests {
		if got := ExecutionFailure([]byte(tc.in)); got != tc.want {
			t.Errorf("ExecutionFailure(%s) = %+v want %+v", tc.in, got, tc.want)
		}
	}
}

func TestDiffNoChangeIgnoresNoise(t *testing.T) {
	a := mustParse(t, sampleWorkflow)
	var body map[string]any
	_ = json.Unmarshal([]byte(sampleWorkflow), &body)
	for _, n := range body["nodes"].([]any) {
		n.(map[string]any)["position"] = []any{999, 999}
	}
	body["updatedAt"] = "2030-01-01"
	raw, _ := json.Marshal(body)
	b := mustParse(t, string(raw))
	if d := Diff(a, b); d.Changed() {
		t.Fatalf("expected no change, got %+v", d)
	}
}

func TestDiffDetectsChanges(t *testing.T) {
	a := mustParse(t, sampleWorkflow)
	var body map[string]any
	_ = json.Unmarshal([]byte(sampleWorkflow), &body)
	if _, err := SetNodeParam(body, "Notify", "text", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := RenameNode(body, "Call Sub", "Run Child"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetNodeDisabled(body, "Run Child", false); err != nil {
		t.Fatal(err)
	}
	nodes := body["nodes"].([]any)
	body["nodes"] = append(nodes, map[string]any{"id": "n4", "name": "Log", "type": "n8n-nodes-base.noOp", "parameters": map[string]any{}})
	body["active"] = false
	raw, _ := json.Marshal(body)
	b := mustParse(t, string(raw))
	d := Diff(a, b)
	if len(d.ParamChanges) != 1 || d.ParamChanges[0].Path != "parameters.text" || d.ParamChanges[0].After != "hello" {
		t.Errorf("param changes: %+v", d.ParamChanges)
	}
	if len(d.NodesRenamed) != 1 || d.NodesRenamed[0].To != "Run Child" {
		t.Errorf("renames: %+v", d.NodesRenamed)
	}
	if len(d.NodesAdded) != 1 || d.NodesAdded[0] != "Log" || len(d.NodesRemoved) != 0 {
		t.Errorf("added/removed: %v %v", d.NodesAdded, d.NodesRemoved)
	}
	if len(d.DisabledChanges) != 1 || !d.ActiveChanged {
		t.Errorf("disabled/active: %+v %v", d.DisabledChanges, d.ActiveChanged)
	}
	if len(d.EdgesAdded) != 0 || len(d.EdgesRemoved) != 0 {
		t.Errorf("rename must not show as edge churn: +%v -%v", d.EdgesAdded, d.EdgesRemoved)
	}
}

func TestRenameNodeRejectsClash(t *testing.T) {
	var body map[string]any
	_ = json.Unmarshal([]byte(sampleWorkflow), &body)
	if err := RenameNode(body, "Notify", "Webhook"); err == nil {
		t.Fatal("expected clash error")
	}
	if _, err := SetNodeParam(body, "Missing", "x", 1); err == nil {
		t.Fatal("expected missing-node error")
	}
}

func TestUpdatableBody(t *testing.T) {
	var body map[string]any
	_ = json.Unmarshal([]byte(sampleWorkflow), &body)
	body["settings"].(map[string]any)["bogus"] = true
	out := UpdatableBody(body)
	for _, k := range []string{"id", "active", "isArchived"} {
		if _, ok := out[k]; ok {
			t.Errorf("read-only key %q kept", k)
		}
	}
	if _, ok := out["settings"].(map[string]any)["bogus"]; ok {
		t.Error("unknown settings key kept")
	}
	if out["settings"].(map[string]any)["errorWorkflow"] != "errwf" {
		t.Error("allowed settings key dropped")
	}
}

func TestDiffSeesVersionFlagsSettingsName(t *testing.T) {
	a := mustParse(t, sampleWorkflow)
	var body map[string]any
	_ = json.Unmarshal([]byte(sampleWorkflow), &body)
	nodes := body["nodes"].([]any)
	nodes[1].(map[string]any)["typeVersion"] = 3
	nodes[1].(map[string]any)["onError"] = "continueRegularOutput"
	body["settings"].(map[string]any)["executionTimeout"] = 60
	body["name"] = "Order intake v2"
	raw, _ := json.Marshal(body)
	d := Diff(a, mustParse(t, string(raw)))
	if len(d.TypeChanges) != 1 || d.TypeChanges[0].Path != "typeVersion" {
		t.Errorf("typeVersion: %+v", d.TypeChanges)
	}
	if len(d.ParamChanges) != 1 || d.ParamChanges[0].Path != "onError" {
		t.Errorf("node flag: %+v", d.ParamChanges)
	}
	if len(d.SettingsChanges) != 1 || d.SettingsChanges[0].Path != "settings.executionTimeout" {
		t.Errorf("settings: %+v", d.SettingsChanges)
	}
	if d.NameChange == nil || !d.ContentChanged() {
		t.Errorf("name/content: %+v", d)
	}
}

func TestActiveOnlyIsNotContentChange(t *testing.T) {
	a := mustParse(t, sampleWorkflow)
	var body map[string]any
	_ = json.Unmarshal([]byte(sampleWorkflow), &body)
	body["active"] = false
	raw, _ := json.Marshal(body)
	d := Diff(a, mustParse(t, string(raw)))
	if !d.Changed() || d.ContentChanged() {
		t.Fatalf("changed=%v content=%v", d.Changed(), d.ContentChanged())
	}
}

func TestRenameRewritesExpressions(t *testing.T) {
	body := map[string]any{
		"nodes": []any{
			map[string]any{"name": "Fetch", "parameters": map[string]any{}},
			map[string]any{"name": "Use", "parameters": map[string]any{"v": "={{ $('Fetch').item.json.x }} {{ $node[\"Fetch\"].json.y }}"}},
		},
		"connections": map[string]any{},
		"pinData":     map[string]any{"Fetch": []any{}},
	}
	if err := RenameNode(body, "Fetch", "Get"); err != nil {
		t.Fatal(err)
	}
	v := body["nodes"].([]any)[1].(map[string]any)["parameters"].(map[string]any)["v"].(string)
	if v != "={{ $('Get').item.json.x }} {{ $node[\"Get\"].json.y }}" {
		t.Errorf("expression not rewritten: %s", v)
	}
	if _, ok := body["pinData"].(map[string]any)["Get"]; !ok {
		t.Error("pinData key not renamed")
	}
}

func TestDiffIgnoresMissingSettingsSide(t *testing.T) {
	a := mustParse(t, sampleWorkflow)
	var body map[string]any
	_ = json.Unmarshal([]byte(sampleWorkflow), &body)
	delete(body, "settings")
	delete(body, "name")
	raw, _ := json.Marshal(body)
	d := Diff(mustParse(t, string(raw)), a)
	if len(d.SettingsChanges) != 0 || d.ErrorWorkflowDelta != nil || d.NameChange != nil {
		t.Fatalf("missing settings/name must not diff: %+v", d)
	}
}

func TestBodiesDifferAndDroppedSettings(t *testing.T) {
	var a, b map[string]any
	_ = json.Unmarshal([]byte(sampleWorkflow), &a)
	_ = json.Unmarshal([]byte(sampleWorkflow), &b)
	if BodiesDiffer(a, b) {
		t.Fatal("identical bodies differ")
	}
	b["nodes"].([]any)[0].(map[string]any)["position"] = []any{1, 2}
	if !BodiesDiffer(a, b) {
		t.Fatal("position change must count for writes")
	}
	a["settings"].(map[string]any)["futureKey"] = 1
	if got := DroppedSettings(a); len(got) != 1 || got[0] != "futureKey" {
		t.Fatalf("dropped: %v", got)
	}
}

func TestBodiesDifferIgnoresDerivedSettings(t *testing.T) {
	var a, b map[string]any
	_ = json.Unmarshal([]byte(sampleWorkflow), &a)
	_ = json.Unmarshal([]byte(sampleWorkflow), &b)
	b["settings"].(map[string]any)["binaryMode"] = "separate"
	b["settings"].(map[string]any)["credentialResolverId"] = "r1"
	if BodiesDiffer(a, b) {
		t.Fatal("server-derived settings must not count as a change")
	}
}

func TestRenameEscapesQuotesInExpressions(t *testing.T) {
	body := map[string]any{
		"nodes": []any{
			map[string]any{"name": "Fetch", "parameters": map[string]any{}},
			map[string]any{"name": "Use", "parameters": map[string]any{"v": `={{ $('Fetch').item }} {{ $node["Fetch"].json }}`}},
		},
		"connections": map[string]any{},
	}
	if err := RenameNode(body, "Fetch", `Customer's "Data"`); err != nil {
		t.Fatal(err)
	}
	v := body["nodes"].([]any)[1].(map[string]any)["parameters"].(map[string]any)["v"].(string)
	if want := `={{ $('Customer\'s "Data"').item }} {{ $node["Customer's \"Data\""].json }}`; v != want {
		t.Errorf("got %s\nwant %s", v, want)
	}
}

func TestDiffSeesMergeInputRewiring(t *testing.T) {
	wf := func(index int) Workflow {
		return mustParse(t, fmt.Sprintf(`{"id":"w","name":"w","nodes":[{"name":"A","type":"x"},{"name":"M","type":"n8n-nodes-base.merge"}],
		"connections":{"A":{"main":[[{"node":"M","type":"main","index":%d}]]}}}`, index))
	}
	d := Diff(wf(0), wf(1))
	if !d.ContentChanged() || len(d.EdgesAdded) != 1 || d.EdgesAdded[0].Input != 1 {
		t.Fatalf("input rewiring not detected: %+v", d)
	}
}
