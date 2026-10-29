// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"context"
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

// tollVehicleTypes is the vehicle_type enum of /toll_charges_by_route.
var tollVehicleTypes = []string{"motorcycle", "car_jeep_van", "lcv", "bus_truck", "upto_3_axle", "axle_4_to_6", "hcm_eme", "axle_7_plus"}

const (
	// tripMaxGeometryPoints caps the polyline sent to the toll endpoint.
	tripMaxGeometryPoints = 2000
	// tripTollRouteName is a constant label for the toll request, so raw
	// addresses are not sent to the toll endpoint.
	tripTollRouteName = "route"
	tripDefaultMode   = "auto"
	tripDefaultCar    = "car_jeep_van"
)

type tripPoint struct {
	Input  string  `json:"input"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Source string  `json:"source"` // coordinates | geocoded | cached
}

type tripQuote struct {
	From           string           `json:"from"`
	To             string           `json:"to"`
	FromResolved   *tripPoint       `json:"from_resolved,omitempty"`
	ToResolved     *tripPoint       `json:"to_resolved,omitempty"`
	Via            []tripPoint      `json:"via,omitempty"`
	Vehicle        string           `json:"vehicle"`
	TravelMode     string           `json:"travel_mode"`
	TrafficAware   bool             `json:"traffic_aware"`
	DepartureTime  string           `json:"departure_time,omitempty"`
	DistanceKM     float64          `json:"distance_km"`
	DurationMin    float64          `json:"duration_min"`
	TollPlazaCount int              `json:"toll_plaza_count"`
	TotalTollINR   float64          `json:"total_toll_inr"`
	Plazas         []map[string]any `json:"plazas"`
	RoutePoints    int              `json:"route_points"`
	APICalls       int64            `json:"api_calls"`
	CacheHits      int64            `json:"cache_hits"`
	Deferred       bool             `json:"deferred,omitempty"`
	Error          string           `json:"error,omitempty"`
}

type tripOptions struct {
	travelMode, departure string
	traffic               bool
	via                   []string
}

// routeCacheable is true only when neither --traffic-aware nor
// --departure-time is set: those make the answer time-dependent.
func (o tripOptions) routeCacheable() bool { return !o.traffic && o.departure == "" }

// dmRouteInfo is what trip quote needs from a /route reply.
type dmRouteInfo struct {
	DistanceKM  float64
	DurationSec float64
	Geometry    [][2]float64
}

// dmTollInfo is what trip quote needs from a /toll_charges_by_route reply.
type dmTollInfo struct {
	Count  int
	Total  float64
	Plazas []map[string]any
}

func newNovelTripQuoteCmd(flags *rootFlags) *cobra.Command {
	var flagFrom, flagTo, flagVehicle, flagTravelMode, flagDeparture, flagFile, flagOut, flagDB string
	var flagTraffic, flagForce, flagStdin bool
	var flagVia []string
	var flagMaxCalls, flagMaxRows int

	cmd := &cobra.Command{
		Use:   "quote",
		Short: "Get distance, traffic ETA and toll plazas for a trip in one record.",
		Long: strings.Trim(fmt.Sprintf(`
Quotes a trip end to end in one record. Each endpoint may be a "lat,lng" pair
or an address (geocoded through the local cache). The command then calls the
route endpoint with plain [lat, lng] geometry, feeds that same geometry
(downsampled to at most %[1]d points, keeping the first and last) into the toll
endpoint for the chosen vehicle, and merges the results: resolved coordinates,
distance_km, duration_min, toll_plaza_count, total_toll_inr and the plaza list.

With --file lanes.csv (columns from, to, optional vehicle; CSV, JSON Lines or a
JSON array, or standard input with --stdin or --file -) it quotes many lanes,
one record each; any --via stops apply to every lane. A failed lane is reported
in its own record and does not stop the others, but a 429 or a rejected token
(401/403) does: the remaining lanes are deferred, except lanes that are already
fully cached, which still run. Geocodes are cached for %[7]s and toll results
for %[8]s; a route is cached for %[8]s only when neither
--traffic-aware nor --departure-time is set, since traffic changes the answer.

A quote costs up to %[2]d API calls (two geocodes, route, toll) against a quota of
about %[3]d per %[6]s. A lane whose calls do not fit the remaining budget
(lowered by --max-calls) is not started: its record is marked deferred. The
--out CSV is never overwritten without --force and may not be the input file.

%[4]s

%[5]s`, tripMaxGeometryPoints, dmCallsPerTrip, dmDefaultDailyBudget, dmExitCodesNote, dmRetentionNote, dmWindowText, dmAddressTTLText, dmRouteTTLText), "\n"),
		Example: strings.Trim(`
  delhivery-maps-pp-cli trip quote --from 28.5954,77.0319 --to 28.4089,77.3178 --vehicle car_jeep_van
  delhivery-maps-pp-cli trip quote --from 'Sector 45 Gurgaon' --to Jaipur --vehicle bus_truck --agent
  delhivery-maps-pp-cli trip quote --from 28.4599,77.0679 --to 26.9124,75.7873 --traffic-aware --departure-time 2026-11-03T09:30:00+05:30
  delhivery-maps-pp-cli trip quote --file lanes.csv --out quotes.csv --vehicle lcv`, "\n"),
		// Coordinates need no geocoding: the live-dogfood happy path costs 2 API
		// calls (route + toll) and the repeat run is served from the cache.
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
			"pp:happy-args":  "--from=28.5954,77.0319;--to=28.4089,77.3178;--vehicle=car_jeep_van",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return dmWriteDryRunPlan(cmd, flags, "trip quote",
					"geocode any address endpoints, call route, then toll charges for the route geometry; no request sent",
					map[string]any{
						"from": flagFrom, "to": flagTo, "via": dmTrimmed(flagVia), "file": flagFile, "stdin": flagStdin,
						"vehicle": flagVehicle, "travel_mode": flagTravelMode, "traffic_aware": flagTraffic,
						"planned_calls": fmt.Sprintf("up to %d per trip (2 geocodes for address endpoints, route, toll); cached results cost 0", dmCallsPerTrip),
					})
			}
			inPath, err := dmInputPath(cmd, "file", flagFile, flagStdin)
			if err != nil {
				return err
			}
			flagFile = inPath
			if flagFile == "" && (flagFrom == "" || flagTo == "") {
				return dmUsage(cmd, "--from and --to are required (or pass --file/--stdin with from,to columns)")
			}
			if flagFile != "" && (flagFrom != "" || flagTo != "") {
				return dmUsage(cmd, "use either --file or --from/--to, not both")
			}
			if !slices.Contains(tollVehicleTypes, flagVehicle) {
				return dmUsage(cmd, "--vehicle %q is not valid; use one of: %s", flagVehicle, strings.Join(tollVehicleTypes, ", "))
			}
			if !dmValidTravelMode(flagTravelMode) {
				return dmUsage(cmd, "--travel-mode %q is not valid; use one of: %s", flagTravelMode, strings.Join(dmTravelModes, ", "))
			}
			if flagDeparture != "" && !validDepartureTime(flagDeparture) {
				return dmUsage(cmd, "--departure-time %q must be an ISO 8601 timestamp such as 2026-11-03T09:30:00+05:30", flagDeparture)
			}
			if err := dmCheckOut(cmd, flagFile, flagOut, flagForce); err != nil {
				return err
			}
			opts := tripOptions{travelMode: flagTravelMode, departure: flagDeparture, traffic: flagTraffic, via: dmTrimmed(flagVia)}

			lanes := make([]tripQuote, 0)
			if flagFile != "" {
				t, err := dmReadTable(cmd, flagFile, flagMaxRows)
				if err != nil {
					return err
				}
				fromCol, toCol, vehCol := t.Col("from"), t.Col("to"), t.Col("vehicle")
				if fromCol < 0 || toCol < 0 {
					return dmUsage(cmd, "%s needs from and to columns (found: %s)", flagFile, strings.Join(t.Header, ", "))
				}
				for i, row := range t.Rows {
					lane := tripQuote{From: strings.TrimSpace(addrkit.Cell(row, fromCol)), To: strings.TrimSpace(addrkit.Cell(row, toCol)), Vehicle: flagVehicle}
					if v := strings.TrimSpace(addrkit.Cell(row, vehCol)); v != "" {
						if !slices.Contains(tollVehicleTypes, v) {
							return dmUsage(cmd, "%s row %d: vehicle %q is not valid; use one of: %s", flagFile, i+2, v, strings.Join(tollVehicleTypes, ", "))
						}
						lane.Vehicle = v
					}
					lanes = append(lanes, lane)
				}
				if len(lanes) == 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "trip quote: %s has no lanes\n", flagFile)
				}
			} else {
				lanes = append(lanes, tripQuote{From: flagFrom, To: flagTo, Vehicle: flagVehicle})
			}

			ctx := cmd.Context()
			s, err := openDMStore(ctx, flagDB)
			if err != nil {
				return err
			}
			defer s.Close()
			api, err := newDMAPI(cmd, flags, s.DB(), 0)
			if err != nil {
				return err
			}
			budget, err := dmPlanBudget(ctx, api, cmd, flagMaxCalls, dmBucketPool)
			if err != nil {
				return err
			}

			var fatal error
			stopped := false
			failed, deferred := 0, 0
			for i := range lanes {
				lane := &lanes[i]
				need, err := tripLaneCost(ctx, api, lane, opts)
				if err != nil {
					return err
				}
				// After a 429 or a rejected token only fully cached lanes can still run.
				if stopped && need > 0 {
					lane.Deferred, lane.Error = true, "deferred: run stopped early (rate limit or rejected token)"
					deferred++
					continue
				}
				if left := budget.Max - int(api.calls.Load()); need > left {
					lane.Deferred = true
					lane.Error = fmt.Sprintf("deferred: needs up to %d call(s) but %d fit within the call budget", need, max(left, 0))
					deferred++
					continue
				}
				before, hitsBefore := api.calls.Load(), api.hits.Load()
				err = runTripQuote(ctx, api, lane, opts)
				lane.APICalls = api.calls.Load() - before
				lane.CacheHits = api.hits.Load() - hitsBefore
				if err == nil {
					continue
				}
				lane.Error = dmErrText(err)
				switch {
				case errors.Is(err, errDMRateLimited):
					stopped = true
					lane.Deferred = true
					deferred++
					if len(lanes) == 1 {
						fatal = err
					}
				case dmIsFatal(err):
					stopped, fatal = true, err
					failed++
				default:
					failed++
					if len(lanes) == 1 {
						fatal = err
					}
				}
			}
			if flagFile == "" && fatal == nil && deferred > 0 {
				fatal = fmt.Errorf("%w: %s", errDMRateLimited, lanes[0].Error)
			}
			if flagFile == "" && fatal != nil {
				return dmFinishRunErr(fatal)
			}

			if flagOut != "" {
				if err := dmWriteCSV(cmd, flagOut, tripCSVColumns, tripCSVRows(lanes), flagForce); err != nil {
					return err
				}
			}
			if failed > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "trip quote: %d of %d lane(s) failed; see the error field\n", failed, len(lanes))
			}
			if deferred > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "trip quote: %d of %d lane(s) deferred (rate limit, lockout or call budget); re-run once the quota resets\n", deferred, len(lanes))
			}
			if err := printTripQuotes(cmd, flags, lanes, flagFile != ""); err != nil {
				return err
			}
			if fatal != nil && dmIsFatal(fatal) {
				return dmFinishRunErr(fatal)
			}
			if flagFile != "" && len(lanes) > 0 && failed == len(lanes) {
				return apiErr(fmt.Errorf("all %d lane(s) failed (see the error field)", len(lanes)))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagFrom, "from", "", `Start of the trip: a "lat,lng" pair or an address.`)
	cmd.Flags().StringVar(&flagTo, "to", "", `End of the trip: a "lat,lng" pair or an address.`)
	cmd.Flags().StringArrayVar(&flagVia, "via", nil, `Intermediate stop as "lat,lng" or an address; repeat the flag for several, in travel order (applies to every lane with --file).`)
	cmd.Flags().StringVar(&flagVehicle, "vehicle", tripDefaultCar, "Vehicle type for toll rates: "+strings.Join(tollVehicleTypes, ", ")+".")
	cmd.Flags().StringVar(&flagTravelMode, "travel-mode", tripDefaultMode, "Travel mode sent to the route endpoint: "+strings.Join(dmTravelModes, ", ")+".")
	cmd.Flags().BoolVar(&flagTraffic, "traffic-aware", false, "Ask the route endpoint for a traffic-aware duration (the route is then not cached).")
	cmd.Flags().StringVar(&flagDeparture, "departure-time", "", "ISO 8601 departure time used with --traffic-aware, for example 2026-11-03T09:30:00+05:30 (the route is then not cached).")
	cmd.Flags().StringVar(&flagFile, "file", "", "CSV, JSON Lines or JSON-array file of lanes with from and to columns (optional vehicle column) to quote in one run; - reads standard input.")
	cmd.Flags().BoolVar(&flagStdin, "stdin", false, dmStdinHelp("file"))
	cmd.Flags().StringVar(&flagOut, "out", "", "Also write the quotes to this CSV file.")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Overwrite --out if it already exists.")
	cmd.Flags().IntVar(&flagMaxCalls, "max-calls", 0, dmMaxCallsUsage("live API calls", "Lanes"))
	cmd.Flags().IntVar(&flagMaxRows, "max-rows", dmDefaultMaxRows, fmt.Sprintf("Refuse a --file with more rows than this (at most %d).", addrkit.HardMaxRows))
	cmd.Flags().StringVar(&flagDB, "db", "", "SQLite database for the request cache (default: the CLI's data database).")
	return cmd
}

func validDepartureTime(s string) bool {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

// resolveTripPoint turns "lat,lng" or an address into coordinates.
func resolveTripPoint(ctx context.Context, api *dmAPI, input string) (tripPoint, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return tripPoint{}, errors.New("empty location")
	}
	if lat, lng, ok := addrkit.ParseLatLng(input); ok {
		return tripPoint{Input: input, Lat: lat, Lng: lng, Source: "coordinates"}, nil
	}
	g, hit, err := api.geocode(ctx, input)
	if err != nil {
		return tripPoint{}, err
	}
	src := "geocoded"
	if hit {
		src = "cached"
	}
	return tripPoint{Input: input, Lat: *g.Lat, Lng: *g.Lng, Source: src}, nil
}

func tripRouteBody(coords [][2]float64, opts tripOptions) map[string]any {
	body := map[string]any{
		"geo_coords":    coords,
		"travel_mode":   opts.travelMode,
		"traffic_aware": opts.traffic,
		"output_format": map[string]any{"encode_polyline": false, "maneuvers": false},
	}
	if opts.departure != "" {
		body["departure_time"] = opts.departure
	}
	return body
}

func tripTollBody(geometry [][2]float64, vehicle string) map[string]any {
	return map[string]any{
		"routes":       []map[string]any{{"route_name": tripTollRouteName, "geometry": geometry}},
		"vehicle_type": vehicle,
	}
}

// dmPeek decodes a fresh cached response without counting a cache hit or
// touching the network; ok is false when there is none.
func dmPeek[T any](ctx context.Context, a *dmAPI, path string, body any, decode func(json.RawMessage) (T, error)) (T, bool, error) {
	var zero T
	if a.noCache {
		return zero, false, nil
	}
	key, _, err := dmCacheKey(a.scope, body)
	if err != nil {
		return zero, false, err
	}
	raw, ok, err := dmCacheGet(ctx, a.db, path, key, dmCacheTTLs[path])
	if err != nil || !ok {
		return zero, false, err
	}
	v, err := decode(raw)
	if err != nil {
		return zero, false, nil
	}
	return v, true, nil
}

// tripLaneCost is the most live calls one lane can need, probing the cache as
// far as it can: uncached geocodes, plus the route and the toll lookup unless
// those are cached too. It never calls the API.
func tripLaneCost(ctx context.Context, api *dmAPI, q *tripQuote, opts tripOptions) (int, error) {
	// runTripQuote resolves from, then to, then the via stops; mirror that
	// order so an empty location is charged for exactly the geocodes before it.
	inputs := append([]string{q.From, q.To}, opts.via...)
	resolved := make([][2]float64, len(inputs))
	uncached := 0
	for i, in := range inputs {
		in = strings.TrimSpace(in)
		if in == "" {
			// The quote fails here with "empty location".
			return uncached, nil
		}
		if lat, lng, ok := addrkit.ParseLatLng(in); ok {
			resolved[i] = [2]float64{lat, lng}
			continue
		}
		g, ok, err := dmPeek(ctx, api, epGeocode, dmAddressBody(in), decodeGeocode)
		if err != nil {
			return 0, err
		}
		if !ok {
			uncached++
			continue
		}
		resolved[i] = [2]float64{*g.Lat, *g.Lng}
	}
	if uncached > 0 || !opts.routeCacheable() {
		return uncached + 2, nil
	}
	coords := [][2]float64{resolved[0]}
	for i := 2; i < len(inputs); i++ {
		coords = append(coords, resolved[i])
	}
	coords = append(coords, resolved[1])
	route, ok, err := dmPeek(ctx, api, epRoute, tripRouteBody(coords, opts), decodeRoute)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 2, nil
	}
	has, err := api.cacheHas(ctx, epToll, tripTollBody(addrkit.Downsample(route.Geometry, tripMaxGeometryPoints), q.Vehicle))
	if err != nil {
		return 0, err
	}
	if has {
		return 0, nil
	}
	return 1, nil
}

// runTripQuote fills q in place: geocode, route, then tolls for that geometry.
func runTripQuote(ctx context.Context, api *dmAPI, q *tripQuote, opts tripOptions) error {
	q.TravelMode, q.TrafficAware, q.DepartureTime = opts.travelMode, opts.traffic, opts.departure
	from, err := resolveTripPoint(ctx, api, q.From)
	if err != nil {
		return err
	}
	q.FromResolved = &from
	to, err := resolveTripPoint(ctx, api, q.To)
	if err != nil {
		return err
	}
	q.ToResolved = &to
	coords := [][2]float64{{from.Lat, from.Lng}}
	for _, v := range opts.via {
		p, err := resolveTripPoint(ctx, api, v)
		if err != nil {
			return err
		}
		q.Via = append(q.Via, p)
		coords = append(coords, [2]float64{p.Lat, p.Lng})
	}
	coords = append(coords, [2]float64{to.Lat, to.Lng})

	routeBody := tripRouteBody(coords, opts)
	var route dmRouteInfo
	if opts.routeCacheable() {
		route, _, err = dmFetch(ctx, api, epRoute, routeBody, decodeRoute)
	} else {
		route, err = dmFetchLive(ctx, api, epRoute, routeBody, decodeRoute)
	}
	if err != nil {
		return fmt.Errorf("route: %w", err)
	}
	q.DistanceKM = dmRound(route.DistanceKM, 2)
	q.DurationMin = dmRound(route.DurationSec/60, 1)
	sent := addrkit.Downsample(route.Geometry, tripMaxGeometryPoints)
	q.RoutePoints = len(sent)

	toll, _, err := dmFetch(ctx, api, epToll, tripTollBody(sent, q.Vehicle), decodeToll)
	if err != nil {
		return fmt.Errorf("tolls: %w", err)
	}
	q.TollPlazaCount = toll.Count
	q.TotalTollINR = toll.Total
	q.Plazas = toll.Plazas
	return nil
}

// decodeRoute reads a /route reply. The live API nests the route under
// recommended_route (next to alternates); a flat reply is accepted as a
// fallback.
func decodeRoute(raw json.RawMessage) (dmRouteInfo, error) {
	type routeBody struct {
		Distance float64         `json:"distance"`
		Duration float64         `json:"duration"`
		Geometry json.RawMessage `json:"geometry"`
	}
	var resp struct {
		routeBody
		Recommended *routeBody `json:"recommended_route"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return dmRouteInfo{}, fmt.Errorf("%w: decoding %s: %v", errDMUnusableResponse, epRoute, err)
	}
	body := resp.routeBody
	if resp.Recommended != nil {
		body = *resp.Recommended
	}
	geometry, err := parseRouteGeometry(body.Geometry)
	if err != nil {
		return dmRouteInfo{}, fmt.Errorf("%w: %s geometry: %v", errDMUnusableResponse, epRoute, err)
	}
	if len(geometry) < 2 {
		return dmRouteInfo{}, fmt.Errorf("%w: %s returned %d geometry point(s), need at least 2", errDMUnusableResponse, epRoute, len(geometry))
	}
	return dmRouteInfo{DistanceKM: body.Distance, DurationSec: body.Duration, Geometry: geometry}, nil
}

func decodeToll(raw json.RawMessage) (dmTollInfo, error) {
	var resp struct {
		Data struct {
			Routes []struct {
				Summary struct {
					Count     float64  `json:"toll_plaza_count"`
					TotalToll *float64 `json:"total_toll_inr"`
					TotalCost *float64 `json:"total_cost_inr"`
				} `json:"summary"`
				Plazas []map[string]any `json:"toll_plazas"`
			} `json:"routes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return dmTollInfo{}, fmt.Errorf("%w: decoding %s: %v", errDMUnusableResponse, epToll, err)
	}
	if len(resp.Data.Routes) == 0 {
		return dmTollInfo{}, fmt.Errorf("%w: %s returned no routes", errDMUnusableResponse, epToll)
	}
	r := resp.Data.Routes[0]
	info := dmTollInfo{Count: int(r.Summary.Count), Plazas: r.Plazas}
	switch {
	case r.Summary.TotalToll != nil:
		info.Total = *r.Summary.TotalToll
	case r.Summary.TotalCost != nil:
		info.Total = *r.Summary.TotalCost
	}
	if info.Plazas == nil {
		info.Plazas = []map[string]any{}
	}
	return info, nil
}

// parseRouteGeometry reads [[lat,lng],...] or, if the API sent an encoded
// polyline, decodes it at dmRoutePolylinePrecision.
func parseRouteGeometry(raw json.RawMessage) ([][2]float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("missing")
	}
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		return addrkit.DecodePolyline(encoded, dmRoutePolylinePrecision)
	}
	var pts [][]float64
	if err := json.Unmarshal(raw, &pts); err != nil {
		return nil, err
	}
	out := make([][2]float64, 0, len(pts))
	for i, p := range pts {
		if len(p) < 2 {
			return nil, fmt.Errorf("point %d has %d value(s)", i, len(p))
		}
		out = append(out, [2]float64{p[0], p[1]})
	}
	return out, addrkit.ValidatePoints(out)
}

// plazaLabel names a toll plaza. A close-loop toll has no top-level name, only
// entry_toll and exit_toll, and is shown as "entry -> exit".
func plazaLabel(p map[string]any) string {
	if n, ok := p["name"].(string); ok && n != "" {
		return n
	}
	name := func(key string) string {
		m, _ := p[key].(map[string]any)
		n, _ := m["name"].(string)
		return n
	}
	entry, exit := name("entry_toll"), name("exit_toll")
	if entry == "" && exit == "" {
		return ""
	}
	return entry + " -> " + exit
}

var tripCSVColumns = []string{"from", "to", "from_lat", "from_lng", "to_lat", "to_lng", "vehicle", "travel_mode", "traffic_aware", "distance_km", "duration_min", "toll_plaza_count", "total_toll_inr", "plazas", "error"}

func tripCSVRows(lanes []tripQuote) [][]string {
	rows := make([][]string, 0, len(lanes))
	for _, q := range lanes {
		var fl, fg, tl, tg string
		if q.FromResolved != nil {
			fl, fg = addrkit.FormatFloat(q.FromResolved.Lat), addrkit.FormatFloat(q.FromResolved.Lng)
		}
		if q.ToResolved != nil {
			tl, tg = addrkit.FormatFloat(q.ToResolved.Lat), addrkit.FormatFloat(q.ToResolved.Lng)
		}
		var names []string
		for _, p := range q.Plazas {
			if n := plazaLabel(p); n != "" {
				names = append(names, n)
			}
		}
		rows = append(rows, []string{
			q.From, q.To, fl, fg, tl, tg, q.Vehicle, q.TravelMode, strconv.FormatBool(q.TrafficAware),
			addrkit.FormatFloat(q.DistanceKM), addrkit.FormatFloat(q.DurationMin), strconv.Itoa(q.TollPlazaCount),
			addrkit.FormatFloat(q.TotalTollINR), strings.Join(names, "; "), q.Error,
		})
	}
	return rows
}

func printTripQuotes(cmd *cobra.Command, flags *rootFlags, lanes []tripQuote, many bool) error {
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		if many {
			return dmPrint(cmd.OutOrStdout(), lanes, flags)
		}
		return dmPrint(cmd.OutOrStdout(), lanes[0], flags)
	}
	w := cmd.OutOrStdout()
	for _, q := range lanes {
		if q.Error != "" {
			fmt.Fprintf(w, "%s -> %s: FAILED: %s\n", dmStripControl(q.From), dmStripControl(q.To), dmStripControl(q.Error))
			continue
		}
		fmt.Fprintf(w, "%s -> %s (%s)\n", dmStripControl(q.From), dmStripControl(q.To), q.Vehicle)
		fmt.Fprintf(w, "  distance %.1f km, duration %.0f min, %d toll plaza(s), total toll INR %.0f\n", q.DistanceKM, q.DurationMin, q.TollPlazaCount, q.TotalTollINR)
	}
	return nil
}
