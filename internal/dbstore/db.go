// Package dbstore is the SQLite storage layer.
//
// Tables (per PLAN.md):
//
//	courses        — one row per section, per term
//	seat_snapshots — one row per poll, per watched CRN
//	watchlist      — CRNs the user is actively tracking
//
// Plus terms_meta, an implementation detail that tracks when each term's
// bulk catalog was last synced so old/finished terms are fetched once and
// never re-fetched, while the current term is refreshed periodically. And
// course_prereqs, one row per (term, subject, course_number) holding the
// course-level prerequisite tree used by `gtclass tree`.
package dbstore

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers as "sqlite"

	"github.com/azhu97/gt-scheduler-cli/internal/config"
)

const schema = `
CREATE TABLE IF NOT EXISTS courses (
    term TEXT NOT NULL,
    crn TEXT NOT NULL,
    subject TEXT NOT NULL,
    course_number TEXT NOT NULL,
    section TEXT NOT NULL,
    title TEXT NOT NULL,
    instructor TEXT NOT NULL DEFAULT '',
    meeting_days TEXT NOT NULL DEFAULT '',
    meeting_time TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    credits REAL,
    PRIMARY KEY (term, crn)
);

CREATE INDEX IF NOT EXISTS idx_courses_subject_number
    ON courses (term, subject, course_number);

CREATE TABLE IF NOT EXISTS course_prereqs (
    term TEXT NOT NULL,
    subject TEXT NOT NULL,
    course_number TEXT NOT NULL,
    title TEXT NOT NULL,
    prereqs_json TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (term, subject, course_number)
);

CREATE TABLE IF NOT EXISTS terms_meta (
    term TEXT PRIMARY KEY,
    upstream_updated_at TEXT,
    synced_at TEXT NOT NULL,
    course_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS seat_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    term TEXT NOT NULL,
    crn TEXT NOT NULL,
    ts TEXT NOT NULL,
    seats_available INTEGER,
    seats_total INTEGER,
    waitlist_available INTEGER,
    waitlist_total INTEGER
);

CREATE INDEX IF NOT EXISTS idx_seat_snapshots_term_crn_ts
    ON seat_snapshots (term, crn, ts);

CREATE TABLE IF NOT EXISTS watchlist (
    term TEXT NOT NULL,
    crn TEXT NOT NULL,
    added_at TEXT NOT NULL,
    notify_channels TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (term, crn)
);
`

// Connect opens (creating parent dirs as needed) the SQLite database at
// path, or the default config.DBPath() if path is "". WAL mode + a busy
// timeout are enabled so a foreground `watch list --refresh` and a
// background `daemon start` can coexist without lock errors — the Python
// version didn't set these, but nothing about the schema depends on their
// absence, so this is a safe improvement rather than a behavior change.
func Connect(path string) (*sql.DB, error) {
	if path == "" {
		path = config.DBPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

// InitDB applies the schema; safe to call on every command (idempotent
// CREATE TABLE/INDEX IF NOT EXISTS, no migration framework).
func InitDB(db *sql.DB) error {
	_, err := db.Exec(schema)
	return err
}
