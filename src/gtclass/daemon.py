"""Background poller: `gtclass daemon start/stop/status`.

MVP implementation: a simple sleep loop (no APScheduler dependency),
optionally detached into the background via subprocess + a PID file
under the data dir, since that covers the "run poller in background"
requirement without extra process-management machinery.
"""

from __future__ import annotations

import os
import signal
import sys
import time
from datetime import datetime, timezone

from gtclass import db, gtdata, notify, poller
from gtclass.config import Config, load_config, pid_path

_stop_requested = False


def _handle_sigterm(signum, frame) -> None:
    global _stop_requested
    _stop_requested = True


def is_running(pid: int) -> bool:
    try:
        os.kill(pid, 0)
    except OSError:
        return False
    return True


def read_pid() -> int | None:
    path = pid_path()
    if not path.exists():
        return None
    try:
        return int(path.read_text().strip())
    except ValueError:
        return None


def status() -> tuple[bool, int | None]:
    pid = read_pid()
    if pid is None:
        return False, None
    if is_running(pid):
        return True, pid
    pid_path().unlink(missing_ok=True)
    return False, None


def stop() -> bool:
    running, pid = status()
    if not running or pid is None:
        return False
    os.kill(pid, signal.SIGTERM)
    for _ in range(20):
        if not is_running(pid):
            break
        time.sleep(0.1)
    pid_path().unlink(missing_ok=True)
    return True


def run_foreground(interval: int | None = None, on_tick=None) -> None:
    """Blocking poll loop. Installs a SIGTERM handler so `stop()` is graceful."""
    signal.signal(signal.SIGTERM, _handle_sigterm)

    pid_path().parent.mkdir(parents=True, exist_ok=True)
    pid_path().write_text(str(os.getpid()))

    cfg = load_config()
    poll_interval = interval or cfg.poll_interval_seconds

    try:
        with db.connect() as conn, gtdata.new_client() as client:
            db.init_db(conn)
            while not _stop_requested:
                events, errors = poller.poll_once(conn, client, cfg)
                conn.commit()
                if on_tick:
                    on_tick(events, errors)
                for _ in range(poll_interval * 10):
                    if _stop_requested:
                        break
                    time.sleep(0.1)
    finally:
        pid_path().unlink(missing_ok=True)


def start_detached(interval: int | None = None) -> int:
    running, pid = status()
    if running:
        return pid  # type: ignore[return-value]

    from gtclass.config import log_path

    log_path().parent.mkdir(parents=True, exist_ok=True)
    with open(log_path(), "ab") as log_file:
        args = [sys.executable, "-m", "gtclass", "daemon", "start", "--foreground"]
        if interval:
            args += ["--interval", str(interval)]
        process = subprocess_popen(args, log_file)
    return process.pid


def subprocess_popen(args: list[str], log_file):
    import subprocess

    return subprocess.Popen(
        args,
        stdout=log_file,
        stderr=log_file,
        stdin=subprocess.DEVNULL,
        start_new_session=True,
    )
