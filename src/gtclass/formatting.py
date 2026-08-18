"""Rich table/tree helpers for CLI output."""

from __future__ import annotations

import json
import sqlite3

from rich.console import Console
from rich.table import Table
from rich.tree import Tree

console = Console()
err_console = Console(stderr=True)

# Guards against pathological/cyclic prereq trees when recursing into
# referenced courses in print_prereq_tree.
MAX_PREREQ_DEPTH = 6


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


def _course_label(subject: str, course_number: str, title: str | None, grade: str | None) -> str:
    label = f"[bold]{subject} {course_number}[/bold]"
    if title:
        label += f" — {title}"
    if grade:
        label += f" [dim](min grade {grade})[/dim]"
    return label


def _leaf_course_label(
    conn: sqlite3.Connection, term: str, subject: str, course_number: str, grade: str | None
) -> str:
    """Label for a course reference with no further recursion (used by direct_only)."""
    row = conn.execute(
        "SELECT title FROM course_prereqs WHERE term = ? AND subject = ? AND course_number = ?",
        (term, subject, course_number),
    ).fetchone()
    return _course_label(subject, course_number, row["title"] if row else None, grade)


def _prereq_course_node(
    conn: sqlite3.Connection,
    term: str,
    subject: str,
    course_number: str,
    grade: str | None,
    ancestors: frozenset[tuple[str, str]],
    depth: int,
    direct_only: bool = False,
) -> Tree:
    """Build the tree node for one course, recursing into its own prereqs."""
    row = conn.execute(
        "SELECT title, prereqs_json FROM course_prereqs WHERE term = ? AND subject = ? AND course_number = ?",
        (term, subject, course_number),
    ).fetchone()

    key = (subject, course_number)
    node = Tree(_course_label(subject, course_number, row["title"] if row else None, grade))

    if row is None:
        node.add("[dim](not offered this term — no prerequisite data)[/dim]")
        return node
    if key in ancestors:
        node.add("[dim](already shown above — skipping to avoid a cycle)[/dim]")
        return node
    if depth >= MAX_PREREQ_DEPTH:
        node.add("[dim](max depth reached)[/dim]")
        return node

    prereqs = json.loads(row["prereqs_json"]) if row["prereqs_json"] else []
    if prereqs:
        _add_prereq_expr(node, conn, term, prereqs, ancestors | {key}, depth + 1, direct_only)
    else:
        node.add("[dim](no prerequisites)[/dim]")
    return node


def _add_prereq_expr(
    node: Tree,
    conn: sqlite3.Connection,
    term: str,
    expr,
    ancestors: frozenset[tuple[str, str]],
    depth: int,
    direct_only: bool = False,
) -> None:
    if not expr:
        return
    if isinstance(expr, dict):
        subject, _, course_number = expr.get("id", "").partition(" ")
        if not subject or not course_number:
            return
        if direct_only:
            node.add(_leaf_course_label(conn, term, subject, course_number, expr.get("grade")))
        else:
            node.add(
                _prereq_course_node(conn, term, subject, course_number, expr.get("grade"), ancestors, depth)
            )
        return
    if isinstance(expr, list) and expr and isinstance(expr[0], str) and expr[0] in ("and", "or"):
        op, *children = expr
        op_node = node.add(f"[italic]{op.upper()}[/italic]")
        for child in children:
            _add_prereq_expr(op_node, conn, term, child, ancestors, depth, direct_only)


def print_prereq_tree(
    conn: sqlite3.Connection, term: str, subject: str, course_number: str, direct_only: bool = False
) -> None:
    root = _prereq_course_node(conn, term, subject, course_number, None, frozenset(), 0, direct_only)
    console.print(root)
