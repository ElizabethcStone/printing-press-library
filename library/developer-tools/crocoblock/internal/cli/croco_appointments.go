package cli

import (
	"context"
	"fmt"
	"net/mail"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		for _, c := range []*cobra.Command{crocoAppointmentsList(f), crocoAppointmentsGet(f), crocoAppointmentsWrite(f, false), crocoAppointmentsWrite(f, true), crocoAppointmentsDelete(f), crocoAppointmentsSlots(f), crocoAppointmentsProviders(f)} {
			crocoAttach(root, "appointments", c)
		}
	})
}
func crocoAppointmentPage(ctx context.Context, c *client.Client, limit, offset int, filters, mode string) (map[string]any, error) {
	return crocoReservationPage(ctx, c, "/jet-engine/v2/appointments-list", "filter", limit, offset, filters, mode)
}
func crocoAppointmentsList(f *rootFlags) *cobra.Command {
	var limit, offset int
	var filters, mode string
	cmd := crocoCommand(f, "list", "List a bounded page of saved appointments (external calendars excluded)", true, func(ctx context.Context, c *client.Client, _ *cobra.Command, args []string) (any, error) {
		if len(args) != 0 {
			return nil, usageErr(fmt.Errorf("list accepts flags only"))
		}
		return crocoAppointmentPage(ctx, c, limit, offset, filters, mode)
	})
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum rows in this page (1..1000)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Starting row offset")
	cmd.Flags().StringVar(&filters, "filters", "{}", "JetAppointment filter JSON, for example service, provider, status or date")
	cmd.Flags().StringVar(&mode, "mode", "all", "all, upcoming or past; plugin date semantics")
	cmd.Example = "  crocoblock-pp-cli appointments list --filters '{\"service\":13}' --limit 50 --agent"
	return cmd
}
func crocoAppointmentGet(ctx context.Context, c *client.Client, id string) (map[string]any, error) {
	v, e := crocoRequest(ctx, c, "GET", "/jet-engine/v2/get-appointment/"+id, nil, nil, true)
	if e != nil {
		return nil, e
	}
	m, e := crocoMap(v)
	if e != nil {
		return nil, e
	}
	if m["success"] != true {
		return nil, apiErr(fmt.Errorf("appointment response lacks explicit success"))
	}
	row, e := crocoMap(m["item"])
	if e != nil {
		return nil, e
	}
	if crocoString(row["ID"]) != id {
		return nil, notFoundErr(fmt.Errorf("appointment %s not found in authorized readback", id))
	}
	return row, nil
}
func crocoAppointmentsGet(f *rootFlags) *cobra.Command {
	cmd := crocoCommand(f, "get <id>", "Read an appointment with personal fields redacted", true, func(ctx context.Context, c *client.Client, _ *cobra.Command, args []string) (any, error) {
		id, e := crocoID(args, 0)
		if e != nil {
			return nil, e
		}
		if len(args) != 1 {
			return nil, usageErr(fmt.Errorf("get requires one ID"))
		}
		return crocoAppointmentGet(ctx, c, id)
	})
	cmd.Example = "  crocoblock-pp-cli appointments get 42 --agent"
	return cmd
}
func crocoAppointmentZone(zone string) (*time.Location, error) {
	if zone == "" || zone == "Local" {
		return nil, usageErr(fmt.Errorf("--timezone is required when supplying dates or times; use an IANA zone or UTC"))
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return nil, usageErr(fmt.Errorf("invalid IANA --timezone"))
	}
	return loc, nil
}
func crocoAppointmentWall(day, clock string, loc *time.Location) (time.Time, error) {
	text := day + " " + clock
	t, e := time.ParseInLocation("2006-01-02 15:04", text, loc)
	if e != nil || t.Format("2006-01-02 15:04") != text {
		return time.Time{}, usageErr(fmt.Errorf("invalid or nonexistent local date/time; use YYYY-MM-DD and HH:MM"))
	}
	// The API accepts no fold indicator. Refuse ambiguous wall-clock times.
	for _, delta := range []time.Duration{-2 * time.Hour, -time.Hour, -30 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour} {
		if t.Add(delta).Format("2006-01-02 15:04") == text {
			return time.Time{}, usageErr(fmt.Errorf("ambiguous daylight-saving local time; use UTC"))
		}
	}
	return t, nil
}
func crocoAppointmentValidate(body map[string]any, create bool) error {
	if e := crocoOnly(body, "service", "provider", "date", "slot", "slot_end", "status", "user_name", "user_email"); e != nil {
		return e
	}
	for _, k := range []string{"service", "provider"} {
		if v, ok := body[k]; ok {
			n, e := crocoInt(v)
			if e != nil || n < 0 || (k == "service" && n == 0) {
				return usageErr(fmt.Errorf("%s must be a valid nonnegative integer (service must be positive)", k))
			}
		}
	}
	if v, ok := body["user_email"]; ok {
		s, ok := v.(string)
		a, e := mail.ParseAddress(s)
		if !ok || e != nil || a.Address != s {
			return usageErr(fmt.Errorf("user_email must contain a plain email address"))
		}
	}
	for _, k := range []string{"user_name", "status"} {
		if v, ok := body[k]; ok {
			if _, ok = v.(string); !ok {
				return usageErr(fmt.Errorf("%s must be a string", k))
			}
		}
	}
	for _, k := range []string{"date", "slot", "slot_end"} {
		if v, ok := body[k]; ok {
			text, ok := v.(string)
			if !ok {
				return usageErr(fmt.Errorf("%s must be a string", k))
			}
			layout := "15:04"
			if k == "date" {
				layout = "2006-01-02"
			}
			parsed, e := time.Parse(layout, text)
			if e != nil || parsed.Format(layout) != text {
				return usageErr(fmt.Errorf("invalid %s format", k))
			}
		}
	}

	if create {
		for _, k := range []string{"service", "date", "slot", "slot_end", "user_email"} {
			if crocoString(body[k]) == "" {
				return usageErr(fmt.Errorf("%s is required", k))
			}
		}
	}
	return nil
}
func crocoAppointmentInput(body, current map[string]any, zone string) (map[string]any, error) {
	if e := crocoRequireBody(body); e != nil {
		return nil, e
	}
	if e := crocoAppointmentValidate(body, current == nil); e != nil {
		return nil, e
	}
	out := map[string]any{}
	for _, k := range []string{"service", "provider", "status", "user_name", "user_email"} {
		if v, ok := current[k]; ok {
			out[k] = v
		}
	}
	temporal := current == nil
	for _, k := range []string{"date", "slot", "slot_end"} {
		if _, ok := body[k]; ok {
			temporal = true
		}
	}
	if temporal {
		loc, e := crocoAppointmentZone(zone)
		if e != nil {
			return nil, e
		}
		for _, k := range []string{"date", "slot", "slot_end"} {
			if current != nil {
				n, e := crocoInt(current[k+"_timestamp"])
				if e != nil || n <= 0 {
					return nil, apiErr(fmt.Errorf("appointment readback lacks %s_timestamp", k))
				}
				layout := "15:04"
				if k == "date" {
					layout = "2006-01-02"
				}
				out[k] = time.Unix(n, 0).In(loc).Format(layout)
			}
		}
		for k, v := range body {
			out[k] = v
		}
		day := crocoString(out["date"])
		if _, e = crocoDate(day); e != nil {
			return nil, e
		}
		date, e := crocoAppointmentWall(day, "00:00", loc)
		if e != nil {
			return nil, e
		}
		start, e := crocoAppointmentWall(day, crocoString(out["slot"]), loc)
		if e != nil {
			return nil, e
		}
		end, e := crocoAppointmentWall(day, crocoString(out["slot_end"]), loc)
		if e != nil {
			return nil, e
		}
		if !end.After(start) {
			return nil, usageErr(fmt.Errorf("slot_end must be later than slot on the selected date"))
		}
		out["date_timestamp"] = date.Unix()
		out["slot_timestamp"] = start.Unix()
		out["slot_end_timestamp"] = end.Unix()
	} else {
		for k, v := range body {
			out[k] = v
		}
		for _, k := range []string{"date", "slot", "slot_end"} {
			n, e := crocoInt(current[k+"_timestamp"])
			if e != nil || n <= 0 {
				return nil, apiErr(fmt.Errorf("appointment readback lacks %s_timestamp", k))
			}
			out[k] = current[k]
			out[k+"_timestamp"] = n
		}
	}
	if _, ok := out["provider"]; !ok {
		out["provider"] = 0
	}
	if _, ok := out["status"]; !ok {
		out["status"] = "pending"
	}
	return out, nil
}
func crocoAppointmentMatches(row, item map[string]any) bool {
	for k, v := range item {
		switch k {
		case "date", "slot", "slot_end":
			continue
		}
		if crocoString(row[k]) != crocoString(v) {
			return false
		}
	}
	return true
}
func crocoAppointmentWrite(ctx context.Context, c *client.Client, id string, patch map[string]any, zone string) (map[string]any, error) {
	// Validate before any request, including readback.
	if e := crocoRequireBody(patch); e != nil {
		return nil, e
	}
	if e := crocoAppointmentValidate(patch, id == ""); e != nil {
		return nil, e
	}
	if id == "" {
		if _, e := crocoAppointmentInput(patch, nil, zone); e != nil {
			return nil, e
		}
	} else {
		for _, k := range []string{"date", "slot", "slot_end"} {
			if _, ok := patch[k]; ok {
				if _, e := crocoAppointmentZone(zone); e != nil {
					return nil, e
				}
			}
		}
	}
	var current map[string]any
	before := map[string]bool{}
	if id != "" {
		var e error
		current, e = crocoAppointmentGet(ctx, c, id)
		if e != nil {
			return nil, e
		}
	}
	item, e := crocoAppointmentInput(patch, current, zone)
	if e != nil {
		return nil, e
	}
	if id == "" {
		rows, e := crocoReservationAll(ctx, c, false, map[string]any{"service": item["service"]})
		if e != nil {
			return nil, e
		}
		for _, r := range rows {
			before[crocoString(r["ID"])] = true
		}
	}
	path := "/jet-engine/v2/appointment-add-appointment"
	var payload any = []any{item}
	if id != "" {
		path = "/jet-engine/v2/update-appointment/" + id
		payload = map[string]any{"item": item}
	}
	if _, e = crocoRequest(ctx, c, "POST", path, nil, payload, false); e != nil {
		return nil, e
	}
	if id != "" {
		row, e := crocoAppointmentGet(ctx, c, id)
		if e != nil {
			return nil, apiErr(fmt.Errorf("mutation_applied_or_unknown: update sent but readback failed; inspect before retry: %w", e))
		}
		if !crocoAppointmentMatches(row, item) {
			return nil, apiErr(fmt.Errorf("mutation_applied_or_unknown: appointment %s does not match submitted values", id))
		}
		return row, nil
	}
	rows, e := crocoReservationAll(ctx, c, false, map[string]any{"service": item["service"]})
	if e != nil {
		return nil, apiErr(fmt.Errorf("mutation_applied_or_unknown: create sent but readback failed; inspect before retry: %w", e))
	}
	matches := []map[string]any{}
	for _, r := range rows {
		if !before[crocoString(r["ID"])] && crocoAppointmentMatches(r, item) {
			matches = append(matches, r)
		}
	}
	if len(matches) != 1 {
		return nil, apiErr(fmt.Errorf("mutation_applied_or_unknown: create reported success but found %d new matching appointments (an occupied slot may be a no-op); inspect before retry", len(matches)))
	}
	return matches[0], nil
}
func crocoAppointmentsWrite(f *rootFlags, update bool) *cobra.Command {
	var data, input, zone string
	op := "create"
	use := "create"
	if update {
		op = "update"
		use = "update <id>"
	}
	cmd := crocoCommand(f, use, "Write one appointment and verify persistence (existing site workflows may run)", false, func(ctx context.Context, c *client.Client, _ *cobra.Command, args []string) (any, error) {
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
		row, e := crocoAppointmentWrite(ctx, c, id, body, zone)
		if e != nil {
			return nil, e
		}
		id = crocoString(row["ID"])
		if e = crocoJournal(f, ctx, c, "write", "appointments", crocoMapKeys(body), map[string]any{"id": id, "operation": op, "verified": true}); e != nil {
			return nil, e
		}
		return map[string]any{"verified": true, "item": row, "timezone": zone, "availability": "persistence verified; not a general availability guarantee"}, nil
	})
	crocoDataFlags(cmd, &data, &input)
	cmd.Flags().StringVar(&zone, "timezone", "", "Explicit IANA timezone (or UTC) for date/time conversion")
	if update {
		cmd.Example = "  crocoblock-pp-cli appointments update 42 --data '{\"status\":\"cancelled\"}' --agent"
	} else {
		cmd.Example = "  crocoblock-pp-cli appointments create --timezone UTC --input appointment.json --agent"
	}
	return cmd
}
func crocoAppointmentsDelete(f *rootFlags) *cobra.Command {
	cmd := crocoCommand(f, "delete <id> [<id>...]", "Delete only the selected appointment IDs; never expand a group", false, func(ctx context.Context, c *client.Client, _ *cobra.Command, args []string) (any, error) {
		if len(args) == 0 {
			return nil, usageErr(fmt.Errorf("at least one appointment ID is required"))
		}
		ids := []string{}
		seen := map[string]bool{}
		for i := range args {
			id, e := crocoID(args, i)
			if e != nil {
				return nil, e
			}
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		if e := crocoAppointmentDeleteRecords(ctx, c, ids); e != nil {
			return nil, e
		}
		if e := crocoJournal(f, ctx, c, "write", "appointments", nil, map[string]any{"operation": "delete", "count": len(ids), "verified": true}); e != nil {
			return nil, e
		}
		return map[string]any{"ids": ids, "deleted": true, "verified": true, "group_expanded": false}, nil
	})
	cmd.Example = "  crocoblock-pp-cli appointments delete 42 43 --agent"
	return cmd
}
func crocoAppointmentsSlots(f *rootFlags) *cobra.Command {
	var date, zone string
	var provider int64
	cmd := crocoCommand(f, "slots <service-id>", "Render the plugin slot view; may record an excluded calendar day", false, func(ctx context.Context, c *client.Client, _ *cobra.Command, args []string) (any, error) {
		id, e := crocoID(args, 0)
		if e != nil {
			return nil, e
		}
		if len(args) != 1 {
			return nil, usageErr(fmt.Errorf("slots requires one service ID"))
		}
		if provider < 0 {
			return nil, usageErr(fmt.Errorf("provider must be nonnegative"))
		}
		loc, e := crocoAppointmentZone(zone)
		if e != nil {
			return nil, e
		}
		if _, e = crocoDate(date); e != nil {
			return nil, e
		}
		day, e := crocoAppointmentWall(date, "00:00", loc)
		if e != nil {
			return nil, e
		}
		result, e := crocoAppointmentSlotView(ctx, c, id, provider, day.Unix(), zone)
		if e != nil {
			return nil, e
		}
		if e = crocoAppointmentSlotsJournal(f, ctx, c, id); e != nil {
			return nil, e
		}
		return map[string]any{"service": id, "date": date, "timezone": zone, "representation": "plugin_rendered_view", "availability_guaranteed": false, "provider_side_effects": "may record an excluded calendar day", "result": result}, nil
	})
	cmd.Annotations["mcp:read-only"] = "false"
	cmd.Long = "Render JetAppointment's slot view. The provider may persist an excluded calendar day when it finds no slots and the day is unavailable. This command is treated as a possible write, is never automatically replayed, and records a metadata-only local journal event. Returned markup is not an availability guarantee."
	cmd.Flags().StringVar(&date, "date", "", "Service date in YYYY-MM-DD")
	cmd.Flags().StringVar(&zone, "timezone", "", "Explicit IANA timezone (or UTC)")
	cmd.Flags().Int64Var(&provider, "provider", 0, "Provider ID; 0 asks the plugin to select/default")
	cmd.Example = "  crocoblock-pp-cli appointments slots 13 --date 2026-11-10 --timezone UTC --agent"
	return cmd
}
func crocoAppointmentsProviders(f *rootFlags) *cobra.Command {
	cmd := crocoCommand(f, "providers <service-id>", "Read providers for a service without a rendered custom template", true, func(ctx context.Context, c *client.Client, _ *cobra.Command, args []string) (any, error) {
		id, e := crocoID(args, 0)
		if e != nil {
			return nil, e
		}
		if len(args) != 1 {
			return nil, usageErr(fmt.Errorf("providers requires one service ID"))
		}
		return crocoRequest(ctx, c, "GET", "/jet-engine/v2/appointment-service-providers", map[string]string{"service": id}, nil, true)
	})
	cmd.Example = "  crocoblock-pp-cli appointments providers 13 --agent"
	return cmd
}

func crocoAppointmentDeleteRecords(ctx context.Context, c *client.Client, ids []string) error {
	for _, id := range ids {
		if _, e := crocoAppointmentGet(ctx, c, id); e != nil {
			return e
		}
	}
	if _, e := crocoRequest(ctx, c, "POST", "/jet-engine/v2/delete-appointment", nil, map[string]any{"items": ids}, false); e != nil {
		return e
	}
	for _, id := range ids {
		rows, e := crocoReservationAll(ctx, c, false, map[string]any{"ID": id})
		if e != nil {
			return apiErr(fmt.Errorf("mutation_applied_or_unknown: delete sent; verification failed: %w", e))
		}
		for _, r := range rows {
			if crocoString(r["ID"]) == id {
				return apiErr(fmt.Errorf("appointment %s remains after delete", id))
			}
		}
	}
	return nil
}

func crocoAppointmentSlotView(ctx context.Context, c *client.Client, service string, provider, date int64, zone string) (any, error) {
	return crocoRequest(ctx, c, "POST", "/jet-engine/v2/appointment-date-slots", nil, map[string]any{"service": service, "provider": provider, "date": date, "timezone": zone}, false)
}

func crocoAppointmentSlotsJournal(f *rootFlags, ctx context.Context, c *client.Client, id string) error {
	return crocoJournal(f, ctx, c, "calendar-view", "appointments-calendar", nil, map[string]any{"id": id, "operation": "read", "method": "POST", "success": true})
}
