package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
	"github.com/azhu97/gt-scheduler-cli/internal/formatting"
	"github.com/azhu97/gt-scheduler-cli/internal/gtdata"
	"github.com/azhu97/gt-scheduler-cli/internal/live"
)

func newInfoCmd(stdout, stderr io.Writer) *cobra.Command {
	var term string
	var history bool
	cmd := &cobra.Command{
		Use:   "info CRN",
		Short: "Show full detail for a CRN, including live seat/waitlist status.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfo(cmd.Context(), stdout, stderr, args[0], term, history)
		},
	}
	cmd.Flags().StringVar(&term, "term", "", "Term code. Defaults to the current term.")
	cmd.Flags().BoolVar(&history, "history", false, "Show recorded seat history for this CRN.")
	return cmd
}

func runInfo(ctx context.Context, stdout, stderr io.Writer, crn, term string, history bool) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	client := gtdata.NewClient()

	resolvedTerm, err := resolveTerm(ctx, db, client, term, cfg)
	if err != nil {
		return err
	}
	if err := syncTerm(ctx, db, client, resolvedTerm); err != nil {
		return err
	}

	row := db.QueryRow(`
		SELECT term, crn, subject, course_number, section, title,
		       instructor, meeting_days, meeting_time, location, credits
		FROM courses WHERE term = ? AND crn = ?
	`, resolvedTerm, crn)
	var course gtdata.CourseRow
	if err := row.Scan(&course.Term, &course.CRN, &course.Subject, &course.CourseNumber, &course.Section,
		&course.Title, &course.Instructor, &course.MeetingDays, &course.MeetingTime, &course.Location, &course.Credits); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("CRN %s not found in term %s", crn, resolvedTerm)
		}
		return err
	}

	var seats *live.SeatStatus
	status, err := live.FetchSeatStatus(ctx, client, resolvedTerm, crn)
	if err != nil {
		printYellow(stderr, "warning: %s", err)
	} else {
		seats = &status
	}

	formatting.PrintCourseInfo(stdout, course, seats)

	if history {
		rows, err := db.Query(`
			SELECT ts, seats_available, seats_total, waitlist_available, waitlist_total
			FROM seat_snapshots
			WHERE term = ? AND crn = ?
			ORDER BY ts DESC LIMIT 50
		`, resolvedTerm, crn)
		if err != nil {
			return err
		}
		defer rows.Close()

		var historyRows []formatting.SeatSnapshotRow
		for rows.Next() {
			var r formatting.SeatSnapshotRow
			if err := rows.Scan(&r.TS, &r.SeatsAvailable, &r.SeatsTotal, &r.WaitlistAvailable, &r.WaitlistTotal); err != nil {
				return err
			}
			historyRows = append(historyRows, r)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		formatting.PrintSeatHistory(stdout, historyRows)
	}
	return nil
}
