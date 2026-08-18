"""Paths and config file (~/.config/gtclass/config.toml) handling."""

from __future__ import annotations

import os
import tomllib
from dataclasses import dataclass, field
from pathlib import Path

DEFAULT_POLL_INTERVAL_SECONDS = 60
DEFAULT_NTFY_SERVER = "https://ntfy.sh"

# Set by the interactive shell (shell.py) after its startup semester prompt,
# read by cli._resolve_term. Session-scoped (this process only) — never
# written to config.toml, so it doesn't leak into other invocations.
SESSION_TERM_ENV_VAR = "GTCLASS_SESSION_TERM"


def config_dir() -> Path:
    base = os.environ.get("XDG_CONFIG_HOME")
    path = Path(base) if base else Path.home() / ".config"
    return path / "gtclass"


def data_dir() -> Path:
    base = os.environ.get("XDG_DATA_HOME")
    path = Path(base) if base else Path.home() / ".local" / "share"
    return path / "gtclass"


def config_path() -> Path:
    return config_dir() / "config.toml"


def db_path() -> Path:
    return data_dir() / "gtclass.db"


def pid_path() -> Path:
    return data_dir() / "daemon.pid"


def log_path() -> Path:
    return data_dir() / "daemon.log"


@dataclass
class NtfyConfig:
    topic: str = ""
    server: str = DEFAULT_NTFY_SERVER


@dataclass
class Config:
    default_term: str | None = None
    poll_interval_seconds: int = DEFAULT_POLL_INTERVAL_SECONDS
    notify_channels: list[str] = field(default_factory=lambda: ["desktop"])
    ntfy: NtfyConfig = field(default_factory=NtfyConfig)


def load_config() -> Config:
    path = config_path()
    if not path.exists():
        return Config()

    with path.open("rb") as f:
        raw = tomllib.load(f)

    notify = raw.get("notify", {})
    ntfy_raw = notify.get("ntfy", {})
    return Config(
        default_term=raw.get("default_term"),
        poll_interval_seconds=raw.get(
            "poll_interval_seconds", DEFAULT_POLL_INTERVAL_SECONDS
        ),
        notify_channels=notify.get("channels", ["desktop"]),
        ntfy=NtfyConfig(
            topic=ntfy_raw.get("topic", ""),
            server=ntfy_raw.get("server", DEFAULT_NTFY_SERVER),
        ),
    )


def save_config(cfg: Config) -> None:
    config_dir().mkdir(parents=True, exist_ok=True)

    lines: list[str] = []
    if cfg.default_term:
        lines.append(f'default_term = "{cfg.default_term}"')
    lines.append(f"poll_interval_seconds = {cfg.poll_interval_seconds}")
    lines.append("")
    lines.append("[notify]")
    channels = ", ".join(f'"{c}"' for c in cfg.notify_channels)
    lines.append(f"channels = [{channels}]")
    lines.append("")
    lines.append("[notify.ntfy]")
    lines.append(f'topic = "{cfg.ntfy.topic}"')
    lines.append(f'server = "{cfg.ntfy.server}"')
    lines.append("")

    config_path().write_text("\n".join(lines))
