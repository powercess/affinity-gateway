package sessionaffinity

import (
	"errors"
	"net/http"
)

const (
	opencodeGoSessionPlugin  = "opencode-go-session"
	opencodeGoSessionVersion = "1.0.0"
)

// PluginRef pins one adapter implementation in a supplier binding.
type PluginRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// requestMutation is deliberately narrower than http.Request. Adapters return
// instructions; the Go core validates and applies them.
type requestMutation struct {
	SetHeaders    map[string]string
	RemoveHeaders []string
}

func validatePluginChain(refs []PluginRef) error {
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref.ID != opencodeGoSessionPlugin || ref.Version != opencodeGoSessionVersion {
			return errors.New("unsupported plugin id or version")
		}
		key := ref.ID + "@" + ref.Version
		if seen[key] {
			return errors.New("duplicate plugin")
		}
		seen[key] = true
	}
	return nil
}

func buildPluginMutation(ref PluginRef, secret, bindingID, affinity string) (requestMutation, error) {
	if ref.ID != opencodeGoSessionPlugin || ref.Version != opencodeGoSessionVersion {
		return requestMutation{}, errors.New("unsupported plugin id or version")
	}
	if len(secret) < 32 {
		return requestMutation{}, errors.New("gateway secret is unavailable")
	}
	if !canonicalID(affinity) {
		return requestMutation{}, errors.New("missing or invalid trusted affinity identity")
	}
	return requestMutation{SetHeaders: map[string]string{
		"x-opencode-session": "ses_" + derive(secret, "supplier-session:v1", bindingID, affinity),
	}}, nil
}

func applyMutation(header http.Header, mutation requestMutation) error {
	for name, value := range mutation.SetHeaders {
		// Version 1 adapters may only set the supplier session header. Identity
		// impersonation belongs in a separate, explicitly enabled future plugin.
		if http.CanonicalHeaderKey(name) != "X-Opencode-Session" || !validID(value) {
			return errors.New("plugin attempted an unsupported header mutation")
		}
		header.Set(name, value)
	}
	for _, name := range mutation.RemoveHeaders {
		if http.CanonicalHeaderKey(name) != http.CanonicalHeaderKey(internalHeader) {
			return errors.New("plugin attempted an unsupported header removal")
		}
		header.Del(name)
	}
	return nil
}
