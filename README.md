# Cloudflare DDNS Traefik Plugin

## Quick answer: what is `.traefik.yml`?
It is the Traefik plugin manifest.  
Traefik uses it to identify the plugin (`displayName`, `type`, `import`) and validate basic test data.

## Support
- Router type support: **HTTP routers only**
- Tested with: **Traefik v3**

## Mode 1: Traefik plugin mode

### 1) Put plugin source in Traefik local plugin folder
Use:

`<traefik-root>/plugins-local/src/ddns-traefik-plugin`

### 2) Enable plugin in static `traefik.yml`
```yaml
experimental:
  localPlugins:
    ddns-traefik-plugin:
      moduleName: ddns-traefik-plugin
```

### 3) Configure middleware in dynamic config
```yaml
http:
  middlewares:
    ddns-sync:
      plugin:
        ddns-traefik-plugin:
          enabled: true
          apiToken: "CLOUDFLARE_API_TOKEN_VALUE"
          zone: "example.com"
          syncIntervalSeconds: 300
          requestTimeoutSeconds: 10
          autoDiscoverHost: true
          routerRule: "Host(`app.example.com`)"
          domains:
            - "api.example.com"
          domainsCsv: "app2.example.com,app3.example.com"
          defaultProxied: false
          ipSources:
            - "https://api.ipify.org"
            - "https://ifconfig.me/ip"
            - "https://checkip.amazonaws.com"
```

## Mode 2: Docker container mode
Use this if you want a standalone sync container that reads Traefik config files.

### Key behavior
- Reads config files only
- **Does not modify mounted files**
- Intended mount mode: **read-only**
- Safe to restart repeatedly (`restart: unless-stopped`, stateless start)
- Fast startup: performs first sync immediately, then interval loop

### Run with Docker Compose
Use `docker-compose.sync.yml`:
```yaml
services:
  ddns-traefik-sync:
    build:
      context: .
      dockerfile: Dockerfile.sync
    restart: unless-stopped
    environment:
      CF_API_TOKEN: "YOUR_CLOUDFLARE_API_TOKEN"
      CF_ZONE: "example.com"
      TRAEFIK_SOURCE: "/configs"
      SYNC_INTERVAL_SECONDS: "300"
      REQUEST_TIMEOUT_SECONDS: "10"
      DEFAULT_PROXIED: "false"
      IP_SOURCES: "https://api.ipify.org,https://ifconfig.me/ip,https://checkip.amazonaws.com"
    volumes:
      - ./traefik-dynamic-configs:/configs:ro
```

Container mode parses `http.routers.*.rule` with `Host(...)` from mounted `.yml/.yaml` files.

## Cloudflare token permissions
- `Zone:Read`
- `DNS:Read`
- `DNS:Edit`

## Local test
```bash
go test ./...
```
