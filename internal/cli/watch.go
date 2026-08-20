package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
	"github.com/azhu97/gt-scheduler-cli/internal/formatting"
	"github.com/azhu97/gt-scheduler-cli/internal/gtdata"
	"github.com/azhu97/gt-scheduler-cli/internal/poller"
)

func newWatchCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Manage the CRN watchlist.",
	}
	cmd.AddCommand(newWatchAddCmd(stdout, stderr))
	cmd.AddCommand(newWatchListCmd(stdout, stderr))
	cmd.AddCommand(newWatchRemoveCmd(stdout, stderr))
	return cmd
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func newWatchAddCmd(stdout, stderr io.Writer) *cobra.Command {
	var term, section, channels string
	cmd := &cobra.Command{
		Use:   "add TARGET",
		Short: `Start tracking a CRN, or resolve TARGET ("CS 4210") + --section to one.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatchAdd(cmd.Context(), stdout, args[0], term, section, channels)
		},
	}
	cmd.Flags().StringVar(&term, "term", "", "Term code. Defaults to the current term.")
	cmd.Flags().StringVar(&section, "section", "", "Section id, e.g. A, B, O1 (needed if TARGET is a course, not a CRN).")
	cmd.Flags().StringVar(&channels, "channels", "", "Comma-separated notify channels for this CRN (default: config default).")
	return cmd
}

func runWatchAdd(ctx context.Context, stdout io.Writer, target, term, section, channels string) error {
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

	var crn string
	var course gtdata.CourseRow

	if isDigits(target) {
		crn = target
		row := db.QueryRow(`
			SELECT term, crn, subject, course_number, section, title,
			       instructor, meeting_days, meeting_time, location, credits
			FROM courses WHERE term = ? AND crn = ?
		`, resolvedTerm, crn)
		if err := scanCourseRow(row, &course); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("CRN %s not found in term %s", crn, resolvedTerm)
			}
			return err
		}
	} else {
		subject, number, ok := splitCourseQuery(target)
		if !ok {
			return fmt.Errorf("couldn't parse %q as a CRN or \"SUBJECT NUMBER\"", target)
		}
		query := `
			SELECT term, crn, subject, course_number, section, title,
			       instructor, meeting_days, meeting_time, location, credits
			FROM courses WHERE term = ? AND subject = ? AND course_number = ?
		`
		params := []any{resolvedTerm, subject, number}
		if section != "" {
			query += " AND section = ?"
			params = append(params, strings.ToUpper(section))
		}
		rows, err := db.Query(query, params...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var matches []gtdata.CourseRow
		for rows.Next() {
			var r gtdata.CourseRow
			if err := rows.Scan(&r.Term, &r.CRN, &r.Subject, &r.CourseNumber, &r.Section, &r.Title,
				&r.Instructor, &r.MeetingDays, &r.MeetingTime, &r.Location, &r.Credits); err != nil {
				return err
			}
			matches = append(matches, r)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(matches) == 0 {
			return fmt.Errorf("no matching section found for %q", target)
		}
		if len(matches) > 1 {
			sections := make([]string, len(matches))
			for i, m := range matches {
				sections[i] = m.Section
			}
			return fmt.Errorf("multiple sections match %q (%s); pass --section", target, strings.Join(sections, ", "))
		}
		course = matches[0]
		crn = course.CRN
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`
		INSERT INTO watchlist (term, crn, added_at, notify_channels)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (term, crn) DO UPDATE SET notify_channels = excluded.notify_channels
	`, resolvedTerm, crn, now, channels)
	if err != nil {
		return err
	}

	printGreen(stdout, "Watching CRN %s (%s %s-%s, term %s)",
		crn, course.Subject, course.CourseNumber, course.Section, resolvedTerm)
	return nil
}

func scanCourseRow(row *sql.Row, course *gtdata.CourseRow) error {
	return row.Scan(&course.Term, &course.CRN, &course.Subject, &course.CourseNumber, &course.Section,
		&course.Title, &course.Instructor, &course.MeetingDays, &course.MeetingTime, &course.Location, &course.Credits)
}

func newWatchListCmd(stdout, stderr io.Writer) *cobra.Command {
	var refresh, noRefresh bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show everything tracked, with current seat/waitlist status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			effective := refresh
			if cmd.Flags().Changed("no-refresh") {
				effective = !noRefresh
			}
			return runWatchList(cmd.Context(), stdout, stderr, effective)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", true, "Poll live status before listing (default on).")
	cmd.Flags().BoolVar(&noRefresh, "no-refresh", false, "Skip polling live status before listing.")
	return cmd
}

// runWatchList refreshes live status by default (--refresh/--no-refresh,
// default on) before rendering — a side effect of just listing, preserved
// from the Python original per CLAUDE.md.
func runWatchList(ctx context.Context, stdout, stderr io.Writer, refresh bool) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	if refresh {
		client := gtdata.NewClient()
		_, errs, err := poller.PollOnce(ctx, db, client, cfg)
		if err != nil {
			return err
		}
		for _, e := range errs {
			printYellow(stderr, "warning: CRN %s: %s", e.CRN, e.Error)
		}
	}

	rows, err := db.Query(`
		SELECT
			w.term, w.crn, w.added_at, w.notify_channels,
			c.subject, c.course_number, c.section,
			s.seats_available, s.waitlist_available
		FROM watchlist w
		LEFT JOIN courses c ON c.term = w.term AND c.crn = w.crn
		LEFT JOIN (
			SELECT term, crn, seats_available, waitlist_available,
			       ROW_NUMBER() OVER (PARTITION BY term, crn ORDER BY ts DESC) rn
			FROM seat_snapshots
		) s ON s.term = w.term AND s.crn = w.crn AND s.rn = 1
		ORDER BY w.term, w.crn
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var out []formatting.WatchlistRow
	for rows.Next() {
		var r formatting.WatchlistRow
		var subject, courseNumber, section sql.NullString
		if err := rows.Scan(&r.Term, &r.CRN, &r.AddedAt, &r.NotifyChannels,
			&subject, &courseNumber, &section, &r.SeatsAvailable, &r.WaitlistAvailable); err != nil {
			return err
		}
		if subject.Valid {
			r.CourseLabel = fmt.Sprintf("%s %s-%s", subject.String, courseNumber.String, section.String)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	formatting.PrintWatchlist(stdout, out)
	return nil
}

func newWatchRemoveCmd(stdout, stderr io.Writer) *cobra.Command {
	var term string
	cmd := &cobra.Command{
		Use:   "remove CRN",
		Short: "Stop tracking a CRN.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatchRemove(cmd.Context(), stdout, args[0], term)
		},
	}
	cmd.Flags().StringVar(&term, "term", "", "Term code. Defaults to the current term.")
	return cmd
}

func runWatchRemove(ctx context.Context, stdout io.Writer, crn, term string) error {
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

	res, err := db.Exec("DELETE FROM watchlist WHERE term = ? AND crn = ?", resolvedTerm, crn)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("CRN %s was not on the watchlist for term %s", crn, resolvedTerm)
	}
	printGreen(stdout, "Stopped watching CRN %s (term %s)", crn, resolvedTerm)
	return nil
}
