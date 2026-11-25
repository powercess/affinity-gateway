# Affinity Gateway

Independent AI request gateway with soft session affinity, configurable inbound and egress routes, global and per-route outbound proxies, and Lua request transformation plugins.

## Workspace

- `apps/console`: React + Vite + Tailwind CSS v4 + shadcn/ui console.
- `apps/gateway`: Go gateway runtime; inbound `:8236`, egress `:8237`, console/API `:8238`.
- `packages/protocol`: first supported OpenAI Chat Completions contract.
- `docs`: architecture and runtime notes.

## URL model

- `POST /v1/chat/completions` — inbound client traffic.
- `POST /egress/{route-id}/v1/chat/completions` — egress provider traffic.
- `/api/v1/*` — control API.
- `GET /healthz` — process health.

## Outbound proxies

In **Settings**, choose the global outbound policy: environment variables (the
backward-compatible default), direct connections, or a custom proxy. Each
**egress route** can inherit that policy, connect directly, or use a different
proxy. HTTP, HTTPS, SOCKS5, and SOCKS5H URLs are supported, including URL-encoded
username/password authentication.

Settings are saved through the authenticated console API and take effect for
new requests without restarting the gateway. Existing configurations without
proxy fields retain their environment-variable behavior. The global outbound
policy does not change the inbound connection to new-api.

For channels whose base URL is `http://affinity-gateway:8237/egress/...`, leave
new-api's channel proxy empty and configure the proxy on the gateway's egress
route instead. See [proxy precedence and API examples](docs/architecture.md#outbound-proxy-policy).

## Local development

```bash
bun install
bun run dev
bun run build
bun run gateway:dev
bun run gateway:test
```

For the three-listener gateway, run the console with
`AFFINITY_GATEWAY=http://127.0.0.1:8238` so `/api` reaches the control port.
Set `AFFINITY_ALLOWED_HOSTS` (comma separated, or `*`) to allow tunnel hosts,
and `AFFINITY_CONFIG_FILE` / `AFFINITY_LISTEN` to run the gateway elsewhere.
`AFFINITY_EGRESS_RESPONSE_HEADER_TIMEOUT` (Go duration, default `120s`, `0`
disables) bounds how long an egress request waits for upstream response headers.

