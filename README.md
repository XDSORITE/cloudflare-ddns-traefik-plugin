# ddns-traefik-plugin

Traefik middleware plugin that syncs Cloudflare DNS A records for router domains to your current public IPv4.

## Behavior

- Per-router enable/disable: apply middleware on routers you want managed, do not apply on routers you want ignored.
- Auto domain discovery for HTTP: captures `Host` seen on requests.
- Optional fixed `domains` list: useful for pre-seeding or non-request-driven domains.
- Sync cadence default: every `300` seconds (5 minutes).
- Only A record IP is changed on update.
- Existing Cloudflare proxy mode (`proxied`) is preserved on updates.
- New records use `defaultProxied` only at creation time.

## Important scope

- Current implementation is an HTTP middleware plugin.
- TCP `HostSNI` rules are not auto-discovered by this plugin API path.
- For TCP domains, use explicit `domains` in a middleware instance as manual input.

## Static Traefik plugin registration (`traefik.yml`)

```yaml
experimental:
  localPlugins:
    ddns-traefik-plugin:
      moduleName: github.com/xdsorite/ddns-traefik-plugin
```

## Dynamic middleware and router config (file provider)

```yaml
http:
  middlewares:
    ddns-enabled:
      plugin:
        ddns-traefik-plugin:
          enabled: true
          apiTokenEnv: CLOUDFLARE_API_TOKEN
          syncIntervalSeconds: 300
          requestTimeoutSeconds: 10
          autoDiscoverHost: true
          defaultProxied: false
          domains: []
          ipSources:
            - https://api.ipify.org
            - https://ifconfig.me/ip
            - https://checkip.amazonaws.com
          managedComment: managed-by=traefik-plugin-ddns

  routers:
    app1:
      rule: Host(`app1.example.com`)
      middlewares:
        - ddns-enabled
      service: app1-svc

    app2:
      rule: Host(`app2.example.com`)
      # no middleware => DDNS disabled for this router
      service: app2-svc
```

## Plugin options

- `enabled`: enable/disable sync for this middleware instance (default `true`).
- `apiToken`: Cloudflare token inline (optional).
- `apiTokenEnv`: env var containing Cloudflare token (default `CLOUDFLARE_API_TOKEN`).
- `syncIntervalSeconds`: sync period (default `300`).
- `requestTimeoutSeconds`: timeout for Cloudflare/IP HTTP calls (default `10`).
- `autoDiscoverHost`: learn domain from incoming HTTP host (default `true`).
- `domains`: explicit domain list to manage in addition to discovered hosts.
- `defaultProxied`: value used when creating a new A record (default `false`).
- `ipSources`: public IP discovery endpoints in order.
- `managedComment`: comment marker for created records.

## Cloudflare token permissions

Use a token with:
- `Zone:Read`
- `DNS:Read`
- `DNS:Edit`

## Development

```bash
go test ./...
```
