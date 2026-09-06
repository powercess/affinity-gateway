package sessionaffinity

import (
	"encoding/json"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStrictAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, body, header string
		cache              bool
		status             int
	}{
		{"cache is not session", `{"prompt_cache_key":"one"}`, "", false, 400},
		{"explicit cache contract", `{"prompt_cache_key":"one"}`, "", true, 200},
		{"header metadata conflict", `{"metadata":{"user_id":"{\"session_id\":\"two\"}"}}`, "one", false, 400},
		{"duplicate key", `{"metadata":{},"metadata":{}}`, "one", false, 400},
		{"duplicate nested identity", `{"metadata":{"user_id":"{\"session_id\":\"one\",\"session_id\":\"two\"}"}}`, "one", false, 400},
		{"cache differs legitimately", `{"prompt_cache_key":"cache-group"}`, "one", false, 200},
		{"invalid body even with header", `{`, "one", false, 400},
		{"plain user is not session", `{"metadata":{"user_id":"user-a"}}`, "", false, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := configured(t, "inbound")
			h.CacheKeyAsSession = tc.cache
			r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer test")
			if tc.header != "" {
				r.Header.Set("X-Claude-Code-Session-Id", tc.header)
			}
			w := httptest.NewRecorder()
			called := false
			err := h.ServeHTTP(w, r, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				called = true
				b, _ := io.ReadAll(r.Body)
				if string(b) != tc.body {
					t.Fatal("inbound body altered")
				}
				return nil
			}))
			if err != nil || w.Code != tc.status || called != (tc.status == 200) {
				t.Fatalf("status=%d called=%v err=%v body=%s", w.Code, called, err, w.Body)
			}
			if tc.status != 200 {
				var body map[string]any
				if json.Unmarshal(w.Body.Bytes(), &body) != nil || body["type"] != "error" {
					t.Fatal("invalid Anthropic error")
				}
			}
		})
	}
}

func TestBodyIsolationPreservesResourceReferences(t *testing.T) {
	b := []byte(`{"prompt_cache_key":"cache","conversation":"conv_resource","previous_response_id":"resp_resource","metadata":{"user_id":"{\"session_id\":\"private\",\"device_id\":\"device\"}"},"input":[{"role":"user","content":"unchanged"}],"temperature":0}`)
	a, err := isolateBody(b, "secret", "site-a", "canonical")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := isolateBody(b, "secret", "site-b", "canonical")
	if err != nil {
		t.Fatal(err)
	}
	var original, got map[string]json.RawMessage
	_ = json.Unmarshal(b, &original)
	_ = json.Unmarshal(a, &got)
	for _, field := range []string{"conversation", "previous_response_id", "input", "temperature"} {
		if string(original[field]) != string(got[field]) {
			t.Fatalf("changed %s", field)
		}
	}
	sid, err := metadataSession(got)
	if err != nil || sid != derive("secret", "outbound:v1", "site-a", "canonical") {
		t.Fatal("metadata not scoped")
	}
	if strings.Contains(string(a), "private") || string(a) == string(b2) {
		t.Fatal("site isolation failed")
	}
}
