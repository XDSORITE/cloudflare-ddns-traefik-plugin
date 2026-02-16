from __future__ import annotations

import argparse
import os
from dataclasses import dataclass
from pathlib import Path

from app.ip_resolver import DEFAULT_IP_SOURCES


def _parse_bool(value: str | bool | None, default: bool) -> bool:
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    normalized = value.strip().lower()
    if normalized in {"1", "true", "yes", "on"}:
        return True
    if normalized in {"0", "false", "no", "off"}:
        return False
    raise ValueError(f"Invalid boolean value: {value}")


@dataclass(frozen=True)
class AppConfig:
    source: Path
    interval_seconds: int
    once: bool
    dry_run: bool
    cleanup_stale: bool
    cloudflare_api_token: str
    ip_sources: list[str]
    default_proxied: bool
    log_level: str
    request_timeout_seconds: int


def load_config(argv: list[str] | None = None) -> AppConfig:
    parser = argparse.ArgumentParser(description="Sync Traefik domains to Cloudflare A records.")
    parser.add_argument("--source", help="Path to a Traefik dynamic config file or a directory of configs.")
    parser.add_argument("--interval", type=int, help="Sync interval in seconds (default from env or 300).")
    parser.add_argument("--once", action="store_true", help="Run one sync cycle and exit.")
    parser.add_argument("--dry-run", action="store_true", help="Print actions without mutating DNS.")
    parser.add_argument(
        "--cleanup-stale",
        action="store_true",
        help="Delete stale managed records no longer present in Traefik domains.",
    )

    args = parser.parse_args(argv)

    source_raw = args.source or os.getenv("TRAEFIK_SOURCE")
    if not source_raw:
        raise ValueError("Missing source path. Set --source or TRAEFIK_SOURCE.")
    source = Path(source_raw)
    if not source.exists():
        raise ValueError(f"Source path does not exist: {source}")
    if not source.is_file() and not source.is_dir():
        raise ValueError(f"Source path must be a file or directory: {source}")

    token = os.getenv("CLOUDFLARE_API_TOKEN")
    if not token:
        raise ValueError("Missing CLOUDFLARE_API_TOKEN.")

    interval = args.interval if args.interval is not None else int(os.getenv("SYNC_INTERVAL_SECONDS", "300"))
    if interval <= 0:
        raise ValueError(f"SYNC_INTERVAL_SECONDS/--interval must be > 0, got {interval}")

    timeout = int(os.getenv("REQUEST_TIMEOUT_SECONDS", "10"))
    if timeout <= 0:
        raise ValueError(f"REQUEST_TIMEOUT_SECONDS must be > 0, got {timeout}")

    ip_sources_raw = os.getenv("IP_SOURCES", "")
    if ip_sources_raw.strip():
        ip_sources = [entry.strip() for entry in ip_sources_raw.split(",") if entry.strip()]
    else:
        ip_sources = DEFAULT_IP_SOURCES.copy()

    default_proxied = _parse_bool(os.getenv("DEFAULT_PROXIED"), default=False)
    log_level = os.getenv("LOG_LEVEL", "INFO")

    return AppConfig(
        source=source,
        interval_seconds=interval,
        once=args.once,
        dry_run=args.dry_run,
        cleanup_stale=args.cleanup_stale,
        cloudflare_api_token=token,
        ip_sources=ip_sources,
        default_proxied=default_proxied,
        log_level=log_level,
        request_timeout_seconds=timeout,
    )

