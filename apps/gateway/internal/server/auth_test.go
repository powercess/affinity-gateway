package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// TestControlPlaneRequiresToken verifies the console port protects /api/v1.
func TestControlPlaneRequiresToken(t *testing.T) {
	t.Setenv("AFFINITY_CONFIG_FILE", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("AFFINITY_TOKEN", "test-token")

	g, err := build()
	if err != nil {
		t.Fatal(err)
	}

	// No token -> 401
	rec := httptest.NewRecorder()
	g.console.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}

	// Wrong token -> 401
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	g.console.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", rec.Code)
	}

	// Correct token -> 200
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	g.console.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with token, got %d", rec.Code)
	}

	// Console port health is public.
	rec = httptest.NewRecorder()
	g.console.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz should be public, got %d", rec.Code)
	}
}
