package cli

// pp:data-source local

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
)

type crocoAuditSnapshot struct {
	Capture     store.CrocoCapture
	Rows        []map[string]any
	Unavailable bool
}
type crocoAuditInventory struct {
	Snapshot      crocoAuditSnapshot
	IDs           map[string]bool
	Usable        bool
	AbsenceProven bool
	Reason        string
}

func newNovelRelationsAuditCmd(f *rootFlags) *cobra.Command { return newCrocoRelationAuditCommand(f) }
func newCrocoRelationAuditCommand(f *rootFlags) *cobra.Command {
	var db string
	limit := 100
	cmd := crocoLocalCommand(f, &cobra.Command{Use: "audit [relation-id]", Short: "Audit saved relation edges against typed local inventories"}, func(ctx context.Context, cmd *cobra.Command, args []string) (any, error) {
		if len(args) > 1 {
			return nil, usageErr(fmt.Errorf("relations audit accepts at most one relation ID"))
		}
		id := ""
		var e error
		if len(args) == 1 {
			id, e = crocoID(args, 0)
			if e != nil {
				return nil, e
			}
		}
		if e = crocoPageBounds(limit, 0); e != nil {
			return nil, e
		}
		cap, rows, e := crocoLoadCapture(ctx, db, "engine:relations")
		if e != nil {
			return nil, e
		}
		defs := crocoAuditSnapshot{Capture: cap, Rows: rows}
		loaded := map[string]crocoAuditSnapshot{}
		load := func(resource string) {
			if resource == "" {
				return
			}
			if _, ok := loaded[resource]; ok {
				return
			}
			c, r, e := crocoLoadCapture(ctx, db, resource)
			loaded[resource] = crocoAuditSnapshot{Capture: c, Rows: r, Unavailable: e != nil}
		}
		for _, d := range rows {
			rid := crocoString(d["id"])
			if id != "" && rid != id {
				continue
			}
			load("relations:" + rid)
			a, _ := d["args"].(map[string]any)
			for _, side := range []string{"parent", "child"} {
				resource, _, _ := crocoAuditEndpoint(crocoString(a[side+"_object"]))
				load(resource)
			}
		}
		return crocoComputeRelationAudit(defs, loaded, id, limit)
	})
	cmd.Long = "Use this command for relation edges whose referenced entities are missing or duplicated in a complete local snapshot. Do NOT use this command for differences in entity field values; use 'crocoblock-pp-cli cct reconcile' instead. Definitions and edges must come from the same site and JetEngine version. Missing or restricted inventories produce unknown findings. WordPress status:any excludes trash, so an absent post cannot be certified as dangling. Complete unfiltered CCT inventories can establish absence within their captured view. This analysis describes the saved capture times, not current live integrity. --limit bounds displayed findings and relation summaries; scanned counts cover all selected rows."
	cmd.Flags().StringVar(&db, "db", "", "SQLite snapshot containing definitions, relation edges and endpoint inventories")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum findings and relation summaries displayed, from 1 to 500")
	cmd.Annotations["mcp:read-flags"] = "db"
	cmd.Annotations["pp:verified-by-proof"] = "crocoblock-local-analysis-lifecycle.md"
	cmd.Example = "  crocoblock-pp-cli relations audit 2 --db candidate.db --agent\n  crocoblock-pp-cli relations audit --db candidate.db --limit 100 --json"
	return cmd
}
func crocoAuditEndpoint(endpoint string) (resource, idKey, reason string) {
	kind, slug, ok := strings.Cut(endpoint, "::")
	if !ok {
		return "", "", "unsupported or unspecified endpoint type"
	}
	if _, e := crocoSlug(slug); e != nil {
		return "", "", "invalid endpoint type slug"
	}
	switch kind {
	case "posts":
		return "objects:" + slug, "id", ""
	case "cct":
		return "cct:" + slug, "_ID", ""
	default:
		return "", "", "this endpoint type has no verified inventory adapter"
	}
}
func crocoAuditCaptureCompatible(cap store.CrocoCapture, base store.CrocoCapture, resource string) string {
	if cap.Resource != resource || cap.Site == "" || cap.CapturedAt == "" {
		return "capture provenance is missing or names another resource"
	}
	if cap.Site != base.Site {
		return "inventory belongs to a different site"
	}
	if cap.Versions["jet-engine"] == "" || cap.Versions["jet-engine"] != base.Versions["jet-engine"] {
		return "JetEngine capture versions are missing or incompatible"
	}
	return ""
}
func crocoComputeRelationAudit(defs crocoAuditSnapshot, loaded map[string]crocoAuditSnapshot, requested string, limit int) (map[string]any, error) {
	if e := crocoPageBounds(limit, 0); e != nil {
		return nil, e
	}
	if requested != "" {
		if _, e := crocoID([]string{requested}, 0); e != nil {
			return nil, e
		}
	}
	if defs.Unavailable {
		return nil, configErr(fmt.Errorf("relation definitions capture is unavailable; sync relations first"))
	}
	if e := crocoRequireComplete(defs.Capture); e != nil {
		return nil, e
	}
	if e := crocoRequireVersion(defs.Capture, "jet-engine"); e != nil {
		return nil, e
	}
	if defs.Capture.Resource != "engine:relations" {
		return nil, configErr(fmt.Errorf("expected engine:relations definitions capture"))
	}
	if len(defs.Capture.Scope) != 0 {
		return nil, configErr(fmt.Errorf("relation definition scope is restricted or unknown; sync the complete definitions collection"))
	}
	findings := []any{}
	summaries := []any{}
	inventoryReports := map[string]any{}
	inventories := map[string]crocoAuditInventory{}
	counts := map[string]int{"definitions": len(defs.Rows), "relations": 0, "edges": 0, "inventory_rows": 0, "valid_edges": 0, "duplicate_edges": 0, "duplicate_inventory_ids": 0, "dangling_endpoints": 0, "unknown": 0}
	analysisComplete := true
	add := func(finding map[string]any) {
		findings = append(findings, finding)
		kind := crocoString(finding["kind"])
		if strings.HasPrefix(kind, "unknown") || kind == "invalid_edge" || kind == "invalid_inventory" {
			counts["unknown"]++
			analysisComplete = false
		}
	}
	loadInventory := func(endpoint string) crocoAuditInventory {
		resource, idKey, reason := crocoAuditEndpoint(endpoint)
		if resource == "" {
			return crocoAuditInventory{Reason: reason}
		}
		if cached, ok := inventories[resource]; ok {
			return cached
		}
		snap, present := loaded[resource]
		result := crocoAuditInventory{Snapshot: snap, IDs: map[string]bool{}}
		if !present || snap.Unavailable {
			result.Reason = "inventory capture is missing or unreadable"
		} else {
			result.Reason = crocoAuditCaptureCompatible(snap.Capture, defs.Capture, resource)
		}
		if result.Reason == "" {
			fields := map[string]bool{}
			for _, f := range snap.Capture.Fields {
				fields[f] = true
			}
			if !fields[idKey] {
				result.Reason = "inventory did not capture the object ID field"
			}
		}
		if result.Reason == "" {
			result.Usable = true
			counts["inventory_rows"] += len(snap.Rows)
			for _, row := range snap.Rows {
				id, e := crocoID([]string{crocoString(row[idKey])}, 0)
				if e != nil {
					result.Usable = false
					result.Reason = "inventory contains a row without a valid object ID"
					add(map[string]any{"kind": "invalid_inventory", "resource": resource, "reason": result.Reason})
					continue
				}
				if strings.HasPrefix(resource, "objects:") && crocoString(row["type"]) != strings.TrimPrefix(resource, "objects:") {
					result.Usable = false
					result.Reason = "inventory contains an object of another or missing type"
					add(map[string]any{"kind": "invalid_inventory", "resource": resource, "object_id": id, "reason": result.Reason})
					continue
				}
				if result.IDs[id] {
					counts["duplicate_inventory_ids"]++
					add(map[string]any{"kind": "duplicate_inventory_id", "resource": resource, "object_id": id})
				}
				result.IDs[id] = true
			}
			if result.Reason == "" && !snap.Capture.Complete {
				result.Reason = "inventory collection is incomplete"
			}
			if result.Reason == "" {
				if strings.HasPrefix(resource, "cct:") && reflect.DeepEqual(snap.Capture.Scope, map[string]any{"type": strings.TrimPrefix(resource, "cct:")}) {
					result.AbsenceProven = true
				} else if strings.HasPrefix(resource, "objects:") {
					result.Reason = "WordPress inventory covers accessible non-trash objects; absent objects may be trashed or restricted"
				} else {
					result.Reason = "inventory scope is filtered or not known to include every endpoint"
				}
			}
		}
		inventories[resource] = result
		inventoryReports[resource] = map[string]any{"capture": snap.Capture, "available": present && !snap.Unavailable, "scanned_rows": len(snap.Rows), "absence_verifiable": result.AbsenceProven, "limitation": result.Reason}
		return result
	}
	seenDefinitions := map[string]bool{}
	found := requested == ""
	for _, definition := range defs.Rows {
		id, e := crocoID([]string{crocoString(definition["id"])}, 0)
		if e != nil {
			return nil, configErr(fmt.Errorf("relation definition has an invalid ID"))
		}
		if seenDefinitions[id] {
			return nil, configErr(fmt.Errorf("relation definition ID %s is duplicated", id))
		}
		seenDefinitions[id] = true
		if requested != "" && id != requested {
			continue
		}
		found = true
		counts["relations"]++
		args, _ := definition["args"].(map[string]any)
		parentType, childType := crocoString(args["parent_object"]), crocoString(args["child_object"])
		startFindings := len(findings)
		inventoryBySide := map[string]crocoAuditInventory{"parent": loadInventory(parentType), "child": loadInventory(childType)}
		edgeResource := "relations:" + id
		edges, present := loaded[edgeResource]
		relationComplete := true
		if !present || edges.Unavailable {
			add(map[string]any{"kind": "unknown_edges", "relation_id": id, "reason": "edge capture is missing or unreadable"})
			relationComplete = false
		} else if why := crocoAuditCaptureCompatible(edges.Capture, defs.Capture, edgeResource); why != "" {
			add(map[string]any{"kind": "unknown_edges", "relation_id": id, "reason": why})
			relationComplete = false
			edges.Rows = nil
		} else {
			if !reflect.DeepEqual(edges.Capture.Scope, map[string]any{"relation_id": id}) {
				add(map[string]any{"kind": "unknown_edges", "relation_id": id, "reason": "edge scope is restricted or names another relation"})
				relationComplete = false
				edges.Rows = nil
			}
			if !edges.Capture.Complete {
				add(map[string]any{"kind": "unknown_edges", "relation_id": id, "reason": "edge capture is incomplete"})
				relationComplete = false
			}
			have := map[string]bool{}
			for _, k := range edges.Capture.Fields {
				have[k] = true
			}
			if !have["parent_object_id"] || !have["child_object_id"] {
				add(map[string]any{"kind": "unknown_edges", "relation_id": id, "reason": "edge endpoint fields were not captured"})
				relationComplete = false
				edges.Rows = nil
			}
		}
		for _, side := range []string{"parent", "child"} {
			inv := inventoryBySide[side]
			// A complete restricted inventory still establishes membership for observed IDs.
			unusable := !inv.Usable || !inv.Snapshot.Capture.Complete
			if unusable {
				add(map[string]any{"kind": "unknown_inventory", "relation_id": id, "side": side, "reason": inv.Reason})
				relationComplete = false
			}
		}
		seenEdges := map[string]bool{}
		validEdges := 0
		for _, edge := range edges.Rows {
			counts["edges"]++
			parent, pe := crocoID([]string{crocoString(edge["parent_object_id"])}, 0)
			child, ce := crocoID([]string{crocoString(edge["child_object_id"])}, 0)
			if pe != nil || ce != nil {
				add(map[string]any{"kind": "invalid_edge", "relation_id": id, "reason": "edge has a missing or invalid endpoint ID"})
				relationComplete = false
				continue
			}
			identity := parent + ":" + child
			if seenEdges[identity] {
				counts["duplicate_edges"]++
				add(map[string]any{"kind": "duplicate_edge", "relation_id": id, "parent_id": parent, "child_id": child})
				continue
			}
			seenEdges[identity] = true
			valid := true
			for _, side := range []string{"parent", "child"} {
				objectID := parent
				if side == "child" {
					objectID = child
				}
				inv := inventoryBySide[side]
				if inv.IDs[objectID] {
					continue
				}
				valid = false
				finding := map[string]any{"relation_id": id, "parent_id": parent, "child_id": child, "side": side, "object_id": objectID, "inventory": inv.Snapshot.Capture.Resource}
				if inv.AbsenceProven {
					finding["kind"] = "dangling_endpoint"
					finding["reason"] = "object is absent from the complete unfiltered captured CCT inventory"
					counts["dangling_endpoints"]++
				} else {
					finding["kind"] = "unknown_endpoint"
					finding["reason"] = inv.Reason
					relationComplete = false
				}
				add(finding)
			}
			if valid {
				validEdges++
				counts["valid_edges"]++
			}
		}
		summaries = append(summaries, map[string]any{"relation_id": id, "parent_type": parentType, "child_type": childType, "edge_capture": edges.Capture, "scanned_edges": len(edges.Rows), "valid_edges": validEdges, "finding_count": len(findings) - startFindings, "analysis_complete": relationComplete})
	}
	if !found {
		return nil, notFoundErr(fmt.Errorf("relation %s is absent from the complete definitions capture", requested))
	}
	totalFindings, totalRelations := len(findings), len(summaries)
	if len(findings) > limit {
		findings = findings[:limit]
	}
	if len(summaries) > limit {
		summaries = summaries[:limit]
	}
	keys := make([]string, 0, len(inventoryReports))
	for k := range inventoryReports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	reports := []any{}
	for _, k := range keys {
		if len(reports) == limit {
			break
		}
		reports = append(reports, map[string]any{"resource": k, "coverage": inventoryReports[k]})
	}
	return map[string]any{"definitions": defs.Capture, "analysis_complete": analysisComplete, "findings": findings, "relations": summaries, "inventories": reports, "scanned": counts, "total_findings": totalFindings, "total_relations": totalRelations, "total_inventories": len(keys), "truncated": totalFindings > limit || totalRelations > limit || len(keys) > limit, "limit": limit, "scope_note": "Findings describe captured views at their recorded times. status:any excludes WordPress trash and may exclude inaccessible objects. Unsupported endpoint types remain unknown; no live existence claim is made."}, nil
}
