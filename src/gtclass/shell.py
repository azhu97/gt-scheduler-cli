"""Interactive shell: bare `gtclass` (no subcommand) drops into this.

Reuses the existing Click command tree as the parser/dispatcher — each typed
line is split and fed straight into `gtclass.cli.main`, so every command
(`search`, `watch add`, `daemon start`, ...) gets shell support for free with
no duplicated logic. The shell itself stays stateless: `--term` flags and
`config.toml`'s `default_term` behave exactly as they do for one-shot
invocations.
"""

from __future__ import annotations

import shlex
import sys

import click

from gtclass.formatting import console, err_console

PROMPT = "gtclass> "
EXIT_COMMANDS = {"exit", "quit"}


def run_repl() -> None:
    if not sys.stdin.isatty():
        console.print("Not an interactive terminal; run `gtclass --help` for usage.")
        return

    from gtclass.cli import main

    try:
        import readline  # noqa: F401  (side effect: line editing + history)
    except ImportError:
        pass

    console.print(
        "gtclass interactive shell — type a command (e.g. `search CS 4210`), "
        "or `exit`/Ctrl+D to quit."
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
