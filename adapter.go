package sessionaffinity

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/http"
)

const (
	opencodeGoSessionPlugin        = "opencode-go-session"
	opencodeGoSessionVersion       = "1.1.0"
	opencodeGoSessionLegacyVersion = "1.0.0"
	opencodeIDBase62               = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
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
		if !supportedPlugin(ref) {
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
	if !supportedPlugin(ref) {
		return requestMutation{}, errors.New("unsupported plugin id or version")
	}
	if len(secret) < 32 {
		return requestMutation{}, errors.New("gateway secret is unavailable")
	}
	if !canonicalID(affinity) {
		return requestMutation{}, errors.New("missing or invalid trusted affinity identity")
	}
	session := "ses_" + derive(secret, "supplier-session:v1", bindingID, affinity)
	if ref.Version == opencodeGoSessionVersion {
		session = nativeOpenCodeSessionID(secret, bindingID, affinity)
	}
	return requestMutation{SetHeaders: map[string]string{"x-opencode-session": session}}, nil
}

func supportedPlugin(ref PluginRef) bool {
	return ref.ID == opencodeGoSessionPlugin &&
		(ref.Version == opencodeGoSessionVersion || ref.Version == opencodeGoSessionLegacyVersion)
}

// nativeOpenCodeSessionID matches OpenCode's visible ID shape. It remains a
// gateway-derived affinity ID and does not claim to have been issued by OpenCode.
func nativeOpenCodeSessionID(secret, bindingID, affinity string) string {
	h := hmac.New(sha256.New, []byte(secret))
	for _, part := range []string{"supplier-session:v2", bindingID, affinity} {
		h.Write([]byte{byte(len(part) >> 24), byte(len(part) >> 16), byte(len(part) >> 8), byte(len(part))})
		h.Write([]byte(part))
	}
	sum := h.Sum(nil)
	hexPart := make([]byte, 12)
	const hexChars = "0123456789abcdef"
	for i := range 6 {
		hexPart[i*2] = hexChars[sum[i]>>4]
		hexPart[i*2+1] = hexChars[sum[i]&15]
	}
	base62Part := make([]byte, 14)
	for i := range base62Part {
		base62Part[i] = opencodeIDBase62[int(sum[6+i])%len(opencodeIDBase62)]
	}
	return "ses_" + string(hexPart) + string(base62Part)
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
