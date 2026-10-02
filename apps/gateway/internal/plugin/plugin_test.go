package plugin

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
)

func TestOpenCodeSessionIDShape(t *testing.T) {
	id := OpenCodeSessionID("secret", "opencode", "affinity-1")
	if len(id) != 30 {
		t.Fatalf("expected 30 chars, got %d (%q)", len(id), id)
	}
	if !strings.HasPrefix(id, "ses_") {
		t.Fatalf("expected ses_ prefix, got %q", id)
	}
	if !regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`).MatchString(id) {
		t.Fatalf("unexpected shape %q", id)
	}
	if again := OpenCodeSessionID("secret", "opencode", "affinity-1"); again != id {
		t.Fatalf("expected deterministic id")
	}
	other := OpenCodeSessionID("secret", "opencode", "affinity-2")
	if other == id {
		t.Fatalf("different affinity ids must map to different sessions")
	}
}

func TestRunChainMapsSession(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Load(config.Default().Plugins); err != nil {
		t.Fatal(err)
	}
	rc := &Context{
		Direction: config.Egress,
		RouteID:   "opencode",
		Session:   "affinity-1",
		Headers:   map[string]string{},
		Query:     map[string]string{},
	}
	result, err := registry.RunChain(context.Background(), []string{"opencode.session"}, rc, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "continue" {
		t.Fatalf("unexpected action %q", result.Action)
	}
	want := OpenCodeSessionID("secret", "opencode", "affinity-1")
	if got := rc.Headers["x-opencode-session"]; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestSandboxBlocksOSAndIO(t *testing.T) {
	program, err := Compile("evil", `function handle(ctx)
  local f = io.open("/etc/passwd")
  return "continue"
end`)
	if err != nil {
		t.Fatal(err)
	}
	rc := &Context{Direction: config.Inbound, Headers: map[string]string{}, Query: map[string]string{}}
	if _, err := program.Run(context.Background(), rc, "secret"); err == nil {
		t.Fatalf("expected sandbox to block io access")
	}
}

func TestValidateSourceRejectsBrokenLua(t *testing.T) {
	if err := ValidateSource("bad", "function handle(ctx) return"); err == nil {
		t.Fatalf("expected compile error")
	}
	if err := ValidateSource("nohandle", "x = 1"); err == nil {
		t.Fatalf("expected missing handle error")
	}
	if err := ValidateSource("ok", `function handle(ctx) return "continue" end`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
