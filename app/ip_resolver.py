from __future__ import annotations

import ipaddress
import logging
from typing import Iterable

import requests

DEFAULT_IP_SOURCES = [
    "https://api.ipify.org",
    "https://ifconfig.me/ip",
    "https://checkip.amazonaws.com",
]


def resolve_public_ipv4(
    sources: Iterable[str],
    timeout_seconds: int,
    logger: logging.Logger | None = None,
    session: requests.Session | None = None,
) -> str:
    logger = logger or logging.getLogger(__name__)
    session = session or requests.Session()
    errors: list[str] = []

    for source in sources:
        try:
            response = session.get(source, timeout=timeout_seconds)
            response.raise_for_status()
            value = response.text.strip()
            ipaddress.IPv4Address(value)
            return value
        except Exception as exc:  # noqa: BLE001
            message = f"{source}: {exc}"
            errors.append(message)
            logger.warning("Failed resolving IP from %s: %s", source, exc)
            continue

    raise RuntimeError(f"Unable to resolve public IPv4 from configured sources: {'; '.join(errors)}")

