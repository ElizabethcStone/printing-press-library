// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
)

const (
	// coverageResolution is the isochrone resolution requested from the API.
	coverageResolution  = "medium"
	coverageDefaultMode = "auto"
	// The API's isochrone limits: 120 minutes of travel time, 100 km of distance.
	coverageMaxSeconds = 7200
	coverageMaxMeters  = 100000
)

// coverageSummary is the JSON envelope printed after a check.
type coverageSummary struct {
	Hub          [2]float64 `json:"hub"`
	CostType     string     `json:"cost_type"`
	CostValue    int        `json:"cost_value"`
	Total        int        `json:"total"`
	Inside       int        `json:"inside"`
	Outside      int        `json:"outside"`
	Ungeocoded   int        `json:"ungeocoded"` // missing + failed + deferred
	Missing      int        `json:"missing"`    // no coordinates and no address
	Failed       int        `json:"failed"`     // the geocode call failed or returned nothing
	Deferred     int        `json:"deferred"`   // not geocoded because of the quota or call budget
	PlannedCalls int        `json:"planned_calls"`
	CallsMade    int64      `json:"calls_made"`
	CacheHits    int64      `json:"cache_hits"`
	MaxCalls     int        `json:"max_calls"`
	RateLimited  bool       `json:"rate_limited"`
	Out          string     `json:"out,omitempty"`
	// Flagged holds every order with its verdict when no --out file is written.
	Flagged []map[string]any `json:"flagged,omitempty"`
}

// coveragePoint is one order's position and how it was obtained.
type coveragePoint struct {
	lat, lng float64
	status   string // given | geocoded | cached | failed | deferred | missing
	has      bool
}

func newNovelCoverageCheckCmd(flags *rootFlags) *cobra.Command {
	var flagHub, flagFile, flagOut, flagTravelMode, flagDB string
	var flagMinutes, flagKm float64
	var flagMaxCalls, flagMaxRows int
	var flagForce, flagStdin bool

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Flag orders that fall outside a hub's 45-minute (or N km) service area.",
		Long: strings.Trim(fmt.Sprintf(`
Draws the hub's service area with one isochrone call (--minutes of driving
time, or --km of distance), then tests every order against that polygon
locally. Orders (a CSV, JSON Lines or JSON-array file, or standard input with
--stdin or --file -) need lat and lng columns, or an address column that is
geocoded through the local cache. The output repeats every order column and adds
inside (true or false) and geocode_status (given, geocoded, cached, failed,
deferred or missing): with --out it is written to that CSV file, without it no
file is written and the orders are returned in the JSON output (flagged) or,
with --csv, printed as CSV. Orders outside the area are a result, not an
error: the command exits 0.

The isochrone is cached for %[4]s and each geocode for %[5]s, so
re-running costs nothing. Calls needed are planned up front (%[1]d-call quota per
%[6]s) and capped by the estimated remaining budget, lowered by --max-calls;
addresses beyond the cap are marked deferred. The isochrone is required: if it
cannot be fetched because of the quota or the call budget the command exits 7.
An --out CSV is never overwritten without --force and may not be the input
file. Input cells that start with = + - @ are written back with a leading
apostrophe, a deliberate guard against spreadsheet formula injection.

%[2]s

%[3]s`, dmDefaultDailyBudget, dmExitCodesNote, dmRetentionNote, dmRouteTTLText, dmAddressTTLText, dmWindowText), "\n"),
		Example: strings.Trim(`
  delhivery-maps-pp-cli coverage check --hub 28.4599,77.0679 --minutes 20 --stdin
  delhivery-maps-pp-cli coverage check --hub 28.4599,77.0679 --minutes 45 --file orders.csv --out flagged.csv
  delhivery-maps-pp-cli coverage check --hub 26.9124,75.7873 --km 25 --file jaipur_orders.csv --out flagged.csv --travel-mode auto`, "\n"),
		// The live-dogfood runner feeds pp:happy-stdin (valid JSON: two orders with
		// coordinates) on standard input and appends --stdin: 1 isochrone call,
		// then served from cache.
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
			"pp:happy-args":  "--hub=28.4599,77.0679;--minutes=20",
			"pp:happy-stdin": `[{"lat":28.4650,"lng":77.0700},{"lat":28.7041,"lng":77.1025}]`,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return dmWriteDryRunPlan(cmd, flags, "coverage check",
					"call the isochrone endpoint once for the hub, geocode address-only orders, and flag orders outside the area; no request sent",
					map[string]any{
						"hub": flagHub, "minutes": flagMinutes, "km": flagKm, "file": flagFile, "stdin": flagStdin, "out": flagOut,
						"planned_calls": "1 isochrone + 1 per uncached address-only order (the input is not read in dry-run)",
					})
			}
			hubLat, hubLng, ok := addrkit.ParseLatLng(flagHub)
			if !ok {
				return dmUsage(cmd, `--hub must be a "lat,lng" pair such as 28.4599,77.0679`)
			}
			for name, v := range map[string]float64{"minutes": flagMinutes, "km": flagKm} {
				if err := dmFiniteFlag(cmd, name, v, true); err != nil {
					return err
				}
			}
			if (flagMinutes > 0) == (flagKm > 0) {
				return dmUsage(cmd, "pass exactly one of --minutes or --km, greater than 0")
			}
			if flagMinutes*60 > coverageMaxSeconds {
				return dmUsage(cmd, "--minutes %v is over the isochrone limit of %d minutes", flagMinutes, coverageMaxSeconds/60)
			}
			if flagKm*1000 > coverageMaxMeters {
				return dmUsage(cmd, "--km %v is over the isochrone limit of %d km", flagKm, coverageMaxMeters/1000)
			}
			if !dmValidTravelMode(flagTravelMode) {
				return dmUsage(cmd, "--travel-mode %q is not valid; use one of: %s", flagTravelMode, strings.Join(dmTravelModes, ", "))
			}
			inPath, err := dmInputPath(cmd, "file", flagFile, flagStdin)
			if err != nil {
				return err
			}
			if inPath == "" {
				return dmUsage(cmd, "--file is required (or --stdin to read standard input)")
			}
			flagFile = inPath
			if err := dmCheckOut(cmd, flagFile, flagOut, flagForce); err != nil {
				return err
			}

			table, err := dmReadTable(cmd, flagFile, flagMaxRows)
			if err != nil {
				return err
			}
			latCol := table.Col("lat", "latitude")
			lngCol := table.Col("lng", "lon", "long", "longitude")
			addrCol := table.Col("address")
			if (latCol < 0 || lngCol < 0) && addrCol < 0 {
				return dmUsage(cmd, "%s needs lat and lng columns or an address column (found: %s)", flagFile, strings.Join(table.Header, ", "))
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

			costType, costValue := "time", int(math.Round(flagMinutes*60))
			if flagKm > 0 {
				costType, costValue = "distance", int(math.Round(flagKm*1000))
			}
			isoBody := map[string]any{
				"origin":         [2]float64{hubLat, hubLng},
				"cost_type":      costType,
				"cost_value":     costValue,
				"travel_mode":    flagTravelMode,
				"resolution":     coverageResolution,
				"encode_geojson": false,
			}

			// Positions: given coordinates, or distinct addresses to geocode
			// (keyed like the cache so "Sector 45  Gurgaon" and "sector 45 gurgaon"
			// are one lookup).
			points := make([]coveragePoint, len(table.Rows))
			addrKeys := make([]string, len(table.Rows))
			firstAddr := map[string]string{}
			var geocodeOrder []string
			for i, row := range table.Rows {
				if latCol >= 0 && lngCol >= 0 {
					if lat, lng, ok := addrkit.ParseLatLngPair(addrkit.Cell(row, latCol), addrkit.Cell(row, lngCol)); ok {
						points[i] = coveragePoint{lat: lat, lng: lng, status: "given", has: true}
						continue
					}
				}
				addr := strings.TrimSpace(addrkit.Cell(row, addrCol))
				if addrCol < 0 || addr == "" {
					points[i].status = "missing"
					continue
				}
				key := addrkit.NormalizeKey(addr)
				addrKeys[i] = key
				if _, seen := firstAddr[key]; !seen {
					firstAddr[key] = addr
					geocodeOrder = append(geocodeOrder, key)
				}
			}

			// Plan the calls: one isochrone plus one geocode per uncached address.
			isoNeeded := 1
			if has, err := api.cacheHas(ctx, epIsochrone, isoBody); err != nil {
				return err
			} else if has {
				isoNeeded = 0
			}
			var uncached []string
			isUncached := map[string]bool{}
			for _, key := range geocodeOrder {
				has, err := api.cacheHas(ctx, epGeocode, dmAddressBody(firstAddr[key]))
				if err != nil {
					return err
				}
				if !has {
					uncached = append(uncached, key)
					isUncached[key] = true
				}
			}
			budget, err := dmPlanBudget(ctx, api, cmd, flagMaxCalls, dmBucketPool)
			if err != nil {
				return err
			}
			if isoNeeded > budget.Max {
				return dmFinishRunErr(fmt.Errorf("%w: the isochrone is not cached and the call budget (%d) leaves no room for it; nothing was sent", errDMRateLimited, budget.Max))
			}
			allowedGeocodes := budget.Max - isoNeeded
			deferredKeys := map[string]bool{}
			if len(uncached) > allowedGeocodes {
				for _, key := range uncached[allowedGeocodes:] {
					deferredKeys[key] = true
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "coverage check: %d address(es) need geocoding but only %d call(s) fit the call budget; the rest are marked deferred\n", len(uncached), allowedGeocodes)
			}

			// Draw the service area.
			area, _, err := dmFetch(ctx, api, epIsochrone, isoBody, parseIsochrone)
			if err != nil {
				return dmFinishRunErr(err)
			}

			summary := coverageSummary{
				Hub: [2]float64{hubLat, hubLng}, CostType: costType, CostValue: costValue,
				Total: len(table.Rows), PlannedCalls: isoNeeded + len(uncached) - len(deferredKeys),
				MaxCalls: budget.Max, Out: flagOut, RateLimited: budget.LockedOut && len(deferredKeys) > 0,
			}
			geocoded := map[string]coveragePoint{}
			stopped := false
			var fatal error
			for _, key := range geocodeOrder {
				// Once stopped (429 or rejected token) only addresses that are
				// already cached, and so need no call, can still be located.
				if deferredKeys[key] || (stopped && isUncached[key]) {
					geocoded[key] = coveragePoint{status: "deferred"}
					continue
				}
				g, hit, err := api.geocode(ctx, firstAddr[key])
				switch {
				case err == nil:
					st := "geocoded"
					if hit {
						st = "cached"
					}
					geocoded[key] = coveragePoint{lat: *g.Lat, lng: *g.Lng, status: st, has: true}
				case errors.Is(err, errDMRateLimited):
					stopped, summary.RateLimited = true, true
					geocoded[key] = coveragePoint{status: "deferred"}
				case dmIsFatal(err):
					stopped, fatal = true, err
					geocoded[key] = coveragePoint{status: "failed"}
				default:
					fmt.Fprintf(cmd.ErrOrStderr(), "coverage check: could not geocode %q: %s\n", dmStripControl(firstAddr[key]), dmErrText(err))
					geocoded[key] = coveragePoint{status: "failed"}
				}
			}

			header := append(append([]string{}, table.Header...), "inside", "geocode_status")
			rows := make([][]string, 0, len(table.Rows))
			for i, row := range table.Rows {
				p := points[i]
				if p.status == "" {
					p = geocoded[addrKeys[i]]
				}
				inside := ""
				switch {
				case p.has:
					// GeoJSON is [lng, lat]: x is longitude, y is latitude.
					if area.Contains(p.lng, p.lat) {
						inside = "true"
						summary.Inside++
					} else {
						inside = "false"
						summary.Outside++
					}
				case p.status == "missing":
					summary.Missing++
				case p.status == "deferred":
					summary.Deferred++
				default:
					summary.Failed++
				}
				rows = append(rows, append(append([]string{}, row...), inside, p.status))
			}
			summary.Ungeocoded = summary.Missing + summary.Failed + summary.Deferred
			summary.CallsMade, summary.CacheHits = api.calls.Load(), api.hits.Load()
			where := "geocode_status in each order"
			if flagOut != "" {
				if err := dmWriteCSV(cmd, flagOut, header, rows, flagForce); err != nil {
					return err
				}
				where = "geocode_status in " + flagOut
			} else {
				summary.Flagged = make([]map[string]any, 0, len(rows))
				for i, row := range rows {
					obj := make(map[string]any, len(header))
					for c, h := range table.Header {
						obj[h] = addrkit.Cell(row, c)
					}
					switch rows[i][len(rows[i])-2] {
					case "true":
						obj["inside"] = true
					case "false":
						obj["inside"] = false
					default:
						obj["inside"] = nil
					}
					obj["geocode_status"] = rows[i][len(rows[i])-1]
					summary.Flagged = append(summary.Flagged, obj)
				}
			}
			if summary.Ungeocoded > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "coverage check: %d order(s) could not be geocoded (%d with no coordinates or address, %d failed, %d deferred by the quota or call budget)\n",
					summary.Ungeocoded, summary.Missing, summary.Failed, summary.Deferred)
			}
			if summary.RateLimited {
				fmt.Fprintln(cmd.ErrOrStderr(), "coverage check: the API quota is exhausted (HTTP 429 or an earlier lockout); deferred orders can be retried after it resets")
			}
			switch {
			case flags.csv && flagOut == "":
				if err := dmWriteRowsCSV(cmd, header, rows); err != nil {
					return err
				}
			case wantsHumanTable(cmd.OutOrStdout(), flags):
				dest := flagOut
				if dest == "" {
					dest = "the table below"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%d order(s): %d inside, %d outside, %d ungeocoded -> %s\n", summary.Total, summary.Inside, summary.Outside, summary.Ungeocoded, dest)
				if flagOut == "" && len(rows) > 0 {
					printable := make([][]string, 0, len(rows))
					for _, r := range rows {
						c := make([]string, len(r))
						for i, v := range r {
							c[i] = dmStripControl(v)
						}
						printable = append(printable, c)
					}
					if err := flags.printTable(cmd, header, printable); err != nil {
						return err
					}
				}
			default:
				if err := dmPrint(cmd.OutOrStdout(), summary, flags); err != nil {
					return err
				}
			}
			if fatal != nil {
				return dmFinishRunErr(fatal)
			}
			if summary.Failed > 0 && summary.Inside+summary.Outside == 0 && summary.Deferred == 0 {
				return apiErr(fmt.Errorf("no order could be located: %d geocode(s) failed (see %s)", summary.Failed, where))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagHub, "hub", "", `Hub location as "lat,lng", for example 28.4599,77.0679.`)
	cmd.Flags().Float64Var(&flagMinutes, "minutes", 0, "Service-area size as driving minutes from the hub (use this or --km).")
	cmd.Flags().Float64Var(&flagKm, "km", 0, "Service-area size as kilometres of travel distance from the hub (use this or --minutes).")
	cmd.Flags().StringVar(&flagFile, "file", "", "CSV, JSON Lines or JSON-array file of orders with lat and lng columns, or an address column to geocode; - reads standard input.")
	cmd.Flags().BoolVar(&flagStdin, "stdin", false, dmStdinHelp("file"))
	cmd.Flags().StringVar(&flagOut, "out", "", "Optional path of the flagged CSV to write (every order column plus inside and geocode_status); without it no file is written and the orders are returned in the JSON output.")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Overwrite --out if it already exists.")
	cmd.Flags().StringVar(&flagTravelMode, "travel-mode", coverageDefaultMode, "Travel mode used to draw the service area: "+strings.Join(dmTravelModes, ", ")+".")
	cmd.Flags().IntVar(&flagMaxCalls, "max-calls", 0, dmMaxCallsUsage("live API calls", "Addresses"))
	cmd.Flags().IntVar(&flagMaxRows, "max-rows", dmDefaultMaxRows, fmt.Sprintf("Refuse a --file with more rows than this (at most %d).", addrkit.HardMaxRows))
	cmd.Flags().StringVar(&flagDB, "db", "", "SQLite database for the request cache (default: the CLI's data database).")
	return cmd
}

// parseIsochrone builds the covered area from a GeoJSON FeatureCollection.
// Polygon and MultiPolygon features are supported; coordinates are [lng, lat].
func parseIsochrone(raw json.RawMessage) (addrkit.MultiPolygon, error) {
	var fc struct {
		Features []struct {
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(raw, &fc); err != nil {
		return nil, fmt.Errorf("%w: decoding /isochrone: %v", errDMUnusableResponse, err)
	}
	var area addrkit.MultiPolygon
	for _, f := range fc.Features {
		switch f.Geometry.Type {
		case "Polygon":
			var rings [][][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &rings); err != nil {
				return nil, fmt.Errorf("%w: /isochrone polygon: %v", errDMUnusableResponse, err)
			}
			area = append(area, toPolygon(rings))
		case "MultiPolygon":
			var polys [][][][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &polys); err != nil {
				return nil, fmt.Errorf("%w: /isochrone multipolygon: %v", errDMUnusableResponse, err)
			}
			for _, rings := range polys {
				area = append(area, toPolygon(rings))
			}
		}
	}
	if len(area) == 0 {
		return nil, fmt.Errorf("%w: /isochrone returned no polygon", errDMUnusableResponse)
	}
	return area, nil
}

func toPolygon(rings [][][2]float64) addrkit.Polygon {
	p := make(addrkit.Polygon, 0, len(rings))
	for _, r := range rings {
		p = append(p, addrkit.Ring(r))
	}
	return p
}
