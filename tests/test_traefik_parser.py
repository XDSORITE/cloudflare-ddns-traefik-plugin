from pathlib import Path

from app.traefik_parser import extract_domains_from_source, list_yaml_files


def test_extracts_http_and_tcp_domains(tmp_path: Path) -> None:
    config = tmp_path / "config.yml"
    config.write_text(
        """
http:
  routers:
    r1:
      rule: Host(`one.example.com`,`two.example.com`)
tcp:
  routers:
    r2:
      rule: HostSNI(`three.example.com`)
""",
        encoding="utf-8",
    )
    domains = extract_domains_from_source(config)
    assert domains == {"one.example.com", "two.example.com", "three.example.com"}


def test_skips_wildcard_and_hostregexp(tmp_path: Path) -> None:
    config = tmp_path / "config.yml"
    config.write_text(
        """
http:
  routers:
    r1:
      rule: Host(`*.example.com`)
    r2:
      rule: HostRegexp(`{subdomain:[a-z]+}.example.com`)
    r3:
      rule: Host(`good.example.com`)
""",
        encoding="utf-8",
    )
    domains = extract_domains_from_source(config)
    assert domains == {"good.example.com"}


def test_recursive_yaml_discovery(tmp_path: Path) -> None:
    nested = tmp_path / "a" / "b"
    nested.mkdir(parents=True)
    (tmp_path / "root.yml").write_text("http: {}", encoding="utf-8")
    (nested / "nested.yaml").write_text("http: {}", encoding="utf-8")
    (nested / "ignore.txt").write_text("x", encoding="utf-8")

    files = list_yaml_files(tmp_path)
    assert [f.name for f in files] == ["nested.yaml", "root.yml"]

