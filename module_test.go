package sessionaffinity

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
)

func configured(t *testing.T, mode string) *Handler {
	t.Helper()
	t.Setenv("TEST_AFFINITY_SECRET", strings.Repeat("s", 32))
	h := &Handler{Mode: mode, SecretEnv: "TEST_AFFINITY_SECRET"}
	if mode == "outbound" {
		h.Policy = "derive"
		h.OutputHeader = "x-opencode-session"
		h.IdentityScope = "site-a"
	}
	if err := h.Provision(caddy.Context{}); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestIdentityIsolationAndBodyReplay(t *testing.T) {
	h := configured(t, "inbound")
	run := func(key, body string) string {
		r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		r.Header.Set("Authorization", key)
		var id string
		err := h.ServeHTTP(httptest.NewRecorder(), r, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
			b, e := io.ReadAll(r.Body)
			if e != nil || string(b) != body {
				t.Fatal("body changed", e)
			}
			id = r.Header.Get(internalHeader)
			return nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := run("Bearer a", `{"metadata":{"user_id":"{\"session_id\":\"one\"}"},"messages":[]}`)
	b := run("Bearer a", `{"metadata":{"user_id":"{\"session_id\":\"one\"}"},"messages":[{"role":"user","content":"next"}]}`)
	c := run("Bearer b", `{"metadata":{"user_id":"{\"session_id\":\"one\"}"}}`)
	if a != b || a == c || !canonicalID(a) {
		t.Fatal("identity instability or tenant collision")
	}
}

func TestOutboundAndSSE(t *testing.T) {
	h := configured(t, "outbound")
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set(internalHeader, "sa:v1:"+strings.Repeat("a", 64))
	r.Header.Set("Authorization", "Bearer upstream")
	r.Header.Set("Session_id", "private")
	w := httptest.NewRecorder()
	err := h.ServeHTTP(w, r, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		if r.Header.Get("Authorization") != "Bearer upstream" || r.Header.Get(internalHeader) != "" || r.Header.Get("Session_id") != "" {
			t.Fatal("header isolation failed")
		}
		if len(r.Header.Get("x-opencode-session")) != 64 {
			t.Fatal("missing derived identity")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_start\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		return nil
	}))
	if err != nil || !w.Flushed || w.Body.String() != "event: message_start\ndata: {}\n\n" {
		t.Fatal("SSE changed", err)
	}
	a := derive(h.secret, "outbound:v1", "site-a", "sid")
	b := derive(h.secret, "outbound:v1", "site-b", "sid")
	if a == b {
		t.Fatal("site identities collide")
	}
}

func TestMissingConflictAndOverflow(t *testing.T) {
	h := configured(t, "inbound")
	h.BodyLimit = 8
	called := false
	next := caddyhttp.HandlerFunc(func(http.ResponseWriter, *http.Request) error { called = true; return nil })
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"messages":[]}`))
	r.Header.Set("Authorization", "Bearer a")
	if h.serveHTTP(httptest.NewRecorder(), r, next) == nil || called {
		t.Fatal("missing identity accepted")
	}
	b, _ := io.ReadAll(r.Body)
	if string(b) != `{"messages":[]}` {
		t.Fatal("overflow body lost")
	}
	r.Header.Set("X-Session-Id", "one")
	r.Header.Set("Session_id", "two")
	if h.serveHTTP(httptest.NewRecorder(), r, next) == nil {
		t.Fatal("conflict accepted")
	}
	out := configured(t, "outbound")
	if out.serveHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), next) == nil {
		t.Fatal("missing outbound identity accepted")
	}
}

func TestCredentialFallback(t *testing.T) {
	h := configured(t, "inbound")
	h.Fallback = "credential"
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("X-Api-Key", "abc")
	if h.Validate() == nil {
		t.Fatal("credential fallback accepted in strict-only configuration")
	}
	w := httptest.NewRecorder()
	if err := h.ServeHTTP(w, r, caddyhttp.HandlerFunc(func(http.ResponseWriter, *http.Request) error { t.Fatal("missing identity forwarded"); return nil })); err != nil {
		t.Fatal(err)
	}
	if w.Code != 400 || w.Header().Get("X-Affinity-Error") != "affinity_identity_required" {
		t.Fatal("missing actionable rejection")
	}
}

func TestCaddyfile(t *testing.T) {
	h := new(Handler)
	if err := h.UnmarshalCaddyfile(caddyfile.NewTestDispenser("session_affinity {\nmode inbound\nunknown value\n}")); err == nil {
		t.Fatal("unknown option accepted")
	}
	_, _, err := (caddyfile.Adapter{ServerType: httpcaddyfile.ServerType{}}).Adapt([]byte(":8236 {\nsession_affinity {\nsecret_env TEST_AFFINITY_SECRET\n}\nreverse_proxy localhost:8235\n}"), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("configs/Caddyfile.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = (caddyfile.Adapter{ServerType: httpcaddyfile.ServerType{}}).Adapt(b, nil); err != nil {
		t.Fatal(err)
	}
}

func TestBodySignals(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"conversation":"conv-a"}`, "conv-a"},
		{`{"conversation":{"id":"conv-b"}}`, "conv-b"},
		{`{"metadata":{"user_id":"ordinary-user"}}`, ""},
		{`{"previous_response_id":"resp-a"}`, ""},
		{`{"conversation":`, ""},
	} {
		if got := bodySession([]byte(tc.body)); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}

// Models the documented new-api boundary: only explicitly restored headers
// survive, and the channel supplies its own upstream credential.
func TestInboundRelayOutbound(t *testing.T) {
	inbound := configured(t, "inbound")
	outbound := configured(t, "outbound")
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	r.Header.Set("Authorization", "Bearer client")
	r.Header.Set("X-Session-Id", "chat-a")
	reached := false
	err := inbound.ServeHTTP(httptest.NewRecorder(), r, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		internal := r.Header.Get(internalHeader)
		r.Header = http.Header{"Authorization": {"Bearer supplier"}}
		r.Header.Set(internalHeader, internal)
		return outbound.ServeHTTP(w, r, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
			reached = true
			if r.Header.Get(internalHeader) != "" || r.Header.Get("Authorization") != "Bearer supplier" {
				t.Fatal("relay isolation failed")
			}
			want := derive(outbound.secret, "outbound:v1", "site-a", internal)
			if r.Header.Get("x-opencode-session") != want {
				t.Fatal("wrong supplier identity")
			}
			return nil
		}))
	}))
	if err != nil || !reached {
		t.Fatal("pipeline failed", err)
	}
}
