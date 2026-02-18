from pathlib import Path

from app.config import AppConfig
from app.main import run_sync_cycle


class FakeCloudflare:
    def __init__(self) -> None:
        self.synced = False
        self._zones = [{"id": "z1", "name": "example.com"}]
        self._records = {}

    def list_zones(self):
        return self._zones

    def find_best_zone_match(self, domain, zones):
        return type("Match", (), {"zone_id": "z1", "zone_name": "example.com"})()

    def list_a_records(self, zone_id, name=None):
        if name:
            return self._records.get(name, [])
        return []

    def create_a_record(self, zone_id, name, ip, proxied, comment):
        self.synced = True
        self._records[name] = [{"id": f"id-{name}", "name": name, "content": ip, "proxied": proxied, "comment": comment}]
        return self._records[name][0]

    def update_a_record(self, zone_id, record_id, name, ip, proxied, comment):
        self.synced = True
        self._records[name] = [{"id": record_id, "name": name, "content": ip, "proxied": proxied, "comment": comment}]
        return self._records[name][0]

    def delete_record(self, zone_id, record_id):
        self.synced = True


def test_run_sync_cycle_end_to_end(monkeypatch, tmp_path: Path) -> None:
    config_file = tmp_path / "http.yml"
    config_file.write_text(
        """
http:
  routers:
    metube:
      rule: Host(`metube.example.com`)
""",
        encoding="utf-8",
    )
    cfg = AppConfig(
        source=config_file,
        interval_seconds=300,
        once=True,
        dry_run=False,
        cleanup_stale=False,
        cloudflare_api_token="x",
        ip_sources=["https://api.ipify.org"],
        default_proxied=False,
        log_level="INFO",
        request_timeout_seconds=10,
    )
    cf = FakeCloudflare()

    monkeypatch.setattr("app.main.resolve_public_ipv4", lambda **kwargs: "203.0.113.10")
    run_sync_cycle(config=cfg, logger=__import__("logging").getLogger("test"), cloudflare=cf)  # type: ignore[arg-type]
    assert cf.synced is True

