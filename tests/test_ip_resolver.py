import requests

from app.ip_resolver import resolve_public_ipv4


class _Response:
    def __init__(self, text: str, status_code: int = 200) -> None:
        self.text = text
        self.status_code = status_code

    def raise_for_status(self) -> None:
        if self.status_code >= 400:
            raise requests.HTTPError(f"status={self.status_code}")


class _Session:
    def __init__(self) -> None:
        self.calls = 0

    def get(self, source: str, timeout: int) -> _Response:
        self.calls += 1
        if self.calls == 1:
            raise requests.RequestException("network issue")
        if self.calls == 2:
            return _Response("not-an-ip")
        return _Response("203.0.113.10\n")


def test_ip_resolver_fallbacks_across_sources() -> None:
    ip = resolve_public_ipv4(
        sources=["s1", "s2", "s3"],
        timeout_seconds=5,
        session=_Session(),  # type: ignore[arg-type]
    )
    assert ip == "203.0.113.10"

