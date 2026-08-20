// Package formatting renders CLI output: tables via text/tabwriter and
// colored text via fatih/color (the Go equivalents of rich's Table/console
// markup), plus (in tree.go) the prerequisite-tree renderer.
package formatting

import (
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"github.com/fatih/color"

	"github.com/azhu97/gt-scheduler-cli/internal/gtdata"
	"github.com/azhu97/gt-scheduler-cli/internal/live"
)

func newTable(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
}

func formatCredits(c *float64) string {
	if c == nil {
		return ""
	}
	return strconv.FormatFloat(*c, 'g', -1, 64)
}

// pyOptInt formats an optional int the way Python's str(x) would for an
// Optional[int] pulled straight out of a sqlite3.Row (None, or the number).
func pyOptInt(v *int) string {
	if v == nil {
		return "None"
	}
	return strconv.Itoa(*v)
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// PrintSearchResults renders `search` results as a table.
func PrintSearchResults(w io.Writer, rows []gtdata.CourseRow, term string) {
	if len(rows) == 0 {
		fmt.Fprintln(w, color.YellowString("No courses found for term %s.", term))
		return
	}

	fmt.Fprintf(w, "Search results — term %s\n", term)
	tw := newTable(w)
	fmt.Fprintln(tw, "CRN\tCourse\tSec\tTitle\tDays\tTime\tLocation\tInstructor\tCr")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s %s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.CRN, r.Subject, r.CourseNumber, r.Section, r.Title,
			r.MeetingDays, r.MeetingTime, r.Location, r.Instructor, formatCredits(r.Credits))
	}
	tw.Flush()
}

// PrintCourseInfo renders full detail for one course/CRN, optionally with
// live seat/waitlist status.
func PrintCourseInfo(w io.Writer, course gtdata.CourseRow, seats *live.SeatStatus) {
	fmt.Fprintf(w, "%s %s-%s — %s\n", course.Subject, course.CourseNumber, course.Section, course.Title)
	tw := newTable(w)
	fmt.Fprintf(tw, "CRN\t%s\n", course.CRN)
	fmt.Fprintf(tw, "Term\t%s\n", course.Term)
	fmt.Fprintf(tw, "Instructor\t%s\n", dashIfEmpty(course.Instructor))
	fmt.Fprintf(tw, "Days\t%s\n", dashIfEmpty(course.MeetingDays))
	fmt.Fprintf(tw, "Time\t%s\n", dashIfEmpty(course.MeetingTime))
	fmt.Fprintf(tw, "Location\t%s\n", dashIfEmpty(course.Location))
	credits := "—"
	if course.Credits != nil {
		credits = formatCredits(course.Credits)
	}
	fmt.Fprintf(tw, "Credits\t%s\n", credits)

	if seats != nil {
		fmt.Fprintf(tw, "Seats available\t%s\n", pyOptInt(seats.SeatsAvailable))
		fmt.Fprintf(tw, "Seats total\t%s\n", pyOptInt(seats.SeatsTotal))
		fmt.Fprintf(tw, "Waitlist available\t%s\n", pyOptInt(seats.WaitlistAvailable))
		fmt.Fprintf(tw, "Waitlist total\t%s\n", pyOptInt(seats.WaitlistTotal))
	}
	tw.Flush()
}

// SeatSnapshotRow is one row of recorded seat history for `info --history`.
type SeatSnapshotRow struct {
	TS                string
	SeatsAvailable    *int
	SeatsTotal        *int
	WaitlistAvailable *int
	WaitlistTotal     *int
}

// PrintSeatHistory renders recorded seat_snapshots rows for a CRN.
func PrintSeatHistory(w io.Writer, rows []SeatSnapshotRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, color.YellowString("No seat history recorded yet for this CRN."))
		return
	}

	fmt.Fprintln(w, "Seat history")
	tw := newTable(w)
	fmt.Fprintln(tw, "Time\tSeats avail\tSeats total\tWaitlist avail\tWaitlist total")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			r.TS, pyOptInt(r.SeatsAvailable), pyOptInt(r.SeatsTotal),
			pyOptInt(r.WaitlistAvailable), pyOptInt(r.WaitlistTotal))
	}
	tw.Flush()
}

// WatchlistRow is one row of `watch list` output.
type WatchlistRow struct {
	Term              string
	CRN               string
	CourseLabel       string // "" if the course isn't cached locally
	AddedAt           string
	NotifyChannels    string // "" means "use the configured default"
	SeatsAvailable    *int
	WaitlistAvailable *int
}

// PrintWatchlist renders the watchlist, one row per watched CRN.
func PrintWatchlist(w io.Writer, rows []WatchlistRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, color.YellowString("Watchlist is empty."))
		return
	}

	fmt.Fprintln(w, "Watchlist")
	tw := newTable(w)
	fmt.Fprintln(tw, "Term\tCRN\tCourse\tAdded\tChannels\tSeats avail\tWaitlist avail")
	for _, r := range rows {
		seats := ""
		if r.SeatsAvailable != nil {
			seats = strconv.Itoa(*r.SeatsAvailable)
		}
		waitlist := ""
		if r.WaitlistAvailable != nil {
			waitlist = strconv.Itoa(*r.WaitlistAvailable)
		}
		channels := r.NotifyChannels
		if channels == "" {
			channels = "(default)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Term, r.CRN, dashIfEmpty(r.CourseLabel), r.AddedAt, channels, seats, waitlist)
	}
	tw.Flush()
}
