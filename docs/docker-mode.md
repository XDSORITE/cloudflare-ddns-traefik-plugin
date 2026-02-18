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

## Example `docker-compose.sync.yml`
```yaml
services:
  ddns-traefik-sync:
    image: ghcr.io/xdsorite/cloudflare-ddns-traefik-plugin:latest
    container_name: ddns-traefik-sync
    restart: unless-stopped
    environment:
      CF_API_TOKEN: "YOUR_CLOUDFLARE_API_TOKEN"
      CF_ZONE: "example.com"
      TRAEFIK_SOURCE: "/configs"
      SYNC_INTERVAL_SECONDS: "300"
      REQUEST_TIMEOUT_SECONDS: "10"
      DEFAULT_PROXIED: "false"
      MANAGED_COMMENT: "managed-by=ddns-traefik-sync"
      IP_SOURCES: "https://api.ipify.org,https://ifconfig.me/ip,https://checkip.amazonaws.com"
    volumes:
      - ./traefik-dynamic-configs:/configs:ro
```

## Environment variables
- `CF_API_TOKEN` (required): Cloudflare API token.
- `CF_ZONE` (optional): restrict updates to one zone (example: `example.com`).
- `TRAEFIK_SOURCE` (optional): path inside container to parse; default `/configs`.
- `SYNC_INTERVAL_SECONDS` (optional): sync frequency in seconds; default `300`.
- `REQUEST_TIMEOUT_SECONDS` (optional): HTTP timeout in seconds; default `10`.
- `DEFAULT_PROXIED` (optional): used only when creating a new A record; default `false`.
- `MANAGED_COMMENT` (optional): comment on created records; default `managed-by=ddns-traefik-sync`.
- `IP_SOURCES` (optional): comma-separated public IP endpoints in priority order.

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
Mount only your Traefik config files as read-only:
```yaml
volumes:
  - ./traefik-dynamic-configs:/configs:ro
```

Default recommended setup in compose:
```yaml
environment:
  TRAEFIK_SOURCE: "/configs"
volumes:
  - ./traefik-dynamic-configs:/configs:ro
```
The container does not edit mounted files.

## Pull prebuilt image
```bash
docker pull ghcr.io/xdsorite/cloudflare-ddns-traefik-plugin:latest
docker pull ghcr.io/xdsorite/cloudflare-ddns-traefik-plugin:main
```
