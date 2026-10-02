package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/affinity"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/observe"
)

type echo struct {
	Path    string      `json:"path"`
	Session string      `json:"session"`
	Body    string      `json:"body"`
	Header  http.Header `json:"-"`
}

func newStore(t *testing.T, inbound, egress []config.Route) *config.Store {
	t.Helper()
	store, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	// reset to an empty baseline then add the requested routes
	if err := store.Delete(config.Inbound, "default"); err != nil && len(inbound) > 0 {
		t.Fatal(err)
	}
	for _, r := range inbound {
		if _, err := store.Add(config.Inbound, r); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range egress {
		if _, err := store.Add(config.Egress, r); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func upstream(t *testing.T) (*httptest.Server, *echo) {
	t.Helper()
	got := &echo{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.Path = r.URL.Path
		got.Session = r.Header.Get("X-Affinity-Session-Id")
		got.Body = string(body)
		got.Header = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestInboundGeneratesSession(t *testing.T) {
	srv, got := upstream(t)
	store := newStore(t, []config.Route{{ID: "chat", Name: "Chat", Path: "/v1/chat/completions", Target: srv.URL, State: config.StateActive}}, nil)
	metrics := &observe.Metrics{}
	recorder := observe.NewRecorder(10)
	h := proxyHandler{store: store, metrics: metrics, recorder: recorder}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-test"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.HasPrefix(got.Session, "sess_") {
		t.Fatalf("expected generated session header, got %q", got.Session)
	}
	if got.Path != "/v1/chat/completions" {
		t.Fatalf("unexpected upstream path %q", got.Path)
	}
	records := recorder.List()
	if len(records) != 1 || records[0].Model != "gpt-test" || records[0].Direction != config.Inbound {
		t.Fatalf("unexpected records %+v", records)
	}
	if metrics.Snapshot().AffinitySessions != 1 {
		t.Fatalf("expected one affinity session")
	}
}

func TestInboundReusesHeaderSession(t *testing.T) {
	srv, got := upstream(t)
	store := newStore(t, []config.Route{{ID: "chat", Name: "Chat", Path: "/v1/chat/completions", Target: srv.URL, State: config.StateActive}}, nil)
	h := proxyHandler{store: store, metrics: &observe.Metrics{}, recorder: observe.NewRecorder(10)}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Id", "stable-123")
	h.ServeHTTP(rec, req)

	if got.Session != "stable-123" {
		t.Fatalf("expected reused session, got %q", got.Session)
	}
}

func TestEgressStripsRoutePrefix(t *testing.T) {
	srv, got := upstream(t)
	store := newStore(t, nil, []config.Route{{ID: "prov", Name: "Provider", Target: srv.URL, State: config.StateActive}})
	metrics := &observe.Metrics{}
	h := proxyHandler{store: store, egress: true, metrics: metrics, recorder: observe.NewRecorder(10)}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/egress/prov/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got.Path != "/v1/chat/completions" {
		t.Fatalf("expected stripped path, got %q", got.Path)
	}
	if got.Session != "" {
		t.Fatalf("egress must not inject gateway session, got %q", got.Session)
	}
	if metrics.Snapshot().EgressSuccess != 1 {
		t.Fatalf("expected egress success metric")
	}
}

func TestStreamingResponsePassesThrough(t *testing.T) {
	stream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, chunk := range []string{"data: one\n\n", "data: two\n\n"} {
			_, _ = w.Write([]byte(chunk))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(stream.Close)

	store := newStore(t, []config.Route{{ID: "chat", Name: "Chat", Path: "/v1/chat/completions", Target: stream.URL, State: config.StateActive}}, nil)
	h := proxyHandler{store: store, metrics: &observe.Metrics{}, recorder: observe.NewRecorder(10)}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if body := rec.Body.String(); body != "data: one\n\ndata: two\n\n" {
		t.Fatalf("unexpected streamed body %q", body)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("unexpected content type %q", contentType)
	}
}

func TestEgressStripsInternalHeadersByDefault(t *testing.T) {
	srv, got := upstream(t)
	store := newStore(t, nil, []config.Route{{ID: "prov", Name: "Provider", Target: srv.URL, State: config.StateActive}})
	h := proxyHandler{store: store, egress: true, metrics: &observe.Metrics{}, recorder: observe.NewRecorder(10)}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/egress/prov/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Affinity-Session-Id", "must-not-leak")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if leaked := got.Header.Get("X-Affinity-Session-Id"); leaked != "" {
		t.Fatalf("internal header leaked to provider: %q", leaked)
	}
}

func TestInboundHistoryAffinity(t *testing.T) {
	srv, _ := upstream(t)
	store := newStore(t, []config.Route{{ID: "chat", Name: "Chat", Path: "/v1/chat/completions", Target: srv.URL, State: config.StateActive}}, nil)
	recorder := observe.NewRecorder(10)
	h := proxyHandler{store: store, resolver: affinity.NewResolver(), metrics: &observe.Metrics{}, recorder: recorder}

	send := func(body string) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	send(`{"model":"m","messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}]}`)
	send(`{"model":"m","messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"},{"role":"assistant","content":"hello"},{"role":"user","content":"again"}]}`)

	list := recorder.List()
	if len(list) != 2 {
		t.Fatalf("expected two records, got %d", len(list))
	}
	if list[0].Session == "" || list[0].Session != list[1].Session {
		t.Fatalf("history affinity failed: %q vs %q", list[0].Session, list[1].Session)
	}
	if list[0].SessionSource != "history:prefix-2" {
		t.Fatalf("unexpected source %q", list[0].SessionSource)
	}
}

func TestInboundForwardsFullPath(t *testing.T) {
	srv, got := upstream(t)
	store := newStore(t, []config.Route{{
		ID: "site1", Name: "Site1", Path: "/site1", Target: srv.URL, State: config.StateActive,
	}}, nil)
	h := proxyHandler{store: store, metrics: &observe.Metrics{}, recorder: observe.NewRecorder(10)}

	req := httptest.NewRequest(http.MethodPost, "/site1/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got.Path != "/site1/v1/chat/completions" {
		t.Fatalf("expected full path forwarded, got %q", got.Path)
	}
}

func TestNoPrefixInboundMatchesEverything(t *testing.T) {
	srv, got := upstream(t)
	store := newStore(t, []config.Route{{
		ID: "all", Name: "all", Path: "", Target: srv.URL, State: config.StateActive,
	}}, nil)
	h := proxyHandler{store: store, metrics: &observe.Metrics{}, recorder: observe.NewRecorder(10)}

	req := httptest.NewRequest(http.MethodPost, "/anything/here", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got.Path != "/anything/here" {
		t.Fatalf("expected catch-all to forward full path, got %q", got.Path)
	}
}

func TestInboundUnknownRoute(t *testing.T) {
	store := newStore(t, []config.Route{{ID: "chat", Name: "Chat", Path: "/v1/chat/completions", Target: "http://127.0.0.1:1", State: config.StateActive}}, nil)
	h := proxyHandler{store: store, metrics: &observe.Metrics{}, recorder: observe.NewRecorder(10)}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/unknown", strings.NewReader("{}")))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	var payload map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if payload["error"] == "" {
		t.Fatalf("expected error payload")
	}
}
