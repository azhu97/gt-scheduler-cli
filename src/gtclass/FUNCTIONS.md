# Function reference

One line per function/method/class in `src/gtclass/`, stating what it's
for. Organized by module, in source order. When a function is added,
removed, or its purpose changes, update this file in the same change —
see the note in `CLAUDE.md`.

## `__main__.py`

- `main` (imported from `cli`, invoked via `python -m gtclass`) — entrypoint
  for running the package as a module.

## `cli.py`

- `_resolve_term` — picks the term to operate on for a single command,
  in precedence order: `--term` flag > shell session term > `config.toml`
  default > GT Scheduler's current term.
- `_sync` — wraps `gtdata.sync_term`, turning `GTDataError` into a
  `ClickException` so failures surface as normal CLI errors.
- `_split_course_query` — parses a `"SUBJECT NUMBER"` string (e.g. `"CS 4210"`)
  into `(subject, number)`, or `None` if it doesn't match that shape.
- `main` — root Click group; with no subcommand, hands off to the
  interactive shell (`shell.run_repl`) instead of printing help.
- `search` — looks up courses by subject/number or title substring and
  prints a results table.
- `info` — shows full detail for one CRN (catalog row + live seat status),
  optionally with recorded seat history.
- `watch` — Click group namespacing the `watch add|list|remove` commands.
- `watch_add` — starts tracking a CRN, resolving a `"SUBJECT NUMBER"` +
  `--section` query to a CRN first if needed.
- `watch_list` — prints the watchlist with current seat/waitlist status;
  polls live data first by default (`--refresh`).
- `watch_remove` — stops tracking a CRN.
- `notify_group` — Click group namespacing the `notify test|config` commands.
- `notify_test` — sends a test notification through every configured channel.
- `notify_config` — interactive prompt to choose notify channels and
  configure ntfy.
- `daemon_group` — Click group namespacing the `daemon start|stop|status`
  commands.
- `daemon_start` — starts the background poller (detached by default, or
  blocking in the current terminal with `--foreground`).
- `daemon_stop` — stops the background poller.
- `daemon_status` — reports whether the background poller is running.

## `config.py`

- `config_dir` — resolves the config directory (`$XDG_CONFIG_HOME` or
  `~/.config`) `/gtclass`.
- `data_dir` — resolves the data directory (`$XDG_DATA_HOME` or
  `~/.local/share`) `/gtclass`.
- `config_path` — path to `config.toml` inside `config_dir()`.
- `db_path` — path to `gtclass.db` inside `data_dir()`.
- `pid_path` — path to the daemon's PID file inside `data_dir()`.
- `log_path` — path to the daemon's log file inside `data_dir()`.
- `NtfyConfig` — dataclass holding the ntfy topic/server pair.
- `Config` — dataclass holding the full parsed config (default term, poll
  interval, notify channels, ntfy settings).
- `load_config` — reads and parses `config.toml`, falling back to defaults
  if it doesn't exist.
- `save_config` — hand-rolled TOML writer that persists a `Config` back to
  `config.toml`.

## `daemon.py`

- `_handle_sigterm` — signal handler that flips the module-level
  `_stop_requested` flag so the poll loop exits gracefully.
- `is_running` — checks whether a PID is alive via `os.kill(pid, 0)`.
- `read_pid` — reads and parses the daemon's PID file, if present.
- `status` — reports `(running, pid)`, cleaning up a stale PID file if the
  process is gone.
- `stop` — sends `SIGTERM` to the running daemon and waits (briefly) for
  it to exit.
- `run_foreground` — the blocking poll loop itself: writes the PID file,
  calls `poller.poll_once` on an interval, and cleans up on exit.
- `start_detached` — spawns `python -m gtclass daemon start --foreground`
  as a detached background process.
- `subprocess_popen` — thin wrapper around `subprocess.Popen` (isolated
  for easy mocking in tests).

## `db.py`

- `connect` — context manager yielding a `sqlite3.Connection` to the
  gtclass DB (row factory set, commits on clean exit).
- `init_db` — runs the schema (`courses`, `terms_meta`, `seat_snapshots`,
  `watchlist`) as `CREATE TABLE IF NOT EXISTS`, safe to call every time.

## `formatting.py`

- `print_search_results` — renders `search` output as a Rich table.
- `print_course_info` — renders `info` output (course detail + optional
  live seat status) as a Rich table.
- `print_seat_history` — renders the `--history` seat-snapshot table for
  `info`.
- `print_watchlist` — renders `watch list` output as a Rich table.

## `gtdata.py`

- `GTDataError` — raised when GT Scheduler data can't be fetched or parsed.
- `new_client` — builds the shared `httpx.Client` used for all GT
  Scheduler requests.
- `term_label` — converts a term code (`"202608"`) to a human label
  (`"Fall 2026"`).
- `fetch_term_index` — fetches the list of term codes GT Scheduler's
  crawler knows about.
- `fetch_term_catalog` — fetches the raw packed JSON catalog blob for one term.
- `_clean_days` — strips `&nbsp;` and whitespace from a raw meeting-days string.
- `parse_term_catalog` — flattens the packed catalog blob into one flat
  dict per (term, CRN) row, ready for SQLite insertion.
- `SyncResult` — dataclass reporting whether a `sync_term` call actually
  fetched, and the resulting course count.
- `_get_terms_meta` — reads a term's row from `terms_meta`.
- `sync_term` — ensures a term's catalog is cached in SQLite, re-fetching
  only if stale (or forced).
- `resolve_default_term` — walks GT Scheduler's term index newest-first
  and returns the first term that actually has course data, since the
  index can list terms ahead of what the crawler has populated.

## `live.py`

- `LiveDataError` — raised when live seat data can't be fetched or the
  CRN is unknown.
- `SeatStatus` — dataclass holding parsed seat/waitlist counts for one CRN.
- `parse_seat_html` — regex-scrapes seat/waitlist numbers out of the live
  endpoint's HTML fragment.
- `fetch_seat_status` — fetches and parses live seat status for one
  (term, CRN).

## `notify.py`

- `NotifyError` — raised when a channel fails to deliver a notification.
- `_applescript_quote` — escapes a string for safe interpolation into an
  AppleScript literal.
- `_notify_desktop` — sends a macOS desktop notification via `osascript`.
- `_notify_ntfy` — POSTs a notification to an ntfy topic/server.
- `available_channels` — lists the notify channel names that are
  currently implemented.
- `send` — sends a message through each requested channel, collecting
  per-channel success/error results.

## `poller.py`

- `PollEvent` — dataclass reporting one CRN's poll result: status, diffed
  changes, and notification results.
- `PollError` — dataclass reporting one CRN's poll failure.
- `_last_snapshot` — reads the most recent `seat_snapshots` row for a
  (term, CRN).
- `_diff` — compares the previous snapshot to a fresh `SeatStatus` and
  describes what changed (closed→open, seat/waitlist deltas).
- `poll_once` — walks the whole watchlist, fetches live status per CRN,
  records a snapshot, and fires notifications for anything that changed.

## `shell.py`

- `_prompt_choice` — reads a line from stdin, re-prompting until it's one
  of a set of valid answers (or a default is accepted).
- `_select_term` — asks current-vs-previous semester, then which previous
  one if applicable; drives the shell's startup term prompt.
- `run_repl` — the interactive shell's main loop: picks a term, builds the
  `gtclass>{term}>` prompt, and dispatches typed lines into `cli.main`.
