package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
)

// TestCustomPrefixRouteIsDispatched ensures non-/v1 prefixes like /site1 reach
// the inbound proxy instead of the console fallback. The path is forwarded
// unchanged (transparent pass-through).
func TestCustomPrefixRouteIsDispatched(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("AFFINITY_CONFIG_FILE", configPath)

	store, err := config.NewStore(configPath)
	if err != nil {
		t.Fatal(err)
	}
	srv, got := upstream(t)
	if _, err := store.Add(config.Inbound, config.Route{
		ID: "site1", Name: "site1", Path: "/site1", Target: srv.URL, State: config.StateActive,
	}); err != nil {
		t.Fatal(err)
	}

	handler, err := New()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/site1/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	if got.Path != "/site1/v1/chat/completions" {
		t.Fatalf("expected full path forwarded, got %q", got.Path)
	}
}
