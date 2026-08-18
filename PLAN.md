# gtclass — GT course availability CLI

A command-line tool to browse Georgia Tech course data (past and current
semesters), track live seat/waitlist status for specific sections, and get
notified when something you're watching changes.

## Goals

- Search and view course/section info for any semester, old or current
- Live, continuously updated seat/waitlist data for the current term
- Watch specific CRNs and get notified on status changes (closed → open,
  waitlist movement, seats dropping)
- Multiple notification channels, not locked to just email

## Non-goals (for now)

- No auto-enrollment / automated registration submission
- No web dashboard — CLI only

## Data sources

No official Georgia Tech API exists, but GT Scheduler (open source, actively
maintained) already solved most of the hard part and publishes usable
endpoints with no auth required:

| Source | URL | Use for |
| --- | --- | --- |
| Term list | `https://gt-scheduler.github.io/crawler/index.json` | Discover available terms |
| Bulk term catalog | `https://gt-scheduler.github.io/crawler/{term}.json` | Full course/section listing for one term, updated every ~30 min |
| Live seat count | `https://gt-scheduler.azurewebsites.net/proxy/class_section?term={term}&crn={crn}` | Real-time seats/waitlist for one CRN, proxied live from Oscar |

**Old semesters**: pull once from the bulk term JSON, cache in SQLite,
never re-fetch — they're finished and static.

**Current semester**: use bulk JSON for browsing/search (subject, number,
title, meeting times), but use the live per-CRN endpoint for anything
actively watched, since seats change faster than the 30-minute crawl cycle.

**Phase 3 stretch goal**: replace the GT Scheduler dependency with a
self-built Banner 9 client (reverse-engineered from Oscar's own network
requests) for full independence and portfolio value. Everything downstream
(store, diff engine, notifier) stays unchanged when this swap happens.

## Data model (SQLite)

**courses** — one row per section, per term
`term, crn, subject, course_number, section, title, instructor, meeting_days, meeting_time, location`

**seat_snapshots** — one row per poll, per watched CRN
`term, crn, timestamp, seats_available, seats_total, waitlist_available, waitlist_total`

**watchlist** — CRNs the user is actively tracking
`term, crn, added_at, notify_channels`

## Command surface

```
gtclass search "CS 4210"                    # browse catalog, any term
gtclass search "CS 4210" --term 202508      # specific term, defaults to current
gtclass info 87086                          # full detail for a CRN
gtclass info 87086 --history                # seat trend over time (if watched)

gtclass watch add 87086                     # start tracking a CRN
gtclass watch add "CS 4210" --section A     # or resolve by subject/number/section
gtclass watch list                          # everything tracked + current status
gtclass watch remove 87086

gtclass notify test                         # send a test notification
gtclass notify config                       # interactive setup for channels

gtclass daemon start                        # run poller in background
gtclass daemon stop
gtclass daemon status
```

## Notification channels

Support multiple channels simultaneously (config, not a single choice) so a
missed device doesn't mean a missed spot.

| Channel | Setup effort | Notes |
| --- | --- | --- |
| macOS desktop notification | Zero | `osascript`, local only, requires daemon running on your Mac |
| ntfy.sh | ~2 min | Free, no account, POST to a topic, phone app or browser |
| Email (SMTP) | ~5 min | Gmail app password, or a transactional API (Resend/Mailgun free tier) |
| Discord webhook | ~5 min | One-liner POST, good if you already live in Discord |
| Pushover | ~10 min, $5 one-time | Purpose-built for script-to-phone push |
| Telegram bot | ~10 min | Message yourself via bot token |
| SMS (Twilio) | ~15 min, per-message cost | Only if a literal text is required |

Recommended starting set: **ntfy (phone) + macOS notification (desktop) +
email (fallback/log)**.

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
 Local store   Notifier (pluggable: ntfy, email, desktop, ...)
 (SQLite)
```

## Build order

1. **Catalog import** — bulk term JSON → SQLite; `search` and `info` working
   for old and current semesters
2. **Live seat polling** — current-term seat data pulled from the live
   per-CRN endpoint; `--history` flag once snapshots exist
3. **Watch + diff** — watchlist table, background daemon, diff logic
   (closed→open, waitlist movement, seat count deltas)
4. **Notifications** — pluggable notifier interface; start with ntfy +
   desktop, add email
5. **Polish** — `rich` tables for output, config file at
   `~/.config/gtclass/config.toml` for default term + notification channels

## Suggested stack

- Python — `httpx`/`requests` for polling, `APScheduler` or a simple
  `asyncio` loop for the daemon, `sqlite3` (stdlib) for storage, `rich`
  for CLI tables, `click` or `typer` for the command interface
- No official API means the GT Scheduler dependency (or your own Banner
  client in Phase 3) is a hard requirement of the whole project — worth
  monitoring GT Scheduler's uptime/status if you stay dependent on it
