# gtclass — GT course availability CLI

A command-line tool to browse Georgia Tech course data, track live
seat/waitlist status for specific sections, and get notified when
something you're watching changes. See [PLAN.md](PLAN.md) for the full
design.

## Install (dev)

```
python3 -m venv .venv
.venv/bin/pip install -e ".[dev]"
```

Requires Python 3.11+ (uses `tomllib`).

## Usage

```
gtclass search "CS 4210"                    # browse catalog, any term
gtclass search "CS 4210" --term 202502      # specific term, defaults to current

gtclass info 87086                          # full detail + live seats for a CRN
gtclass info 87086 --history                # seat trend over time (if watched)

gtclass watch add 87086                     # start tracking a CRN
gtclass watch add "CS 4210" --section A     # or resolve by subject/number/section
gtclass watch list                          # everything tracked + live status
gtclass watch remove 87086

gtclass notify config                       # interactive channel setup
gtclass notify test                         # send a test notification

gtclass daemon start                        # run poller in background
gtclass daemon start --foreground           # run in this terminal instead
gtclass daemon stop
gtclass daemon status
```

Config lives at `~/.config/gtclass/config.toml`; the SQLite store lives at
`~/.local/share/gtclass/gtclass.db`.

## Interactive shell

Running `gtclass` with no arguments drops you into an interactive shell —
type any of the commands above without the leading `gtclass`:

```
$ gtclass
gtclass interactive shell — type a command (e.g. `search CS 4210`), or `exit`/Ctrl+D to quit.
gtclass> search "CS 4210"
gtclass> watch add 87086
gtclass> watch list
gtclass> exit
```

`gtclass <command> ...` from a normal shell still runs one-shot as before —
the interactive shell is purely additive.

## Notification channels (MVP)

- **desktop** — macOS notification via `osascript`, zero setup, requires
  the daemon running locally
- **ntfy** — POST to an [ntfy.sh](https://ntfy.sh) topic, ~2 min setup

Run `gtclass notify config` to choose channels and set an ntfy topic.

## Tests

```
.venv/bin/pytest
```

## What's implemented vs. planned

This is the MVP: catalog search/info, live seat lookups, watchlist,
polling (foreground or backgrounded), diffing, and two notification
channels. Not yet implemented: additional notify channels
(email/Discord/Pushover/Telegram/SMS) and the Phase 3 self-built Banner
client — see PLAN.md.
