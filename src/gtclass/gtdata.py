"""Fetching and parsing GT Scheduler's bulk term catalog.

GT Scheduler (https://github.com/gt-scheduler/crawler) publishes a
heavily-packed JSON blob per term: `courses` maps "SUBJ NUMBER" to
`[title, sections, attributes, description]`, where `sections` maps a
section id to `[crn, meetings, credits, scheduleTypeIdx, campusIdx,
attributeIdxs, gradeBaseIdx]`, and each meeting is `[periodIdx, days,
room, scheduleTypeIdx, instructors, campusIdx, dateRangeIdx,
finalDateIdx]`. `periodIdx` indexes into `caches.periods` to get a human
time range (e.g. "9:30 am - 10:45 am"). No seat data lives here — that
only comes from the live per-CRN endpoint (see live.py).
"""

from __future__ import annotations

import sqlite3
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone

import httpx

INDEX_URL = "https://gt-scheduler.github.io/crawler-v2/index.json"
TERM_URL_TMPL = "https://gt-scheduler.github.io/crawler-v2/{term}.json"

# How long a synced term catalog is considered fresh before a command
# will re-fetch it. Finished terms don't change, so in practice this
# only ever causes re-fetches for the current term.
STALE_AFTER = timedelta(minutes=25)


class GTDataError(RuntimeError):
    """Raised when GT Scheduler data can't be fetched or parsed."""


def new_client() -> httpx.Client:
    return httpx.Client(timeout=20.0, headers={"User-Agent": "gtclass-cli"})


_SEASON_NAMES = {"02": "Spring", "05": "Summer", "08": "Fall"}


def term_label(term: str) -> str:
    """Human label for a term code, e.g. "202302" -> "Spring 2023"."""
    year, season = term[:4], term[4:6]
    return f"{_SEASON_NAMES.get(season, season)} {year}"


def fetch_term_index(client: httpx.Client) -> list[str]:
    try:
        resp = client.get(INDEX_URL)
        resp.raise_for_status()
    except httpx.HTTPError as exc:
        raise GTDataError(f"failed to fetch term index: {exc}") from exc
    terms = resp.json().get("terms", [])
    # crawler-v2's index.json lists terms as {"term": ..., "finalized": ...}
    # objects rather than bare strings (the old crawler repo's format).
    return [t["term"] if isinstance(t, dict) else t for t in terms]


def fetch_term_catalog(client: httpx.Client, term: str) -> dict:
    try:
        resp = client.get(TERM_URL_TMPL.format(term=term))
        resp.raise_for_status()
    except httpx.HTTPError as exc:
        raise GTDataError(f"failed to fetch catalog for term {term}: {exc}") from exc
    return resp.json()


def _clean_days(days: str) -> str:
    days = (days or "").replace("&nbsp;", "").strip()
    return days


def parse_term_catalog(data: dict, term: str) -> list[dict]:
    """Flatten the packed catalog blob into one dict per (term, crn)."""
    periods = data.get("caches", {}).get("periods", [])
    rows: list[dict] = []

    for course_key, course_val in data.get("courses", {}).items():
        if not isinstance(course_val, list) or len(course_val) < 2:
            continue
        title = course_val[0] or ""
        sections = course_val[1] if isinstance(course_val[1], dict) else {}
        subject, _, course_number = course_key.partition(" ")

        for section_id, section_val in sections.items():
            if not isinstance(section_val, list) or len(section_val) < 2:
                continue
            crn = str(section_val[0])
            meetings = section_val[1] if isinstance(section_val[1], list) else []
            credits = section_val[2] if len(section_val) > 2 else None

            days_parts: list[str] = []
            time_parts: list[str] = []
            location_parts: list[str] = []
            instructors: list[str] = []

            for m in meetings:
                if not isinstance(m, list) or len(m) < 5:
                    continue
                period_idx, days, room = m[0], m[1], m[2]
                meeting_instructors = m[4] if isinstance(m[4], list) else []

                days_clean = _clean_days(days) or "TBA"
                time_str = (
                    periods[period_idx]
                    if isinstance(period_idx, int) and 0 <= period_idx < len(periods)
                    else "TBA"
                )
                days_parts.append(days_clean)
                time_parts.append(time_str)
                location_parts.append(room or "TBA")

                for instr in meeting_instructors:
                    if instr and instr not in instructors:
                        instructors.append(instr)

            rows.append(
                {
                    "term": term,
                    "crn": crn,
                    "subject": subject,
                    "course_number": course_number,
                    "section": section_id,
                    "title": title,
                    "instructor": ", ".join(instructors),
                    "meeting_days": "; ".join(days_parts),
                    "meeting_time": "; ".join(time_parts),
                    "location": "; ".join(location_parts),
                    "credits": float(credits) if isinstance(credits, (int, float)) else None,
                }
            )

    return rows


@dataclass
class SyncResult:
    synced: bool
    course_count: int


def _get_terms_meta(conn: sqlite3.Connection, term: str) -> sqlite3.Row | None:
    return conn.execute(
        "SELECT * FROM terms_meta WHERE term = ?", (term,)
    ).fetchone()


def sync_term(
    conn: sqlite3.Connection,
    client: httpx.Client,
    term: str,
    force: bool = False,
) -> SyncResult:
    """Ensure `term`'s catalog is cached in SQLite, fetching if stale/missing."""
    meta = _get_terms_meta(conn, term)
    if meta and not force:
        synced_at = datetime.fromisoformat(meta["synced_at"])
        if datetime.now(timezone.utc) - synced_at < STALE_AFTER:
            return SyncResult(synced=False, course_count=meta["course_count"])

    data = fetch_term_catalog(client, term)
    rows = parse_term_catalog(data, term)
    now = datetime.now(timezone.utc).isoformat()

    conn.execute("DELETE FROM courses WHERE term = ?", (term,))
    conn.executemany(
        """
        INSERT INTO courses
            (term, crn, subject, course_number, section, title,
             instructor, meeting_days, meeting_time, location, credits)
        VALUES
            (:term, :crn, :subject, :course_number, :section, :title,
             :instructor, :meeting_days, :meeting_time, :location, :credits)
        """,
        rows,
    )
    conn.execute(
        """
        INSERT INTO terms_meta (term, upstream_updated_at, synced_at, course_count)
        VALUES (?, ?, ?, ?)
        ON CONFLICT (term) DO UPDATE SET
            upstream_updated_at = excluded.upstream_updated_at,
            synced_at = excluded.synced_at,
            course_count = excluded.course_count
        """,
        (term, data.get("updatedAt"), now, len(rows)),
    )
    return SyncResult(synced=True, course_count=len(rows))


def resolve_default_term(conn: sqlite3.Connection, client: httpx.Client | None) -> str:
    """Pick the current term: the latest one GT Scheduler actually has data for.

    GT Scheduler's index sometimes lists terms ahead of what its crawler has
    populated yet (an empty `{"courses": {}}` placeholder), so this walks
    the index newest-first and syncs each candidate until one has course
    data, rather than trusting the max term code blindly. Falls back to the
    most recently synced *non-empty* local term if the index can't be
    reached (e.g. offline), and finally raises if nothing is cached.
    """
    if client is not None:
        try:
            terms = fetch_term_index(client)
        except GTDataError:
            terms = []
        for term in sorted(terms, reverse=True):
            try:
                result = sync_term(conn, client, term)
            except GTDataError:
                continue
            if result.course_count > 0:
                return term

    row = conn.execute(
        "SELECT term FROM terms_meta WHERE course_count > 0 ORDER BY term DESC LIMIT 1"
    ).fetchone()
    if row:
        return row["term"]

    raise GTDataError(
        "couldn't find a term with course data (GT Scheduler's crawler may "
        "be behind for recent terms); pass --term explicitly"
    )
