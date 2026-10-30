// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
)

type queueDay struct {
	Date   string         `json:"date"`
	Count  int            `json:"count"`
	Boards map[string]int `json:"boards,omitempty"`
}

type queueGapsView struct {
	WindowStart     string     `json:"window_start"`
	Days            int        `json:"days"`
	PerDayLimit     int        `json:"per_day_limit"`
	Board           string     `json:"board,omitempty"`
	PinsSeen        int        `json:"pins_seen"`
	PinsInWindow    int        `json:"pins_in_window"`
	PinsWithoutTime int        `json:"pins_without_time"`
	QueuedNoTime    int        `json:"queued_awaiting_timeslot"`
	EmptyDays       []string   `json:"empty_days"`
	OverloadedDays  []queueDay `json:"overloaded_days"`
	ByDay           []queueDay `json:"by_day"`
	Note            string     `json:"note,omitempty"`
}

// pinOnBoard matches --board against any board field, so a name or an ID
// both work when a pin carries both.
func pinOnBoard(pin map[string]any, board string) bool {
	for _, k := range []string{"boardName", "board/name", "board", "boardId", "board/id"} {
		if v := pgmcp.Str(pin, k); v != "" && strings.EqualFold(v, board) {
			return true
		}
	}
	return false
}

// queueGaps buckets scheduled pins into calendar days starting at start.
func queueGaps(pins []map[string]any, start time.Time, days, perDay int, board string) queueGapsView {
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	view := queueGapsView{WindowStart: start.Format("2006-01-02"), Days: days, PerDayLimit: perDay, Board: board,
		EmptyDays: make([]string, 0), OverloadedDays: make([]queueDay, 0), ByDay: make([]queueDay, 0, days)}
	buckets := map[string]*queueDay{}
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		buckets[d] = &queueDay{Date: d, Boards: map[string]int{}}
	}
	for _, pin := range pins {
		view.PinsSeen++
		b := pgmcp.Str(pin, "boardName", "board/name", "board", "boardId", "board/id")
		if board != "" && !pinOnBoard(pin, board) {
			continue
		}
		at, ok := pgmcp.Time(pin, "scheduledTimeUTC", "scheduledTime", "scheduled_time", "scheduledAt", "publishAt", "publish_at", "date", "time")
		if !ok {
			view.PinsWithoutTime++
			continue
		}
		bucket := buckets[at.Format("2006-01-02")]
		if bucket == nil {
			continue
		}
		view.PinsInWindow++
		bucket.Count++
		if b == "" {
			b = "(unknown board)"
		}
		bucket.Boards[b]++
	}
	for i := 0; i < days; i++ {
		d := buckets[start.AddDate(0, 0, i).Format("2006-01-02")]
		if len(d.Boards) == 0 {
			d.Boards = nil
		}
		view.ByDay = append(view.ByDay, *d)
		if d.Count == 0 {
			view.EmptyDays = append(view.EmptyDays, d.Date)
		} else if perDay > 0 && d.Count > perDay {
			view.OverloadedDays = append(view.OverloadedDays, *d)
		}
	}
	sort.Slice(view.OverloadedDays, func(i, j int) bool { return view.OverloadedDays[i].Count > view.OverloadedDays[j].Count })
	if view.PinsSeen > 0 && view.PinsWithoutTime == view.PinsSeen {
		view.Note = "no scheduled pin carried a recognizable time field; queued pins without a time cannot be placed on a day"
	}
	return view
}

func newNovelQueueGapsCmd(flags *rootFlags) *cobra.Command {
	var days, perDay int
	var board string

	cmd := &cobra.Command{
		Use:         "queue-gaps",
		Short:       "Find empty and overloaded days in the scheduled-pin queue, per board.",
		Example:     "  pingenerator-pp-cli queue-gaps --days 14 --per-day 5 --agent",
		Annotations: map[string]string{"pp:data-source": "live", "mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "queue-gaps")
			}
			if days < 1 || days > 366 {
				return usageErr(fmt.Errorf("--days must be between 1 and 366"))
			}
			payload, err := pgCall(cmd, flags, "list_scheduled_pins", nil)
			if err != nil {
				return err
			}
			scheduled, queued := scheduledAndQueued(payload)
			view := queueGaps(scheduled, time.Now().UTC(), days, perDay, board)
			view.QueuedNoTime = len(queued)
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			w := cmd.OutOrStdout()
			for _, d := range view.ByDay {
				flag := ""
				if d.Count == 0 {
					flag = "  <- empty"
				} else if perDay > 0 && d.Count > perDay {
					flag = "  <- over limit"
				}
				fmt.Fprintf(w, "%s  %3d%s\n", d.Date, d.Count, flag)
			}
			fmt.Fprintf(w, "\n%d empty days, %d overloaded days in the next %d days\n", len(view.EmptyDays), len(view.OverloadedDays), days)
			if view.QueuedNoTime > 0 {
				fmt.Fprintf(w, "%d queued pins are still waiting for a timeslot\n", view.QueuedNoTime)
			}
			if view.Note != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "note:", view.Note)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 14, "Days ahead to check, starting today (UTC)")
	cmd.Flags().IntVar(&perDay, "per-day", 0, "Flag days with more than this many pins (0 = no limit)")
	cmd.Flags().StringVar(&board, "board", "", "Only count pins on this board (name or ID)")
	return cmd
}
