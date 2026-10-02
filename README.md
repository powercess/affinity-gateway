# Affinity Gateway

Independent AI request gateway with soft session affinity, configurable inbound and egress routes, and Lua request transformation plugins.

## Workspace

- `apps/console`: React + Vite + Tailwind CSS v4 + shadcn/ui console.
- `apps/gateway`: Go gateway runtime. All traffic shares `:8236`.
- `packages/protocol`: first supported OpenAI Chat Completions contract.
- `docs`: architecture and runtime notes.

## URL model

- `POST /v1/chat/completions` — inbound client traffic.
- `POST /egress/{route-id}/v1/chat/completions` — egress provider traffic.
- `/api/v1/*` — control API.
- `GET /healthz` — process health.

## Local development

```bash
bun install
bun run dev
bun run build
bun run gateway:dev
bun run gateway:test
```

The console proxies `/api` to `http://127.0.0.1:8236` during development. Set
`AFFINITY_GATEWAY` to point the proxy at another gateway address,
`AFFINITY_ALLOWED_HOSTS` (comma separated, or `*`) to allow tunnel hosts, and
`AFFINITY_CONFIG_FILE` / `AFFINITY_LISTEN` to run the gateway elsewhere.

