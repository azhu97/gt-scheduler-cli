"""Rich table helpers for CLI output."""

from __future__ import annotations

import sqlite3

from rich.console import Console
from rich.table import Table

console = Console()
err_console = Console(stderr=True)


def print_search_results(rows: list[sqlite3.Row], term: str) -> None:
    if not rows:
        console.print(f"[yellow]No courses found for term {term}.[/yellow]")
        return

    table = Table(title=f"Search results — term {term}")
    table.add_column("CRN")
    table.add_column("Course")
    table.add_column("Sec")
    table.add_column("Title")
    table.add_column("Days")
    table.add_column("Time")
    table.add_column("Location")
    table.add_column("Instructor")
    table.add_column("Cr", justify="right")

    for r in rows:
        table.add_row(
            r["crn"],
            f"{r['subject']} {r['course_number']}",
            r["section"],
            r["title"],
            r["meeting_days"],
            r["meeting_time"],
            r["location"],
            r["instructor"],
            "" if r["credits"] is None else f"{r['credits']:g}",
        )
    console.print(table)


def print_course_info(course: sqlite3.Row, seats=None) -> None:
    table = Table(title=f"{course['subject']} {course['course_number']}-{course['section']} — {course['title']}")
    table.add_column("Field")
    table.add_column("Value")
    table.add_row("CRN", course["crn"])
    table.add_row("Term", course["term"])
    table.add_row("Instructor", course["instructor"] or "—")
    table.add_row("Days", course["meeting_days"] or "—")
    table.add_row("Time", course["meeting_time"] or "—")
    table.add_row("Location", course["location"] or "—")
    table.add_row("Credits", "—" if course["credits"] is None else f"{course['credits']:g}")

    if seats is not None:
        table.add_row("Seats available", str(seats.seats_available))
        table.add_row("Seats total", str(seats.seats_total))
        table.add_row("Waitlist available", str(seats.waitlist_available))
        table.add_row("Waitlist total", str(seats.waitlist_total))

    console.print(table)


def print_seat_history(rows: list[sqlite3.Row]) -> None:
    if not rows:
        console.print("[yellow]No seat history recorded yet for this CRN.[/yellow]")
        return

    table = Table(title="Seat history")
    table.add_column("Time")
    table.add_column("Seats avail", justify="right")
    table.add_column("Seats total", justify="right")
    table.add_column("Waitlist avail", justify="right")
    table.add_column("Waitlist total", justify="right")

    for r in rows:
        table.add_row(
            r["ts"],
            str(r["seats_available"]),
            str(r["seats_total"]),
            str(r["waitlist_available"]),
            str(r["waitlist_total"]),
        )
    console.print(table)


def print_watchlist(rows: list[sqlite3.Row]) -> None:
    if not rows:
        console.print("[yellow]Watchlist is empty.[/yellow]")
        return

    table = Table(title="Watchlist")
    table.add_column("Term")
    table.add_column("CRN")
    table.add_column("Course")
    table.add_column("Added")
    table.add_column("Channels")
    table.add_column("Seats avail", justify="right")
    table.add_column("Waitlist avail", justify="right")

    for r in rows:
        table.add_row(
            r["term"],
            r["crn"],
            r["course_label"] or "—",
            r["added_at"],
            r["notify_channels"] or "(default)",
            "" if r["seats_available"] is None else str(r["seats_available"]),
            "" if r["waitlist_available"] is None else str(r["waitlist_available"]),
        )
    console.print(table)
