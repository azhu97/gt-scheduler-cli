package poller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
	"github.com/azhu97/gt-scheduler-cli/internal/dbstore"
	"github.com/azhu97/gt-scheduler-cli/internal/live"
)

const sampleSeatHTML = `
<span class="status-bold">Enrollment Actual:</span> <span dir="ltr">70</span><br/>
<span class="status-bold">Enrollment Maximum:</span> <span dir="ltr">70</span><br/>
<span class="status-bold">Enrollment Seats Available:</span> <span dir="ltr">0</span><br/>
<span class="status-bold">Waitlist Capacity:</span> <span dir="ltr">25</span><br/>
<span class="status-bold">Waitlist Actual:</span> <span dir="ltr">0</span><br/>
<span class="status-bold">Waitlist Seats Available:</span> <span dir="ltr">25</span><br/>
`

// TestPollOnceFansOutFetchesConcurrently proves the worker-pool design
// actually parallelizes the HTTP fetches: N watched CRNs against a server
// that sleeps per-request should take roughly one request's worth of wall
// time, not N times that, and every CRN should still get a snapshot
// written despite completing out of order.
func TestPollOnceFansOutFetchesConcurrently(t *testing.T) {
	const n = 6
	const perRequestDelay = 100 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(perRequestDelay)
		w.Write([]byte(sampleSeatHTML))
	}))
	defer srv.Close()

	prevURL := live.LiveURL
	live.LiveURL = srv.URL
	defer func() { live.LiveURL = prevURL }()

	db, err := dbstore.Connect(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := dbstore.InitDB(db); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < n; i++ {
		crn := string(rune('A' + i))
		if _, err := db.Exec(
			"INSERT INTO watchlist (term, crn, added_at, notify_channels) VALUES (?, ?, ?, ?)",
			"202608", crn, "2024-01-01T00:00:00Z", "",
		); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Config{NotifyChannels: nil}
	start := time.Now()
	events, errs, err := PollOnce(context.Background(), db, srv.Client(), cfg)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}
	if len(events) != n {
		t.Fatalf("len(events) = %d, want %d", len(events), n)
	}

	// Serial would take n*perRequestDelay (600ms); concurrent (limit 8,
	// so all n=6 fetches run at once) should be close to one delay.
	if elapsed > perRequestDelay*time.Duration(n)/2 {
		t.Errorf("elapsed = %v, expected well under serial time of %v — fetches don't appear parallelized",
			elapsed, perRequestDelay*time.Duration(n))
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM seat_snapshots").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != n {
		t.Errorf("seat_snapshots rows = %d, want %d (every CRN should have been written)", count, n)
	}
}
