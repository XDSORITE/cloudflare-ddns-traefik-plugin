# Docker/Compose Mode Quickstart

Use this mode if you want a standalone DDNS container (no Traefik plugin loading required).

## Behavior
- Reads Traefik config files from mounted path.
- Parses `http.routers.*.rule` with `Host(...)`.
- Updates Cloudflare A records only.
- Preserves existing Cloudflare proxy setting on updates.
- Safe for restarts (`restart: unless-stopped`).
- First sync runs immediately on startup.
- Retries and request timeouts are built in.

## Required files
- `Dockerfile.sync`
- `docker-compose.sync.yml`

## Directory layout example
```text
project/
  docker-compose.sync.yml
  traefik-dynamic-configs/
    http.yml
    other.yml
```

## Run with compose
1. Set real values in `docker-compose.sync.yml`:
   - `CF_API_TOKEN`
   - optional `CF_ZONE`
2. Start:
```bash
docker compose -f docker-compose.sync.yml up -d --build
```
3. Logs:
```bash
docker compose -f docker-compose.sync.yml logs -f ddns-traefik-sync
```
4. Stop:
```bash
docker compose -f docker-compose.sync.yml down
```

## Read-only mount requirement
Keep config mount read-only:
```yaml
volumes:
  - ./traefik-dynamic-configs:/configs:ro
```
The container does not edit mounted files.

## Pull prebuilt image
```bash
docker pull ghcr.io/xdsorite/cloudflare-ddns-traefik-plugin:latest
docker pull ghcr.io/xdsorite/cloudflare-ddns-traefik-plugin:main
```
