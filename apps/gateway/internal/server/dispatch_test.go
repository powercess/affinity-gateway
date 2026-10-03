package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
)

// newGateway builds the three handlers with a temp config.
func newGateway(t *testing.T) *gateway {
	t.Helper()
	t.Setenv("AFFINITY_CONFIG_FILE", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("AFFINITY_CONSOLE_DIR", "")
	g, err := build()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestInboundPassesThroughEveryPath checks the inbound port forwards any path
// unchanged to the single inbound target.
func TestInboundPassesThroughEveryPath(t *testing.T) {
	srv, got := upstream(t)
	g := newGateway(t)
	if _, err := g.store.Update(config.Inbound, "default", config.Route{
		ID: "default", Name: "default", Target: srv.URL, State: config.StateActive,
	}); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"/v1/chat/completions", "/", "/api/status", "/assets/app.js"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{"model":"m"}`))
		req.Header.Set("Content-Type", "application/json")
		g.inbound.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %q to be proxied, got %d", p, rec.Code)
		}
		if got.Path != p {
			t.Fatalf("expected path %q forwarded, got %q", p, got.Path)
		}
	}
}

// TestEgressPortOnlyProxiesEgress checks the egress port serves /egress/ and
// rejects other paths.
func TestEgressPortOnlyProxiesEgress(t *testing.T) {
	srv, got := upstream(t)
	g := newGateway(t)
	if _, err := g.store.Update(config.Egress, "opencode", config.Route{
		ID: "opencode", Name: "opencode", Target: srv.URL, State: config.StateActive,
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/egress/opencode/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Content-Type", "application/json")
	g.egress.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || got.Path != "/v1/chat/completions" {
		t.Fatalf("egress proxy failed: status %d path %q", rec.Code, got.Path)
	}

	rec = httptest.NewRecorder()
	g.egress.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on egress for non-egress path, got %d", rec.Code)
	}
}

// TestConsolePortServesUIAndControl checks the console port hosts the control
// API (token-protected) and does not proxy AI traffic.
func TestConsolePortServesUIAndControl(t *testing.T) {
	g := newGateway(t)

	rec := httptest.NewRecorder()
	g.console.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}

	token := g.store.Token()
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	g.console.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with token, got %d", rec.Code)
	}
}
