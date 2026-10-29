// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
)

const (
	// fleetChunkSize is the route-plan limit of orders per call.
	fleetChunkSize = 50
	// fleetDefaultVehicleMode is the vehicle mode used when none is given.
	fleetDefaultVehicleMode = "auto"
	// fleetMinCallTimeout keeps the per-call timeout above route-plan's ~30s
	// solve time even when --timeout is left at its default.
	fleetMinCallTimeout = 120 * time.Second
)

var fleetVehicleModes = []string{"auto", "truck", "bike", "foot"}

var fleetCSVColumns = []string{"chunk", "vehicle_id", "sequence", "order_id", "lat", "lng"}

type fleetStop struct {
	Chunk     int      `json:"chunk"`
	VehicleID string   `json:"vehicle_id"`
	Sequence  string   `json:"sequence"`
	OrderID   string   `json:"order_id"`
	Lat       *float64 `json:"lat"`
	Lng       *float64 `json:"lng"`
}

type fleetFailure struct {
	Chunk  int    `json:"chunk"`
	Orders int    `json:"orders"`
	Error  string `json:"error"`
}

type fleetSummary struct {
	Orders           int            `json:"orders"`
	Chunks           int            `json:"chunks"`
	Assigned         int            `json:"assigned"`
	Unassigned       int            `json:"unassigned"`
	UnassignedOrders []string       `json:"unassigned_orders"`
	VehiclesUsed     int            `json:"vehicles_used"`
	TotalDistance    float64        `json:"total_distance"`
	TotalDuration    float64        `json:"total_duration"`
	PlannedCalls     int            `json:"planned_calls"`
	CallsMade        int64          `json:"calls_made"`
	FetchFailures    int            `json:"fetch_failures"`
	FailedChunks     []fleetFailure `json:"failed_chunks"`
	DeferredChunks   int            `json:"deferred_chunks"`
	DeferredOrders   int            `json:"deferred_orders"`
	FailedOrders     int            `json:"failed_orders"`
	MaxCalls         int            `json:"max_calls"`
	RateLimited      bool           `json:"rate_limited"`
	PlanStatuses     []string       `json:"plan_statuses"`
	Out              string         `json:"out,omitempty"`
	Stops            []fleetStop    `json:"stops,omitempty"`
}

type fleetOrder struct {
	body     map[string]any
	id       string
	lat, lng *float64
}

func newNovelFleetPlanCmd(flags *rootFlags) *cobra.Command {
	var flagOrders, flagDepot, flagVehicleMode, flagOut, flagDB string
	var flagVehicles, flagMaxCalls, flagMaxRows int
	var flagReturn, flagForce, flagStdin bool
	var flagMaxWeight, flagMaxVolume float64

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Turn an orders file of addresses or coordinates into per-vehicle stop sequences.",
		Long: strings.Trim(fmt.Sprintf(`
Plans delivery routes for an orders file with the route-plan solver. The
solver accepts at most %[1]d orders per call, so larger files are split into
chunks of %[1]d and each chunk is planned on its own with --vehicles vehicles
from the depot; vehicle ids are prefixed per chunk (c1_vehicle_1). Results are
merged into one flat CSV: chunk, vehicle_id, sequence, order_id, lat, lng,
with a summary on stdout. Without --out no file is written and the stops are
returned in the JSON output (stops).

The orders (a CSV, JSON Lines or JSON-array file, or standard input with --stdin
or --orders -) need an order_id column and either lat and lng columns or an
address column (addresses are geocoded by the API itself); weight, volume and
service_duration columns are optional. Each call can take about 30 seconds, so
the per-call timeout is raised to at least %[2]s. A failed chunk is reported in
fetch_failures and excluded from the totals while successful chunks are kept.
A chunk whose call does not fit the remaining route-plan budget (route-plan has
its own limit of about %[3]d calls per %[6]s, separate from the other endpoints;
lowered by --max-calls) is not sent: it is reported in
deferred_chunks and deferred_orders, as is everything after a 429. Plans are
never cached. If no chunk could be planned (every chunk failed, or none could be
sent) no summary is printed: the command exits with the error. Distances and
durations are summed as the API returns them.
An --out CSV is never overwritten without --force and may not be the input
file.

%[4]s

%[5]s`, fleetChunkSize, fleetMinCallTimeout, dmDefaultPlanBudget, dmExitCodesNote, dmRetentionNote, dmWindowText), "\n"),
		Example: strings.Trim(`
  delhivery-maps-pp-cli fleet plan --stdin --vehicles 1 --depot 28.4599,77.0679
  delhivery-maps-pp-cli fleet plan --orders orders.csv --vehicles 6 --depot 28.4599,77.0679 --out plan.csv
  delhivery-maps-pp-cli fleet plan --orders jaipur.csv --vehicles 3 --depot 26.9124,75.7873 --return-to-depot --vehicle-mode truck --max-weight 1500`, "\n"),
		// The live-dogfood runner feeds pp:happy-stdin (valid JSON: three orders) on
		// standard input and appends --stdin: 1 route-plan call per run (plans are
		// never cached).
		Annotations: map[string]string{
			"pp:data-source": "live",
			// Reads and computes; it writes a file only when --out is given, and MCP hosts
			// are barred from that destination flag.
			"mcp:read-only":              "true",
			"mcp:read-only-unless-flags": "out",
			"mcp:write-flags":            "out",
			// --stdin reads the host's standard input, which an MCP call cannot supply, so
			// the flag is withheld from the MCP tool. MCP callers pass a server-local file.
			"mcp:read-flags": "stdin",
			"pp:happy-args":  "--vehicles=1;--depot=28.4599,77.0679",
			"pp:happy-stdin": `[{"order_id":"ORD001","lat":28.48,"lng":77.09,"weight":10},{"order_id":"ORD002","lat":28.50,"lng":77.10,"weight":15},{"order_id":"ORD003","lat":28.4650,"lng":77.0700,"weight":5}]`,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return dmWriteDryRunPlan(cmd, flags, "fleet plan",
					fmt.Sprintf("split the orders into chunks of %d and call route-plan once per chunk; no request sent", fleetChunkSize),
					map[string]any{
						"orders": flagOrders, "stdin": flagStdin, "vehicles": flagVehicles, "depot": flagDepot, "out": flagOut,
						"vehicle_mode":  flagVehicleMode,
						"planned_calls": fmt.Sprintf("ceil(orders / %d) (the input is not read in dry-run)", fleetChunkSize),
					})
			}
			inPath, err := dmInputPath(cmd, "orders", flagOrders, flagStdin)
			if err != nil {
				return err
			}
			if inPath == "" {
				return dmUsage(cmd, "--orders is required (or --stdin to read standard input)")
			}
			flagOrders = inPath
			if flagVehicles < 1 {
				return dmUsage(cmd, "--vehicles is required and must be at least 1")
			}
			depotLat, depotLng, ok := addrkit.ParseLatLng(flagDepot)
			if !ok {
				return dmUsage(cmd, `--depot must be a "lat,lng" pair such as 28.4599,77.0679`)
			}
			if !slices.Contains(fleetVehicleModes, flagVehicleMode) {
				return dmUsage(cmd, "--vehicle-mode %q is not valid; use one of: %s", flagVehicleMode, strings.Join(fleetVehicleModes, ", "))
			}
			for name, v := range map[string]float64{"max-weight": flagMaxWeight, "max-volume": flagMaxVolume} {
				if err := dmFiniteFlag(cmd, name, v, true); err != nil {
					return err
				}
			}
			if err := dmCheckOut(cmd, flagOrders, flagOut, flagForce); err != nil {
				return err
			}

			table, err := dmReadTable(cmd, flagOrders, flagMaxRows)
			if err != nil {
				return err
			}
			orders, err := parseFleetOrders(table)
			if err != nil {
				return dmUsage(cmd, "%s: %v", flagOrders, err)
			}
			if len(orders) == 0 {
				return dmUsage(cmd, "%s has no orders", flagOrders)
			}

			ctx := cmd.Context()
			s, err := openDMStore(ctx, flagDB)
			if err != nil {
				return err
			}
			defer s.Close()
			api, err := newDMAPI(cmd, flags, s.DB(), fleetMinCallTimeout)
			if err != nil {
				return err
			}

			chunks := addrkit.Chunk(orders, fleetChunkSize)
			summary := fleetSummary{Orders: len(orders), Chunks: len(chunks), PlannedCalls: len(chunks), Out: flagOut,
				UnassignedOrders: []string{}, FailedChunks: []fleetFailure{}, PlanStatuses: []string{}}
			budget, err := dmPlanBudget(ctx, api, cmd, flagMaxCalls, dmBucketPlan)
			if err != nil {
				return err
			}
			summary.MaxCalls = budget.Max
			summary.PlannedCalls = min(len(chunks), budget.Max)
			summary.RateLimited = budget.LockedOut && len(chunks) > budget.Max
			if len(chunks) > budget.Max {
				fmt.Fprintf(cmd.ErrOrStderr(), "fleet plan: needs %d route-plan call(s) but the route-plan budget allows %d; the remaining chunks are deferred\n", len(chunks), budget.Max)
			}

			var stops []fleetStop
			var fatal error
			stopped := false
			succeeded := 0
			for i, chunk := range chunks {
				n := i + 1
				if stopped || n > budget.Max {
					summary.DeferredChunks++
					summary.DeferredOrders += len(chunk)
					continue
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "fleet plan: chunk %d/%d (%d orders)...\n", n, len(chunks), len(chunk))
				body := map[string]any{
					"num_vehicles":    flagVehicles,
					"depot_lat":       depotLat,
					"depot_lng":       depotLng,
					"return_to_depot": flagReturn,
					"vehicle_mode":    flagVehicleMode,
					"orders":          fleetOrderBodies(chunk),
				}
				if flagMaxWeight > 0 {
					body["vehicle_max_weight"] = flagMaxWeight
				}
				if flagMaxVolume > 0 {
					body["vehicle_max_volume"] = flagMaxVolume
				}
				raw, err := api.post(ctx, epRoutePlan, body)
				if err != nil {
					if errors.Is(err, errDMRateLimited) {
						summary.RateLimited, stopped = true, true
						summary.DeferredChunks++
						summary.DeferredOrders += len(chunk)
						continue
					}
					if dmIsFatal(err) {
						stopped, fatal = true, err
					}
					summary.FetchFailures++
					summary.FailedOrders += len(chunk)
					summary.FailedChunks = append(summary.FailedChunks, fleetFailure{Chunk: n, Orders: len(chunk), Error: dmErrText(err)})
					continue
				}
				res, err := mergeFleetChunk(n, chunk, raw)
				if err != nil {
					summary.FetchFailures++
					summary.FailedOrders += len(chunk)
					summary.FailedChunks = append(summary.FailedChunks, fleetFailure{Chunk: n, Orders: len(chunk), Error: dmErrText(err)})
					continue
				}
				succeeded++
				stops = append(stops, res.stops...)
				summary.Assigned += res.assigned
				summary.Unassigned += len(res.unassigned)
				summary.UnassignedOrders = append(summary.UnassignedOrders, res.unassigned...)
				summary.VehiclesUsed += res.vehiclesUsed
				summary.TotalDistance += res.distance
				summary.TotalDuration += res.duration
				summary.PlanStatuses = append(summary.PlanStatuses, res.planStatus)
			}
			summary.CallsMade = api.calls.Load()

			if succeeded == 0 {
				switch {
				case fatal != nil:
					return dmFinishRunErr(fatal)
				case summary.RateLimited || summary.DeferredChunks > 0:
					return dmFinishRunErr(fmt.Errorf("%w: no chunk could be planned within the quota or call budget", errDMRateLimited))
				default:
					return dmFinishRunErr(errors.New(summary.FailedChunks[0].Error))
				}
			}
			if flagOut != "" {
				rows := make([][]string, 0, len(stops))
				for _, st := range stops {
					rows = append(rows, st.csvRow())
				}
				if err := dmWriteCSV(cmd, flagOut, fleetCSVColumns, rows, flagForce); err != nil {
					return err
				}
			} else {
				summary.Stops = stops
			}
			if summary.FetchFailures > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "fleet plan: warning: %d of %d chunk(s) failed (%d orders); they are excluded from the totals\n", summary.FetchFailures, len(chunks), summary.FailedOrders)
			}
			if summary.DeferredChunks > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "fleet plan: %d chunk(s) (%d orders) were not planned: rate limit (HTTP 429), lockout or call budget\n", summary.DeferredChunks, summary.DeferredOrders)
			}
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				fmt.Fprintf(cmd.OutOrStdout(), "Planned %d of %d order(s) in %d call(s): %d assigned, %d unassigned, %d vehicle(s) used\n",
					summary.Assigned, summary.Orders, summary.CallsMade, summary.Assigned, summary.Unassigned, summary.VehiclesUsed)
				if flagOut != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  stops written to %s\n", flagOut)
				}
			} else if err := dmPrint(cmd.OutOrStdout(), summary, flags); err != nil {
				return err
			}
			if fatal != nil {
				return dmFinishRunErr(fatal)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagOrders, "orders", "", "CSV, JSON Lines or JSON-array file of orders: order_id plus lat and lng, or address; optional weight, volume, service_duration; - reads standard input.")
	cmd.Flags().BoolVar(&flagStdin, "stdin", false, dmStdinHelp("orders"))
	cmd.Flags().IntVar(&flagVehicles, "vehicles", 0, fmt.Sprintf("Number of vehicles available per chunk of up to %d orders.", fleetChunkSize))
	cmd.Flags().StringVar(&flagDepot, "depot", "", `Depot location as "lat,lng" where every vehicle starts.`)
	cmd.Flags().BoolVar(&flagReturn, "return-to-depot", false, "Have each vehicle return to the depot after its last stop.")
	cmd.Flags().StringVar(&flagVehicleMode, "vehicle-mode", fleetDefaultVehicleMode, "Vehicle mode for routing: "+strings.Join(fleetVehicleModes, ", ")+".")
	cmd.Flags().Float64Var(&flagMaxWeight, "max-weight", 0, "Weight capacity per vehicle in kg, matched against the orders' weight column (0 means no limit).")
	cmd.Flags().Float64Var(&flagMaxVolume, "max-volume", 0, "Volume capacity per vehicle, matched against the orders' volume column (0 means no limit).")
	cmd.Flags().StringVar(&flagOut, "out", "", "Write the merged stop sequences to this CSV (chunk, vehicle_id, sequence, order_id, lat, lng); without it the stops are included in the JSON output.")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Overwrite --out if it already exists.")
	cmd.Flags().IntVar(&flagMaxCalls, "max-calls", 0, dmMaxCallsUsage("route-plan calls", "Chunks"))
	cmd.Flags().IntVar(&flagMaxRows, "max-rows", dmDefaultMaxRows, fmt.Sprintf("Refuse an orders file with more rows than this (at most %d).", addrkit.HardMaxRows))
	cmd.Flags().StringVar(&flagDB, "db", "", "SQLite database used to track quota usage (default: the CLI's data database).")
	return cmd
}

// parseFleetOrders turns the orders table into route-plan order bodies.
func parseFleetOrders(t *addrkit.Table) ([]fleetOrder, error) {
	idCol := t.Col("order_id")
	if idCol < 0 {
		return nil, fmt.Errorf("needs an order_id column (found: %s)", strings.Join(t.Header, ", "))
	}
	latCol, lngCol := t.Col("lat", "latitude"), t.Col("lng", "lon", "long", "longitude")
	addrCol := t.Col("address")
	if (latCol < 0 || lngCol < 0) && addrCol < 0 {
		return nil, errors.New("needs lat and lng columns or an address column")
	}
	weightCol, volCol, svcCol := t.Col("weight", "order_weight"), t.Col("volume", "order_volume"), t.Col("service_duration", "order_service_duration")
	seen := map[string]bool{}
	out := make([]fleetOrder, 0, len(t.Rows))
	for i, row := range t.Rows {
		line := i + 2
		id := strings.TrimSpace(addrkit.Cell(row, idCol))
		if id == "" {
			return nil, fmt.Errorf("row %d has an empty order_id", line)
		}
		if seen[id] {
			return nil, fmt.Errorf("row %d repeats order_id %q", line, id)
		}
		seen[id] = true
		o := fleetOrder{id: id, body: map[string]any{"order_id": id}}
		if latCol >= 0 && lngCol >= 0 {
			if lat, lng, ok := addrkit.ParseLatLngPair(addrkit.Cell(row, latCol), addrkit.Cell(row, lngCol)); ok {
				o.lat, o.lng = &lat, &lng
				o.body["order_lat"], o.body["order_lng"] = lat, lng
			}
		}
		if o.lat == nil {
			addr := strings.TrimSpace(addrkit.Cell(row, addrCol))
			if addrCol < 0 || addr == "" {
				return nil, fmt.Errorf("row %d (order %s) has neither valid lat/lng nor an address", line, id)
			}
			o.body["order_address"] = addr
		}
		for _, f := range []struct {
			col int
			key string
		}{{weightCol, "order_weight"}, {volCol, "order_volume"}, {svcCol, "order_service_duration"}} {
			if v := strings.TrimSpace(addrkit.Cell(row, f.col)); v != "" {
				n, err := addrkit.ParseFiniteFloat(v)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("row %d (order %s): %s %q is not a non-negative number", line, id, f.key, v)
				}
				o.body[f.key] = n
			}
		}
		out = append(out, o)
	}
	return out, nil
}

func fleetOrderBodies(chunk []fleetOrder) []map[string]any {
	out := make([]map[string]any, 0, len(chunk))
	for _, o := range chunk {
		out = append(out, o.body)
	}
	return out
}

type fleetChunkResult struct {
	stops        []fleetStop
	assigned     int
	unassigned   []string
	vehiclesUsed int
	distance     float64
	duration     float64
	planStatus   string
}

// mergeFleetChunk reads one route-plan response into flat stops and totals.
func mergeFleetChunk(chunk int, orders []fleetOrder, raw json.RawMessage) (fleetChunkResult, error) {
	var resp struct {
		PlanStatus any `json:"plan_status"` // a JSON boolean on the live API; a string is accepted too
		RoutesInfo []struct {
			VehicleID     any     `json:"vehicle_id"`
			Orders        []any   `json:"orders"`
			TotalDistance float64 `json:"total_distance"`
			TotalDuration float64 `json:"total_duration"`
			Route         []struct {
				Lat      *float64 `json:"lat"`
				Lng      *float64 `json:"lng"`
				Type     string   `json:"type"`
				OrderID  any      `json:"order_id"`
				Sequence any      `json:"sequence"`
			} `json:"route"`
		} `json:"routes_info"`
		UnassignedOrders []any `json:"unassigned_orders"`
		PlanSummary      struct {
			TotalDistance float64 `json:"total_distance"`
			TotalDuration float64 `json:"total_duration"`
		} `json:"plan_summary"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fleetChunkResult{}, fmt.Errorf("%w: decoding /route-plan: %v", errDMUnusableResponse, err)
	}
	if resp.RoutesInfo == nil && len(resp.UnassignedOrders) == 0 {
		return fleetChunkResult{}, fmt.Errorf("%w: /route-plan returned no routes (plan_status %v)", errDMUnusableResponse, resp.PlanStatus)
	}
	byID := make(map[string]fleetOrder, len(orders))
	for _, o := range orders {
		byID[o.id] = o
	}
	res := fleetChunkResult{planStatus: anyString(resp.PlanStatus), unassigned: []string{}}
	for _, r := range resp.RoutesInfo {
		vehicle := "c" + strconv.Itoa(chunk) + "_" + anyString(r.VehicleID)
		before := len(res.stops)
		for _, st := range r.Route {
			if !strings.EqualFold(st.Type, "order") {
				continue
			}
			res.stops = append(res.stops, fleetStop{Chunk: chunk, VehicleID: vehicle, Sequence: anyString(st.Sequence), OrderID: anyString(st.OrderID), Lat: st.Lat, Lng: st.Lng})
		}
		if len(res.stops) == before {
			// No per-stop route: fall back to the order list in solved order.
			for j, id := range r.Orders {
				sid := anyString(id)
				stop := fleetStop{Chunk: chunk, VehicleID: vehicle, Sequence: strconv.Itoa(j + 1), OrderID: sid}
				if o, ok := byID[sid]; ok {
					stop.Lat, stop.Lng = o.lat, o.lng
				}
				res.stops = append(res.stops, stop)
			}
		}
		if n := len(res.stops) - before; n > 0 {
			res.assigned += n
			res.vehiclesUsed++
		}
		res.distance += r.TotalDistance
		res.duration += r.TotalDuration
	}
	if len(resp.RoutesInfo) == 0 {
		res.distance, res.duration = resp.PlanSummary.TotalDistance, resp.PlanSummary.TotalDuration
	}
	for _, u := range resp.UnassignedOrders {
		if m, ok := u.(map[string]any); ok {
			res.unassigned = append(res.unassigned, anyString(m["order_id"]))
			continue
		}
		res.unassigned = append(res.unassigned, anyString(u))
	}
	// Reconcile: an order the solver neither routed nor reported as
	// unassigned must not vanish from the totals.
	accounted := map[string]bool{}
	for _, st := range res.stops {
		accounted[st.OrderID] = true
	}
	for _, id := range res.unassigned {
		accounted[id] = true
	}
	for _, o := range orders {
		if !accounted[o.id] {
			res.unassigned = append(res.unassigned, o.id)
		}
	}
	if res.assigned == 0 && anyString(resp.PlanStatus) == "false" {
		return fleetChunkResult{}, fmt.Errorf("%w: /route-plan could not plan this chunk (plan_status false, no orders assigned)", errDMUnusableResponse)
	}
	return res, nil
}

func (s fleetStop) csvRow() []string {
	lat, lng := "", ""
	if s.Lat != nil {
		lat = addrkit.FormatFloat(*s.Lat)
	}
	if s.Lng != nil {
		lng = addrkit.FormatFloat(*s.Lng)
	}
	return []string{strconv.Itoa(s.Chunk), s.VehicleID, s.Sequence, s.OrderID, lat, lng}
}

// anyString renders a decoded JSON scalar (string or number) as text.
func anyString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprint(t)
	}
}
