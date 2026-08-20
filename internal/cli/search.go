package cli

import (
	"context"
	"database/sql"
	"io"

	"github.com/spf13/cobra"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
	"github.com/azhu97/gt-scheduler-cli/internal/formatting"
	"github.com/azhu97/gt-scheduler-cli/internal/gtdata"
)

func newSearchCmd(stdout, stderr io.Writer) *cobra.Command {
	var term string
	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: `Search the catalog by subject/number ("CS 4210") or title substring.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd.Context(), stdout, args[0], term)
		},
	}
	cmd.Flags().StringVar(&term, "term", "", "Term code, e.g. 202502. Defaults to the current term.")
	return cmd
}

func runSearch(ctx context.Context, stdout io.Writer, query, term string) error {
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

	var rows *sql.Rows
	if subject, number, ok := splitCourseQuery(query); ok {
		rows, err = db.Query(`
			SELECT term, crn, subject, course_number, section, title,
			       instructor, meeting_days, meeting_time, location, credits
			FROM courses
			WHERE term = ? AND subject = ? AND course_number LIKE ?
			ORDER BY course_number, section
		`, resolvedTerm, subject, number+"%")
	} else {
		rows, err = db.Query(`
			SELECT term, crn, subject, course_number, section, title,
			       instructor, meeting_days, meeting_time, location, credits
			FROM courses
			WHERE term = ? AND title LIKE ?
			ORDER BY subject, course_number, section
		`, resolvedTerm, "%"+query+"%")
	}
	if err != nil {
		return err
	}
	defer rows.Close()

	courseRows, err := scanCourseRows(rows)
	if err != nil {
		return err
	}
	formatting.PrintSearchResults(stdout, courseRows, resolvedTerm)
	return nil
}

func scanCourseRows(rows *sql.Rows) ([]gtdata.CourseRow, error) {
	var out []gtdata.CourseRow
	for rows.Next() {
		var r gtdata.CourseRow
		if err := rows.Scan(&r.Term, &r.CRN, &r.Subject, &r.CourseNumber, &r.Section, &r.Title,
			&r.Instructor, &r.MeetingDays, &r.MeetingTime, &r.Location, &r.Credits); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
