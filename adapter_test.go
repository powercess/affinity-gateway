package sessionaffinity

import (
	"net/http"
	"strings"
	"testing"
)

func TestOpenCodeGoMutationCannotInventClientIdentity(t *testing.T) {
	mutation, err := buildPluginMutation(PluginRef{ID: opencodeGoSessionPlugin, Version: opencodeGoSessionVersion}, strings.Repeat("s", 32), "go-main", "sa:v1:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	header := http.Header{}
	if err := applyMutation(header, mutation); err != nil {
		t.Fatal(err)
	}
	if header.Get("x-opencode-session") == "" {
		t.Fatal("supplier session was not generated")
	}
	if got := header.Get("x-opencode-session"); len(got) != 30 {
		t.Fatalf("session does not match native OpenCode length: %q", got)
	}
	for _, name := range []string{"User-Agent", "x-opencode-client", "x-opencode-project", "x-opencode-request"} {
		if header.Get(name) != "" {
			t.Fatalf("plugin invented %s", name)
		}
	}
}

func TestOpenCodeGoNativeSessionShapeAndStability(t *testing.T) {
	secret := strings.Repeat("s", 32)
	affinity := "sa:v1:" + strings.Repeat("a", 64)
	first := nativeOpenCodeSessionID(secret, "go-main", affinity)
	if second := nativeOpenCodeSessionID(secret, "go-main", affinity); first != second {
		t.Fatalf("session is not stable: %q != %q", first, second)
	}
	if len(first) != 30 || !strings.HasPrefix(first, "ses_") {
		t.Fatalf("invalid OpenCode session shape: %q", first)
	}
	for i, c := range first[4:] {
		allowed := opencodeIDBase62
		if i < 12 {
			allowed = "0123456789abcdef"
		}
		if !strings.ContainsRune(allowed, c) {
			t.Fatalf("invalid character %q at suffix offset %d in %q", c, i, first)
		}
	}
	if first == nativeOpenCodeSessionID(secret, "go-other", affinity) {
		t.Fatal("binding isolation was lost")
	}
}

func TestOpenCodeGoLegacyVersionRemainsPinned(t *testing.T) {
	mutation, err := buildPluginMutation(PluginRef{ID: opencodeGoSessionPlugin, Version: opencodeGoSessionLegacyVersion}, strings.Repeat("s", 32), "go-main", "sa:v1:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if got := mutation.SetHeaders["x-opencode-session"]; len(got) != 68 {
		t.Fatalf("legacy derivation changed: %q", got)
	}
}

func TestMutationValidatorRejectsIdentityImpersonation(t *testing.T) {
	header := http.Header{}
	err := applyMutation(header, requestMutation{SetHeaders: map[string]string{"User-Agent": "opencode/fake"}})
	if err == nil || header.Get("User-Agent") != "" {
		t.Fatal("identity impersonation mutation was accepted")
	}
}
