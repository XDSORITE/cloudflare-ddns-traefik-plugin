from app.cloudflare_client import CloudflareClient


def test_find_best_zone_match_uses_longest_suffix() -> None:
    zones = [
        {"id": "1", "name": "example.com"},
        {"id": "2", "name": "sub.example.com"},
    ]
    match = CloudflareClient.find_best_zone_match("a.sub.example.com", zones)
    assert match is not None
    assert match.zone_id == "2"
    assert match.zone_name == "sub.example.com"


def test_find_best_zone_match_returns_none_when_no_suffix() -> None:
    zones = [{"id": "1", "name": "example.com"}]
    match = CloudflareClient.find_best_zone_match("a.other.net", zones)
    assert match is None

