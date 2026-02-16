from __future__ import annotations

import logging
import time
from dataclasses import dataclass
from typing import Any

import requests

MANAGED_MARKER = "managed-by=ddns-traefik-sync"


class CloudflareAPIError(RuntimeError):
    pass


@dataclass(frozen=True)
class ZoneMatch:
    zone_id: str
    zone_name: str


class CloudflareClient:
    def __init__(
        self,
        api_token: str,
        timeout_seconds: int,
        logger: logging.Logger | None = None,
        session: requests.Session | None = None,
    ) -> None:
        self._base_url = "https://api.cloudflare.com/client/v4"
        self._timeout_seconds = timeout_seconds
        self._logger = logger or logging.getLogger(__name__)
        self._session = session or requests.Session()
        self._headers = {
            "Authorization": f"Bearer {api_token}",
            "Content-Type": "application/json",
        }

    def _request(
        self,
        method: str,
        path: str,
        params: dict[str, Any] | None = None,
        payload: dict[str, Any] | None = None,
        retries: int = 3,
    ) -> dict[str, Any]:
        url = f"{self._base_url}{path}"
        last_error: Exception | None = None
        for attempt in range(1, retries + 1):
            try:
                response = self._session.request(
                    method=method,
                    url=url,
                    params=params,
                    json=payload,
                    headers=self._headers,
                    timeout=self._timeout_seconds,
                )
                if response.status_code == 429 or 500 <= response.status_code < 600:
                    raise CloudflareAPIError(f"Retryable Cloudflare status {response.status_code}: {response.text}")
                response.raise_for_status()
                data = response.json()
                if not data.get("success", False):
                    errors = data.get("errors", [])
                    raise CloudflareAPIError(f"Cloudflare API error for {method} {path}: {errors}")
                return data
            except (requests.RequestException, CloudflareAPIError) as exc:
                last_error = exc
                if attempt >= retries:
                    break
                sleep_seconds = attempt
                self._logger.warning(
                    "Cloudflare request failed attempt %d/%d for %s %s: %s. Retrying in %ss.",
                    attempt,
                    retries,
                    method,
                    path,
                    exc,
                    sleep_seconds,
                )
                time.sleep(sleep_seconds)
        raise CloudflareAPIError(f"Cloudflare request failed for {method} {path}: {last_error}")

    def list_zones(self) -> list[dict[str, Any]]:
        zones: list[dict[str, Any]] = []
        page = 1
        while True:
            data = self._request("GET", "/zones", params={"page": page, "per_page": 50})
            zones.extend(data.get("result", []))
            info = data.get("result_info", {})
            total_pages = info.get("total_pages", 1)
            if page >= total_pages:
                break
            page += 1
        return zones

    @staticmethod
    def find_best_zone_match(domain: str, zones: list[dict[str, Any]]) -> ZoneMatch | None:
        best: tuple[int, ZoneMatch] | None = None
        for zone in zones:
            zone_name = str(zone.get("name", "")).lower()
            zone_id = str(zone.get("id", ""))
            if not zone_name or not zone_id:
                continue
            if domain == zone_name or domain.endswith(f".{zone_name}"):
                score = len(zone_name)
                match = ZoneMatch(zone_id=zone_id, zone_name=zone_name)
                if best is None or score > best[0]:
                    best = (score, match)
        return best[1] if best else None

    def list_a_records(self, zone_id: str, name: str | None = None) -> list[dict[str, Any]]:
        page = 1
        records: list[dict[str, Any]] = []
        while True:
            params: dict[str, Any] = {"type": "A", "page": page, "per_page": 100}
            if name:
                params["name"] = name
            data = self._request("GET", f"/zones/{zone_id}/dns_records", params=params)
            records.extend(data.get("result", []))
            info = data.get("result_info", {})
            total_pages = info.get("total_pages", 1)
            if page >= total_pages:
                break
            page += 1
        return records

    def create_a_record(self, zone_id: str, name: str, ip: str, proxied: bool, comment: str) -> dict[str, Any]:
        payload = {
            "type": "A",
            "name": name,
            "content": ip,
            "ttl": 1,
            "proxied": proxied,
            "comment": comment,
        }
        data = self._request("POST", f"/zones/{zone_id}/dns_records", payload=payload)
        return data["result"]

    def update_a_record(
        self,
        zone_id: str,
        record_id: str,
        name: str,
        ip: str,
        proxied: bool,
        comment: str,
    ) -> dict[str, Any]:
        payload = {
            "type": "A",
            "name": name,
            "content": ip,
            "ttl": 1,
            "proxied": proxied,
            "comment": comment,
        }
        data = self._request("PUT", f"/zones/{zone_id}/dns_records/{record_id}", payload=payload)
        return data["result"]

    def delete_record(self, zone_id: str, record_id: str) -> None:
        self._request("DELETE", f"/zones/{zone_id}/dns_records/{record_id}")

