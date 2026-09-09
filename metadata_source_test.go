package sessionaffinity

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func TestMetadataConversationSource(t *testing.T) {
	h := configured(t, "inbound")
	if err := h.UnmarshalCaddyfile(caddyfile.NewTestDispenser("session_affinity {\nidentity_source metadata\n}")); err != nil {
		t.Fatal(err)
	}
	if err := h.Validate(); err != nil {
		t.Fatal(err)
	}
	var parent string
	for _, tc := range []struct {
		name, body, header, credential string
		status                         int
		same                           bool
	}{
		{"main", `{"metadata":{"user_id":"{\"session_id\":\"parent\"}"}}`, "parent", "Bearer a", 200, true},
		{"side", `{"metadata":{"user_id":"{\"session_id\":\"parent\"}"}}`, "parent:side:one", "Bearer a", 200, true},
		{"new conversation", `{"metadata":{"user_id":"{\"session_id\":\"new\"}"}}`, "parent", "Bearer a", 200, false},
		{"other credential", `{"metadata":{"user_id":"{\"session_id\":\"parent\"}"}}`, "parent", "Bearer b", 200, false},
		{"no metadata no fallback", `{"conversation":"parent","prompt_cache_key":"parent"}`, "parent", "Bearer a", 400, false},
		{"malformed", `{`, "parent", "Bearer a", 400, false},
		{"duplicate", `{"metadata":{},"metadata":{}}`, "parent", "Bearer a", 400, false},
		{"duplicate nested", `{"metadata":{"user_id":"{\"session_id\":\"a\",\"session_id\":\"b\"}"}}`, "parent", "Bearer a", 400, false},
		{"plain user", `{"metadata":{"user_id":"parent"}}`, "parent", "Bearer a", 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(tc.body))
			r.Header.Set("Authorization", tc.credential)
			r.Header.Set("X-Claude-Code-Session-Id", tc.header)
			r.Header.Set("X-Session-Id", "different-header")
			w := httptest.NewRecorder()
			called := false
			err := h.ServeHTTP(w, r, caddyhttp.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) error {
				called = true
				id := r.Header.Get(internalHeader)
				if parent == "" {
					parent = id
				}
				if (id == parent) != tc.same {
					t.Fatal("incorrect conversation isolation")
				}
				for _, name := range sessionHeaders {
					if name != internalHeader && r.Header.Get(name) != "" {
						t.Fatal("identity header leaked", name)
					}
				}
				b, _ := io.ReadAll(r.Body)
				if string(b) != tc.body {
					t.Fatal("inbound body changed")
				}
				return nil
			}))
			if err != nil || w.Code != tc.status || called != (tc.status == 200) {
				t.Fatalf("status %d, called %v, err %v", w.Code, called, err)
			}
		})
	}
	h.IdentitySource = "typo"
	if h.Validate() == nil {
		t.Fatal("unknown source accepted")
	}
	h.IdentitySource = "metadata"
	h.CacheKeyAsSession = true
	if h.Validate() == nil {
		t.Fatal("ambiguous source accepted")
	}
}
