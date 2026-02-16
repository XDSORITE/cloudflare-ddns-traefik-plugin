from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import Any

from app.cloudflare_client import MANAGED_MARKER, CloudflareClient


@dataclass(frozen=True)
class SyncSummary:
    creates: int = 0
    updates: int = 0
    noops: int = 0
    deletes: int = 0
    skipped: int = 0


def _compose_comment(existing_comment: str | None) -> str:
    current = (existing_comment or "").strip()
    if not current:
        return MANAGED_MARKER
    if MANAGED_MARKER in current:
        return current
    return f"{current} | {MANAGED_MARKER}"


def _pick_record(records: list[dict[str, Any]]) -> dict[str, Any]:
    return sorted(records, key=lambda record: str(record.get("id", "")))[0]


def sync_domains(
    domains: set[str],
    public_ip: str,
    default_proxied: bool,
    dry_run: bool,
    cleanup_stale: bool,
    cloudflare: CloudflareClient,
    logger: logging.Logger | None = None,
) -> SyncSummary:
    logger = logger or logging.getLogger(__name__)
    zones = cloudflare.list_zones()
    creates = updates = noops = deletes = skipped = 0

    domain_to_zone: dict[str, tuple[str, str]] = {}
    for domain in sorted(domains):
        match = cloudflare.find_best_zone_match(domain, zones)
        if not match:
            logger.warning("Skipping %s: no matching Cloudflare zone.", domain)
            skipped += 1
            continue
        domain_to_zone[domain] = (match.zone_id, match.zone_name)

    for domain in sorted(domain_to_zone):
        zone_id, _zone_name = domain_to_zone[domain]
        records = [record for record in cloudflare.list_a_records(zone_id=zone_id, name=domain) if record.get("name") == domain]
        if not records:
            creates += 1
            logger.info("Create A record %s -> %s", domain, public_ip)
            if not dry_run:
                cloudflare.create_a_record(
                    zone_id=zone_id,
                    name=domain,
                    ip=public_ip,
                    proxied=default_proxied,
                    comment=MANAGED_MARKER,
                )
            continue

        if len(records) > 1:
            logger.warning("Multiple A records found for %s, using deterministic first record.", domain)
        target = _pick_record(records)
        current_ip = str(target.get("content", ""))
        proxied = bool(target.get("proxied", default_proxied))
        comment = _compose_comment(target.get("comment"))

        if current_ip == public_ip and comment == (target.get("comment") or "").strip():
            noops += 1
            logger.info("No change for %s (%s)", domain, public_ip)
            continue

        updates += 1
        logger.info("Update A record %s: %s -> %s", domain, current_ip, public_ip)
        if not dry_run:
            cloudflare.update_a_record(
                zone_id=zone_id,
                record_id=str(target["id"]),
                name=domain,
                ip=public_ip,
                proxied=proxied,
                comment=comment,
            )

    if cleanup_stale:
        desired = set(domains)
        for zone in zones:
            zone_id = str(zone.get("id", ""))
            if not zone_id:
                continue
            a_records = cloudflare.list_a_records(zone_id=zone_id)
            for record in a_records:
                name = str(record.get("name", "")).lower()
                comment = str(record.get("comment", ""))
                if MANAGED_MARKER not in comment:
                    continue
                if name in desired:
                    continue
                deletes += 1
                logger.info("Delete stale managed A record %s", name)
                if not dry_run:
                    cloudflare.delete_record(zone_id=zone_id, record_id=str(record["id"]))

    return SyncSummary(creates=creates, updates=updates, noops=noops, deletes=deletes, skipped=skipped)

