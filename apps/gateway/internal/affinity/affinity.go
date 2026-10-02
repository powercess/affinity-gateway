// Package affinity extracts and generates soft-affinity session identities.
//
// Resolution is a fixed three-tier cascade:
//
//  1. dedicated session headers (see headerOrder);
//  2. protocol session fields in the JSON body (metadata.*, prompt_cache_key,
//     previous_response_id, ...);
//  3. a conversation fingerprint rebuilt from the chat history, so stateless
//     clients that only send the OpenAI messages array still keep a stable
//     session across turns.
//
// The Go core always decides the id; plugins decide how it is written out.
package affinity

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// Identity is a resolved soft-affinity session id and where it came from.
type Identity struct {
	ID     string
	Source string
}

// headerOrder lists request headers that may carry a stable session identity,
// most specific first. This intentionally covers the header names used by the
// common agent harnesses (OpenCode/omp, Claude Code, Hermes, DeepSeek Harness,
// generic OpenAI-compatible clients, ...).
var headerOrder = []string{
	"X-Affinity-Session-Id",
	"X-Session-Id",
	"Session-Id",
	"Session_id",
	"X-Session-Affinity",
	"Session-Affinity",
	"X-Conversation-Id",
	"Conversation-Id",
	"Conversation_id",
	"Thread-Id",
	"X-Thread-Id",
	"X-Opencode-Session",
	"X-Claude-Code-Session-Id",
	"X-Hermes-Session-Key",
	"X-Deepseek-Harness-Session-Id",
	"X-Client-Session-Id",
	"X-Chat-Session-Id",
}

var metadataKeys = []string{
	"session_id", "conversation_id", "sessionId", "conversationId",
	"thread_id", "threadId", "session", "conversation",
}

var topLevelKeys = []string{
	"session_id", "conversation_id", "conversation", "session",
	"thread_id", "threadId", "prompt_cache_key", "previous_response_id",
}

// FromHeaders returns the first stable identity found in known headers.
func FromHeaders(h http.Header) (Identity, bool) {
	for _, name := range headerOrder {
		if v := strings.TrimSpace(h.Get(name)); v != "" {
			return Identity{ID: v, Source: "header:" + strings.ToLower(name)}, true
		}
	}
	return Identity{}, false
}

// FromMetadata reads an identity from structured JSON body fields without
// modifying the body.
func FromMetadata(body []byte) (Identity, bool) {
	if len(body) == 0 {
		return Identity{}, false
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return Identity{}, false
	}
	if meta, ok := payload["metadata"].(map[string]any); ok {
		for _, key := range metadataKeys {
			if v, ok := meta[key].(string); ok && strings.TrimSpace(v) != "" {
				return Identity{ID: strings.TrimSpace(v), Source: "body:metadata." + key}, true
			}
		}
	}
	for _, key := range topLevelKeys {
		if v, ok := payload[key].(string); ok && strings.TrimSpace(v) != "" {
			return Identity{ID: strings.TrimSpace(v), Source: "body:" + key}, true
		}
	}
	return Identity{}, false
}

// Generate creates a fresh soft-affinity session id.
func Generate() Identity {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Identity{ID: "sess_unknown", Source: "generated"}
	}
	return Identity{ID: "sess_" + hex.EncodeToString(raw[:]), Source: "generated"}
}
