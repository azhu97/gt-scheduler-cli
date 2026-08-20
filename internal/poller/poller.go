// Package poller polls watched CRNs, diffs against the last snapshot, and
// notifies on change.
//
// Unlike the original Python implementation (which fetched each watched
// CRN sequentially), PollOnce fans the live HTTP fetches out across a
// bounded worker pool — the fetches touch no shared state, so this is safe
// — and then performs all SQLite reads/writes and notification dispatch
// sequentially, in submission order, inside a single transaction. That
// keeps SQLite writes single-writer (SQLite doesn't like concurrent
// writers) while still parallelizing the part of a poll cycle that
// dominates wall-clock time on a large watchlist: N blocking HTTP round
// trips.
package poller

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
	"github.com/azhu97/gt-scheduler-cli/internal/live"
	"github.com/azhu97/gt-scheduler-cli/internal/notify"
)

// maxConcurrentFetches bounds how many live-status HTTP requests run at
// once during a single PollOnce call.
const maxConcurrentFetches = 8

type PollEvent struct {
	Term     string
	CRN      string
	Status   live.SeatStatus
	Changes  []string
	Notified []notify.Result
}

type PollError struct {
	Term  string
	CRN   string
	Error string
}

type watchEntry struct {
	Term           string
	CRN            string
	NotifyChannels string
}

type snapshotRow struct {
	SeatsAvailable    *int
	SeatsTotal        *int
	WaitlistAvailable *int
	WaitlistTotal     *int
}

func lastSnapshot(tx *sql.Tx, term, crn string) (*snapshotRow, error) {
	row := tx.QueryRow(`
		SELECT seats_available, seats_total, waitlist_available, waitlist_total
		FROM seat_snapshots
		WHERE term = ? AND crn = ?
		ORDER BY ts DESC LIMIT 1
	`, term, crn)
	var s snapshotRow
	err := row.Scan(&s.SeatsAvailable, &s.SeatsTotal, &s.WaitlistAvailable, &s.WaitlistTotal)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// pyInt formats an optional int the way Python's f-string interpolation of
// an Optional[int] would (None, or the bare number), so diff messages read
// identically to the original implementation.
func pyInt(v *int) string {
	if v == nil {
		return "None"
	}
	return strconv.Itoa(*v)
}

func diff(prev *snapshotRow, curr live.SeatStatus) []string {
	if prev == nil {
		return nil
	}

	var changes []string

	prevAvailIsZero := prev.SeatsAvailable != nil && *prev.SeatsAvailable == 0
	currAvail := 0
	if curr.SeatsAvailable != nil {
		currAvail = *curr.SeatsAvailable
	}
	switch {
	case prevAvailIsZero && currAvail > 0:
		changes = append(changes, fmt.Sprintf("seats OPENED UP: %s/%s available",
			pyInt(curr.SeatsAvailable), pyInt(curr.SeatsTotal)))
	case !intPtrEqual(prev.SeatsAvailable, curr.SeatsAvailable):
		changes = append(changes, fmt.Sprintf("seats available %s -> %s",
			pyInt(prev.SeatsAvailable), pyInt(curr.SeatsAvailable)))
	}

	if !intPtrEqual(prev.WaitlistAvailable, curr.WaitlistAvailable) {
		changes = append(changes, fmt.Sprintf("waitlist available %s -> %s",
			pyInt(prev.WaitlistAvailable), pyInt(curr.WaitlistAvailable)))
	}

	return changes
}

type fetchResult struct {
	entry  watchEntry
	status live.SeatStatus
	err    error
}

// PollOnce fetches live status for every watched CRN, records a snapshot,
// and fires notifications for anything that changed since the last poll.
func PollOnce(ctx context.Context, db *sql.DB, client *http.Client, cfg config.Config) ([]PollEvent, []PollError, error) {
	rows, err := db.Query("SELECT term, crn, notify_channels FROM watchlist")
	if err != nil {
		return nil, nil, err
	}
	var entries []watchEntry
	for rows.Next() {
		var e watchEntry
		if err := rows.Scan(&e.Term, &e.CRN, &e.NotifyChannels); err != nil {
			rows.Close()
			return nil, nil, err
		}
		entries = append(entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	results := make([]fetchResult, len(entries))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentFetches)
	for i, e := range entries {
		g.Go(func() error {
			status, err := live.FetchSeatStatus(gctx, client, e.Term, e.CRN)
			results[i] = fetchResult{entry: e, status: status, err: err}
			return nil // per-CRN errors are collected, not fatal to the group
		})
	}
	_ = g.Wait() // no goroutine above returns a non-nil error

	tx, err := db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var events []PollEvent
	var errs []PollError

	for _, r := range results {
		if r.err != nil {
			errs = append(errs, PollError{Term: r.entry.Term, CRN: r.entry.CRN, Error: r.err.Error()})
			continue
		}

		prev, err := lastSnapshot(tx, r.entry.Term, r.entry.CRN)
		if err != nil {
			return nil, nil, err
		}
		changes := diff(prev, r.status)

		now := time.Now().UTC().Format(time.RFC3339)
		_, err = tx.Exec(`
			INSERT INTO seat_snapshots
				(term, crn, ts, seats_available, seats_total, waitlist_available, waitlist_total)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, r.entry.Term, r.entry.CRN, now, r.status.SeatsAvailable, r.status.SeatsTotal,
			r.status.WaitlistAvailable, r.status.WaitlistTotal)
		if err != nil {
			return nil, nil, err
		}

		var notified []notify.Result
		if len(changes) > 0 {
			channels := cfg.NotifyChannels
			if r.entry.NotifyChannels != "" {
				channels = nil
				for _, c := range strings.Split(r.entry.NotifyChannels, ",") {
					if c = strings.TrimSpace(c); c != "" {
						channels = append(channels, c)
					}
				}
			}
			title := fmt.Sprintf("CRN %s (%s)", r.entry.CRN, r.entry.Term)
			message := strings.Join(changes, "; ")
			notified = notify.Send(ctx, cfg, channels, title, message)
		}

		events = append(events, PollEvent{
			Term: r.entry.Term, CRN: r.entry.CRN, Status: r.status,
			Changes: changes, Notified: notified,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return events, errs, nil
}
