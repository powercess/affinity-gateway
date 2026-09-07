package sessionaffinity

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupplierRegistryPersistsImmutableOrigins(t *testing.T) {
	r := &supplierRegistry{path: filepath.Join(t.TempDir(), "suppliers.json"), rows: map[string]Supplier{}}
	if err := r.add(Supplier{ID: "openai-main", Origin: "https://API.Example.com/"}); err != nil {
		t.Fatal(err)
	}
	if got := r.list(); len(got) != 1 || got[0].Origin != "https://api.example.com" || got[0].InternalBaseURL != "http://affinity-gateway:8237/r/openai-main" {
		t.Fatalf("unexpected view: %#v", got)
	}
	if err := r.add(Supplier{ID: "openai-main", Origin: "https://other.example.com"}); err == nil {
		t.Fatal("origin mutation was accepted")
	}
	reloaded := &supplierRegistry{rows: map[string]Supplier{}}
	if err := reloaded.configure(r.path); err != nil || len(reloaded.list()) != 1 {
		t.Fatal("persisted registry did not reload", err)
	}
	if err := reloaded.remove("openai-main"); err != nil || len(reloaded.list()) != 0 {
		t.Fatal("remove failed", err)
	}
}

func TestEgressTransparentProxy(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.URL.RawQuery != "beta=1" || r.Method != "POST" || r.Header.Get("X-Test") != "yes" {
			t.Errorf("request changed: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "payload" {
			t.Errorf("body changed: %q", body)
		}
		w.Header().Set("X-Upstream", "ok")
		w.WriteHeader(201)
		_, _ = w.Write([]byte("response"))
	}))
	defer upstream.Close()
	original := suppliers
	suppliers = &supplierRegistry{rows: map[string]Supplier{"site-a": {ID: "site-a", Origin: upstream.URL}}}
	defer func() { suppliers = original }()
	// The production validator requires HTTPS; use the TLS test origin and its client transport.
	oldTransport := http.DefaultTransport
	http.DefaultTransport = upstream.Client().Transport
	defer func() { http.DefaultTransport = oldTransport }()
	req := httptest.NewRequest("POST", "http://gateway/r/site-a/v1/messages?beta=1", strings.NewReader("payload"))
	req.Header.Set("X-Test", "yes")
	w := httptest.NewRecorder()
	if err := new(Egress).ServeHTTP(w, req, nil); err != nil {
		t.Fatal(err)
	}
	if w.Code != 201 || w.Body.String() != "response" || w.Header().Get("X-Upstream") != "ok" {
		t.Fatalf("response changed: %#v", w.Result())
	}
}

func TestSupplierInputValidation(t *testing.T) {
	for _, row := range []Supplier{{ID: "UPPER", Origin: "https://a.example"}, {ID: "ok", Origin: "http://a.example"}, {ID: "ok", Origin: "https://a.example/v1"}, {ID: "ok", Origin: "https://user:key@a.example"}} {
		r := &supplierRegistry{path: filepath.Join(t.TempDir(), "suppliers.json"), rows: map[string]Supplier{}}
		if err := r.add(row); err == nil {
			t.Fatalf("accepted %#v", row)
		}
	}
}
