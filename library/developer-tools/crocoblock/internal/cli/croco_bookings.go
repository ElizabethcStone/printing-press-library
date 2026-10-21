package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		for _, c := range []*cobra.Command{crocoBookingsList(f), crocoBookingsConfig(f), crocoBookingsWrite(f, false), crocoBookingsWrite(f, true), crocoBookingsDelete(f)} {
			crocoAttach(root, "bookings", c)
		}
	})
}

func crocoReservationQuery(limit, offset int, filter, mode, key string) (map[string]string, error) {
	if limit < 1 || limit > 1000 || offset < 0 {
		return nil, usageErr(fmt.Errorf("--limit must be 1..1000 and --offset nonnegative"))
	}
	if mode != "all" && mode != "past" && mode != "upcoming" {
		return nil, usageErr(fmt.Errorf("--mode must be all, past or upcoming"))
	}
	parsed, e := crocoBody(filter, "")
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(parsed)
	if e != nil {
		return nil, e
	}
	return map[string]string{"per_page": strconv.Itoa(limit), "offset": strconv.Itoa(offset), key: string(raw), "mode": mode}, nil
}
func crocoReservationPage(ctx context.Context, c *client.Client, path, key string, limit, offset int, filters, mode string) (map[string]any, error) {
	q, e := crocoReservationQuery(limit, offset, filters, mode, key)
	if e != nil {
		return nil, e
	}
	v, e := crocoRequest(ctx, c, "GET", path, q, nil, true)
	if e != nil {
		return nil, e
	}
	m, e := crocoMap(v)
	if e != nil {
		return nil, e
	}
	if m["success"] != true {
		return nil, apiErr(fmt.Errorf("reservation response lacks explicit success"))
	}
	rows, ok := m["data"].([]any)
	if !ok {
		return nil, apiErr(fmt.Errorf("reservation response data is missing, null or not an array; coverage unknown"))
	}
	total, e := crocoInt(m["total"])
	if e != nil || total < 0 {
		return nil, apiErr(fmt.Errorf("reservation response has invalid total"))
	}
	if int64(len(rows)) > total {
		return nil, apiErr(fmt.Errorf("reservation response count exceeds total"))
	}
	m["data"] = rows
	m["offset"] = offset
	m["limit"] = limit
	m["complete"] = offset == 0 && int64(len(rows)) == total
	return m, nil
}
func crocoBookingPage(ctx context.Context, c *client.Client, limit, offset int, filters, mode string) (map[string]any, error) {
	return crocoReservationPage(ctx, c, "/jet-booking/v2/bookings-list", "filters", limit, offset, filters, mode)
}
func crocoBookingsList(f *rootFlags) *cobra.Command {
	var limit, offset int
	var filters, mode string
	cmd := crocoCommand(f, "list", "List a bounded page of bookings with provider totals", true, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
		if len(args) != 0 {
			return nil, usageErr(fmt.Errorf("list accepts flags only"))
		}
		return crocoBookingPage(ctx, c, limit, offset, filters, mode)
	})
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum rows in this page (1..1000)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Starting row offset")
	cmd.Flags().StringVar(&filters, "filters", "{}", "JetBooking filter JSON, for example apartment_id, check_in_date, check_out_date, status")
	cmd.Flags().StringVar(&mode, "mode", "all", "all, upcoming or past; plugin date semantics")
	cmd.Example = "  crocoblock-pp-cli bookings list --filters '{\"apartment_id\":13}' --limit 50 --agent"
	return cmd
}
func crocoBookingConfig(ctx context.Context, c *client.Client, id string) (any, error) {
	return crocoRequest(ctx, c, "POST", "/jet-booking/v2/booking-config", nil, map[string]any{"item": map[string]any{"apartment_id": id}}, true)
}
func crocoBookingsConfig(f *rootFlags) *cobra.Command {
	cmd := crocoCommand(f, "config <resource-id>", "Read booking configuration for one resource with sensitive keys redacted", true, func(ctx context.Context, c *client.Client, _ *cobra.Command, args []string) (any, error) {
		id, e := crocoID(args, 0)
		if e != nil {
			return nil, e
		}
		if len(args) != 1 {
			return nil, usageErr(fmt.Errorf("config requires one resource ID"))
		}
		return crocoBookingConfig(ctx, c, id)
	})
	cmd.Example = "  crocoblock-pp-cli bookings config 13 --agent"
	return cmd
}

// Scoped readback paginates to a stable total, and refuses incomplete or repeated pages.
func crocoReservationAll(ctx context.Context, c *client.Client, booking bool, filter map[string]any) ([]map[string]any, error) {
	raw, _ := json.Marshal(filter)
	var out = []map[string]any{}
	seen := map[string]bool{}
	var total int64 = -1
	for offset := 0; offset < 100000; {
		var page map[string]any
		var e error
		if booking {
			page, e = crocoBookingPage(ctx, c, 200, offset, string(raw), "all")
		} else {
			page, e = crocoAppointmentPage(ctx, c, 200, offset, string(raw), "all")
		}
		if e != nil {
			return nil, e
		}
		n, _ := crocoInt(page["total"])
		if total >= 0 && total != n {
			return nil, apiErr(fmt.Errorf("reservation count changed during verification; mutation outcome requires inspection"))
		}
		total = n
		rows, _ := crocoArray(page["data"])
		if len(rows) == 0 && int64(offset) < total {
			return nil, apiErr(fmt.Errorf("incomplete reservation readback at offset %d", offset))
		}
		for _, v := range rows {
			m, e := crocoMap(v)
			if e != nil {
				return nil, e
			}
			key := "ID"
			if booking {
				key = "booking_id"
			}
			id, e := crocoID([]string{crocoString(m[key])}, 0)
			if e != nil {
				return nil, apiErr(fmt.Errorf("readback has invalid identity"))
			}
			if seen[id] {
				return nil, apiErr(fmt.Errorf("reservation pagination repeated ID %s", id))
			}
			seen[id] = true
			out = append(out, m)
		}
		offset += len(rows)
		if int64(offset) == total {
			return out, nil
		}
		if int64(offset) > total {
			return nil, apiErr(fmt.Errorf("reservation total inconsistent during readback"))
		}
	}
	return nil, apiErr(fmt.Errorf("reservation readback exceeds 100000 rows; narrow scope"))
}
func crocoBookingFind(ctx context.Context, c *client.Client, id string) (map[string]any, error) {
	rows, e := crocoReservationAll(ctx, c, true, map[string]any{"booking_id": id})
	if e != nil {
		return nil, e
	}
	for _, r := range rows {
		if crocoString(r["booking_id"]) == id {
			return r, nil
		}
	}
	return nil, notFoundErr(fmt.Errorf("booking %s not found in authorized readback", id))
}
func crocoMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func crocoDate(s string) (time.Time, error) {
	t, e := time.Parse("2006-01-02", s)
	if e != nil || t.Format("2006-01-02") != s {
		return time.Time{}, usageErr(fmt.Errorf("date must use valid YYYY-MM-DD"))
	}
	return t, nil
}
func crocoBookingValidate(item map[string]any, complete bool) error {
	if e := crocoOnly(item, "apartment_id", "apartment_unit", "check_in_date", "check_out_date", "check_in_time", "check_out_time", "status", "user_email", "__guests", "attributes"); e != nil {
		return e
	}
	for _, k := range []string{"apartment_id", "apartment_unit", "__guests"} {
		if v, ok := item[k]; ok {
			n, e := crocoInt(v)
			if e != nil || n < 0 || (k == "apartment_id" && n == 0) {
				return usageErr(fmt.Errorf("%s must be a valid nonnegative integer (resource must be positive)", k))
			}
		}
	}
	if v, ok := item["status"]; ok {
		if text, valid := v.(string); !valid || text == "" {
			return usageErr(fmt.Errorf("status must be a nonempty string"))
		}
	}
	for _, k := range []string{"check_in_date", "check_out_date"} {
		if v, ok := item[k]; ok {
			if _, e := crocoDate(crocoString(v)); e != nil {
				return e
			}
		}
	}
	if complete || (item["check_in_date"] != nil && item["check_out_date"] != nil) {
		if _, ok := item["apartment_id"]; complete && !ok {
			return usageErr(fmt.Errorf("apartment_id is required"))
		}
		start, e := crocoDate(crocoString(item["check_in_date"]))
		if e != nil {
			return e
		}
		end, e := crocoDate(crocoString(item["check_out_date"]))
		if e != nil {
			return e
		}
		if !end.After(start) {
			return usageErr(fmt.Errorf("check_out_date must be later than check_in_date; refusing upstream date adjustment"))
		}
	}
	return nil
}
func crocoBookingInput(row map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for _, k := range []string{"apartment_id", "apartment_unit", "status", "user_email", "check_in_time", "check_out_time", "__guests", "attributes"} {
		if v, ok := row[k]; ok {
			out[k] = v
		}
	}
	for _, k := range []string{"check_in_date", "check_out_date"} {
		n, e := crocoInt(row[k+"_timestamp"])
		if e != nil || n <= 0 {
			return nil, apiErr(fmt.Errorf("booking readback lacks valid %s_timestamp", k))
		}
		out[k] = time.Unix(n, 0).UTC().Format("2006-01-02")
	}
	return out, nil
}
func crocoBookingMatches(row, item map[string]any) bool {
	canonical, e := crocoBookingInput(row)
	if e != nil {
		return false
	}
	for k, v := range item {
		if k == "attributes" {
			a, _ := json.Marshal(v)
			b, _ := json.Marshal(canonical[k])
			if string(a) != string(b) {
				return false
			}
			continue
		}
		if crocoString(canonical[k]) != crocoString(v) {
			return false
		}
	}
	return true
}
func crocoBookingWrite(ctx context.Context, c *client.Client, id string, patch map[string]any) (map[string]any, error) {
	if e := crocoRequireBody(patch); e != nil {
		return nil, e
	}
	if e := crocoBookingValidate(patch, id == ""); e != nil {
		return nil, e
	}
	item := patch
	before := map[string]bool{}
	if id != "" {
		current, e := crocoBookingFind(ctx, c, id)
		if e != nil {
			return nil, e
		}
		item, e = crocoBookingInput(current)
		if e != nil {
			return nil, e
		}
		for k, v := range patch {
			item[k] = v
		}
		if e = crocoBookingValidate(item, true); e != nil {
			return nil, e
		}
	} else {
		rows, e := crocoReservationAll(ctx, c, true, map[string]any{"apartment_id": item["apartment_id"]})
		if e != nil {
			return nil, e
		}
		for _, r := range rows {
			before[crocoString(r["booking_id"])] = true
		}
	}
	path := "/jet-booking/v2/add-booking"
	body := map[string]any{"item": item, "createRelatedOrder": false}
	if id != "" {
		path = "/jet-booking/v2/update-booking/" + id
	}
	if _, e := crocoRequest(ctx, c, "POST", path, nil, body, false); e != nil {
		return nil, e
	}
	if id != "" {
		row, e := crocoBookingFind(ctx, c, id)
		if e != nil {
			return nil, apiErr(fmt.Errorf("mutation_applied_or_unknown: booking write sent; readback failed; do not retry blindly: %w", e))
		}
		if !crocoBookingMatches(row, patch) {
			return nil, apiErr(fmt.Errorf("mutation_applied_or_unknown: booking %s did not match requested fields", id))
		}
		return row, nil
	}
	rows, e := crocoReservationAll(ctx, c, true, map[string]any{"apartment_id": item["apartment_id"]})
	if e != nil {
		return nil, apiErr(fmt.Errorf("mutation_applied_or_unknown: booking create sent; readback failed: %w", e))
	}
	matches := []map[string]any{}
	for _, r := range rows {
		if !before[crocoString(r["booking_id"])] && crocoBookingMatches(r, item) {
			matches = append(matches, r)
		}
	}
	if len(matches) != 1 {
		return nil, apiErr(fmt.Errorf("mutation_applied_or_unknown: create reported success but found %d new matching bookings; inspect before retry", len(matches)))
	}
	return matches[0], nil
}
func crocoBookingsWrite(f *rootFlags, update bool) *cobra.Command {
	var data, input string
	op := "create"
	use := "create"
	if update {
		op = "update"
		use = "update <id>"
	}
	cmd := crocoCommand(f, use, "Write booking fields and verify persisted values; never create an order", false, func(ctx context.Context, c *client.Client, _ *cobra.Command, args []string) (any, error) {
		id := ""
		if update {
			var e error
			id, e = crocoID(args, 0)
			if e != nil {
				return nil, e
			}
			if len(args) != 1 {
				return nil, usageErr(fmt.Errorf("update requires one ID"))
			}
		} else if len(args) != 0 {
			return nil, usageErr(fmt.Errorf("create accepts flags only"))
		}
		body, e := crocoBody(data, input)
		if e != nil {
			return nil, e
		}
		row, e := crocoBookingWrite(ctx, c, id, body)
		if e != nil {
			return nil, e
		}
		id = crocoString(row["booking_id"])
		if e = crocoJournal(f, ctx, c, "write", "bookings", crocoMapKeys(body), map[string]any{"id": id, "operation": op, "verified": true}); e != nil {
			return nil, e
		}
		return map[string]any{"verified": true, "item": row, "related_order_requested": false}, nil
	})
	crocoDataFlags(cmd, &data, &input)
	cmd.Example = "  crocoblock-pp-cli bookings " + op + " "
	if update {
		cmd.Example += "42 --data '{\"status\":\"cancelled\"}' --agent"
	} else {
		cmd.Example += "--data '{\"apartment_id\":13,\"check_in_date\":\"2026-11-10\",\"check_out_date\":\"2026-11-12\",\"status\":\"pending\"}' --agent"
	}
	return cmd
}
func crocoBookingsDelete(f *rootFlags) *cobra.Command {
	cmd := crocoCommand(f, "delete <id>", "Delete one booking and verify its absence", false, func(ctx context.Context, c *client.Client, _ *cobra.Command, args []string) (any, error) {
		id, e := crocoID(args, 0)
		if e != nil {
			return nil, e
		}
		if len(args) != 1 {
			return nil, usageErr(fmt.Errorf("delete requires one ID"))
		}
		if e = crocoBookingDeleteRecord(ctx, c, id); e != nil {
			return nil, e
		}
		if e = crocoJournal(f, ctx, c, "write", "bookings", nil, map[string]any{"id": id, "operation": "delete", "verified": true}); e != nil {
			return nil, e
		}
		return map[string]any{"id": id, "deleted": true, "verified": true}, nil
	})
	cmd.Example = "  crocoblock-pp-cli bookings delete 42 --agent"
	return cmd
}

func crocoBookingDeleteRecord(ctx context.Context, c *client.Client, id string) error {
	if _, e := crocoBookingFind(ctx, c, id); e != nil {
		return e
	}
	if _, e := crocoRequest(ctx, c, "DELETE", "/jet-booking/v2/delete-booking/"+id, nil, nil, false); e != nil {
		return e
	}
	rows, e := crocoReservationAll(ctx, c, true, map[string]any{"booking_id": id})
	if e != nil {
		return apiErr(fmt.Errorf("mutation_applied_or_unknown: delete sent; verification failed: %w", e))
	}
	for _, r := range rows {
		if crocoString(r["booking_id"]) == id {
			return apiErr(fmt.Errorf("booking %s remains after delete", id))
		}
	}
	return nil
}
