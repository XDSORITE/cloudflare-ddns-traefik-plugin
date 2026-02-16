from __future__ import annotations

import logging
import sys
import time

from app.cloudflare_client import CloudflareAPIError, CloudflareClient
from app.config import AppConfig, load_config
from app.ip_resolver import resolve_public_ipv4
from app.logging_setup import setup_logging
from app.sync_engine import sync_domains
from app.traefik_parser import extract_domains_from_source


def run_sync_cycle(config: AppConfig, logger: logging.Logger, cloudflare: CloudflareClient) -> None:
    domains = extract_domains_from_source(config.source, logger=logger)
    if not domains:
        logger.warning("No domains discovered from source %s. Skipping DNS mutations for this cycle.", config.source)
        return

    public_ip = resolve_public_ipv4(
        sources=config.ip_sources,
        timeout_seconds=config.request_timeout_seconds,
        logger=logger,
    )
    logger.info("Resolved public IPv4: %s", public_ip)

    summary = sync_domains(
        domains=domains,
        public_ip=public_ip,
        default_proxied=config.default_proxied,
        dry_run=config.dry_run,
        cleanup_stale=config.cleanup_stale,
        cloudflare=cloudflare,
        logger=logger,
    )
    logger.info(
        "Sync cycle completed. creates=%d updates=%d noops=%d deletes=%d skipped=%d",
        summary.creates,
        summary.updates,
        summary.noops,
        summary.deletes,
        summary.skipped,
    )


def main(argv: list[str] | None = None) -> int:
    try:
        config = load_config(argv=argv)
    except Exception as exc:  # noqa: BLE001
        print(f"Configuration error: {exc}", file=sys.stderr)
        return 2

    setup_logging(config.log_level)
    logger = logging.getLogger("ddns-traefik-sync")
    logger.info(
        "Starting ddns-traefik-sync source=%s once=%s dry_run=%s cleanup_stale=%s",
        config.source,
        config.once,
        config.dry_run,
        config.cleanup_stale,
    )

    cloudflare = CloudflareClient(
        api_token=config.cloudflare_api_token,
        timeout_seconds=config.request_timeout_seconds,
        logger=logger,
    )

    while True:
        try:
            run_sync_cycle(config=config, logger=logger, cloudflare=cloudflare)
        except CloudflareAPIError as exc:
            logger.error("Cloudflare API error: %s", exc)
        except Exception as exc:  # noqa: BLE001
            logger.exception("Unexpected sync error: %s", exc)

        if config.once:
            break
        time.sleep(config.interval_seconds)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())

