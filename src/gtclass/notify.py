"""Pluggable notification channels.

Multiple channels can be active at once (config, not a single choice) so
a missed device doesn't mean a missed spot. MVP ships two zero/low-effort
channels; more (email, Discord, Pushover, Telegram, SMS) can be added
later behind the same `send(channel, message)` interface.
"""

from __future__ import annotations

import shutil
import subprocess

import httpx

from gtclass.config import Config


class NotifyError(RuntimeError):
    """Raised when a channel fails to deliver a notification."""


def _applescript_quote(value: str) -> str:
    escaped = value.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def _notify_desktop(title: str, message: str) -> None:
    if shutil.which("osascript") is None:
        raise NotifyError("desktop notifications require macOS (osascript not found)")
    script = (
        f"display notification {_applescript_quote(message)} "
        f"with title {_applescript_quote(title)}"
    )
    result = subprocess.run(
        ["osascript", "-e", script], capture_output=True, text=True
    )
    if result.returncode != 0:
        raise NotifyError(f"osascript failed: {result.stderr.strip()}")


def _notify_ntfy(cfg: Config, title: str, message: str) -> None:
    if not cfg.ntfy.topic:
        raise NotifyError("ntfy topic not configured; run `gtclass notify config`")
    url = f"{cfg.ntfy.server.rstrip('/')}/{cfg.ntfy.topic}"
    try:
        resp = httpx.post(
            url,
            content=message.encode("utf-8"),
            headers={"Title": title},
            timeout=10.0,
        )
        resp.raise_for_status()
    except httpx.HTTPError as exc:
        raise NotifyError(f"ntfy post failed: {exc}") from exc


_CHANNELS = {
    "desktop": lambda cfg, title, message: _notify_desktop(title, message),
    "ntfy": lambda cfg, title, message: _notify_ntfy(cfg, title, message),
}


def available_channels() -> list[str]:
    return sorted(_CHANNELS)


def send(cfg: Config, channels: list[str], title: str, message: str) -> list[tuple[str, str | None]]:
    """Send to each channel; returns (channel, error_or_None) per channel."""
    results: list[tuple[str, str | None]] = []
    for channel in channels:
        handler = _CHANNELS.get(channel)
        if handler is None:
            results.append((channel, f"unknown channel {channel!r}"))
            continue
        try:
            handler(cfg, title, message)
            results.append((channel, None))
        except NotifyError as exc:
            results.append((channel, str(exc)))
    return results
