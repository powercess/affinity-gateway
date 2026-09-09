package sessionaffinity

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func TestConfigurableInboundAdmission(t *testing.T) {
	original := inboundRules
	defer func() { inboundRules = original }()
	for _, tc := range []struct {
		name, mode, body string
		enabled, strip   bool
		status           int
	}{
		{"strict validates body", "strict", "not-json", true, true, 400},
		{"headers skips body", "headers_only", "not-json", true, true, 200},
		{"disabled but strip", "headers_only", "not-json", false, true, 200},
		{"disabled and retain", "headers_only", "not-json", false, false, 200},
		{"enabled retained", "headers_only", "not-json", true, false, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := configured(t, "inbound")
			h.ObserveID = "custom"
			rules := defaultInboundRules(*h)
			rules.Mode = tc.mode
			rules.Headers = []HeaderRule{{"X-Custom-Session", tc.enabled, tc.strip}, {"X-Session-Id", true, true}}
			inboundRules = &inboundRegistry{saved: map[string]InboundRules{"custom": rules}}
			r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer private-key")
			r.Header.Set("X-Custom-Session", "private-session")
			r.Header.Set("X-Session-Id", "private-session")
			r.Header.Set(internalHeader, "untrusted-forged")
			if tc.mode == "headers_only" {
				r.Header.Set("Content-Encoding", "gzip")
			}
			w := httptest.NewRecorder()
			called := false
			err := h.ServeHTTP(w, r, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				called = true
				data, _ := io.ReadAll(r.Body)
				if string(data) != tc.body {
					t.Fatal("body changed")
				}
				if (r.Header.Get("X-Custom-Session") == "") != tc.strip {
					t.Fatal("strip flag ignored")
				}
				if !canonicalID(r.Header.Get(internalHeader)) {
					t.Fatal("internal identity not regenerated")
				}
				return nil
			}))
			if err != nil || w.Code != tc.status || called != (tc.status == 200) {
				t.Fatalf("status %d error %v", w.Code, err)
			}
			rows, _, _ := observations.snapshot()
			raw, _ := json.Marshal(rows[0])
			for _, secret := range []string{"private-key", "private-session", "untrusted-forged", "not-json"} {
				if bytes.Contains(raw, []byte(secret)) {
					t.Fatal("sensitive observation", secret)
				}
			}
		})
	}
}
func TestRulesValidationAndPersistence(t *testing.T) {
	h := configured(t, "inbound")
	h.ObserveID = "main"
	s := &inboundRegistry{saved: map[string]InboundRules{}, defaults: map[string]InboundRules{}}
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := s.configure(path); err != nil {
		t.Fatal(err)
	}
	s.register(*h)
	r := defaultInboundRules(*h)
	r.Mode = "headers_only"
	r.Headers = append(r.Headers, HeaderRule{"X-Custom-Id", true, false})
	if err := s.update(r); err != nil {
		t.Fatal(err)
	}
	if err := s.update(r); err == nil {
		t.Fatal("stale revision accepted")
	}
	loaded := &inboundRegistry{}
	if err := loaded.configure(path); err != nil {
		t.Fatal(err)
	}
	got := loaded.get(*h)
	if got.Mode != "headers_only" || got.Revision != 1 {
		t.Fatal("not persisted")
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "session_value") {
		t.Fatal("unexpected identity")
	}
	for _, name := range []string{"Authorization", "X-Session-Affinity", "Host", "Content-Length", "X-Forwarded-For", "Bad Header"} {
		bad := defaultInboundRules(*h)
		bad.Headers = append(bad.Headers, HeaderRule{name, true, true})
		if validateInboundRules(&bad) == nil {
			t.Fatal("reserved or invalid header accepted", name)
		}
	}
	bad := defaultInboundRules(*h)
	bad.Headers = append(bad.Headers, HeaderRule{"x-session-id", true, true})
	if validateInboundRules(&bad) == nil {
		t.Fatal("duplicate accepted")
	}
	bad = defaultInboundRules(*h)
	bad.Mode = "headers_only"
	for i := range bad.Headers {
		bad.Headers[i].Enabled = false
	}
	if validateInboundRules(&bad) == nil {
		t.Fatal("no source accepted")
	}
}
func TestInboundRulesAPI(t *testing.T) {
	old := inboundRules
	defer func() { inboundRules = old }()
	inboundRules = &inboundRegistry{saved: map[string]InboundRules{}, defaults: map[string]InboundRules{}}
	h := configured(t, "inbound")
	h.ObserveID = "main"
	inboundRules.configure(filepath.Join(t.TempDir(), "rules.json"))
	inboundRules.register(*h)
	rules := defaultInboundRules(*h)
	rules.Mode = "headers_only"
	body, _ := json.Marshal(rules)
	for _, tc := range []struct {
		method, origin string
		auth           bool
		status         int
	}{{"PUT", "http://evil.example", true, 403}, {"PUT", "", false, 401}, {"PATCH", "", true, 405}, {"PUT", "", true, 200}, {"PUT", "", true, 409}, {"GET", "", true, 200}} {
		c := Console{testEnvironment: tc.auth}
		r := httptest.NewRequest(tc.method, "http://localhost/api/inbound-rules", bytes.NewReader(body))
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		c.ServeHTTP(w, r, nil)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.method, w.Code, w.Body.String())
		}
	}
}
func TestProtocolHeaderRedaction(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("Content-Type", "application/json; boundary=SECRET")
	r.Header.Set("Accept", "SECRET")
	r.Header.Set("User-Agent", "SECRET")
	r.Header.Set(internalHeader, "SECRET")
	r.Header.Set("Proxy-Authorization", "SECRET")
	got := observedHeaders(r, "secret")
	raw, _ := json.Marshal(got)
	if bytes.Contains(raw, []byte("SECRET")) {
		t.Fatal(string(raw))
	}
	if got["Content-Type"] != "application/json" || got[internalHeader] != "[redacted]" {
		t.Fatal(got)
	}
}

func TestRulePreviewDoesNotRetainIdentity(t *testing.T) {
	h := configured(t, "inbound")
	h.ObserveID = "preview"
	rules := defaultInboundRules(*h)
	rules.Headers = append(rules.Headers, HeaderRule{"X-Private-Id", true, false})
	payload, _ := json.Marshal(map[string]any{"rules": rules, "headers": map[string]string{"X-Private-Id": "PRIVATE-VALUE"}, "body": `{"model":"test","messages":[{"content":"PRIVATE-BODY"}]}`})
	_, before, _ := observations.snapshot()
	c := Console{testEnvironment: true}
	r := httptest.NewRequest("POST", "http://localhost/api/inbound-rules/preview", bytes.NewReader(payload))
	w := httptest.NewRecorder()
	c.ServeHTTP(w, r, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "PRIVATE-") {
		t.Fatal("preview disclosed values")
	}
	var got struct {
		Status int    `json:"status"`
		Source string `json:"source"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Status != 200 || got.Source != "X-Private-Id" {
		t.Fatal(got)
	}
	_, after, _ := observations.snapshot()
	if before != after {
		t.Fatal("preview stored observations")
	}
}
func TestHeaderModeStillRejectsAmbiguousIdentities(t *testing.T) {
	for _, kind := range []string{"missing", "duplicate", "conflict", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			h := configured(t, "inbound")
			rules := defaultInboundRules(*h)
			rules.Mode = "headers_only"
			h.inbound = &rules
			r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader("opaque body"))
			r.Header.Set("Authorization", "test")
			switch kind {
			case "duplicate":
				r.Header.Add("X-Session-Id", "one")
				r.Header.Add("X-Session-Id", "one")
			case "conflict":
				r.Header.Set("X-Session-Id", "one")
				r.Header.Set("Session-Id", "two")
			case "invalid":
				r.Header.Set("X-Session-Id", " trailing ")
			}
			w := httptest.NewRecorder()
			h.handleHTTP(w, r, caddyhttp.HandlerFunc(func(http.ResponseWriter, *http.Request) error { t.Fatal("invalid identity forwarded"); return nil }))
			if w.Code != 400 {
				t.Fatal(w.Code)
			}
		})
	}
}
func TestRuleSaveFailurePreservesActiveRules(t *testing.T) {
	h := configured(t, "inbound")
	h.ObserveID = "main"
	path := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(path, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &inboundRegistry{path: filepath.Join(path, "rules.json"), saved: map[string]InboundRules{}, defaults: map[string]InboundRules{"main": defaultInboundRules(*h)}}
	r := defaultInboundRules(*h)
	r.Mode = "headers_only"
	if s.update(r) == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	if s.get(*h).Mode != "strict" {
		t.Fatal("failed save changed active config")
	}
}

func TestInboundRuleResponseProvenance(t *testing.T) {
	h := configured(t, "inbound")
	h.ObserveID = "main"
	defaults := defaultInboundRules(*h)
	s := &inboundRegistry{defaults: map[string]InboundRules{"main": defaults}, saved: map[string]InboundRules{}}
	response := s.response()
	if response["sources"].(map[string]string)["main"] != "default" {
		t.Fatal("default origin missing")
	}
	saved := defaults
	saved.Metadata = false
	// Provenance must not be guessed from revision; imported rules can be revision zero.
	s.saved["main"] = saved
	response = s.response()
	if response["sources"].(map[string]string)["main"] != "saved" || response["items"].([]InboundRules)[0].Metadata {
		t.Fatal("saved rules and source do not match")
	}
	response["items"].([]InboundRules)[0].Headers[0].Enabled = false
	if !s.saved["main"].Headers[0].Enabled {
		t.Fatal("response aliases registry headers")
	}
}
