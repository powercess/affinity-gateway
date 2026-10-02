package control

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/observe"
)

func newHandler(t *testing.T) Handler {
	t.Helper()
	store, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return Handler{Store: store, Metrics: &observe.Metrics{}, Recorder: observe.NewRecorder(10)}
}

func TestConfigEndpoint(t *testing.T) {
	h := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestEgressCRUD(t *testing.T) {
	h := newHandler(t)

	create := httptest.NewRecorder()
	h.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/v1/egress", strings.NewReader(`{"id":"demo","name":"Demo","target":"https://example.com"}`)))
	if create.Code != http.StatusCreated {
		body, _ := io.ReadAll(create.Result().Body)
		t.Fatalf("%d %s", create.Code, body)
	}

	list := httptest.NewRecorder()
	h.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/egress", nil))
	if !strings.Contains(list.Body.String(), `"demo"`) {
		t.Fatalf("expected demo in list: %s", list.Body.String())
	}

	update := httptest.NewRecorder()
	h.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/api/v1/egress/demo", strings.NewReader(`{"name":"Demo 2","target":"https://example.com","state":"inactive"}`)))
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), `"inactive"`) {
		t.Fatalf("%d %s", update.Code, update.Body.String())
	}

	del := httptest.NewRecorder()
	h.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/api/v1/egress/demo", nil))
	if del.Code != http.StatusOK {
		t.Fatalf("delete status %d", del.Code)
	}
}

func TestPluginsEndpoint(t *testing.T) {
	h := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil))
	if !strings.Contains(rec.Body.String(), "opencode.session") {
		t.Fatalf("expected plugin catalog: %s", rec.Body.String())
	}
}
