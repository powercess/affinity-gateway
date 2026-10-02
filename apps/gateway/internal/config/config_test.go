package config

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorePersistsRoutes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().Inbound) != 1 {
		t.Fatalf("expected default inbound route")
	}
	if _, err := s.Add("inbound", Route{ID: "chat", Name: "Chat", Path: "/v1/other", Target: "http://relay.example"}); err != nil {
		t.Fatal(err)
	}
	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.Snapshot().Inbound) != 2 {
		t.Fatalf("expected persisted route")
	}
}

func TestStoreCRUD(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.Add("egress", Route{ID: "openai", Name: "OpenAI", Target: "https://api.openai.com"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Path != "/egress/openai" {
		t.Fatalf("unexpected egress path %q", created.Path)
	}
	created.State = StateInactive
	if _, err := s.Update("egress", "openai", created); err != nil {
		t.Fatal(err)
	}
	route, ok := findRoute(s.Snapshot().Egress, "openai")
	if !ok {
		t.Fatal("openai route missing")
	}
	if route.State != StateInactive {
		t.Fatalf("expected inactive, got %q", route.State)
	}
	if route.Plugins == nil {
		t.Fatalf("empty plugin chain must not become null")
	}
	if err := s.Delete("egress", "openai"); err != nil {
		t.Fatal(err)
	}
	if _, ok := findRoute(s.Snapshot().Egress, "openai"); ok {
		t.Fatalf("expected openai route to be deleted")
	}
}

func findRoute(list []Route, id string) (Route, bool) {
	for _, r := range list {
		if r.ID == id {
			return r, true
		}
	}
	return Route{}, false
}

func TestSingleNoPrefixInbound(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The default inbound route has no prefix and is active.
	if _, err := s.Add("inbound", Route{ID: "dup", Name: "dup", Target: "http://a.example", State: StateActive}); err == nil {
		t.Fatalf("expected only one active no-prefix inbound route")
	}
	if _, err := s.Add("inbound", Route{ID: "site1", Name: "site1", Path: "/site1", Target: "http://a.example", State: StateActive}); err != nil {
		t.Fatalf("prefixed route should be allowed: %v", err)
	}
	if _, err := s.Add("inbound", Route{ID: "dormant", Name: "dormant", Target: "http://a.example", State: StateInactive}); err != nil {
		t.Fatalf("inactive no-prefix route should be allowed: %v", err)
	}
}

func TestValidateRejectsBadTarget(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("inbound", Route{ID: "bad", Target: "not-a-url"}); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestSnapshotKeepsEmptyListsNotNull(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("inbound", "default"); err != nil {
		t.Fatal(err)
	}
	snapshot := s.Snapshot()
	if snapshot.Inbound == nil {
		t.Fatalf("inbound must be an empty list, not null")
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"inbound":[]`) {
		t.Fatalf("expected empty JSON array, got %s", raw)
	}
}
