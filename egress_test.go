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
	plugins := []PluginRef{{ID: opencodeGoSessionPlugin, Version: opencodeGoSessionVersion}}
	if err := r.add(Supplier{ID: "openai-main", Origin: "https://API.Example.com/", Plugins: plugins}); err != nil {
		t.Fatal(err)
	}
	if got := r.list(); len(got) != 1 || got[0].Origin != "https://api.example.com" || got[0].InternalBaseURL != "http://affinity-gateway:8237/r/openai-main" || len(got[0].Plugins) != 1 {
		t.Fatalf("unexpected view: %#v", got)
	}
	if err := r.add(Supplier{ID: "openai-main", Origin: "https://other.example.com"}); err == nil {
		t.Fatal("origin mutation was accepted")
	}
	reloaded := &supplierRegistry{rows: map[string]Supplier{}}
	if err := reloaded.configure(r.path); err != nil || len(reloaded.list()) != 1 {
		t.Fatal("persisted registry did not reload", err)
	}
	if err := reloaded.updatePlugins("openai-main", nil); err != nil || len(reloaded.list()[0].Plugins) != 0 {
		t.Fatal("plugin update failed", err)
	}
	updated := &supplierRegistry{rows: map[string]Supplier{}}
	if err := updated.configure(r.path); err != nil || len(updated.list()[0].Plugins) != 0 {
		t.Fatal("plugin update did not persist", err)
	}
	if err := reloaded.remove("openai-main"); err != nil || len(reloaded.list()) != 0 {
		t.Fatal("remove failed", err)
	}
}

func TestEgressTransparentProxy(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.URL.RawQuery != "beta=1" || r.Method != "POST" || r.Header.Get("X-Test") != "yes" || r.Header.Get(internalHeader) != "" {
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
	req.Header.Set(internalHeader, "sa:v1:"+strings.Repeat("a", 64))
	w := httptest.NewRecorder()
	if err := new(Egress).ServeHTTP(w, req, nil); err != nil {
		t.Fatal(err)
	}
	if w.Code != 201 || w.Body.String() != "response" || w.Header().Get("X-Upstream") != "ok" {
		t.Fatalf("response changed: %#v", w.Result())
	}
}

func TestEgressOpenCodeGoPlugin(t *testing.T) {
	secret := strings.Repeat("s", 32)
	affinity := "sa:v1:" + strings.Repeat("a", 64)
	var first string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("x-opencode-session")
		if got == "" || !strings.HasPrefix(got, "ses_") {
			t.Errorf("missing derived OpenCode session: %q", got)
		}
		if first == "" {
			first = got
		} else if got != first {
			t.Errorf("session changed: %q != %q", got, first)
		}
		if r.Header.Get(internalHeader) != "" {
			t.Error("internal affinity header leaked")
		}
		if r.Header.Get("User-Agent") != "real-harness/2" || r.Header.Get("x-opencode-client") != "pi" || r.Header.Get("x-opencode-project") != "project-a" {
			t.Error("client identity headers were not passed through")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	original := suppliers
	suppliers = &supplierRegistry{rows: map[string]Supplier{"go-main": {ID: "go-main", Origin: upstream.URL, Plugins: []PluginRef{{ID: opencodeGoSessionPlugin, Version: opencodeGoSessionVersion}}}}}
	defer func() { suppliers = original }()
	oldTransport := http.DefaultTransport
	http.DefaultTransport = upstream.Client().Transport
	defer func() { http.DefaultTransport = oldTransport }()
	for range 2 {
		req := httptest.NewRequest("POST", "http://gateway/r/go-main/v1/chat/completions", strings.NewReader("{}"))
		req.Header.Set(internalHeader, affinity)
		req.Header.Set("User-Agent", "real-harness/2")
		req.Header.Set("x-opencode-client", "pi")
		req.Header.Set("x-opencode-project", "project-a")
		w := httptest.NewRecorder()
		if err := (&Egress{secret: secret}).ServeHTTP(w, req, nil); err != nil || w.Code != http.StatusOK {
			t.Fatalf("request failed: %v, %d", err, w.Code)
		}
	}
}

func TestEgressOpenCodeGoPluginFailsClosed(t *testing.T) {
	original := suppliers
	suppliers = &supplierRegistry{rows: map[string]Supplier{"go-main": {ID: "go-main", Origin: "https://example.com", Plugins: []PluginRef{{ID: opencodeGoSessionPlugin, Version: opencodeGoSessionVersion}}}}}
	defer func() { suppliers = original }()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://gateway/r/go-main/v1/chat/completions", strings.NewReader("{}"))
	if err := (&Egress{secret: strings.Repeat("s", 32)}).ServeHTTP(w, req, nil); err != nil || w.Code != http.StatusBadRequest || w.Header().Get("X-Affinity-Error") != "affinity_internal_invalid" {
		t.Fatalf("unexpected rejection: %v %d %s", err, w.Code, w.Body.String())
	}
}

func TestEgressOpenCodeGoPluginAllowsSessionlessModelDiscovery(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/zen/v1/models" {
			t.Errorf("unexpected model discovery request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-opencode-session"); got != "" {
			t.Errorf("model discovery received a supplier session: %q", got)
		}
		if got := r.Header.Get(internalHeader); got != "" {
			t.Errorf("internal affinity header leaked: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
	}))
	defer upstream.Close()
	original := suppliers
	suppliers = &supplierRegistry{rows: map[string]Supplier{"go-main": {ID: "go-main", Origin: upstream.URL, Plugins: []PluginRef{{ID: opencodeGoSessionPlugin, Version: opencodeGoSessionVersion}}}}}
	defer func() { suppliers = original }()
	oldTransport := http.DefaultTransport
	http.DefaultTransport = upstream.Client().Transport
	defer func() { http.DefaultTransport = oldTransport }()

	req := httptest.NewRequest(http.MethodGet, "http://gateway/r/go-main/zen/v1/models", nil)
	req.Header.Set("x-opencode-session", "ses_untrusted")
	w := httptest.NewRecorder()
	if err := (&Egress{secret: strings.Repeat("s", 32)}).ServeHTTP(w, req, nil); err != nil || w.Code != http.StatusOK {
		t.Fatalf("model discovery failed: %v, %d, %s", err, w.Code, w.Body.String())
	}
}

func TestSupplierInputValidation(t *testing.T) {
	for _, row := range []Supplier{{ID: "UPPER", Origin: "https://a.example"}, {ID: "ok", Origin: "http://a.example"}, {ID: "ok", Origin: "https://a.example/v1"}, {ID: "ok", Origin: "https://user:key@a.example"}, {ID: "ok", Origin: "https://a.example", Plugins: []PluginRef{{ID: "unknown", Version: "1.0.0"}}}} {
		r := &supplierRegistry{path: filepath.Join(t.TempDir(), "suppliers.json"), rows: map[string]Supplier{}}
		if err := r.add(row); err == nil {
			t.Fatalf("accepted %#v", row)
		}
	}
}
