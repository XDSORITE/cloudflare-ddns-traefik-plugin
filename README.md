# Cloudflare DDNS Traefik Plugin

Sync Cloudflare DNS A records from Traefik HTTP router hosts.

## Basic warnings
- Supports **HTTP routers only**.
- Tested with **Traefik v3**.
- `.traefik.yml` is required for **plugin mode**.
- Docker mode reads mounted config files as read-only (`:ro`) and never edits local files.

## Run options
- Plugin mode quickstart: `docs/plugin-mode.md`
- Docker/Compose quickstart: `docs/docker-mode.md`

## Docker image
- `ghcr.io/xdsorite/cloudflare-ddns-traefik-plugin:latest`
- `ghcr.io/xdsorite/cloudflare-ddns-traefik-plugin:v1.1`

## Build/test
```bash
go test ./...
```
