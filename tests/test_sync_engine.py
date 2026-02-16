from typing import Any

from app.cloudflare_client import MANAGED_MARKER
from app.sync_engine import sync_domains


class FakeCloudflare:
    def __init__(self) -> None:
        self.created: list[tuple[str, str, str, bool, str]] = []
        self.updated: list[tuple[str, str, str, str, bool, str]] = []
        self.deleted: list[tuple[str, str]] = []
        self._zones = [{"id": "zone1", "name": "example.com"}]
        self._records_by_name: dict[str, list[dict[str, Any]]] = {
            "exists.example.com": [
                {"id": "r1", "name": "exists.example.com", "content": "198.51.100.10", "proxied": True, "comment": ""}
            ],
            "same.example.com": [
                {
                    "id": "r2",
                    "name": "same.example.com",
                    "content": "203.0.113.10",
                    "proxied": False,
                    "comment": MANAGED_MARKER,
                }
            ],
        }
        self._all_zone_records = [
            {"id": "r3", "name": "stale.example.com", "comment": MANAGED_MARKER},
            {"id": "r4", "name": "other.example.com", "comment": ""},
        ]

    def list_zones(self) -> list[dict[str, Any]]:
        return self._zones

    def find_best_zone_match(self, domain: str, zones: list[dict[str, Any]]) -> Any:
        for zone in zones:
            if domain.endswith(zone["name"]):
                return type("Match", (), {"zone_id": zone["id"], "zone_name": zone["name"]})()
        return None

    def list_a_records(self, zone_id: str, name: str | None = None) -> list[dict[str, Any]]:
        if name:
            return self._records_by_name.get(name, [])
        return self._all_zone_records

    def create_a_record(self, zone_id: str, name: str, ip: str, proxied: bool, comment: str) -> dict[str, Any]:
        self.created.append((zone_id, name, ip, proxied, comment))
        return {"id": "new"}

    def update_a_record(
        self, zone_id: str, record_id: str, name: str, ip: str, proxied: bool, comment: str
    ) -> dict[str, Any]:
        self.updated.append((zone_id, record_id, name, ip, proxied, comment))
        return {"id": record_id}

    def delete_record(self, zone_id: str, record_id: str) -> None:
        self.deleted.append((zone_id, record_id))


def test_sync_create_update_noop_and_cleanup() -> None:
    cf = FakeCloudflare()
    summary = sync_domains(
        domains={"new.example.com", "exists.example.com", "same.example.com"},
        public_ip="203.0.113.10",
        default_proxied=False,
        dry_run=False,
        cleanup_stale=True,
        cloudflare=cf,  # type: ignore[arg-type]
    )
    assert summary.creates == 1
    assert summary.updates == 1
    assert summary.noops == 1
    assert summary.deletes == 1
    assert cf.created[0][1] == "new.example.com"
    assert cf.updated[0][2] == "exists.example.com"
    assert cf.updated[0][4] is True
    assert MANAGED_MARKER in cf.updated[0][5]
    assert cf.deleted[0][1] == "r3"


def test_sync_dry_run_does_not_mutate() -> None:
    cf = FakeCloudflare()
    summary = sync_domains(
        domains={"new.example.com"},
        public_ip="203.0.113.10",
        default_proxied=False,
        dry_run=True,
        cleanup_stale=True,
        cloudflare=cf,  # type: ignore[arg-type]
    )
    assert summary.creates == 1
    assert cf.created == []
    assert cf.updated == []
    assert cf.deleted == []

