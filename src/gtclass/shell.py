"""Interactive shell: bare `gtclass` (no subcommand) drops into this.

Reuses the existing Click command tree as the parser/dispatcher — each typed
line is split and fed straight into `gtclass.cli.main`, so every command
(`search`, `watch add`, `daemon start`, ...) gets shell support for free with
no duplicated logic.

On startup, the shell asks which semester to browse (current vs. a specific
previous one) and stores the choice in `SESSION_TERM_ENV_VAR` for the rest of
the process — `_resolve_term` in cli.py checks that env var. This is the one
piece of session state the shell carries; a `--term` flag on any single
command still overrides it, and `config.toml`'s `default_term` is unaffected
(the choice is never persisted to disk).
"""

from __future__ import annotations

import os
import shlex
import sys

import click

from gtclass import db, gtdata
from gtclass.config import SESSION_TERM_ENV_VAR
from gtclass.formatting import console, err_console

PROMPT = "gtclass> "
EXIT_COMMANDS = {"exit", "quit"}


def _prompt_choice(prompt_text: str, valid: set[str], default: str | None = None) -> str | None:
    """Read a line, retrying until it's one of `valid`. None on Ctrl+C/Ctrl+D."""
    suffix = f" [{default}]" if default else ""
    while True:
        try:
            raw = input(f"{prompt_text}{suffix}: ").strip()
        except (EOFError, KeyboardInterrupt):
            console.print()
            return None
        if not raw and default is not None:
            return default
        if raw in valid:
            return raw
        err_console.print(f"[red]invalid choice:[/red] {raw!r}")


def _select_term(conn, client) -> str | None:
    """Ask current-vs-previous, then which previous semester. None = skip."""
    try:
        current = gtdata.resolve_default_term(conn, client)
    except gtdata.GTDataError as exc:
        err_console.print(f"[yellow]{exc}[/yellow]")
        return None

    try:
        terms = sorted(gtdata.fetch_term_index(client), reverse=True)
    except gtdata.GTDataError:
        terms = [
            r["term"]
            for r in conn.execute("SELECT term FROM terms_meta ORDER BY term DESC").fetchall()
        ]

    console.print("Which semester do you want to browse?")
    console.print(f"  1) Current (most recent with data): {gtdata.term_label(current)} [{current}]")
    console.print("  2) A previous semester")
    choice = _prompt_choice("Enter 1 or 2", {"1", "2"}, default="1")
    if choice is None:
        return None
    if choice == "1":
        return current

    previous = [t for t in terms if t != current]
    if not previous:
        console.print("[yellow]No previous semesters available.[/yellow]")
        return current

    console.print("Previous semesters:")
    for i, t in enumerate(previous, start=1):
        console.print(f"  {i}) {gtdata.term_label(t)} [{t}]")
    idx = _prompt_choice("Enter a number", {str(i) for i in range(1, len(previous) + 1)})
    if idx is None:
        return None
    return previous[int(idx) - 1]


def run_repl() -> None:
    if not sys.stdin.isatty():
        console.print("Not an interactive terminal; run `gtclass --help` for usage.")
        return

    from gtclass.cli import main

    try:
        import readline  # noqa: F401  (side effect: line editing + history)
    except ImportError:
        pass

    with db.connect() as conn, gtdata.new_client() as client:
        db.init_db(conn)
        term = _select_term(conn, client)

    if term:
        os.environ[SESSION_TERM_ENV_VAR] = term
        console.print(
            f"[green]Using {gtdata.term_label(term)} [{term}][/green] for this "
            "session (pass --term to override for a single command)."
        )

    console.print(
        "Type a command (e.g. `search CS 4210`), or `exit`/Ctrl+D to quit."
    )

    while True:
        try:
            line = input(PROMPT)
        except EOFError:
            console.print()
            break
        except KeyboardInterrupt:
            console.print()
            continue

        line = line.strip()
        if not line:
            continue
        if line in EXIT_COMMANDS:
            break
        if line == "help":
            line = "--help"

        try:
            args = shlex.split(line)
        except ValueError as exc:
            err_console.print(f"[red]parse error:[/red] {exc}")
            continue

        try:
            main.main(args=args, prog_name="gtclass", standalone_mode=False)
        except click.ClickException as exc:
            exc.show()
        except (click.exceptions.Exit, SystemExit):
            pass
        except Exception as exc:  # keep the shell alive on unexpected errors
            err_console.print(f"[red]error:[/red] {exc}")
