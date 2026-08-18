import sqlite3

from gtclass import db


def make_conn() -> sqlite3.Connection:
    conn = sqlite3.connect(":memory:")
    conn.row_factory = sqlite3.Row
    db.init_db(conn)
    return conn


def test_init_db_creates_expected_tables():
    conn = make_conn()
    tables = {
        r["name"]
        for r in conn.execute(
            "SELECT name FROM sqlite_master WHERE type = 'table'"
        ).fetchall()
    }
    assert {"courses", "seat_snapshots", "watchlist", "terms_meta"} <= tables


def test_watchlist_roundtrip():
    conn = make_conn()
    conn.execute(
        "INSERT INTO watchlist (term, crn, added_at, notify_channels) VALUES (?, ?, ?, ?)",
        ("202302", "25096", "2024-01-01T00:00:00+00:00", "desktop"),
    )
    conn.commit()

    row = conn.execute(
        "SELECT * FROM watchlist WHERE term = ? AND crn = ?", ("202302", "25096")
    ).fetchone()
    assert row["notify_channels"] == "desktop"

    conn.execute("DELETE FROM watchlist WHERE term = ? AND crn = ?", ("202302", "25096"))
    conn.commit()
    assert conn.execute("SELECT COUNT(*) c FROM watchlist").fetchone()["c"] == 0


def test_seat_snapshots_ordering():
    conn = make_conn()
    for i, ts in enumerate(["2024-01-01T00:00:00", "2024-01-01T01:00:00"]):
        conn.execute(
            """
            INSERT INTO seat_snapshots
                (term, crn, ts, seats_available, seats_total, waitlist_available, waitlist_total)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            ("202302", "25096", ts, i, 70, 25, 25),
        )
    conn.commit()

    latest = conn.execute(
        "SELECT * FROM seat_snapshots WHERE term = ? AND crn = ? ORDER BY ts DESC LIMIT 1",
        ("202302", "25096"),
    ).fetchone()
    assert latest["seats_available"] == 1
