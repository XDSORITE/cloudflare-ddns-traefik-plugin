# Plugin Mode Quickstart

Use this mode when you want Traefik to run DDNS as a middleware plugin.

Supported scope: HTTP routers with `Host(...)` rules.

## Why `.traefik.yml` is needed
Traefik reads `.traefik.yml` as plugin metadata (name/type/import/test data).  
Without it, plugin mode will not load.

## 1) Place plugin source
Copy this repo into:

`<traefik-root>/plugins-local/src/ddns-traefik-plugin`

## 2) Enable local plugin in static `traefik.yml`
```yaml
experimental:
  localPlugins:
    ddns-traefik-plugin:
      moduleName: ddns-traefik-plugin
```

## 3) Configure middleware in dynamic config
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

## 4) Attach middleware to your router
```yaml
http:
  routers:
    app:
      rule: Host(`app.example.com`)
      middlewares:
        - ddns-sync
      service: app-svc
```

## 5) Restart Traefik and check logs
- Restart Traefik after config changes.
- Confirm plugin loads and sync cycles appear in logs.

## Troubleshooting
- Plugin not loading:
  - confirm `moduleName: ddns-traefik-plugin`
  - confirm repo exists under `plugins-local/src/ddns-traefik-plugin`
  - confirm `.traefik.yml` exists at repo root
- Token missing:
  - set `apiToken` in middleware config
- No hosts parsed:
  - this project reads HTTP `Host(...)` rules only
  - confirm your rule contains literal hosts
