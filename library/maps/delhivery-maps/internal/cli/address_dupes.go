// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source local

package cli

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
)

type dupeMember struct {
	Raw       string   `json:"raw"`
	Formatted string   `json:"formatted,omitempty"`
	Lat       *float64 `json:"lat,omitempty"`
	Lng       *float64 `json:"lng,omitempty"`
}

type dupeGroup struct {
	GroupID int          `json:"group_id"`
	Reason  string       `json:"reason"` // text | geo
	Members []dupeMember `json:"members"`
}

// Only precise rows can be duplicates of a delivery location: coarse
// granularities (LOCALITY, SUBLOCALITY_VILLAGE, CITY_PINCODE) and rows with no
// validate result never group, and a known geocode error radius above
// dupeMaxErrorRadiusM disqualifies a row.
const (
	dupeDefaultRadiusM  = 30
	dupeMaxErrorRadiusM = 100
)

var dupeGranularities = []string{"PREMISE", "STREET_LANDMARK"}

type dupeRow struct {
	rawKey   string
	member   dupeMember
	textKey  string
	hasPoint bool
	eligible bool
}

func newNovelAddressDupesCmd(flags *rootFlags) *cobra.Command {
	var flagRadiusM float64
	var flagDB string

	cmd := &cobra.Command{
		Use:   "dupes",
		Short: "Find repeated delivery locations among addresses you have cleaned.",
		Long: strings.Trim(fmt.Sprintf(`
Groups cleaned addresses that are the same place written differently. Two
addresses land in one group when their formatted text is identical after
lowercasing and dropping punctuation (reason "text"), or when their geocoded
coordinates lie within --radius-m metres of each other (reason "geo"; a chain
of close neighbours forms one group). Only groups of two or more are listed.

Only precise addresses are compared: the row must have a validate result with
granularity PREMISE or STREET_LANDMARK, and a known geocode error radius of at
most %d m. Coarse rows (LOCALITY, SUBLOCALITY_VILLAGE, CITY_PINCODE) and rows
that skipped the validate step never group, because a shared pincode centroid
is not a shared doorstep.

It reads the local address table filled by 'address clean' and never calls
the API. With nothing cleaned yet it prints an empty list and exits 0.

%s`, dupeMaxErrorRadiusM, dmRetentionNote), "\n"),
		Example: strings.Trim(`
  delhivery-maps-pp-cli address dupes --radius-m 30 --agent
  delhivery-maps-pp-cli address dupes --radius-m 100 --json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local", "pp:happy-args": "--radius-m=30"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "address dupes")
			}
			if !(flagRadiusM > 0) || flagRadiusM > addrkit.MaxClusterRadiusM {
				return dmUsage(cmd, "--radius-m must be a number greater than 0 and at most %d (got %v)", int(addrkit.MaxClusterRadiusM), flagRadiusM)
			}

			groups := make([]dupeGroup, 0)
			s, exists, err := openDMAddressReader(cmd.Context(), flagDB)
			if err != nil {
				return err
			}
			if exists {
				defer s.Close()
				rows, err := loadDupeRows(cmd.Context(), s.DB())
				if err != nil {
					return err
				}
				groups = findDupeGroups(rows, flagRadiusM)
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), "address dupes: no cleaned addresses found; run 'address clean' first")
			}
			if wantsHumanTable(cmd.OutOrStdout(), flags) && len(groups) > 0 {
				for _, g := range groups {
					fmt.Fprintf(cmd.OutOrStdout(), "group %d (%s, %d addresses)\n", g.GroupID, g.Reason, len(g.Members))
					for _, m := range g.Members {
						fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", dmStripControl(m.Raw))
					}
				}
				return nil
			}
			return dmPrint(cmd.OutOrStdout(), groups, flags)
		},
	}
	cmd.Flags().Float64Var(&flagRadiusM, "radius-m", dupeDefaultRadiusM, "Addresses whose coordinates are within this many metres count as the same place.")
	cmd.Flags().StringVar(&flagDB, "db", "", "SQLite database holding the cleaned addresses (default: the CLI's data database).")
	return cmd
}

// loadDupeRows drains dm_address into memory before any grouping work.
func loadDupeRows(ctx context.Context, db *sql.DB) ([]dupeRow, error) {
	rs, err := db.QueryContext(ctx, `SELECT raw_key, raw, formatted, lat, lng, granularity, error_radius FROM dm_address ORDER BY raw_key`)
	if err != nil {
		return nil, fmt.Errorf("reading address table: %w", err)
	}
	defer rs.Close()
	var out []dupeRow
	for rs.Next() {
		var key string
		var raw, formatted, granularity sql.NullString
		var lat, lng sql.NullFloat64
		var radius sql.NullInt64
		if err := rs.Scan(&key, &raw, &formatted, &lat, &lng, &granularity, &radius); err != nil {
			return nil, fmt.Errorf("reading address table: %w", err)
		}
		r := dupeRow{rawKey: key, member: dupeMember{Raw: dmStripControl(raw.String), Formatted: dmStripControl(formatted.String)}}
		r.eligible = slices.Contains(dupeGranularities, strings.ToUpper(granularity.String)) && (!radius.Valid || radius.Int64 <= dupeMaxErrorRadiusM)
		if lat.Valid && lng.Valid {
			la, ln := lat.Float64, lng.Float64
			r.member.Lat, r.member.Lng = &la, &ln
			r.hasPoint = true
		}
		r.textKey = addrkit.NormalizeText(formatted.String)
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("reading address table: %w", err)
	}
	return out, nil
}

// findDupeGroups returns text groups and geo groups of two or more rows,
// largest first with ids assigned in that order. A geo cluster whose members
// all share one text key is already reported as a text group and is skipped.
func findDupeGroups(rows []dupeRow, radiusM float64) []dupeGroup {
	type pending struct {
		reason  string
		members []int
	}
	var all []pending

	byText := map[string][]int{}
	var textOrder []string
	for i, r := range rows {
		if r.textKey == "" || !r.eligible {
			continue
		}
		if _, ok := byText[r.textKey]; !ok {
			textOrder = append(textOrder, r.textKey)
		}
		byText[r.textKey] = append(byText[r.textKey], i)
	}
	for _, k := range textOrder {
		if len(byText[k]) >= 2 {
			all = append(all, pending{"text", byText[k]})
		}
	}

	var points []addrkit.LatLng
	var pointRow []int
	for i, r := range rows {
		if r.hasPoint && r.eligible {
			points = append(points, addrkit.LatLng{Lat: *r.member.Lat, Lng: *r.member.Lng})
			pointRow = append(pointRow, i)
		}
	}
	for _, cluster := range addrkit.ClusterByRadius(points, radiusM) {
		members := make([]int, 0, len(cluster))
		sameText := rows[pointRow[cluster[0]]].textKey != ""
		for _, ci := range cluster {
			ri := pointRow[ci]
			members = append(members, ri)
			if rows[ri].textKey != rows[pointRow[cluster[0]]].textKey {
				sameText = false
			}
		}
		if sameText {
			continue
		}
		all = append(all, pending{"geo", members})
	}

	sort.SliceStable(all, func(a, b int) bool {
		if len(all[a].members) != len(all[b].members) {
			return len(all[a].members) > len(all[b].members)
		}
		return rows[all[a].members[0]].rawKey < rows[all[b].members[0]].rawKey
	})
	groups := make([]dupeGroup, 0, len(all))
	for i, p := range all {
		g := dupeGroup{GroupID: i + 1, Reason: p.reason, Members: make([]dupeMember, 0, len(p.members))}
		for _, ri := range p.members {
			g.Members = append(g.Members, rows[ri].member)
		}
		groups = append(groups, g)
	}
	return groups
}
