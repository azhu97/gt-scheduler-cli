"""gtclass command-line interface."""

from __future__ import annotations

import re
import sqlite3
import sys
from datetime import datetime, timezone

import click

from gtclass import daemon, db, formatting, gtdata, live, notify, poller
from gtclass.config import Config, load_config, save_config
from gtclass.formatting import console, err_console
from gtclass.gtdata import GTDataError
from gtclass.live import LiveDataError

_COURSE_KEY_RE = re.compile(r"^\s*([A-Za-z]+)\s*(\d[\dA-Za-z]*)\s*$")


def _resolve_term(conn: sqlite3.Connection, client, term: str | None, cfg: Config) -> str:
    if term:
        return term
    if cfg.default_term:
        return cfg.default_term
    try:
        return gtdata.resolve_default_term(conn, client)
    except GTDataError as exc:
        raise click.ClickException(str(exc))


def _sync(conn: sqlite3.Connection, client, term: str) -> None:
    try:
        gtdata.sync_term(conn, client, term)
    except GTDataError as exc:
        raise click.ClickException(str(exc))


def _split_course_query(query: str) -> tuple[str, str] | None:
    match = _COURSE_KEY_RE.match(query)
    if not match:
        return None
    return match.group(1).upper(), match.group(2).upper()


@click.group(invoke_without_command=True)
@click.pass_context
def main(ctx: click.Context) -> None:
    """gtclass — browse GT course data and watch CRNs for seat/waitlist changes."""
    if ctx.invoked_subcommand is None:
        from gtclass.shell import run_repl

        run_repl()


@main.command()
@click.argument("query")
@click.option("--term", help="Term code, e.g. 202502. Defaults to the current term.")
def search(query: str, term: str | None) -> None:
    """Search the catalog by subject/number (\"CS 4210\") or title substring."""
    cfg = load_config()
    with db.connect() as conn, gtdata.new_client() as client:
        db.init_db(conn)
        resolved_term = _resolve_term(conn, client, term, cfg)
        _sync(conn, client, resolved_term)

        parsed = _split_course_query(query)
        if parsed:
            subject, number = parsed
            rows = conn.execute(
                """
                SELECT * FROM courses
                WHERE term = ? AND subject = ? AND course_number LIKE ?
                ORDER BY course_number, section
                """,
                (resolved_term, subject, f"{number}%"),
            ).fetchall()
        else:
            like = f"%{query}%"
            rows = conn.execute(
                """
                SELECT * FROM courses
                WHERE term = ? AND title LIKE ?
                ORDER BY subject, course_number, section
                """,
                (resolved_term, like),
            ).fetchall()

        formatting.print_search_results(rows, resolved_term)


@main.command()
@click.argument("crn")
@click.option("--term", help="Term code. Defaults to the current term.")
@click.option("--history", is_flag=True, help="Show recorded seat history for this CRN.")
def info(crn: str, term: str | None, history: bool) -> None:
    """Show full detail for a CRN, including live seat/waitlist status."""
    cfg = load_config()
    with db.connect() as conn, gtdata.new_client() as client:
        db.init_db(conn)
        resolved_term = _resolve_term(conn, client, term, cfg)
        _sync(conn, client, resolved_term)

        course = conn.execute(
            "SELECT * FROM courses WHERE term = ? AND crn = ?", (resolved_term, crn)
        ).fetchone()
        if course is None:
            raise click.ClickException(f"CRN {crn} not found in term {resolved_term}")

        try:
            seats = live.fetch_seat_status(client, resolved_term, crn)
        except LiveDataError as exc:
            err_console.print(f"[yellow]warning:[/yellow] {exc}")
            seats = None

        formatting.print_course_info(course, seats)

        if history:
            rows = conn.execute(
                """
                SELECT * FROM seat_snapshots
                WHERE term = ? AND crn = ?
                ORDER BY ts DESC LIMIT 50
                """,
                (resolved_term, crn),
            ).fetchall()
            formatting.print_seat_history(rows)


@main.group()
def watch() -> None:
    """Manage the CRN watchlist."""


@watch.command("add")
@click.argument("target")
@click.option("--term", help="Term code. Defaults to the current term.")
@click.option("--section", help="Section id, e.g. A, B, O1 (needed if TARGET is a course, not a CRN).")
@click.option("--channels", help="Comma-separated notify channels for this CRN (default: config default).")
def watch_add(target: str, term: str | None, section: str | None, channels: str | None) -> None:
    """Start tracking a CRN, or resolve TARGET (\"CS 4210\") + --section to one."""
    cfg = load_config()
    with db.connect() as conn, gtdata.new_client() as client:
        db.init_db(conn)
        resolved_term = _resolve_term(conn, client, term, cfg)
        _sync(conn, client, resolved_term)

        if target.isdigit():
            crn = target
            course = conn.execute(
                "SELECT * FROM courses WHERE term = ? AND crn = ?", (resolved_term, crn)
            ).fetchone()
            if course is None:
                raise click.ClickException(f"CRN {crn} not found in term {resolved_term}")
        else:
            parsed = _split_course_query(target)
            if not parsed:
                raise click.ClickException(
                    f"couldn't parse {target!r} as a CRN or \"SUBJECT NUMBER\""
                )
            subject, number = parsed
            params: list[str] = [resolved_term, subject, number]
            query = "SELECT * FROM courses WHERE term = ? AND subject = ? AND course_number = ?"
            if section:
                query += " AND section = ?"
                params.append(section.upper())
            rows = conn.execute(query, params).fetchall()
            if not rows:
                raise click.ClickException(f"no matching section found for {target!r}")
            if len(rows) > 1:
                sections = ", ".join(r["section"] for r in rows)
                raise click.ClickException(
                    f"multiple sections match {target!r} ({sections}); pass --section"
                )
            course = rows[0]
            crn = course["crn"]

        now = datetime.now(timezone.utc).isoformat()
        conn.execute(
            """
            INSERT INTO watchlist (term, crn, added_at, notify_channels)
            VALUES (?, ?, ?, ?)
            ON CONFLICT (term, crn) DO UPDATE SET notify_channels = excluded.notify_channels
            """,
            (resolved_term, crn, now, channels or ""),
        )
        console.print(
            f"[green]Watching[/green] CRN {crn} "
            f"({course['subject']} {course['course_number']}-{course['section']}, term {resolved_term})"
        )


@watch.command("list")
@click.option("--refresh/--no-refresh", default=True, help="Poll live status before listing (default on).")
def watch_list(refresh: bool) -> None:
    """Show everything tracked, with current seat/waitlist status."""
    cfg = load_config()
    with db.connect() as conn, gtdata.new_client() as client:
        db.init_db(conn)
        if refresh:
            events, errors = poller.poll_once(conn, client, cfg)
            for err in errors:
                err_console.print(f"[yellow]warning:[/yellow] CRN {err.crn}: {err.error}")

        rows = conn.execute(
            """
            SELECT
                w.term, w.crn, w.added_at, w.notify_channels,
                c.subject, c.course_number, c.section,
                s.seats_available, s.waitlist_available
            FROM watchlist w
            LEFT JOIN courses c ON c.term = w.term AND c.crn = w.crn
            LEFT JOIN (
                SELECT term, crn, seats_available, waitlist_available,
                       ROW_NUMBER() OVER (PARTITION BY term, crn ORDER BY ts DESC) rn
                FROM seat_snapshots
            ) s ON s.term = w.term AND s.crn = w.crn AND s.rn = 1
            ORDER BY w.term, w.crn
            """
        ).fetchall()

        rows = [
            {
                **dict(r),
                "course_label": (
                    f"{r['subject']} {r['course_number']}-{r['section']}"
                    if r["subject"]
                    else None
                ),
            }
            for r in rows
        ]
        formatting.print_watchlist(rows)


@watch.command("remove")
@click.argument("crn")
@click.option("--term", help="Term code. Defaults to the current term.")
def watch_remove(crn: str, term: str | None) -> None:
    """Stop tracking a CRN."""
    cfg = load_config()
    with db.connect() as conn, gtdata.new_client() as client:
        db.init_db(conn)
        resolved_term = _resolve_term(conn, client, term, cfg)
        cur = conn.execute(
            "DELETE FROM watchlist WHERE term = ? AND crn = ?", (resolved_term, crn)
        )
        if cur.rowcount == 0:
            raise click.ClickException(f"CRN {crn} was not on the watchlist for term {resolved_term}")
        console.print(f"[green]Stopped watching[/green] CRN {crn} (term {resolved_term})")


@main.group("notify")
def notify_group() -> None:
    """Configure and test notification channels."""


@notify_group.command("test")
def notify_test() -> None:
    """Send a test notification through every configured channel."""
    cfg = load_config()
    if not cfg.notify_channels:
        raise click.ClickException("no notify channels configured; run `gtclass notify config`")
    results = notify.send(cfg, cfg.notify_channels, "gtclass test", "This is a test notification.")
    for channel, error in results:
        if error:
            console.print(f"[red]FAILED[/red] {channel}: {error}")
        else:
            console.print(f"[green]OK[/green] {channel}")


@notify_group.command("config")
def notify_config() -> None:
    """Interactively choose notify channels and set up ntfy."""
    cfg = load_config()

    available = notify.available_channels()
    console.print(f"Available channels: {', '.join(available)}")
    default_channels = ",".join(cfg.notify_channels)
    raw = click.prompt("Channels to enable (comma-separated)", default=default_channels)
    channels = [c.strip() for c in raw.split(",") if c.strip()]

    for c in channels:
        if c not in available:
            raise click.ClickException(f"unknown channel {c!r}; available: {', '.join(available)}")

    if "ntfy" in channels:
        topic = click.prompt("ntfy topic", default=cfg.ntfy.topic or "")
        server = click.prompt("ntfy server", default=cfg.ntfy.server)
        cfg.ntfy.topic = topic
        cfg.ntfy.server = server

    cfg.notify_channels = channels
    save_config(cfg)
    console.print("[green]Saved.[/green]")


@main.group("daemon")
def daemon_group() -> None:
    """Run the background poller."""


@daemon_group.command("start")
@click.option("--interval", type=int, default=None, help="Poll interval in seconds.")
@click.option("--foreground", is_flag=True, help="Run in this terminal instead of detaching.")
def daemon_start(interval: int | None, foreground: bool) -> None:
    """Start the poller (detaches into the background by default)."""
    running, pid = daemon.status()
    if running:
        raise click.ClickException(f"daemon already running (pid {pid})")

    if foreground:
        console.print("Starting poller in foreground (Ctrl+C to stop)...")

        def on_tick(events, errors) -> None:
            ts = datetime.now(timezone.utc).isoformat(timespec="seconds")
            console.print(f"[dim]{ts}[/dim] polled {len(events)} CRN(s), {len(errors)} error(s)")
            for event in events:
                if event.changes:
                    console.print(f"  CRN {event.crn}: {'; '.join(event.changes)}")
            for err in errors:
                console.print(f"  [yellow]warning:[/yellow] CRN {err.crn}: {err.error}")

        try:
            daemon.run_foreground(interval, on_tick=on_tick)
        except KeyboardInterrupt:
            pass
        return

    pid = daemon.start_detached(interval)
    console.print(f"[green]Started[/green] gtclass daemon (pid {pid})")


@daemon_group.command("stop")
def daemon_stop() -> None:
    """Stop the background poller."""
    if daemon.stop():
        console.print("[green]Stopped[/green] gtclass daemon")
    else:
        raise click.ClickException("daemon is not running")


@daemon_group.command("status")
def daemon_status() -> None:
    """Show whether the background poller is running."""
    running, pid = daemon.status()
    if running:
        console.print(f"[green]running[/green] (pid {pid})")
    else:
        console.print("[yellow]not running[/yellow]")


if __name__ == "__main__":
    main()
