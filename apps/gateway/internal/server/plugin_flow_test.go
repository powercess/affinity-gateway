package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/affinity"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/observe"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/plugin"
)

func mustUpdate(t *testing.T, store *config.Store, direction string, route config.Route) {
	t.Helper()
	if _, err := store.Update(direction, route.ID, route); err != nil {
		t.Fatalf("update %s/%s: %v", direction, route.ID, err)
	}
}

// TestAffinityThroughForwardingRelay proves the real chain: the inbound injects
// X-Affinity-Session-Id, a relay that forwards headers passes it along, and the
// egress plugin maps it to x-opencode-session.
func TestAffinityThroughForwardingRelay(t *testing.T) {
	var mu sync.Mutex
	var gotSession string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotSession = r.Header.Get("x-opencode-session")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer provider.Close()

	store, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	registry := plugin.NewRegistry()
	if err := registry.Load(store.Plugins()); err != nil {
		t.Fatal(err)
	}

	mustUpdate(t, store, config.Egress, config.Route{
		ID: "opencode", Name: "opencode", Target: provider.URL,
		State: config.StateActive, Plugins: []string{"opencode.session"},
	})
	egressSrv := httptest.NewServer(proxyHandler{
		store: store, plugins: registry, egress: true,
		metrics: &observe.Metrics{}, recorder: observe.NewRecorder(10),
	})
	defer egressSrv.Close()

	// Relay mimics new-api with header passthrough.
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req, _ := http.NewRequest(http.MethodPost, egressSrv.URL+"/egress/opencode/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if v := r.Header.Get("X-Affinity-Session-Id"); v != "" {
			req.Header.Set("X-Affinity-Session-Id", v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer relay.Close()

	inboundRecorder := observe.NewRecorder(10)
	inbound := proxyHandler{
		store: store, plugins: registry, resolver: affinity.NewResolver(),
		metrics: &observe.Metrics{}, recorder: inboundRecorder,
	}
	mustUpdate(t, store, config.Inbound, config.Route{
		ID: "default", Name: "default", Path: "/v1/chat/completions", Target: relay.URL,
		State: config.StateActive,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	inbound.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("inbound status %d body=%s", rec.Code, rec.Body.String())
	}
	records := inboundRecorder.List()
	if len(records) != 1 || records[0].Session == "" {
		t.Fatalf("expected one inbound session, got %+v", records)
	}
	want := plugin.OpenCodeSessionID(store.Secret(), "opencode", records[0].Session)
	mu.Lock()
	got := gotSession
	mu.Unlock()
	if got != want {
		t.Fatalf("provider session header: want %q got %q", want, got)
	}
}

func TestRequestRecordCapturesHeaders(t *testing.T) {
	srv, _ := upstream(t)
	store := newStore(t, []config.Route{{ID: "chat", Name: "Chat", Path: "/v1/chat/completions", Target: srv.URL, State: config.StateActive}}, nil)
	recorder := observe.NewRecorder(10)
	h := proxyHandler{store: store, metrics: &observe.Metrics{}, recorder: recorder}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer super-secret")
	h.ServeHTTP(httptest.NewRecorder(), req)

	list := recorder.List()
	if len(list) != 1 {
		t.Fatalf("expected one record")
	}
	detail, ok := recorder.Get(list[0].ID)
	if !ok {
		t.Fatal("record not found")
	}
	if got := detail.RequestHeaders["Authorization"]; len(got) != 1 || got[0] != "***" {
		t.Fatalf("expected redacted authorization, got %v", got)
	}
}
