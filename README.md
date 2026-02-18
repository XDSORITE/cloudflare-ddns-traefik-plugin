# Cloudflare DDNS Traefik Plugin

Traefik middleware plugin that keeps Cloudflare **A** records in sync with your server public IPv4.

## How it works
- One global worker runs every 5 minutes (`syncIntervalSeconds: 300` by default).
- Middleware is passive and does not block traffic.
- Worker compares current public IP to Cloudflare A records and updates only when needed.
- Existing Cloudflare proxy mode (`proxied`) is preserved on updates.

## 1. Enable plugin in Traefik static config

```yaml
experimental:
  localPlugins:
    ddns-traefik-plugin:
      moduleName: github.com/xdsorite/ddns-traefik-plugin
```

Mount this repo to:

`/plugins-local/src/github.com/xdsorite/ddns-traefik-plugin`

## 2. Configure middleware in Traefik dynamic config

All plugin configuration is in Traefik config (no external env/config files needed):

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
          defaultProxied: false
          ipSources:
            - "https://api.ipify.org"
            - "https://ifconfig.me/ip"
            - "https://checkip.amazonaws.com"
```

## 3. Attach middleware to router

```yaml
http:
  routers:
    app:
      rule: Host(`app.example.com`)
      middlewares:
        - ddns-sync
      service: app-svc
```

## Config fields
- `enabled`: enable/disable this middleware registration.
- `apiToken`: Cloudflare API token (required).
- `zone`: optional zone restriction.
- `syncIntervalSeconds`: sync interval (default `300`).
- `requestTimeoutSeconds`: request timeout in seconds (default `10`).
- `autoDiscoverHost`: extract hosts from `routerRule`.
- `routerRule`: router rule string (for example `Host(\`app.example.com\`)`).
- `domains`: explicit domains to manage.
- `defaultProxied`: used only when creating new records.
- `ipSources`: public IP endpoints in priority order.

## Cloudflare token permissions
- `Zone:Read`
- `DNS:Read`
- `DNS:Edit`

## Test

```bash
go test ./...
```
