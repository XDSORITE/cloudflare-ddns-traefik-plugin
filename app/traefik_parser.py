from __future__ import annotations

import logging
import re
from pathlib import Path

import yaml

HOST_CALL_PATTERN = re.compile(r"(HostSNI|Host)\(([^)]*)\)")
BACKTICK_VALUE_PATTERN = re.compile(r"`([^`]+)`")
FQDN_PATTERN = re.compile(
    r"^(?=.{1,253}$)(?!-)[a-z0-9-]{1,63}(?<!-)(\.(?!-)[a-z0-9-]{1,63}(?<!-))+$"
)


def list_yaml_files(source: Path) -> list[Path]:
    if source.is_file():
        return [source]
    matches = [path for path in source.rglob("*") if path.is_file() and path.suffix.lower() in {".yml", ".yaml"}]
    return sorted(matches)


def _extract_domains_from_rule(rule: str, logger: logging.Logger, context: str) -> set[str]:
    domains: set[str] = set()

    if "HostRegexp(" in rule:
        logger.warning("Skipping unsupported HostRegexp rule in %s: %s", context, rule)
        return domains

    for match in HOST_CALL_PATTERN.finditer(rule):
        raw_args = match.group(2)
        quoted_values = BACKTICK_VALUE_PATTERN.findall(raw_args)
        if not quoted_values:
            logger.warning("Skipping malformed %s rule in %s: %s", match.group(1), context, rule)
            continue
        for candidate in quoted_values:
            host = candidate.strip().lower()
            if "*" in host:
                logger.warning("Skipping wildcard host in %s: %s", context, candidate)
                continue
            if not FQDN_PATTERN.match(host):
                logger.warning("Skipping non-literal host in %s: %s", context, candidate)
                continue
            domains.add(host)

    if not domains and ("Host(" in rule or "HostSNI(" in rule):
        logger.warning("No literal domains extracted from %s: %s", context, rule)
    return domains


def _extract_from_routers(routers: dict, logger: logging.Logger, prefix: str) -> set[str]:
    domains: set[str] = set()
    for router_name, router_data in routers.items():
        if not isinstance(router_data, dict):
            continue
        rule = router_data.get("rule")
        if not isinstance(rule, str):
            continue
        context = f"{prefix}.{router_name}"
        domains.update(_extract_domains_from_rule(rule, logger, context))
    return domains


def extract_domains_from_source(source: Path, logger: logging.Logger | None = None) -> set[str]:
    logger = logger or logging.getLogger(__name__)
    domains: set[str] = set()

    for file_path in list_yaml_files(source):
        try:
            with file_path.open("r", encoding="utf-8") as handle:
                docs = list(yaml.safe_load_all(handle))
        except yaml.YAMLError as exc:
            logger.warning("Failed parsing YAML in %s: %s", file_path, exc)
            continue
        except OSError as exc:
            logger.warning("Failed reading %s: %s", file_path, exc)
            continue

        for doc in docs:
            if not isinstance(doc, dict):
                continue
            http_section = doc.get("http")
            if isinstance(http_section, dict):
                routers = http_section.get("routers")
                if isinstance(routers, dict):
                    domains.update(_extract_from_routers(routers, logger, f"{file_path}:http.routers"))

            tcp_section = doc.get("tcp")
            if isinstance(tcp_section, dict):
                routers = tcp_section.get("routers")
                if isinstance(routers, dict):
                    domains.update(_extract_from_routers(routers, logger, f"{file_path}:tcp.routers"))

    return set(sorted(domains))

