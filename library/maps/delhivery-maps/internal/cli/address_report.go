// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source local

package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/store"
)

// reportGroupColumns maps each --group-by value to its dm_address column.
// The values are a fixed allow-list, so the column name is never user input.
var reportGroupColumns = map[string]string{
	"pincode":     "pincode",
	"city":        "city",
	"state":       "state",
	"granularity": "granularity",
	"quality":     "validated_quality",
}

// addressReportRow is one group in the report.
type addressReportRow struct {
	Group string `json:"group"`
	Count int    `json:"count"`
	// WithoutValidate counts rows that have no validate result yet; they are
	// left out of both percentages.
	WithoutValidate int      `json:"without_validate"`
	PctPremise      *float64 `json:"pct_premise"`
	PctOK           *float64 `json:"pct_ok"`
	AvgErrorRadius  *float64 `json:"avg_error_radius"`
}

func newNovelAddressReportCmd(flags *rootFlags) *cobra.Command {
	var flagGroupBy string
	var flagDB string

	cmd := &cobra.Command{
		Use:   "report",
		Short: "See address quality and granularity broken down by pincode or city across everything you have cleaned.",
		Long: strings.Trim(fmt.Sprintf(`
Aggregates the local address table filled by 'address clean': for each group
it shows how many addresses there are, the share resolved to PREMISE
granularity, the share with quality "ok", and the average geocode error
radius. The two shares are taken over addresses that have a validate result
(null when none do); without_validate counts the rest. It never calls the API.
With nothing cleaned yet it prints an empty list and exits 0.

%s`, dmRetentionNote), "\n"),
		Example: strings.Trim(`
  delhivery-maps-pp-cli address report --group-by pincode --agent
  delhivery-maps-pp-cli address report --group-by city --json
  delhivery-maps-pp-cli address report --group-by granularity`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local", "pp:happy-args": "--group-by=pincode"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "address report")
			}
			groupBy := strings.ToLower(strings.TrimSpace(flagGroupBy))
			column, ok := reportGroupColumns[groupBy]
			if !ok {
				return dmUsage(cmd, "--group-by must be one of pincode, city, granularity, quality, state (got %q)", flagGroupBy)
			}

			rows := make([]addressReportRow, 0)
			s, exists, err := openDMAddressReader(cmd.Context(), flagDB)
			if err != nil {
				return err
			}
			if exists {
				defer s.Close()
				rows, err = queryAddressReport(cmd.Context(), s.DB(), column)
				if err != nil {
					return err
				}
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "address report: no cleaned addresses found; run 'address clean' first")
			}
			if wantsHumanTable(cmd.OutOrStdout(), flags) && len(rows) > 0 {
				table := make([][]string, 0, len(rows))
				for _, r := range rows {
					table = append(table, []string{dmStripControl(r.Group), strconv.Itoa(r.Count), pctCell(r.PctPremise), pctCell(r.PctOK), floatCell(r.AvgErrorRadius), strconv.Itoa(r.WithoutValidate)})
				}
				return flags.printTable(cmd, []string{strings.ToUpper(groupBy), "COUNT", "PREMISE", "OK", "AVG_ERR_RADIUS", "NO_VALIDATE"}, table)
			}
			return dmPrint(cmd.OutOrStdout(), rows, flags)
		},
	}
	cmd.Flags().StringVar(&flagGroupBy, "group-by", "pincode", "Field to group by: pincode, city, granularity, quality or state.")
	cmd.Flags().StringVar(&flagDB, "db", "", "SQLite database holding the cleaned addresses (default: the CLI's data database).")
	return cmd
}

// openDMAddressReader opens the database read-only for the local address
// commands. exists is false, with no error, when the file or the address
// table is missing: nothing has been cleaned yet. The file is never created.
func openDMAddressReader(ctx context.Context, dbPath string) (*store.Store, bool, error) {
	if dbPath == "" {
		dbPath = defaultDBPath("delhivery-maps-pp-cli")
	}
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("checking %s: %w", dbPath, err)
	}
	s, err := store.OpenReadOnlyContext(ctx, dbPath)
	if err != nil {
		return nil, false, err
	}
	has, err := store.HasDelhiveryMapsAddressTable(ctx, s.DB())
	if err != nil {
		_ = s.Close()
		return nil, false, err
	}
	if !has {
		_ = s.Close()
		return nil, false, nil
	}
	return s, true, nil
}

func pctCell(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", *p)
}

func floatCell(f *float64) string {
	if f == nil {
		return "-"
	}
	return strconv.FormatFloat(*f, 'f', 1, 64)
}

func queryAddressReport(ctx context.Context, db *sql.DB, column string) ([]addressReportRow, error) {
	// #nosec G202 -- column is a constant from the reportGroupColumns allow-list, never user input.
	query := `SELECT COALESCE(NULLIF(TRIM(` + column + `), ''), '(unknown)') AS grp,
			COUNT(*),
			SUM(CASE WHEN validated_quality IS NULL THEN 1 ELSE 0 END),
			SUM(CASE WHEN granularity IS NOT NULL AND UPPER(granularity) = 'PREMISE' THEN 1 ELSE 0 END),
			SUM(CASE WHEN granularity IS NOT NULL THEN 1 ELSE 0 END),
			SUM(CASE WHEN validated_quality IS NOT NULL AND LOWER(validated_quality) = 'ok' THEN 1 ELSE 0 END),
			SUM(CASE WHEN validated_quality IS NOT NULL THEN 1 ELSE 0 END),
			AVG(error_radius)
		FROM dm_address
		GROUP BY grp
		ORDER BY COUNT(*) DESC, grp`
	rs, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying address report: %w", err)
	}
	defer rs.Close()
	pct := func(num, den int) *float64 {
		if den == 0 {
			return nil
		}
		v := dmRound(100*float64(num)/float64(den), 1)
		return &v
	}
	out := make([]addressReportRow, 0)
	for rs.Next() {
		var r addressReportRow
		var premise, withGran, ok, withQual int
		var avg sql.NullFloat64
		if err := rs.Scan(&r.Group, &r.Count, &r.WithoutValidate, &premise, &withGran, &ok, &withQual, &avg); err != nil {
			return nil, fmt.Errorf("reading address report: %w", err)
		}
		r.Group = dmStripControl(r.Group)
		r.PctPremise = pct(premise, withGran)
		r.PctOK = pct(ok, withQual)
		if avg.Valid {
			v := dmRound(avg.Float64, 1)
			r.AvgErrorRadius = &v
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("reading address report: %w", err)
	}
	return out, nil
}
