"""SQLite storage layer.

Tables (per PLAN.md):
  courses        — one row per section, per term
  seat_snapshots — one row per poll, per watched CRN
  watchlist      — CRNs the user is actively tracking

Plus terms_meta, an implementation detail that tracks when each term's
bulk catalog was last synced so old/finished terms are fetched once and
never re-fetched, while the current term is refreshed periodically.
"""

from __future__ import annotations

import sqlite3
from contextlib import contextmanager
from pathlib import Path
from typing import Iterator

from gtclass.config import db_path

SCHEMA = """
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
"""


@contextmanager
def connect(path: Path | None = None) -> Iterator[sqlite3.Connection]:
    target = path or db_path()
    target.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(target)
    conn.row_factory = sqlite3.Row
    try:
        conn.execute("PRAGMA foreign_keys = ON")
        yield conn
        conn.commit()
    finally:
        conn.close()


def init_db(conn: sqlite3.Connection) -> None:
    conn.executescript(SCHEMA)
