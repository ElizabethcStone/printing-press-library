package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
)

// crocoExportCollection follows each plugin's collection contract. It keeps a
// stable page size because page-based APIs derive offsets from that size.
func crocoExportCollection(ctx context.Context, c *client.Client, resource string, limit int, emit func(json.RawMessage) error) (int, error) {
	path, err := resourceReadPath(resource)
	if err != nil {
		return 0, err
	}
	cfg := resourceReadConfigFor(resource)
	if cfg.paginationType == "" {
		return 0, fmt.Errorf("export collection %q has no pagination contract", resource)
	}
	count, offset := 0, 0
	total := int64(-1)
	seen := map[string]bool{}
	for page := 0; page < 100000; page++ {
		params := resourcePageParams(cfg, "", page, 0)
		switch resource {
		case "forms-api":
			params["orderby"] = "id"
			params["order"] = "asc"
		case "records-api":
			params["sort"] = "ASC"
		case "bookings-api", "appointments-api":
			params["offset"] = strconv.Itoa(offset)
			params["mode"] = "all"
		}
		// Both reservation collection endpoints declare GET in the plugin source.
		data, err := c.Get(ctx, path, params)
		if err != nil {
			if resource == "forms-api" && page > 0 && crocoExportWPPageEnd(err) {
				return count, nil
			}
			return count, err
		}
		items, n, err := crocoExportPage(data, resource, cfg)
		if err != nil {
			return count, fmt.Errorf("export %s page %d: %w", resource, page+1, err)
		}
		if len(items) > cfg.pageSize {
			return count, fmt.Errorf("export %s exceeded requested page size", resource)
		}
		if n >= 0 {
			if total >= 0 && total != n {
				return count, fmt.Errorf("export %s total changed during pagination", resource)
			}
			total = n
			if int64(offset+len(items)) > total {
				return count, fmt.Errorf("export %s page exceeds reported total", resource)
			}
			if len(items) == 0 && int64(offset) < total {
				return count, fmt.Errorf("export %s empty page before reported total; coverage incomplete", resource)
			}
			// Page APIs skip rows if an intermediate page is short. Offset APIs can
			// safely continue at the number of rows actually received.
			if cfg.paginationType == "page" && len(items) < cfg.pageSize && int64(offset+len(items)) < total {
				return count, fmt.Errorf("export %s short page before reported total; coverage incomplete", resource)
			}
		}
		for _, item := range items {
			id, err := crocoExportIdentity(item, resource)
			if err != nil {
				return count, err
			}
			if seen[id] {
				return count, fmt.Errorf("export %s repeated record %s; pagination did not advance", resource, id)
			}
			seen[id] = true
			if limit > 0 && count >= limit {
				return count, nil
			}
			if err := emit(item); err != nil {
				return count, err
			}
			count++
		}
		offset += len(items)
		if limit > 0 && count >= limit {
			return count, nil
		}
		if total >= 0 {
			if int64(offset) == total {
				return count, nil
			}
		} else if len(items) < cfg.pageSize {
			return count, nil
		}
	}
	return count, fmt.Errorf("export pagination exceeded 100000 pages for %q", resource)
}

func crocoExportWPPageEnd(err error) bool {
	var api *client.APIError
	if !errors.As(err, &api) || api.StatusCode != 400 {
		return false
	}
	var body struct {
		Code string `json:"code"`
	}
	return json.Unmarshal([]byte(api.Body), &body) == nil && body.Code == "rest_post_invalid_page_number"
}

func crocoExportPage(data json.RawMessage, resource string, cfg resourceReadConfig) ([]json.RawMessage, int64, error) {
	payload := data
	total := int64(-1)
	if resource != "forms-api" {
		var envelope map[string]json.RawMessage
		if json.Unmarshal(data, &envelope) != nil || envelope == nil {
			return nil, 0, fmt.Errorf("missing collection envelope")
		}
		if resource == "bookings-api" || resource == "appointments-api" {
			var success bool
			if json.Unmarshal(envelope["success"], &success) != nil || !success {
				return nil, 0, fmt.Errorf("response lacks explicit success")
			}
		}
		var number json.Number
		if json.Unmarshal(envelope["total"], &number) != nil {
			return nil, 0, fmt.Errorf("missing valid total; coverage unknown")
		}
		var err error
		total, err = number.Int64()
		if err != nil || total < 0 {
			return nil, 0, fmt.Errorf("missing valid total; coverage unknown")
		}
		payload = envelope[cfg.responsePath]
	}
	items, ok := resourceJSONArray(payload)
	if !ok {
		return nil, 0, fmt.Errorf("missing or malformed collection array; coverage unknown")
	}
	return items, total, nil
}

func crocoExportIdentity(item json.RawMessage, resource string) (string, error) {
	var row map[string]json.RawMessage
	if json.Unmarshal(item, &row) != nil || row == nil {
		return "", fmt.Errorf("export %s contains a non-object record", resource)
	}
	key := "id"
	switch resource {
	case "appointments-api":
		key = "ID"
	case "bookings-api":
		key = "booking_id"
	case "records-api":
		if raw, ok := row["choose"]; ok {
			var column map[string]json.RawMessage
			if json.Unmarshal(raw, &column) == nil && column != nil {
				row["id"] = column["value"]
			} else {
				row["id"] = raw
			}
		}
	}
	id := resourceCursorValue(row[key])
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("export %s record lacks valid identity", resource)
	}
	return strconv.FormatInt(n, 10), nil
}
