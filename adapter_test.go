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
	for _, name := range []string{"User-Agent", "x-opencode-client", "x-opencode-project", "x-opencode-request"} {
		if header.Get(name) != "" {
			t.Fatalf("plugin invented %s", name)
		}
	}
}

func TestMutationValidatorRejectsIdentityImpersonation(t *testing.T) {
	header := http.Header{}
	err := applyMutation(header, requestMutation{SetHeaders: map[string]string{"User-Agent": "opencode/fake"}})
	if err == nil || header.Get("User-Agent") != "" {
		t.Fatal("identity impersonation mutation was accepted")
	}
}
