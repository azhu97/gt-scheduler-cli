package dbstore

import "testing"

// TestModernSQLiteSupportsRowNumberWindowFunction is a version smoke test:
// `watch list` relies on ROW_NUMBER() OVER (PARTITION BY ...), a SQLite
// 3.25+ feature. If the modernc.org/sqlite driver's bundled SQLite version
// ever regresses below that, this should fail loud here rather than
// surface as a confusing bug in `watch list`.
func TestModernSQLiteSupportsRowNumberWindowFunction(t *testing.T) {
	db, err := Connect(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow("SELECT ROW_NUMBER() OVER (ORDER BY 1)").Scan(&n); err != nil {
		t.Fatalf("window function support check failed: %v", err)
	}
	if n != 1 {
		t.Errorf("ROW_NUMBER() = %d, want 1", n)
	}
}

func TestInitDBCreatesExpectedTables(t *testing.T) {
	db, err := Connect(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := InitDB(db); err != nil {
		t.Fatal(err)
	}

	want := []string{"courses", "seat_snapshots", "watchlist", "terms_meta", "course_prereqs"}
	for _, table := range want {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestWatchlistRoundtrip(t *testing.T) {
	db, err := Connect(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := InitDB(db); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(
		"INSERT INTO watchlist (term, crn, added_at, notify_channels) VALUES (?, ?, ?, ?)",
		"202302", "25096", "2024-01-01T00:00:00+00:00", "desktop",
	); err != nil {
		t.Fatal(err)
	}

	var channels string
	if err := db.QueryRow(
		"SELECT notify_channels FROM watchlist WHERE term = ? AND crn = ?", "202302", "25096",
	).Scan(&channels); err != nil {
		t.Fatal(err)
	}
	if channels != "desktop" {
		t.Errorf("notify_channels = %q, want desktop", channels)
	}

	if _, err := db.Exec("DELETE FROM watchlist WHERE term = ? AND crn = ?", "202302", "25096"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM watchlist").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestSeatSnapshotsOrdering(t *testing.T) {
	db, err := Connect(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := InitDB(db); err != nil {
		t.Fatal(err)
	}

	for i, ts := range []string{"2024-01-01T00:00:00", "2024-01-01T01:00:00"} {
		if _, err := db.Exec(`
			INSERT INTO seat_snapshots
				(term, crn, ts, seats_available, seats_total, waitlist_available, waitlist_total)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, "202302", "25096", ts, i, 70, 25, 25); err != nil {
			t.Fatal(err)
		}
	}

	var latest int
	if err := db.QueryRow(
		"SELECT seats_available FROM seat_snapshots WHERE term = ? AND crn = ? ORDER BY ts DESC LIMIT 1",
		"202302", "25096",
	).Scan(&latest); err != nil {
		t.Fatal(err)
	}
	if latest != 1 {
		t.Errorf("latest seats_available = %d, want 1", latest)
	}
}
