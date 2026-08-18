# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`gtclass` — a CLI to browse Georgia Tech course data, track live
seat/waitlist status for specific CRNs, and notify the user when a watched
section's status changes (closed → open, waitlist movement, seat deltas).
CLI only; no auto-registration, no web dashboard. Full design rationale
lives in `PLAN.md` — read it for the "why" behind the architecture below.
The MVP described there is implemented; not-yet-built items (Phase 3
Banner client, extra notify channels) are called out in README.md.

## Function reference

`src/gtclass/FUNCTIONS.md` documents the general purpose of every
function, method, and class in `src/gtclass/` — one line each, organized
by module. **Whenever you add, remove, rename, or change the purpose of a
function in `src/gtclass/`, update `FUNCTIONS.md` in the same change.**
This applies to every function ever created here, with no exceptions —
private helpers (`_leading_underscore`) included.

## Commands

```
python3 -m venv .venv                    # requires Python 3.11+ (tomllib)
.venv/bin/pip install -e ".[dev]"
.venv/bin/pytest                         # run the full test suite
.venv/bin/pytest tests/test_gtdata.py    # single file
.venv/bin/pytest tests/test_gtdata.py::test_parse_term_catalog_basic_section  # single test
.venv/bin/gtclass --help
```

No linter/formatter is configured yet.

Do not add a `Co-Authored-By: Claude` (or similar) trailer to commit
messages in this repo.

## Architecture

```
Course data (bulk JSON + live per-CRN feed)
        |
        v
     Poller (interval-based, per watched CRN)
        |
        v
    Diff engine (compares snapshot to last known state)
       / \
      v   v
 Local store   Notifier (pluggable: ntfy, desktop, ...)
 (SQLite)
```

Module map (`src/gtclass/`):

- `gtdata.py` — fetches and parses GT Scheduler's bulk term catalog
  (`https://gt-scheduler.github.io/crawler-v2/{term}.json`, the actively
  maintained crawler; the older `.../crawler/{term}.json` repo it used to
  point at stopped being updated in 2023). This is a
  **heavily packed, positional JSON format**, not a normal REST payload:
  `courses` maps `"SUBJ NUMBER"` → `[title, sections, attributes,
  description]`; `sections` maps a section id → `[crn, meetings, credits,
  scheduleTypeIdx, campusIdx, attributeIdxs, gradeBaseIdx]`; each meeting
  is `[periodIdx, days, room, scheduleTypeIdx, instructors, campusIdx,
  dateRangeIdx, finalDateIdx]`. `periodIdx` indexes into
  `caches.periods` for a human time range. No seat/enrollment data lives
  here at all — see `live.py`. `sync_term()` caches parsed rows into the
  `courses` SQLite table and is safe to call on every command: it no-ops
  if the term was synced within `STALE_AFTER` (25 min), which is how
  "old terms fetched once, current term refreshed periodically" (per
  PLAN.md) is implemented without a separate explicit sync command.
- `live.py` — fetches the live per-CRN seat/waitlist endpoint. This
  returns an **HTML fragment, not JSON**, so it's scraped with regexes
  keyed on the field labels (`Enrollment Actual:`, `Waitlist Capacity:`,
  etc.) rather than parsed as structured data.
- `db.py` — SQLite schema (`courses`, `seat_snapshots`, `watchlist`,
  plus `terms_meta` for sync bookkeeping) and a `connect()` context
  manager. Default DB path: `~/.local/share/gtclass/gtclass.db`.
- `poller.py` — `poll_once()` walks the `watchlist` table, fetches live
  status per CRN, diffs against the most recent `seat_snapshots` row,
  and fires notifications through `notify.py` only when something
  changed (closed→open, seat/waitlist count deltas).
- `notify.py` — pluggable channel dispatch (`send(cfg, channels, title,
  message)`); each channel is a small function keyed in `_CHANNELS`.
  Adding a channel (email, Discord, Pushover, Telegram, SMS — per
  PLAN.md's list) means adding one function and one dict entry.
- `daemon.py` — background poller lifecycle. No APScheduler dependency;
  it's a `time.sleep` loop with a PID file
  (`~/.local/share/gtclass/daemon.pid`) and a `SIGTERM` handler for
  graceful shutdown. `daemon start` detaches by default (spawns
  `python -m gtclass daemon start --foreground` via `subprocess.Popen`
  with `start_new_session=True`, logging to `daemon.log`); `--foreground`
  runs blocking in the current terminal for debugging.
- `config.py` — reads/writes `~/.config/gtclass/config.toml` (channels,
  ntfy topic/server, poll interval, default term). Uses stdlib `tomllib`
  for reading; the writer is hand-rolled (not a generic TOML serializer)
  since the schema is small and fixed.
- `cli.py` — Click entrypoint wiring `search` / `info` / `watch
  add|list|remove` / `notify test|config` / `daemon start|stop|status`.
  `watch add` accepts either a raw CRN or a `"SUBJECT NUMBER"` query plus
  `--section`; ambiguous course queries (multiple matching sections, no
  `--section`) raise a `ClickException` listing the options. The `main`
  group is `invoke_without_command=True`; a bare invocation with no
  subcommand hands off to `shell.run_repl()`.
- `shell.py` — interactive shell (bare `gtclass`). Reuses the same Click
  command tree as the parser/dispatcher: each typed line is `shlex.split()`
  and fed into `cli.main.main(args=..., standalone_mode=False)`, so every
  command gets shell support for free with no duplicated logic. On startup
  it prompts current-vs-previous semester (via `gtdata.resolve_default_term`
  / `gtdata.term_label`) and stores the pick in the
  `config.SESSION_TERM_ENV_VAR` env var for the rest of the process — the
  one piece of session state the shell carries. `cli._resolve_term` checks
  that env var (below `--term`, above `config.toml`'s `default_term`); it's
  never written to disk, so it doesn't leak into other invocations. Once a
  term is picked, the REPL prompt itself is rebuilt to
  `gtclass>{term_label}> ` (falling back to the bare `gtclass> ` from
  `PROMPT` if no term was picked, e.g. Ctrl+C during selection) so it's
  always visible which semester's data a command will hit.

## Non-obvious behavior worth knowing before changing things

- **Term resolution precedence**: `--term` flag > shell session term
  (`SESSION_TERM_ENV_VAR`, set only inside the interactive shell) >
  `config.toml` `default_term` > latest *non-empty* term from GT
  Scheduler's `index.json` (falls back to the most recently locally-synced
  non-empty term if offline). See `cli._resolve_term` /
  `gtdata.resolve_default_term`.
- **GT Scheduler's `index.json` can list terms its crawler hasn't
  populated yet** — a term can be present with an empty `{"courses": {}}`
  placeholder before the crawler backfills it. `resolve_default_term()`
  walks the index newest-first and syncs candidates until one has
  `course_count > 0`, rather than trusting the max term code. Don't
  regress this to `max(terms)`. Note `index.json`'s entries are objects
  (`{"term": "202608", "finalized": true}`), not bare term-code strings —
  `fetch_term_index()` unwraps that.
- **The org's original `crawler` repo/Pages site
  (`gt-scheduler.github.io/crawler/...`) stopped being updated in 2023**
  and now silently serves stale data frozen at Spring 2023 (`202302`) —
  it doesn't 404, so a resolver pointed at it will confidently report the
  wrong "current" term forever. GT Scheduler migrated to `crawler-v2`
  (`gt-scheduler.github.io/crawler-v2/...`), which `gtdata.py`'s
  `INDEX_URL`/`TERM_URL_TMPL` now point at. If "current term" ever looks
  wrong again, check whether the org has moved on to a `crawler-v3` etc.
  before assuming the bug is in `resolve_default_term()`.
- **`watch list` refreshes live by default** (`--refresh/--no-refresh`,
  default on) — it calls `poll_once()` before rendering, so it also
  writes new snapshots and can trigger notifications as a side effect of
  just listing.
- **CRNs are looked up per-term**: `courses` and `seat_snapshots` are
  both keyed on `(term, crn)`, not `crn` alone.
- **Bare `gtclass` launches the interactive shell**, not help text — a
  behavior change from a plain `click.group()`. `gtclass --help` still
  works (Click's `--help` is an eager option, processed before the
  group's callback). Piping into `gtclass` non-interactively prints a
  hint and returns immediately rather than blocking on `input()`.
