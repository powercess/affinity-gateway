package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
)

// TestOnlyV1IsProxied ensures the gateway proxies /v1/ and rejects every other
// path, so it never becomes a web reverse proxy (which caused redirect loops).
func TestOnlyV1IsProxied(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("AFFINITY_CONFIG_FILE", configPath)

	srv, got := upstream(t)
	store, err := config.NewStore(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(config.Inbound, "default", config.Route{
		ID: "default", Name: "default", Target: srv.URL, State: config.StateActive,
	}); err != nil {
		t.Fatal(err)
	}

	handler, err := New()
	if err != nil {
		t.Fatal(err)
	}

	// /v1/ is proxied.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected /v1/ to be proxied, got %d %s", rec.Code, rec.Body.String())
	}
	if got.Path != "/v1/chat/completions" {
		t.Fatalf("unexpected forwarded path %q", got.Path)
	}

	// Non-/v1 paths are rejected and never forwarded.
	for _, p := range []string{"/", "/api/status", "/assets/app.js", "/site1/v1/chat/completions"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for %q, got %d", p, rec.Code)
		}
	}
}
