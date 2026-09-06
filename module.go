package sessionaffinity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// Handler normalizes inbound identities or applies a configured outbound policy.
// Caddy's reverse_proxy owns upstream selection, credentials and response streams.
type Handler struct {
	Mode              string `json:"mode,omitempty"`
	SecretEnv         string `json:"secret_env,omitempty"`
	IdentityScope     string `json:"identity_scope,omitempty"`
	OutputHeader      string `json:"output_header,omitempty"`
	Policy            string `json:"policy,omitempty"`
	Missing           string `json:"missing,omitempty"`
	Fallback          string `json:"fallback,omitempty"`
	BodyLimit         int64  `json:"body_limit,omitempty"`
	CacheKeyAsSession bool   `json:"cache_key_as_session,omitempty"`
	secret            string
}

func init() {
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterHandlerDirective("session_affinity", func(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
		m := new(Handler)
		err := m.UnmarshalCaddyfile(h.Dispenser)
		return m, err
	})
	httpcaddyfile.RegisterDirectiveOrder("session_affinity", "before", "reverse_proxy")
}
func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.session_affinity", New: func() caddy.Module { return new(Handler) }}
}
func (h *Handler) Provision(caddy.Context) error {
	if h.Mode == "" {
		h.Mode = "inbound"
	}
	if h.BodyLimit == 0 {
		h.BodyLimit = 2 << 20
	}
	if h.Missing == "" {
		h.Missing = "reject"
	}
	if h.Fallback == "" {
		h.Fallback = "reject"
	}
	if h.Policy == "" {
		h.Policy = "strip"
	}
	h.secret = os.Getenv(h.SecretEnv)
	return h.Validate()
}
func (h *Handler) Validate() error {
	if h.Mode != "inbound" && h.Mode != "outbound" {
		return fmt.Errorf("invalid mode")
	}
	if h.BodyLimit < 1 || h.BodyLimit > 16<<20 {
		return fmt.Errorf("body_limit must be 1..16777216 bytes")
	}
	if h.Missing != "reject" {
		return fmt.Errorf("strict affinity requires missing reject")
	}
	if h.Fallback != "reject" {
		return fmt.Errorf("strict affinity requires fallback reject; soft affinity is deferred")
	}
	if h.Policy != "strip" && h.Policy != "derive" && h.Policy != "passthrough" {
		return fmt.Errorf("invalid outbound policy")
	}
	if h.Mode == "inbound" || h.Policy == "derive" {
		if len(h.secret) < 32 {
			return fmt.Errorf("secret_env must reference a secret of at least 32 bytes")
		}
	}
	if h.Mode == "outbound" && h.Policy != "strip" {
		// An allowlist prevents accidentally overwriting credentials or HTTP framing.
		if h.OutputHeader != "x-opencode-session" && h.OutputHeader != "x-session-affinity" && h.OutputHeader != "x-session-id" {
			return fmt.Errorf("unsupported output_header")
		}
		if h.Policy == "derive" && !validID(h.IdentityScope) {
			return fmt.Errorf("derive requires identity_scope")
		}
	}
	return nil
}

func strip(r *http.Request) {
	for _, name := range sessionHeaders {
		r.Header.Del(name)
	}
}

type affinityFailure struct {
	status        int
	code, message string
}

func (e *affinityFailure) Error() string            { return e.message }
func reject(status int, code, message string) error { return &affinityFailure{status, code, message} }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	err := h.serveHTTP(w, r, next)
	var failure *affinityFailure
	if !errors.As(err, &failure) {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Affinity-Error", failure.code)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(failure.status)
	if strings.HasPrefix(r.URL.Path, "/v1/messages") {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]string{"type": "invalid_request_error", "message": failure.code + ": " + failure.message}})
	} else {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"type": "invalid_request_error", "code": failure.code, "message": failure.message, "param": "X-Session-Id"}})
	}
	return nil
}

func (h Handler) readBody(r *http.Request) ([]byte, error) {
	if r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
		return nil, reject(415, "affinity_encoding_unsupported", "Send an uncompressed JSON request")
	}
	if r.Body == nil {
		return nil, nil
	}
	original := r.Body
	b, err := io.ReadAll(io.LimitReader(original, h.BodyLimit+1))
	r.Body = &replayBody{Reader: io.MultiReader(bytes.NewReader(b), original), Closer: original}
	if err != nil {
		return nil, reject(400, "affinity_body_invalid", "Cannot read request body")
	}
	if int64(len(b)) > h.BodyLimit {
		return nil, reject(413, "affinity_body_too_large", "Request exceeds configured identity validation limit")
	}
	if len(b) > 0 && uniqueJSON(b) != nil {
		return nil, reject(400, "affinity_body_invalid", "JSON must be valid and contain no duplicate keys")
	}
	return b, nil
}

func (h Handler) serveHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if h.Mode == "outbound" {
		values := r.Header.Values(internalHeader)
		sid := r.Header.Get(internalHeader)
		strip(r)
		if h.Policy == "strip" {
			return next.ServeHTTP(w, r)
		}
		if len(values) != 1 || !canonicalID(sid) {
			return reject(400, "affinity_internal_invalid", "Missing or invalid internal session identity")
		}
		if h.Policy == "derive" {
			b, err := h.readBody(r)
			if err != nil {
				return err
			}
			rewritten, err := isolateBody(b, h.secret, h.IdentityScope, sid)
			if err != nil {
				return reject(400, "affinity_body_invalid", "Invalid session or cache identity in outbound body")
			}
			if !bytes.Equal(b, rewritten) {
				r.Body = &replayBody{Reader: bytes.NewReader(rewritten), Closer: r.Body}
				r.ContentLength = int64(len(rewritten))
				r.Header.Del("Content-Length")
				r.TransferEncoding = nil
			}
			sid = derive(h.secret, "outbound:v1", h.IdentityScope, sid)
		}
		r.Header.Set(h.OutputHeader, sid)
		return next.ServeHTTP(w, r)
	}
	// Use a credential namespace even for explicit IDs. No credential is logged.
	credential := r.Header.Get("Authorization")
	if credential == "" {
		credential = r.Header.Get("X-Api-Key")
	}
	if credential == "" {
		return reject(401, "affinity_credential_required", "Credential required for session namespace")
	}
	sid := ""
	for _, name := range sessionHeaders {
		values := r.Header.Values(name)
		if len(values) == 0 {
			continue
		}
		if len(values) != 1 || !validID(values[0]) {
			return reject(400, "affinity_identity_invalid", "Use one nonempty stable X-Session-Id, at most 512 bytes")
		}
		if sid != "" && sid != values[0] {
			return reject(400, "affinity_identity_conflict", "Conflicting session headers; send one stable X-Session-Id")
		}
		sid = values[0]
	}
	b, err := h.readBody(r)
	if err != nil {
		return err
	}
	var obj map[string]json.RawMessage
	if len(b) > 0 && (json.Unmarshal(b, &obj) != nil || obj == nil) {
		return reject(400, "affinity_body_invalid", "JSON object required")
	}
	meta, err := metadataSession(obj)
	if err != nil {
		return reject(400, "affinity_identity_invalid", "Invalid structured metadata session identity")
	}
	if sid != "" && meta != "" && sid != meta {
		return reject(400, "affinity_identity_conflict", "Header and metadata session IDs disagree")
	}
	if sid == "" {
		sid = meta
	}
	if sid == "" {
		sid = bodySession(b)
	}
	if sid == "" && h.CacheKeyAsSession {
		_ = json.Unmarshal(obj["prompt_cache_key"], &sid)
		if sid != "" && !validID(sid) {
			return reject(400, "affinity_identity_invalid", "Invalid configured cache session identity")
		}
	}
	if sid == "" {
		return reject(400, "affinity_identity_required", "Provide X-Session-Id: keep it unchanged for this conversation and retries; use a different value for a new conversation. Content hashing and credential fallback are disabled.")
	}
	strip(r)
	r.Header.Set(internalHeader, "sa:v1:"+derive(h.secret, "inbound:v1", credential, "conversation", sid))
	return next.ServeHTTP(w, r)
}

type replayBody struct {
	io.Reader
	io.Closer
}

var (
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddy.Validator             = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
	_ caddyfile.Unmarshaler       = (*Handler)(nil)
)
