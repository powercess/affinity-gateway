package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProxyValidation(t *testing.T) {
	for _, raw := range []string{
		"http://proxy.example", "https://user:p%40ss@proxy.example:8443/",
		"socks5://user:p%3Ass@[::1]:1080", "socks5h://proxy.example:1080",
	} {
		if _, err := normalizeProxy(ProxySettings{Mode: "custom", URL: raw}, false); err != nil {
			t.Fatalf("valid proxy rejected: %v", err)
		}
	}
	for _, raw := range []string{
		"", "proxy.example:8080", "ftp://user:secret@proxy.example",
		"http://user:secret@", "http://user:secret@proxy.example/path",
		"http://user:secret@proxy.example?token=secret", "http://user:secret@proxy.example#secret",
		"http://user:secret@proxy.example:0", "http://user:secret@proxy.example:65536",
		"http://user:secret@proxy.example:abc", "http://user:secret@proxy.example:",
		"http://user:secret%zz@proxy.example", "socks5://user:secret@proxy.example/%2fpath",
		"http://::1", "http://[not-an-ip]:8080",
	} {
		_, err := normalizeProxy(ProxySettings{Mode: "custom", URL: raw}, false)
		if err == nil {
			t.Fatal("invalid proxy accepted")
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "user:") {
			t.Fatal("credentials leaked in validation error")
		}
	}
	for _, mode := range []string{"", "inherit", "CUSTOM", "secret"} {
		if _, err := normalizeProxy(ProxySettings{Mode: mode}, false); err == nil {
			t.Fatalf("invalid global mode accepted: %q", mode)
		}
	}
	for _, mode := range []string{"direct", "environment"} {
		got, err := normalizeProxy(ProxySettings{Mode: mode, URL: "secret"}, false)
		if err != nil || got.URL != "" {
			t.Fatalf("stale credentials retained for %s", mode)
		}
	}
	if _, err := normalizeRouteProxy(Egress, &ProxySettings{Mode: "environment"}); err == nil {
		t.Fatal("route environment mode accepted")
	}
	for _, mode := range []string{"direct", "custom"} {
		if _, err := normalizeRouteProxy(Inbound, &ProxySettings{Mode: mode, URL: "http://proxy.example"}); err == nil {
			t.Fatal("inbound override accepted")
		}
	}
	got, err := normalizeRouteProxy(Inbound, &ProxySettings{Mode: "inherit", URL: "secret"})
	if err != nil || got != nil {
		t.Fatal("inherit should canonicalize to nil")
	}
}

func TestProxyConfigCompatibilityAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"inbound":[],"egress":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().EgressProxy.Mode != "environment" {
		t.Fatal("legacy configuration must keep environment proxy behavior")
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.EgressProxy = ProxySettings{Mode: "unknown"} },
		func(c *Config) {
			c.EgressProxy = ProxySettings{Mode: "custom", URL: "http://user:secret@proxy.example/path"}
		},
		func(c *Config) {
			c.Egress[0].Proxy = &ProxySettings{Mode: "custom", URL: "http://user:secret@proxy.example:90000"}
		},
		func(c *Config) { c.Inbound[0].Proxy = &ProxySettings{Mode: "direct"} },
	} {
		cfg := Default()
		mutate(&cfg)
		raw, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(path); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid config must fail without leaking credentials")
		}
	}
}

func TestLoadedProxyNormalizationClearsCredentialsOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.EgressProxy = ProxySettings{Mode: "direct", URL: "credential-marker"}
	cfg.Egress[0].Proxy = &ProxySettings{Mode: "inherit", URL: "credential-marker"}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Snapshot().EgressProxy.URL != "" || store.Snapshot().Egress[0].Proxy != nil {
		t.Fatal("loaded settings were not normalized")
	}
	raw, err = os.ReadFile(path)
	if err != nil || strings.Contains(string(raw), "credential-marker") {
		t.Fatal("cleared proxy credentials remain on disk")
	}
}

func TestProxyPersistenceAndIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	global := ProxySettings{Mode: "custom", URL: "socks5h://user:p%40ss@proxy.example:1080"}
	if _, err := s.SetEgressProxy(global); err != nil {
		t.Fatal(err)
	}
	input := &ProxySettings{Mode: "custom", URL: "http://user:secret@proxy.example:8080"}
	route, err := s.Add(Egress, Route{ID: "proxy-test", Target: "http://provider.example", Proxy: input})
	if err != nil {
		t.Fatal(err)
	}
	want := *input
	input.URL = "input changed"
	route.Proxy.URL = "returned route changed"
	snapshot := s.Snapshot()
	found, _ := findRoute(snapshot.Egress, "proxy-test")
	found.Proxy.URL = "snapshot changed"
	found, _ = findRoute(s.Snapshot().Egress, "proxy-test")
	if *found.Proxy != want {
		t.Fatal("proxy pointers are aliased outside the store")
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Snapshot().EgressProxy != global {
		t.Fatal("global proxy was not persisted")
	}
	found, _ = findRoute(reloaded.Snapshot().Egress, "proxy-test")
	if found.Proxy == nil || *found.Proxy != want {
		t.Fatal("route proxy was not persisted")
	}
	found.Proxy = &ProxySettings{Mode: "inherit", URL: "secret"}
	updated, err := s.Update(Egress, found.ID, found)
	if err != nil || updated.Proxy != nil {
		t.Fatal("inherit was not cleared on update")
	}
	if _, err := s.SetEgressProxy(ProxySettings{Mode: "direct", URL: global.URL}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "p%40ss") {
		t.Fatal("disabled proxy credentials remain on disk")
	}
}

func TestFailedProxyPersistenceRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	diskBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the temporary file path reliably fails writes on every OS.
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func() error{
		"global": func() error { _, err := s.SetEgressProxy(ProxySettings{Mode: "direct"}); return err },
		"add": func() error {
			_, err := s.Add(Egress, Route{ID: "failed", Target: "http://provider.example", Proxy: &ProxySettings{Mode: "direct"}})
			return err
		},
		"update": func() error {
			r := before.Egress[0]
			r.Proxy = &ProxySettings{Mode: "direct"}
			_, err := s.Update(Egress, r.ID, r)
			return err
		},
		"delete": func() error { return s.Delete(Egress, before.Egress[0].ID) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutate(); !errors.Is(err, ErrPersistence) {
				t.Fatalf("expected persistence error, got %v", err)
			}
			if !reflect.DeepEqual(s.Snapshot(), before) {
				t.Fatal("failed persistence changed live configuration")
			}
			diskAfter, err := os.ReadFile(path)
			if err != nil || string(diskAfter) != string(diskBefore) {
				t.Fatal("failed persistence damaged saved configuration")
			}
		})
	}
}
