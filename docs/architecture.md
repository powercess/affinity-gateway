# Architecture

The gateway uses a single HTTP listener (`:8236`) and separates traffic by path.

```text
client ──▶ /v1/*          inbound  ──▶ downstream
downstream ─▶ /egress/{id}/* egress  ──▶ provider
console  ──▶ /api/v1/*    control plane
browser  ──▶ /ui/*        console SPA (built assets, when present)
```

The gateway serves the built console (`apps/console/dist`, override with
`AFFINITY_CONSOLE_DIR`) under `/ui` on the same `:8236` listener. Everything
else is inbound traffic and is forwarded transparently, so the gateway behaves
as a plain pass-through unless a route matches. Unknown console asset paths
return `404`; other `/ui/*` paths fall back to the SPA.

## Request path

Each proxied request follows the same stages:

1. **Resolve route** — inbound requests match the longest active prefix (an
   empty prefix is the single global catch-all); egress requests are selected by
   the `{route-id}` segment. Inbound forwarding is transparent: the request path
   is never rewritten, the prefix only chooses the route.
2. **Resolve session** (fixed three-tier cascade):
   1. dedicated session headers — `X-Affinity-Session-Id`, `X-Session-Id`,
      `Session-Id`, `X-Conversation-Id`, `Thread-Id`, `X-Opencode-Session`,
      `X-Claude-Code-Session-Id`, `X-Hermes-Session-Key`,
      `X-Deepseek-Harness-Session-Id`, … (most specific first);
   2. protocol fields in the JSON body — `metadata.session_id` /
      `conversation_id` / `thread_id`, top-level `session_id`, `conversation`,
      `prompt_cache_key`, `previous_response_id`, …;
   3. **history fingerprint** — for stateless clients that send only the OpenAI
      `messages` array, the gateway keeps a bounded index of conversation
      fingerprints and reuses the session when the current history extends a
      previously seen prefix (robust to the history growing each turn).

   This is soft affinity: an existing id is reused and a missing one is derived
   or generated, but no request is ever locked to a downstream or provider.
3. **Forward** — the request is reverse-proxied to the route target. The matched
   inbound path is preserved; the `/egress/{id}` prefix is stripped. Streaming
   responses pass through unchanged.
4. **Record** — redacted routing metadata (direction, route, model, status,
   session, latency) is appended to an in-memory ring buffer.

Inbound requests carry the resolved identity to the downstream as
`X-Affinity-Session-Id`. Egress requests are forwarded as-is so the gateway does
not leak internal headers to providers; mapping is the job of an egress plugin.

## Plugin chains

Each route stores an ordered list of plugin ids. Plugins are Lua programs
persisted with the config and edited in the console (`/api/v1/plugins`). Each
plugin declares a direction (`inbound`, `egress` or `both`); a route may only
reference plugins that match its direction.

Plugins run in a restricted Lua sandbox (gopher-lua):

- no files, no network, no `os`/`io`/`require`/`dofile`, 250 ms timeout;
- a mutable request view `ctx` with `ctx:set_header`, `ctx:remove_header`,
  `ctx:set_body` and fields (`session`, `session_source`, `route_id`, `method`,
  `path`, `body`, `headers`, `query`);
- `json.decode` / `json.encode` for body editing;
- `session.opencode(value, binding)` which derives a stable OpenCode session id.

Bundled plugins:

- `session.inject-metadata` (inbound) writes the resolved session into
  `metadata.session_id`.
- `opencode.session` (egress) sets `x-opencode-session` from the session.

Gateway-internal headers (currently `X-Affinity-Session-Id`) are always removed
on egress by the Go core before forwarding, so providers never see them. This is
default behavior, not a plugin.

### Affinity across a relay

Some relays (for example new-api) strip custom request headers. To keep affinity
intact the inbound plugin writes the session id into the request **body**, which
relays forward verbatim, and the egress plugin reads it back out:

```text
client → inbound: resolve session, metadata.session_id = <id>
       → relay (drops headers, forwards body)
       → egress: read metadata.session_id, set x-opencode-session
       → provider
```

`session.opencode` derives `ses_` + 12 hex + 14 base62 (30 chars) from the
gateway secret with HMAC-SHA256, so the same session always maps to the same
provider session without exposing the original id. The secret lives in
`.data/secret` and is never returned by the API.

## Configuration

Configuration is a single JSON document (`.data/config.json`, override with
`AFFINITY_CONFIG_FILE`). Writes are validated and persisted atomically enough for
single-process use, and the runtime only reads immutable snapshots. The console
is the only expected writer.

```json
{
  "version": 1,
  "listen": ":8236",
  "inbound": [
    { "id": "default", "name": "默认入站", "path": "/v1/chat/completions",
      "target": "http://127.0.0.1:9000", "state": "active",
      "plugins": ["session.extract", "session.generate"] }
  ],
  "egress": [
    { "id": "openai", "name": "OpenAI", "path": "/egress/openai",
      "target": "https://api.openai.com", "state": "active",
      "plugins": ["provider.session", "strip.internal"] }
  ]
}
```

`state` is `active` or `inactive`; inactive routes are ignored by the proxy.
Egress `path` is always `/egress/{id}`. Inbound paths must be unique.

### Local runtime state

Alongside `config.json`, the gateway persists runtime state under the same
`.data` directory:

- `secret` — gateway secret for deterministic provider session ids;
- `token` — control-plane access token (override with `AFFINITY_TOKEN`);
- `requests.json` — the last request records (already redacted), reloaded on start;
- `metrics.json` — counters, reloaded on start;
- `affinity.json` — the conversation index used by tier-3 affinity.

Writes are debounced and flushed on shutdown (`SIGINT`/`SIGTERM`), so the
console and session affinity survive a restart.

## Control API

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/config` | full config snapshot |
| GET | `/api/v1/status` | mode and route counts |
| GET | `/api/v1/plugins` | plugin definitions (with Lua source) |
| PUT/DELETE | `/api/v1/plugins/{id}` | upsert / delete a plugin |
| GET | `/api/v1/metrics` | request / session / egress counters |
| GET | `/api/v1/requests` | recent request summaries |
| GET | `/api/v1/requests/{id}` | full trace: headers + body (redacted) |
| GET/POST | `/api/v1/inbound` | list / create inbound routes |
| GET/PUT/DELETE | `/api/v1/inbound/{id}` | read / update / delete |
| GET/POST | `/api/v1/egress` | list / create egress routes |
| GET/PUT/DELETE | `/api/v1/egress/{id}` | read / update / delete |
| GET | `/healthz` | process health |

Errors are returned as `{ "error": "message" }` with `400`, `404` or `409`.

The whole `/api/v1/*` control plane requires `Authorization: Bearer <token>`
(the token is generated into `.data/token` on first start, or set via
`AFFINITY_TOKEN`). `/healthz`, the console assets, `/v1/*` and `/egress/*` stay
public.
