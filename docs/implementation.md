# Implemented first version

This document preserves the first-version implementation record. It is superseded
by [the strict-affinity contract](strict-affinity.md). In particular, credential
fallback/missing-strip are now rejected, all JSON bodies are validated, derive
rewrites declared body identities, and strong routing requires the new-api patch.

Build from this checkout with `xcaddy build v2.11.4 --with
github.com/powercess/caddy-session-affinity=.` (one shell command). Supply a random,
at least 32-byte SESSION_AFFINITY_SECRET environment variable, then run the built
Caddy with configs/Caddyfile.example. Replace example upstream domains first.
Secrets are referenced by environment variable name, not embedded in Caddy JSON.

## Contract

Inbound recognizes Session-Id, Session_id, Conversation-Id, Conversation_id,
X-Session-Affinity, X-Session-Id, X-Opencode-Session and Thread-Id. Conflicting or
duplicate headers are rejected. Header names ignore case; underscore and hyphen
are distinct. Headers take precedence over body signals.

Without a header, a bounded body probe recognizes conversation as string or
{id: string}, and metadata.user_id containing a JSON string with session_id.
Plain user IDs, legacy Claude Code encodings, content fingerprints and response
chains are not interpreted. Original request bytes and body Close are preserved.
Bodies exceeding body_limit (default 2097152 bytes) or carrying Content-Encoding
are not parsed; explicit headers still work. Missing identity defaults to HTTP 400.
fallback credential explicitly enables credential-level affinity instead.

An Authorization credential (or X-Api-Key if absent) is required as namespace.
HMAC-SHA256 with length-framed fields produces X-Session-Affinity: sa:v1:<hex>.
Existing recognized identity headers are removed. The proxy does not authenticate
credentials; new-api remains responsible for authentication. Credential rotation
changes namespace. Keep the same secret across instances and restarts; changing
it resets affinity. This deterministic implementation needs no store or TTL.

The pinned strict new-api patch validates X-Session-Affinity for routing and
copies that exact opaque value into its selected-channel request. Per-channel
header_override configuration is not required and cannot replace the value in
strict mode. The outbound gateway consumes and removes it before public egress.

Set channel base_url to http://127.0.0.1:8237/r/opencode-a (or the appropriate
private-network service address). Verify actual path joining for each adapter.
The route ID is matched by Caddy handle_path, which strips that prefix. Unknown
routes return 404. Only configured reverse_proxy destinations are reachable.

Outbound policy strip removes recognized identity headers. derive requires an
identity_scope and output_header; it emits a site-scoped 64-character hex HMAC.
passthrough emits the internal canonical ID unchanged. Allowed output headers:
x-opencode-session, x-session-affinity, x-session-id. These are generic header
policies, not verified vendor contracts; hex output is NOT UUIDv7. Confirm each
supplier's accepted format before deployment. No arbitrary credential/framing
header rewrites are supported. Authorization and X-Api-Key remain untouched.
Missing/invalid internal identity under derive/passthrough defaults to HTTP 400;
missing strip explicitly allows forwarding without a session header.

Outbound must be internal-only: canonical ID validation checks syntax, not an
authentication signature. Never expose this listener as a public credentialed
proxy. Identity scope controls sharing across route instances. The example uses
separate scopes for independent sites; use a shared scope only intentionally.

## Verification and boundaries

Run `go test -race ./...` and `go vet ./...`. Tests cover tenant isolation,
cross-turn stability, body replay/overflow, header conflicts, outbound stripping,
credentials, stream flushing and Caddyfile adaptation. Caddy reverse_proxy owns
networking and SSE; the plugin never wraps the response writer or retries calls.

Real new-api affinity, multi-key channel behavior, supplier cache hits and vendor
formats require integration validation. A fixed channel does not guarantee a
fixed account inside a multi-key channel or another downstream relay. Redis,
response-chain tracking, UUID mappings, migration, metrics and automatic supplier
configuration are deferred. The first implementation intentionally uses one
small package: module.go (handler/config), session.go (identity), caddyfile.go
(adapter), plus colocated Go tests.
