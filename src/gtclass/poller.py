"""Poll watched CRNs, diff against the last snapshot, and notify on change."""

from __future__ import annotations

import sqlite3
from dataclasses import dataclass
from datetime import datetime, timezone

import httpx

from gtclass import notify
from gtclass.config import Config
from gtclass.live import LiveDataError, SeatStatus, fetch_seat_status


@dataclass
class PollEvent:
    term: str
    crn: str
    status: SeatStatus
    changes: list[str]
    notified: list[tuple[str, str | None]]


@dataclass
class PollError:
    term: str
    crn: str
    error: str


def _last_snapshot(conn: sqlite3.Connection, term: str, crn: str) -> sqlite3.Row | None:
    return conn.execute(
        """
        SELECT * FROM seat_snapshots
        WHERE term = ? AND crn = ?
        ORDER BY ts DESC LIMIT 1
        """,
        (term, crn),
    ).fetchone()


def _diff(prev: sqlite3.Row | None, curr: SeatStatus) -> list[str]:
    if prev is None:
        return []

    changes: list[str] = []
    if prev["seats_available"] == 0 and (curr.seats_available or 0) > 0:
        changes.append(f"seats OPENED UP: {curr.seats_available}/{curr.seats_total} available")
    elif prev["seats_available"] != curr.seats_available:
        changes.append(
            f"seats available {prev['seats_available']} -> {curr.seats_available}"
        )

    if prev["waitlist_available"] != curr.waitlist_available:
        changes.append(
            f"waitlist available {prev['waitlist_available']} -> {curr.waitlist_available}"
        )

    return changes


def poll_once(
    conn: sqlite3.Connection,
    client: httpx.Client,
    cfg: Config,
) -> tuple[list[PollEvent], list[PollError]]:
    entries = conn.execute("SELECT * FROM watchlist").fetchall()
    events: list[PollEvent] = []
    errors: list[PollError] = []

    for entry in entries:
        term, crn = entry["term"], entry["crn"]
        try:
            status = fetch_seat_status(client, term, crn)
        except LiveDataError as exc:
            errors.append(PollError(term=term, crn=crn, error=str(exc)))
            continue

        prev = _last_snapshot(conn, term, crn)
        changes = _diff(prev, status)

        now = datetime.now(timezone.utc).isoformat()
        conn.execute(
            """
            INSERT INTO seat_snapshots
                (term, crn, ts, seats_available, seats_total,
                 waitlist_available, waitlist_total)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (
                term,
                crn,
                now,
                status.seats_available,
                status.seats_total,
                status.waitlist_available,
                status.waitlist_total,
            ),
        )

        notified: list[tuple[str, str | None]] = []
        if changes:
            channels = (
                entry["notify_channels"].split(",")
                if entry["notify_channels"]
                else cfg.notify_channels
            )
            channels = [c.strip() for c in channels if c.strip()]
            title = f"CRN {crn} ({term})"
            message = "; ".join(changes)
            notified = notify.send(cfg, channels, title, message)

        events.append(
            PollEvent(term=term, crn=crn, status=status, changes=changes, notified=notified)
        )

    return events, errors
