// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source local

package cli

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// purgeDefaultAge is how old a row must be to be removed when --older-than is
// not given.
const purgeDefaultAge = "30d"

// init attaches `address purge` to the generated `address` group without
// editing the generated parent file.
func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if parent, _, err := root.Find([]string{"address"}); err == nil && parent != nil && parent.Name() == "address" {
			addNovelCommandIfAbsent(parent, newNovelAddressPurgeCmd(flags))
		}
	})
}

func newNovelAddressPurgeCmd(flags *rootFlags) *cobra.Command {
	var flagOlderThan, flagDB string
	var flagAll bool

	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Delete cached API responses and cleaned addresses from the local database by age.",
		Long: strings.Trim(fmt.Sprintf(`
Removes rows from the local response cache (dm_cache) and the cleaned-address
table (dm_address) whose fetch or update time is older than --older-than
(default %s; accepts Go durations such as 72h and day counts such as 30d), or
everything with --all, which needs --yes. The quota call log is kept so the
budget guard stays accurate. Local only: it never calls the API.

%s`, purgeDefaultAge, dmRetentionNote), "\n"),
		Example: strings.Trim(`
  delhivery-maps-pp-cli address purge --older-than 30d
  delhivery-maps-pp-cli address purge --all --yes`, "\n"),
		Annotations: map[string]string{"pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "address purge")
			}
			age, err := parsePurgeAge(flagOlderThan)
			if err != nil {
				return dmUsage(cmd, "--older-than: %v", err)
			}
			if (flagAll || age == 0) && !flags.yes {
				return dmUsage(cmd, "--all (or an age of zero) deletes every cached response and cleaned address; pass --yes to confirm")
			}
			s, exists, err := openDMAddressReader(cmd.Context(), flagDB)
			if err != nil {
				return err
			}
			result := map[string]int64{"deleted_cache": 0, "deleted_address": 0}
			if exists {
				_ = s.Close() // the read-only handle was only an existence probe
				w, err := openDMStore(cmd.Context(), flagDB)
				if err != nil {
					return err
				}
				defer w.Close()
				cutoff := time.Now().UTC().Add(-age).Format(time.RFC3339)
				if flagAll {
					cutoff = "9999" // every RFC 3339 timestamp sorts before this
				}
				if err := purgeRows(cmd.Context(), w.DB(), cutoff, result); err != nil {
					return err
				}
			}
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d cached response(s) and %d cleaned address(es)\n", result["deleted_cache"], result["deleted_address"])
				return nil
			}
			return dmPrint(cmd.OutOrStdout(), result, flags)
		},
	}
	cmd.Flags().StringVar(&flagOlderThan, "older-than", purgeDefaultAge, "Delete rows older than this age, for example 30d or 72h.")
	cmd.Flags().BoolVar(&flagAll, "all", false, "Delete every cached response and cleaned address regardless of age (needs --yes).")
	cmd.Flags().StringVar(&flagDB, "db", "", "SQLite database to purge (default: the CLI's data database).")
	return cmd
}

// purgeRows deletes rows older than cutoff and then makes the deletion real:
// secure_delete zeroes the freed content, VACUUM rewrites the file and a WAL
// checkpoint truncates the log, so the removed addresses are not left in the
// database or its write-ahead log. It runs on one connection because the
// secure_delete pragma is per connection.
func purgeRows(ctx context.Context, db *sql.DB, cutoff string, result map[string]int64) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("purging: %w", err)
	}
	defer conn.Close()
	dmWriteMu.Lock()
	defer dmWriteMu.Unlock()
	if _, err := conn.ExecContext(ctx, `PRAGMA secure_delete = ON`); err != nil {
		return fmt.Errorf("purging: %w", err)
	}
	for _, d := range []struct{ key, query string }{
		{"deleted_cache", `DELETE FROM dm_cache WHERE fetched_at < ? OR fetched_at IS NULL`},
		{"deleted_address", `DELETE FROM dm_address WHERE updated_at < ? OR updated_at IS NULL`},
	} {
		res, err := conn.ExecContext(ctx, d.query, cutoff)
		if err != nil {
			return fmt.Errorf("purging: %w", err)
		}
		result[d.key], _ = res.RowsAffected()
	}
	for _, stmt := range []string{`PRAGMA wal_checkpoint(TRUNCATE)`, `VACUUM`, `PRAGMA wal_checkpoint(TRUNCATE)`} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("purging: %s: %w", stmt, err)
		}
	}
	return nil
}

// purgeMaxDays caps --older-than: larger values would overflow a Duration.
const purgeMaxDays = 36500

// parsePurgeAge accepts "30d" (days, at most purgeMaxDays) or any Go duration
// such as "72h".
func parsePurgeAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%q is not a valid age", s)
		}
		if n > purgeMaxDays {
			return 0, fmt.Errorf("%q is over the limit of %d days; use --all --yes to delete everything", s, purgeMaxDays)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%q is not a valid age (use 30d or 72h)", s)
	}
	if d > purgeMaxDays*24*time.Hour {
		return 0, fmt.Errorf("%q is over the limit of %d days", s, purgeMaxDays)
	}
	return d, nil
}
