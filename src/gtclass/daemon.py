"""Background poller: `gtclass daemon start/stop/status`.

MVP implementation: a simple sleep loop (no APScheduler dependency),
optionally detached into the background via subprocess + a PID file
under the data dir, since that covers the "run poller in background"
requirement without extra process-management machinery.
"""

from __future__ import annotations

import json
import os
import plistlib
import platform
import signal
import subprocess
import sys
import time
import traceback
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

from gtclass import db, gtdata, notify, poller
from gtclass.config import Config, load_config, log_path, pid_path, state_path

_stop_requested = False

LAUNCHD_LABEL = "com.gtclass.daemon"


@dataclass
class StatusInfo:
    running: bool
    pid: int | None
    started_at: str | None
    poll_interval: int | None
    last_polled_at: str | None
    last_poll_crns: int | None
    last_poll_errors: int | None
    last_error: str | None
    last_error_at: str | None


def _handle_sigterm(signum, frame) -> None:
    global _stop_requested
    _stop_requested = True


def is_running(pid: int) -> bool:
    try:
        os.kill(pid, 0)
    except OSError:
        return False
    return True


def _is_gtclass_process(pid: int) -> bool:
    """Best-effort guard against a reused PID: checks the live process's
    command line actually looks like a gtclass daemon, not just that
    *some* process with this PID exists."""
    try:
        result = subprocess.run(
            ["ps", "-p", str(pid), "-o", "command="],
            capture_output=True,
            text=True,
            timeout=5,
        )
    except (OSError, subprocess.SubprocessError):
        return True  # can't verify; don't false-negative a healthy daemon
    return "gtclass" in result.stdout


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
    if is_running(pid) and _is_gtclass_process(pid):
        return True, pid
    pid_path().unlink(missing_ok=True)
    state_path().unlink(missing_ok=True)
    return False, None


def _read_state() -> dict:
    path = state_path()
    if not path.exists():
        return {}
    try:
        return json.loads(path.read_text())
    except (ValueError, OSError):
        return {}


def _write_state(**updates: object) -> None:
    state = _read_state()
    state.update(updates)
    state_path().write_text(json.dumps(state))


def status_detail() -> StatusInfo:
    running, pid = status()
    state = _read_state() if running else {}
    return StatusInfo(
        running=running,
        pid=pid,
        started_at=state.get("started_at"),
        poll_interval=state.get("poll_interval"),
        last_polled_at=state.get("last_polled_at"),
        last_poll_crns=state.get("last_poll_crns"),
        last_poll_errors=state.get("last_poll_errors"),
        last_error=state.get("last_error"),
        last_error_at=state.get("last_error_at"),
    )


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
    state_path().unlink(missing_ok=True)
    return True


def run_foreground(interval: int | None = None, on_tick=None) -> None:
    """Blocking poll loop. Installs a SIGTERM handler so `stop()` is graceful."""
    signal.signal(signal.SIGTERM, _handle_sigterm)

    pid_path().parent.mkdir(parents=True, exist_ok=True)
    pid_path().write_text(str(os.getpid()))

    cfg = load_config()
    poll_interval = interval or cfg.poll_interval_seconds
    _write_state(
        started_at=datetime.now(timezone.utc).isoformat(),
        poll_interval=poll_interval,
        last_polled_at=None,
        last_poll_crns=None,
        last_poll_errors=None,
        last_error=None,
        last_error_at=None,
    )

    try:
        with db.connect() as conn, gtdata.new_client() as client:
            db.init_db(conn)
            while not _stop_requested:
                try:
                    events, errors = poller.poll_once(conn, client, cfg)
                    conn.commit()
                    _write_state(
                        last_polled_at=datetime.now(timezone.utc).isoformat(),
                        last_poll_crns=len(events),
                        last_poll_errors=len(errors),
                    )
                    if on_tick:
                        on_tick(events, errors)
                except Exception as exc:  # noqa: BLE001 - keep the loop alive
                    conn.rollback()
                    traceback.print_exc(file=sys.stderr)
                    _write_state(
                        last_error=str(exc),
                        last_error_at=datetime.now(timezone.utc).isoformat(),
                    )
                for _ in range(poll_interval * 10):
                    if _stop_requested:
                        break
                    time.sleep(0.1)
    finally:
        pid_path().unlink(missing_ok=True)
        state_path().unlink(missing_ok=True)


def start_detached(interval: int | None = None) -> int:
    running, pid = status()
    if running:
        return pid  # type: ignore[return-value]

    log_path().parent.mkdir(parents=True, exist_ok=True)
    with open(log_path(), "ab") as log_file:
        args = [sys.executable, "-m", "gtclass", "daemon", "start", "--foreground"]
        if interval:
            args += ["--interval", str(interval)]
        process = subprocess_popen(args, log_file)
    return process.pid


def subprocess_popen(args: list[str], log_file):
    return subprocess.Popen(
        args,
        stdout=log_file,
        stderr=log_file,
        stdin=subprocess.DEVNULL,
        start_new_session=True,
    )


def launchd_plist_path() -> Path:
    return Path.home() / "Library" / "LaunchAgents" / f"{LAUNCHD_LABEL}.plist"


def _launchd_plist_dict(interval: int | None = None) -> dict:
    args = [sys.executable, "-m", "gtclass", "daemon", "start", "--foreground"]
    if interval:
        args += ["--interval", str(interval)]
    return {
        "Label": LAUNCHD_LABEL,
        "ProgramArguments": args,
        "RunAtLoad": True,
        "KeepAlive": {"SuccessfulExit": False},
        "StandardOutPath": str(log_path()),
        "StandardErrorPath": str(log_path()),
        "ProcessType": "Background",
    }


def is_launchd_installed() -> bool:
    return launchd_plist_path().exists()


def install_launchd(interval: int | None = None) -> Path:
    """Write a LaunchAgent plist and load it: auto-starts at login,
    auto-restarts on a crash (nonzero exit), via launchd's KeepAlive."""
    if platform.system() != "Darwin":
        raise RuntimeError("launchd install is only supported on macOS")

    path = launchd_plist_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "wb") as f:
        plistlib.dump(_launchd_plist_dict(interval), f)

    uid = os.getuid()
    subprocess.run(
        ["launchctl", "bootout", f"gui/{uid}/{LAUNCHD_LABEL}"],
        capture_output=True,
    )
    subprocess.run(
        ["launchctl", "bootstrap", f"gui/{uid}", str(path)],
        check=True,
        capture_output=True,
    )
    return path


def uninstall_launchd() -> bool:
    if not is_launchd_installed():
        return False
    uid = os.getuid()
    subprocess.run(
        ["launchctl", "bootout", f"gui/{uid}/{LAUNCHD_LABEL}"],
        capture_output=True,
    )
    launchd_plist_path().unlink(missing_ok=True)
    return True
