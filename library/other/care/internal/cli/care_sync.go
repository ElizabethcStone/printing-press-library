// Copyright 2026 beetz12. Licensed under Apache-2.0.
// `sync` - persist caregiver search results to a local SQLite db for offline
// listing, dedupe across searches, and outreach (contacted) tracking.
// Hand-authored; safe across regen. Uses modernc.org/sqlite (pure Go, no CGO).

package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mvanhorn/printing-press-library/library/other/care/internal/cliutil"

	_ "modernc.org/sqlite"

	"github.com/spf13/cobra"
)

func careDBPath() string {
	dir, err := cliutil.DataDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "care.db")
}

func openCareDB() (*sql.DB, error) {
	p := careDBPath()
	if p == "" {
		return nil, fmt.Errorf("cannot resolve care database path")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", p)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS caregivers (
  id TEXT PRIMARY KEY,
  name TEXT, city TEXT, state TEXT,
  years_experience INTEGER, hourly_rate REAL,
  total_reviews INTEGER, avg_rating REAL,
  has_care_check INTEGER, bio TEXT, zip TEXT,
  contacted_at TEXT, first_seen TEXT, last_synced TEXT
);
CREATE TABLE IF NOT EXISTS caregiver_zips (
  caregiver_id TEXT,
  zip TEXT,
  PRIMARY KEY (caregiver_id, zip)
);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// careMarkContacted records (by caregiver UUID) that a caregiver was contacted,
// so outreach dedupe (`sync list --uncontacted`) reflects completed sends even
// when the caregiver has not been synced yet (e.g. `find` -> `outreach send` or
// `messages reply`). A later `sync run` updates the caregiver's profile fields
// via ON CONFLICT(id) DO UPDATE while preserving `contacted_at`.
func careMarkContacted(caregiverUUID string, caregiverName ...string) error {
	if caregiverUUID == "" {
		return fmt.Errorf("no caregiver id to record")
	}
	name := ""
	if len(caregiverName) > 0 {
		name = caregiverName[0]
	}
	db, err := openCareDB()
	if err != nil {
		return err
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`INSERT INTO caregivers
  (id,name,city,state,years_experience,hourly_rate,total_reviews,avg_rating,has_care_check,bio,zip,contacted_at,first_seen,last_synced)
  VALUES (?,?, '', '', 0, 0, 0, 0, 0, '', '', ?, ?, '')
  ON CONFLICT(id) DO UPDATE SET
    contacted_at = excluded.contacted_at,
    name = CASE WHEN caregivers.name IS NULL OR caregivers.name = '' THEN excluded.name ELSE caregivers.name END`,
		caregiverUUID, name, now, now)
	return err
}

// careSearchCaregivers runs SearchProvidersChildCare and returns parsed
// summaries. Self-contained so `find` stays untouched.
func careSearchCaregivers(ctx context.Context, flags *rootFlags, zip, careType, sortOrder string, pageSize int) ([]careCaregiverSummary, int, error) {
	vars := map[string]any{
		"input": map[string]any{
			"careType": careType,
			"filters": map[string]any{
				"postalCode":      zip,
				"searchPageSize":  pageSize,
				"searchSortOrder": sortOrder,
			},
		},
	}
	data, err := careGraphQL(ctx, flags, careQSearchProvidersOp, careQSearchProviders, vars)
	if err != nil {
		return nil, 0, err
	}
	var wrap struct {
		Search struct {
			Message string `json:"message"`
			Conn    struct {
				TotalHits int `json:"totalHits"`
				Edges     []struct {
					Node careCaregiverNode `json:"node"`
				} `json:"edges"`
			} `json:"searchProvidersConnection"`
		} `json:"searchProvidersChildCare"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return nil, 0, fmt.Errorf("parsing caregivers: %w", err)
	}
	// Surface a search-level error variant (e.g. invalid postal code) instead of
	// reporting a normal empty run. Mirrors care_search.go.
	if wrap.Search.Message != "" && wrap.Search.Conn.TotalHits == 0 {
		return nil, 0, fmt.Errorf("care.com search error: %s", wrap.Search.Message)
	}
	var out []careCaregiverSummary
	for _, e := range wrap.Search.Conn.Edges {
		if e.Node.Typename == "Caregiver" {
			out = append(out, e.Node.summary())
		}
	}
	return out, wrap.Search.Conn.TotalHits, nil
}

func newCareSyncCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "sync",
		Short:       "Persist caregiver searches locally (offline list, dedupe, contacted-tracking)",
		Long:        "Sync search results into a local SQLite db (care.db in the CLI data directory) so you can list caregivers offline, dedupe across searches, and track who you've contacted.",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:parent-group": "true", "pp:typed-exit-codes": "0,2"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newCareSyncRunCmd(flags))
	cmd.AddCommand(newCareSyncListCmd(flags))
	cmd.AddCommand(newCareSyncMarkContactedCmd(flags))
	return cmd
}

func newCareSyncRunCmd(flags *rootFlags) *cobra.Command {
	var zip, careType, sortOrder string
	var limit int
	cmd := &cobra.Command{
		Use:     "run",
		Short:   "Search a zip and persist the caregivers locally",
		Example: "  care-pp-cli sync run --zip 90210 --limit 40",
		RunE: func(cmd *cobra.Command, args []string) error {
			if zip == "" {
				return usageErr(fmt.Errorf("--zip is required"))
			}
			if dryRunOK(flags) {
				if !wantsHumanTable(cmd.OutOrStdout(), flags) {
					return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
						"dry_run": true,
						"action":  fmt.Sprintf("would sync caregivers near %s", zip),
						"zip":     zip,
						"limit":   limit,
					}, flags)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "would sync caregivers near %s\n", zip)
				return nil
			}
			to := flags.timeout
			if to <= 0 {
				to = 45 * time.Second
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), to)
			defer cancel()
			if limit < 1 {
				limit = 40
			}
			results, _, err := careSearchCaregivers(ctx, flags, zip, careType, sortOrder, limit)
			if err != nil {
				return err
			}
			db, err := openCareDB()
			if err != nil {
				return err
			}
			defer db.Close()
			now := time.Now().UTC().Format(time.RFC3339)
			var added, updated int
			for _, r := range results {
				var exists int
				_ = db.QueryRow(`SELECT 1 FROM caregivers WHERE id=?`, r.ID).Scan(&exists)
				_, err := db.Exec(`INSERT INTO caregivers
  (id,name,city,state,years_experience,hourly_rate,total_reviews,avg_rating,has_care_check,bio,zip,first_seen,last_synced)
  VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
  ON CONFLICT(id) DO UPDATE SET
    name=excluded.name, city=excluded.city, state=excluded.state,
    years_experience=excluded.years_experience, hourly_rate=excluded.hourly_rate,
    total_reviews=excluded.total_reviews, avg_rating=excluded.avg_rating,
    has_care_check=excluded.has_care_check, bio=excluded.bio, zip=excluded.zip,
    last_synced=excluded.last_synced`,
					r.ID, r.Name, r.City, r.State, r.YearsExperience, r.HourlyRate,
					r.TotalReviews, r.AvgRating, boolToInt(r.HasCareCheck), r.Bio, zip, now, now)
				if err != nil {
					return fmt.Errorf("persisting %s: %w", r.ID, err)
				}
				if _, err := db.Exec(`INSERT OR IGNORE INTO caregiver_zips (caregiver_id, zip) VALUES (?, ?)`, r.ID, zip); err != nil {
					return fmt.Errorf("persisting zip membership for %s: %w", r.ID, err)
				}
				if exists == 1 {
					updated++
				} else {
					added++
				}
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(),
					map[string]any{"zip": zip, "synced": len(results), "new": added, "updated": updated}, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Synced %d caregivers near %s (%d new, %d already known).\n", len(results), zip, added, updated)
			return nil
		},
	}
	cmd.Flags().StringVar(&zip, "zip", "", "postal code to search (required)")
	cmd.Flags().StringVar(&careType, "care-type", "SITTER", "care.com careType")
	cmd.Flags().StringVar(&sortOrder, "sort", "SORT_ORDER_RECOMMENDED_DESCENDING", "search sort order")
	cmd.Flags().IntVar(&limit, "limit", 40, "max caregivers to sync")
	return cmd
}

func newCareSyncListCmd(flags *rootFlags) *cobra.Command {
	var zip string
	var minExp int
	var contactedOnly, uncontactedOnly bool
	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List locally-synced caregivers (offline; no network)",
		Example:     "  care-pp-cli sync list --zip 90210 --min-exp 5 --json",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				if !wantsHumanTable(cmd.OutOrStdout(), flags) {
					return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
						"dry_run": true,
						"action":  "would list locally-synced caregivers",
						"zip":     zip,
						"min_exp": minExp,
					}, flags)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "would list locally-synced caregivers")
				return nil
			}
			db, err := openCareDB()
			if err != nil {
				return err
			}
			defer db.Close()
			q := `SELECT id,COALESCE(name,''),COALESCE(city,''),COALESCE(state,''),COALESCE(years_experience,0),COALESCE(hourly_rate,0),COALESCE(total_reviews,0),COALESCE(avg_rating,0),COALESCE(has_care_check,0),COALESCE(zip,''),contacted_at FROM caregivers WHERE 1=1`
			var qargs []any
			if zip != "" {
				q += " AND (zip=? OR id IN (SELECT caregiver_id FROM caregiver_zips WHERE zip=?))"
				qargs = append(qargs, zip, zip)
			}
			if minExp > 0 {
				q += " AND years_experience>=?"
				qargs = append(qargs, minExp)
			}
			if contactedOnly {
				q += " AND contacted_at IS NOT NULL"
			}
			if uncontactedOnly {
				q += " AND contacted_at IS NULL"
			}
			q += " ORDER BY total_reviews DESC, avg_rating DESC, years_experience DESC"
			rows, err := db.Query(q, qargs...)
			if err != nil {
				return err
			}
			defer rows.Close()
			type row struct {
				ID              string  `json:"id"`
				Name            string  `json:"name"`
				City            string  `json:"city"`
				State           string  `json:"state"`
				YearsExperience int     `json:"years_experience"`
				HourlyRate      float64 `json:"hourly_rate,omitempty"`
				TotalReviews    int     `json:"total_reviews"`
				AvgRating       float64 `json:"avg_rating,omitempty"`
				HasCareCheck    bool    `json:"has_care_check"`
				Zip             string  `json:"zip"`
				Contacted       bool    `json:"contacted"`
			}
			out := []row{}
			for rows.Next() {
				var r row
				var check int
				var contactedAt sql.NullString
				if err := rows.Scan(&r.ID, &r.Name, &r.City, &r.State, &r.YearsExperience, &r.HourlyRate, &r.TotalReviews, &r.AvgRating, &check, &r.Zip, &contactedAt); err != nil {
					return err
				}
				r.HasCareCheck = check == 1
				r.Contacted = contactedAt.Valid
				out = append(out, r)
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}
			w := cmd.OutOrStdout()
			if len(out) == 0 {
				fmt.Fprintln(w, "No synced caregivers. Run: care-pp-cli sync run --zip <zip>")
				return nil
			}
			fmt.Fprintf(w, "%-20s %-14s %5s %7s %8s %4s  %s\n", "NAME", "CITY", "EXP", "RATE", "REVIEWS", "MSG", "ID")
			for _, r := range out {
				rate := "-"
				if r.HourlyRate > 0 {
					rate = fmt.Sprintf("$%.0f", r.HourlyRate)
				}
				msg := " "
				if r.Contacted {
					msg = "*"
				}
				fmt.Fprintf(w, "%-20s %-14s %4dy %7s %8d %4s  %s\n",
					truncate(r.Name, 20), truncate(r.City, 14), r.YearsExperience, rate, r.TotalReviews, msg, r.ID)
			}
			fmt.Fprintf(w, "\n%d caregivers ('*' = already contacted).\n", len(out))
			return nil
		},
	}
	cmd.Flags().StringVar(&zip, "zip", "", "filter by the zip they were synced under")
	cmd.Flags().IntVar(&minExp, "min-exp", 0, "minimum years of experience")
	cmd.Flags().BoolVar(&contactedOnly, "contacted", false, "only caregivers you've marked contacted")
	cmd.Flags().BoolVar(&uncontactedOnly, "uncontacted", false, "only caregivers not yet contacted")
	return cmd
}

func newCareSyncMarkContactedCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mark-contacted <caregiverId>",
		Short:   "Mark a caregiver as contacted (outreach tracking / dedupe)",
		Example: "  care-pp-cli sync mark-contacted 44444444-4444-4444-4444-444444444444",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				if !wantsHumanTable(cmd.OutOrStdout(), flags) {
					return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
						"dry_run":   true,
						"action":    "would mark caregiver " + args[0] + " as contacted",
						"id":        args[0],
						"contacted": false,
					}, flags)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "DRY RUN - would mark %s as contacted.\n", args[0])
				return nil
			}
			if err := careMarkContacted(args[0]); err != nil {
				return err
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
					"id":        args[0],
					"contacted": true,
				}, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Marked %s as contacted.\n", args[0])
			return nil
		},
	}
	return cmd
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
