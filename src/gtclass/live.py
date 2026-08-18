"""Live per-CRN seat/waitlist data, proxied live from Oscar.

The endpoint returns an HTML fragment (not JSON) meant to be dropped into
a page, e.g.:

    <span class="status-bold">Enrollment Seats Available:</span>
    <span dir="ltr">0</span>

so this module scrapes the handful of numbers out of it with regexes
rather than a full HTML parser.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

import httpx

LIVE_URL = "https://gt-scheduler.azurewebsites.net/proxy/class_section"

_FIELD_PATTERNS = {
    "enrollment_actual": r"Enrollment Actual:</span>\s*<span[^>]*>\s*(\d+)",
    "seats_total": r"Enrollment Maximum:</span>\s*<span[^>]*>\s*(\d+)",
    "seats_available": r"Enrollment Seats Available:</span>\s*<span[^>]*>\s*(\d+)",
    "waitlist_total": r"Waitlist Capacity:</span>\s*<span[^>]*>\s*(\d+)",
    "waitlist_actual": r"Waitlist Actual:</span>\s*<span[^>]*>\s*(\d+)",
    "waitlist_available": r"Waitlist Seats Available:</span>\s*<span[^>]*>\s*(\d+)",
}


class LiveDataError(RuntimeError):
    """Raised when live seat data can't be fetched or the CRN is unknown."""


@dataclass
class SeatStatus:
    seats_available: int | None
    seats_total: int | None
    waitlist_available: int | None
    waitlist_total: int | None


def parse_seat_html(html: str) -> SeatStatus:
    values: dict[str, int | None] = {}
    for field, pattern in _FIELD_PATTERNS.items():
        match = re.search(pattern, html)
        values[field] = int(match.group(1)) if match else None

    if values["seats_total"] is None and values["waitlist_total"] is None:
        raise LiveDataError("no enrollment data found for this CRN/term")

    return SeatStatus(
        seats_available=values["seats_available"],
        seats_total=values["seats_total"],
        waitlist_available=values["waitlist_available"],
        waitlist_total=values["waitlist_total"],
    )


def fetch_seat_status(client: httpx.Client, term: str, crn: str) -> SeatStatus:
    try:
        resp = client.get(LIVE_URL, params={"term": term, "crn": crn})
        resp.raise_for_status()
    except httpx.HTTPError as exc:
        raise LiveDataError(f"failed to fetch live seats for CRN {crn}: {exc}") from exc
    return parse_seat_html(resp.text)
