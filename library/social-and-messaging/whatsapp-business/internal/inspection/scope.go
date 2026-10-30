package inspection

import (
	"fmt"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/captures"
	"strings"
)

// ScopeMap relates only recorded entities within the explicitly chosen business.
// Each relationship remains tied to its original capture; this is not live state.
func ScopeMap(snapshots []captures.Snapshot, businessID string) (map[string]any, error) {
	if strings.TrimSpace(businessID) == "" {
		return nil, fmt.Errorf("an explicit business ID is required for the scope map")
	}
	relations, gaps, entities := make([]map[string]any, 0), make([]map[string]any, 0), make([]map[string]any, 0)
	used := 0
	for _, s := range snapshots {
		if s.Tool != "whatsapp_biz_businesses" && s.Tool != "whatsapp_biz_accounts" && s.Tool != "whatsapp_biz_phone_numbers" {
			continue
		}
		if b := text(s.Scope["business_id"]); b != "" && b != businessID {
			continue
		}
		if err := explicit(s); err != nil {
			return nil, err
		}
		v, err := normalized(s.Data)
		if err != nil {
			return nil, err
		}
		v = unwrap(v)
		m := object(v)
		envelopeBusiness := text(m["business_id"])
		if envelopeBusiness != "" && envelopeBusiness != businessID {
			continue
		}
		scoped := text(s.Scope["business_id"]) == businessID || envelopeBusiness == businessID
		selectedRows := 0
		for _, group := range []struct{ key, kind, role string }{
			{"businesses", "business", ""}, {"accounts", "account", "owned"}, {"shared_accounts", "account", "shared"},
			{"phone_numbers", "phone", ""}, {"phones", "phone", ""}, {"shared_phone_numbers", "phone", "shared"},
		} {
			if group.kind == "business" && s.Tool != "whatsapp_biz_businesses" {
				continue
			}
			if group.kind == "account" && s.Tool != "whatsapp_biz_accounts" {
				continue
			}
			if group.kind == "phone" && s.Tool != "whatsapp_biz_phone_numbers" {
				continue
			}
			rows := rowsFor(v, group.key)
			// A bare list/data envelope belongs to one source family, not every
			// alternative array key, preventing duplicate invented relationships.
			primary := group.key == "businesses" || group.key == "accounts" || group.key == "phone_numbers"
			if len(rows) == 0 && primary {
				rows = bareRows(v)
				if len(rows) == 0 {
					// Direct lookups can return the entity itself rather than a
					// list. Use only a source-specific identifier actually present.
					idKey := group.kind + "_id"
					if group.kind == "phone" {
						idKey = "phone_number_id"
					}
					if text(m[idKey]) != "" {
						rows = []map[string]any{m}
					}
				}
			}
			for index, row := range rows {
				rowBusiness := text(row["business_id"])
				if group.kind == "business" {
					if rowBusiness == "" {
						rowBusiness = text(row["id"])
					}
					if rowBusiness != businessID {
						continue
					}
				} else if !scoped && rowBusiness != businessID {
					continue
				}
				selectedRows++
				entity := map[string]any{"kind": group.kind, "data": row, "evidence": evidence(s)}
				if group.role != "" {
					entity["role"] = group.role
				}
				entities = append(entities, entity)
				if group.kind == "business" {
					continue
				}
				id := firstID(row, group.kind+"_id", "id")
				if group.kind == "phone" {
					id = firstID(row, "phone_number_id", "id")
				}
				if id == "" {
					gaps = append(gaps, map[string]any{"capture_id": s.ID, "path": fmt.Sprintf("/%s/%d", group.key, index), "reason": "entity identifier was not captured"})
					continue
				}
				if group.kind == "account" {
					relations = append(relations, map[string]any{"kind": "business_account", "business_id": businessID, "account_id": id, "account_type": row["type"], "role": group.role, "reported_business_id": row["business_id"], "evidence": evidence(s)})
					if parent := text(object(row["whatsapp_account"])["id"]); parent != "" {
						relations = append(relations, map[string]any{"kind": "account_parent", "account_id": id, "parent_account_id": parent, "evidence": evidence(s)})
					}
				} else {
					account := firstID(row, "whatsapp_account_id", "account_id", "waba_id")
					if account == "" {
						account = firstID(m, "whatsapp_account_id", "account_id")
					}
					if account == "" {
						gaps = append(gaps, map[string]any{"capture_id": s.ID, "phone_number_id": id, "reason": "parent account identifier is unknown in captured data"})
					} else {
						relations = append(relations, map[string]any{"kind": "account_phone", "account_id": account, "phone_number_id": id, "evidence": evidence(s)})
					}
				}
			}
		}
		if selectedRows > 0 || scoped {
			used++
			if !s.Complete {
				gaps = append(gaps, map[string]any{"capture_id": s.ID, "reason": "capture coverage is partial; missing entities remain unobserved"})
			}
		}
	}
	if used == 0 {
		gaps = append(gaps, map[string]any{"reason": "no matching captured evidence for the selected business"})
	}
	return map[string]any{"business_id": businessID, "entities": entities, "relationships": relations, "gaps": gaps, "captures_used": used, "note": "Relationships describe their labeled captures, not current readiness or ownership beyond the reported account role."}, nil
}
func rowsFor(v any, key string) []map[string]any { return mapRows(object(v)[key]) }
func bareRows(v any) []map[string]any {
	if rows := mapRows(v); len(rows) > 0 {
		return rows
	}
	return mapRows(object(v)["data"])
}
func mapRows(v any) []map[string]any {
	result := make([]map[string]any, 0)
	for _, row := range asSlice(v) {
		if m := object(row); m != nil {
			result = append(result, m)
		}
	}
	return result
}
func asSlice(v any) []any { rows, _ := v.([]any); return rows }
func firstID(row map[string]any, ks ...string) string {
	for _, k := range ks {
		if id := text(row[k]); id != "" {
			return id
		}
	}
	return ""
}
