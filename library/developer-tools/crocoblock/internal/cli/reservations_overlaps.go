package cli

// pp:data-source local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
)

type crocoOverlapModelPolicy struct {
	IncludedStatuses  []string `json:"included_statuses"`
	Boundary          string   `json:"boundary"`
	Capacity          int      `json:"capacity"`
	GroupBy           []string `json:"group_by"`
	DateEncoding      string   `json:"date_encoding,omitempty"`
	CheckInAdjustment string   `json:"check_in_adjustment,omitempty"`
}
type crocoOverlapPolicy struct {
	SchemaVersion int                     `json:"schema_version"`
	Timezone      string                  `json:"timezone"`
	Bookings      crocoOverlapModelPolicy `json:"bookings"`
	Appointments  crocoOverlapModelPolicy `json:"appointments"`
}
type crocoOverlapInterval struct {
	ID         string
	Start, End int64
	Group      string
	Resource   map[string]string
}
type crocoOverlapEvent struct{ Starts, Ends []crocoOverlapInterval }

func newNovelReservationsOverlapsCmd(f *rootFlags) *cobra.Command {
	var db, policyPath string
	var limit int
	cmd := crocoLocalCommand(f, &cobra.Command{Use: "overlaps", Short: "Find capacity violations under an explicit reservation policy in complete local snapshots"}, func(ctx context.Context, _ *cobra.Command, args []string) (any, error) {
		if len(args) != 0 {
			return nil, usageErr(fmt.Errorf("overlaps accepts flags only"))
		}
		if limit < 1 || limit > 100000 {
			return nil, usageErr(fmt.Errorf("--limit must be 1..100000"))
		}
		p, e := crocoReadOverlapPolicy(policyPath)
		if e != nil {
			return nil, e
		}
		bookingCap, bookings, e := crocoLoadCapture(ctx, db, "bookings")
		if e != nil {
			return nil, e
		}
		appointmentCap, appointments, e := crocoLoadCapture(ctx, db, "appointments")
		if e != nil {
			return nil, e
		}
		return crocoAnalyzeOverlaps(ctx, bookingCap, bookings, appointmentCap, appointments, p, limit)
	})
	cmd.Long = "Compare saved booking nights and appointment intervals separately using explicit statuses, timezone, boundary and capacity. Both captures must be complete, cover all statuses, and come from the same site. This report describes captured occupancy under your policy; it does not certify live availability or observe changes after capture. Booking dates use the verified JetBooking calendar-date encoding; appointment timestamps remain absolute Unix times. Each stored booking or appointment counts as one capacity unit; group IDs do not merge rows. Capacity is uniform within each model and grouping. Provider or apartment-unit 0 is a literal bucket within the selected grouping. The example policy must be adapted to your business; no business policy is inferred. Only recorded slot/range appointment intervals are supported; recurring models require an expanded occurrence capture. See examples/reservation-policy.json."
	cmd.Flags().StringVar(&db, "db", "", "Read this existing SQLite snapshot (defaults to configured Crocoblock DB)")
	cmd.Flags().StringVar(&policyPath, "policy", "", "Required explicit reservation policy JSON file")
	cmd.Flags().IntVar(&limit, "limit", 1000, "Maximum reported windows; every captured row is still analyzed")
	cmd.Annotations["mcp:read-flags"] = "db,policy"
	cmd.Annotations["pp:verified-by-proof"] = "crocoblock-local-analysis-lifecycle.md"
	cmd.Example = "  crocoblock-pp-cli reservations overlaps --db candidate.db --policy examples/reservation-policy.json --agent"
	return cmd
}

func crocoReadOverlapPolicy(path string) (crocoOverlapPolicy, error) {
	var p crocoOverlapPolicy
	if path == "" {
		return p, usageErr(fmt.Errorf("--policy is required; see examples/reservation-policy.json"))
	}
	file, e := os.Open(path)
	if e != nil {
		return p, usageErr(e)
	}
	defer file.Close()
	b, e := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if e != nil {
		return p, e
	}
	if len(b) > 1024*1024 {
		return p, usageErr(fmt.Errorf("policy exceeds 1 MiB"))
	}
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&p); e != nil {
		return p, usageErr(fmt.Errorf("invalid reservation policy: %w", e))
	}
	var extra any
	if e = decoder.Decode(&extra); e != io.EOF {
		return p, usageErr(fmt.Errorf("policy must contain exactly one JSON object"))
	}
	if _, e = crocoValidateOverlapPolicy(p); e != nil {
		return p, e
	}
	return p, nil
}
func crocoValidateOverlapPolicy(p crocoOverlapPolicy) (*time.Location, error) {
	if p.SchemaVersion != 1 {
		return nil, usageErr(fmt.Errorf("policy schema_version must be 1"))
	}
	if p.Timezone == "Local" {
		return nil, usageErr(fmt.Errorf("policy timezone must not depend on the host Local timezone"))
	}
	loc, e := crocoAppointmentZone(p.Timezone)
	if e != nil {
		return nil, e
	}
	for name, model := range map[string]crocoOverlapModelPolicy{"bookings": p.Bookings, "appointments": p.Appointments} {
		if len(model.IncludedStatuses) == 0 {
			return nil, usageErr(fmt.Errorf("policy %s.included_statuses must explicitly include at least one status", name))
		}
		seen := map[string]bool{}
		for _, status := range model.IncludedStatuses {
			if strings.TrimSpace(status) == "" || seen[status] {
				return nil, usageErr(fmt.Errorf("policy %s has empty or duplicate status", name))
			}
			seen[status] = true
		}
		if model.Boundary != "half-open" && model.Boundary != "closed" {
			return nil, usageErr(fmt.Errorf("policy %s.boundary must be half-open or closed", name))
		}
		if model.Capacity < 1 || model.Capacity > 1000000 {
			return nil, usageErr(fmt.Errorf("policy %s.capacity must be explicitly set to 1..1000000", name))
		}
		grouping := strings.Join(model.GroupBy, ",")
		allowed := map[string]bool{"service": true, "provider": true, "service,provider": true}
		if name == "bookings" {
			allowed = map[string]bool{"apartment_id": true, "apartment_id,apartment_unit": true}
		}
		if !allowed[grouping] {
			return nil, usageErr(fmt.Errorf("unsupported %s.group_by; bookings: [apartment_id] or [apartment_id,apartment_unit]; appointments: [service], [provider] or [service,provider]", name))
		}
	}
	if p.Bookings.DateEncoding != "utc_calendar_date" || p.Bookings.CheckInAdjustment != "subtract_one_second" {
		return nil, usageErr(fmt.Errorf("booking policy must explicitly select date_encoding utc_calendar_date and check_in_adjustment subtract_one_second"))
	}
	if p.Appointments.DateEncoding != "" || p.Appointments.CheckInAdjustment != "" {
		return nil, usageErr(fmt.Errorf("appointment Unix timestamps do not use booking date adjustments"))
	}
	return loc, nil
}
func crocoOverlapCoverage(cap store.CrocoCapture, resource, plugin, version string) error {
	if cap.Resource != resource {
		return configErr(fmt.Errorf("coverage unknown: expected %s capture", resource))
	}
	if e := crocoRequireComplete(cap); e != nil {
		return e
	}
	if e := crocoRequireVersion(cap, plugin); e != nil {
		return e
	}
	if cap.Versions[plugin] != version {
		return configErr(fmt.Errorf("%s interval compatibility unknown for %s %s; verified version is %s", resource, plugin, cap.Versions[plugin], version))
	}
	if cap.Scope["all_statuses"] != true {
		return configErr(fmt.Errorf("coverage unknown for %s: all_statuses capture is required", resource))
	}
	fields := map[string]bool{}
	for _, f := range cap.Fields {
		fields[f] = true
	}
	for _, f := range []string{"id", "resource", "status", "interval"} {
		if !fields[f] {
			return configErr(fmt.Errorf("coverage unknown for %s: %s fields not captured", resource, f))
		}
	}
	if _, e := time.Parse(time.RFC3339Nano, cap.CapturedAt); e != nil {
		return configErr(fmt.Errorf("invalid %s capture timestamp", resource))
	}
	return nil
}
func crocoBookingCalendarTime(value any, checkIn bool, loc *time.Location) (int64, error) {
	n, e := crocoInt(value)
	if e != nil || n <= 0 {
		return 0, configErr(fmt.Errorf("booking interval unknown: missing/invalid timestamp"))
	}
	if checkIn {
		n--
	}
	t := time.Unix(n, 0).UTC()
	if t.Hour() != 0 || t.Minute() != 0 || t.Second() != 0 {
		return 0, configErr(fmt.Errorf("booking timestamp does not match verified UTC calendar-date encoding and check-in +1s contract"))
	}
	local := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	if local.Year() != t.Year() || local.Month() != t.Month() || local.Day() != t.Day() || local.Hour() != 0 {
		return 0, configErr(fmt.Errorf("booking midnight is nonexistent in policy timezone; calendar boundary is unknown"))
	}
	for _, d := range []time.Duration{-2 * time.Hour, -time.Hour, -30 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour} {
		other := local.Add(d)
		if other.Format("2006-01-02 15:04") == local.Format("2006-01-02 15:04") {
			return 0, configErr(fmt.Errorf("booking midnight is ambiguous in policy timezone; calendar boundary is unknown"))
		}
	}
	return local.Unix(), nil
}
func crocoOverlapIntervals(ctx context.Context, model string, rows []map[string]any, p crocoOverlapModelPolicy, loc *time.Location) ([]crocoOverlapInterval, int, error) {
	out := make([]crocoOverlapInterval, 0, len(rows))
	excluded := 0
	statuses := map[string]bool{}
	for _, s := range p.IncludedStatuses {
		statuses[s] = true
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if e := ctx.Err(); e != nil {
			return nil, 0, e
		}
		idKey := "ID"
		if model == "bookings" {
			idKey = "booking_id"
		}
		id, e := crocoID([]string{crocoString(row[idKey])}, 0)
		if e != nil {
			return nil, 0, configErr(fmt.Errorf("%s contains invalid record identity", model))
		}
		if seen[id] {
			return nil, 0, configErr(fmt.Errorf("%s contains duplicate ID %s", model, id))
		}
		seen[id] = true
		status, ok := row["status"].(string)
		if !ok || status == "" {
			return nil, 0, configErr(fmt.Errorf("%s %s has no valid status", model, id))
		}
		if !statuses[status] {
			excluded++
			continue
		}
		resource := map[string]string{}
		parts := []string{}
		for _, field := range p.GroupBy {
			v, exists := row[field]
			n, e := crocoInt(v)
			if !exists || e != nil || n < 0 || (n == 0 && (field == "apartment_id" || field == "service")) {
				return nil, 0, configErr(fmt.Errorf("%s %s has unknown resource field %s", model, id, field))
			}
			s := fmt.Sprint(n)
			resource[field] = s
			parts = append(parts, field+"="+s)
		}
		var start, end int64
		if model == "bookings" {
			start, e = crocoBookingCalendarTime(row["check_in_date_timestamp"], true, loc)
			if e != nil {
				return nil, 0, e
			}
			end, e = crocoBookingCalendarTime(row["check_out_date_timestamp"], false, loc)
			if e != nil {
				return nil, 0, e
			}
		} else {
			kind := crocoString(row["type"])
			if kind != "slot" && kind != "range" {
				return nil, 0, configErr(fmt.Errorf("appointment %s has unknown or unsupported interval type %q; recurrence expansion is not captured", id, kind))
			}
			start, e = crocoInt(row["slot_timestamp"])
			if e != nil || start <= 0 {
				return nil, 0, configErr(fmt.Errorf("appointment %s has invalid start timestamp", id))
			}
			end, e = crocoInt(row["slot_end_timestamp"])
			if e != nil || end <= 0 {
				return nil, 0, configErr(fmt.Errorf("appointment %s has invalid end timestamp", id))
			}
		}
		if end <= start {
			return nil, 0, configErr(fmt.Errorf("%s %s has an invalid interval; occupancy unknown", model, id))
		}
		out = append(out, crocoOverlapInterval{ID: id, Start: start, End: end, Group: strings.Join(parts, "/"), Resource: resource})
	}
	return out, excluded, nil
}
func crocoOverlapActiveIDs(active map[string]crocoOverlapInterval) []string {
	ids := make([]string, 0, len(active))
	for id := range active {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func crocoOverlapWindow(model string, start, end int64, active map[string]crocoOverlapInterval, p crocoOverlapModelPolicy, loc *time.Location, point bool) map[string]any {
	ids := crocoOverlapActiveIDs(active)
	resource := active[ids[0]].Resource
	return map[string]any{"model": model, "resource": resource, "start": time.Unix(start, 0).In(loc).Format(time.RFC3339), "end": time.Unix(end, 0).In(loc).Format(time.RFC3339), "start_timestamp": start, "end_timestamp": end, "point_overlap": point, "boundary": p.Boundary, "occupancy": len(ids), "capacity": p.Capacity, "excess": len(ids) - p.Capacity, "ids": ids}
}
func crocoFindCapacityWindows(ctx context.Context, model string, intervals []crocoOverlapInterval, p crocoOverlapModelPolicy, loc *time.Location, limit int) ([]any, int, error) {
	groups := map[string][]crocoOverlapInterval{}
	for _, i := range intervals {
		groups[i.Group] = append(groups[i.Group], i)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	windows := []any{}
	total := 0
	emit := func(start, end int64, active map[string]crocoOverlapInterval, point bool) {
		total++
		if len(windows) < limit {
			windows = append(windows, crocoOverlapWindow(model, start, end, active, p, loc, point))
		}
	}
	for _, group := range keys {
		if e := ctx.Err(); e != nil {
			return nil, 0, e
		}
		events := map[int64]*crocoOverlapEvent{}
		for _, i := range groups[group] {
			if events[i.Start] == nil {
				events[i.Start] = &crocoOverlapEvent{}
			}
			if events[i.End] == nil {
				events[i.End] = &crocoOverlapEvent{}
			}
			events[i.Start].Starts = append(events[i.Start].Starts, i)
			events[i.End].Ends = append(events[i.End].Ends, i)
		}
		times := make([]int64, 0, len(events))
		for t := range events {
			times = append(times, t)
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		active := map[string]crocoOverlapInterval{}
		var previous int64
		for index, t := range times {
			if e := ctx.Err(); e != nil {
				return nil, 0, e
			}
			if index > 0 && t > previous && len(active) > p.Capacity {
				emit(previous, t, active, false)
			}
			event := events[t]
			next := map[string]crocoOverlapInterval{}
			for k, v := range active {
				next[k] = v
			}
			for _, i := range event.Ends {
				delete(next, i.ID)
			}
			for _, i := range event.Starts {
				next[i.ID] = i
			}
			if p.Boundary == "closed" {
				point := map[string]crocoOverlapInterval{}
				for k, v := range active {
					point[k] = v
				}
				for _, i := range event.Starts {
					point[i.ID] = i
				}
				sig := strings.Join(crocoOverlapActiveIDs(point), ",")
				if len(point) > p.Capacity && sig != strings.Join(crocoOverlapActiveIDs(active), ",") && sig != strings.Join(crocoOverlapActiveIDs(next), ",") {
					emit(t, t, point, true)
				}
			}
			active = next
			previous = t
		}
	}
	return windows, total, nil
}
func crocoAnalyzeOverlaps(ctx context.Context, bookingCap store.CrocoCapture, bookings []map[string]any, appointmentCap store.CrocoCapture, appointments []map[string]any, p crocoOverlapPolicy, limit int) (map[string]any, error) {
	loc, e := crocoValidateOverlapPolicy(p)
	if e != nil {
		return nil, e
	}
	if limit < 1 {
		return nil, usageErr(fmt.Errorf("report limit must be positive"))
	}
	if e = crocoOverlapCoverage(bookingCap, "bookings", "jet-booking", "4.1.3.1"); e != nil {
		return nil, e
	}
	if e = crocoOverlapCoverage(appointmentCap, "appointments", "jet-appointments-booking", "2.5.3"); e != nil {
		return nil, e
	}
	if bookingCap.Site != appointmentCap.Site {
		return nil, configErr(fmt.Errorf("cannot combine reservation captures from different sites"))
	}
	bi, be, e := crocoOverlapIntervals(ctx, "bookings", bookings, p.Bookings, loc)
	if e != nil {
		return nil, e
	}
	ai, ae, e := crocoOverlapIntervals(ctx, "appointments", appointments, p.Appointments, loc)
	if e != nil {
		return nil, e
	}
	bw, bc, e := crocoFindCapacityWindows(ctx, "bookings", bi, p.Bookings, loc, limit)
	if e != nil {
		return nil, e
	}
	aw, ac, e := crocoFindCapacityWindows(ctx, "appointments", ai, p.Appointments, loc, limit-len(bw))
	if e != nil {
		return nil, e
	}
	windows := append(bw, aw...)
	total := bc + ac
	explanation := "Capacity violations found in captured occupancy under the supplied policy; windows are segments, not unique reservation pairs."
	if total == 0 {
		explanation = "No capacity violations in the complete captured rows under the supplied policy. Excluded statuses are not counted. This is not a live availability guarantee."
	}
	return map[string]any{"site": bookingCap.Site, "captures": map[string]any{"bookings": bookingCap, "appointments": appointmentCap}, "policy": p, "scanned": map[string]any{"bookings": len(bookings), "appointments": len(appointments)}, "included": map[string]any{"bookings": len(bi), "appointments": len(ai)}, "excluded_by_status": map[string]any{"bookings": be, "appointments": ae}, "over_capacity_windows": windows, "count": total, "reported": len(windows), "limit": limit, "truncated": total > len(windows), "explanation": explanation, "availability_guaranteed": false}, nil
}
