package sessionaffinity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

var sessionHeaders = []string{"Session-Id", "Session_id", "Conversation-Id", "Conversation_id", "X-Session-Affinity", "X-Session-Id", "X-Opencode-Session", "Thread-Id", "X-Claude-Code-Session-Id"}

const internalHeader = "X-Session-Affinity"

// derive uses length framing so separators in untrusted IDs cannot collide.
func derive(secret string, parts ...string) string {
	h := hmac.New(sha256.New, []byte(secret))
	for _, p := range parts {
		fmt.Fprintf(h, "%d:", len(p))
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validID(s string) bool {
	if len(s) == 0 || len(s) > 512 || strings.TrimSpace(s) != s {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}

// Reject duplicate JSON keys at any level instead of silently choosing the last
// identity while a downstream parser might choose the first.
func uniqueJSON(b []byte) error {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 128 {
			return fmt.Errorf("JSON nesting too deep")
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		if delim == '{' {
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				k, ok := key.(string)
				if !ok || seen[k] {
					return fmt.Errorf("duplicate JSON key")
				}
				seen[k] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		} else if delim == '[' {
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		} else {
			return fmt.Errorf("invalid JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func metadataSession(obj map[string]json.RawMessage) (string, error) {
	if len(obj["metadata"]) == 0 || string(obj["metadata"]) == "null" {
		return "", nil
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(obj["metadata"], &metadata); err != nil {
		return "", err
	}
	var user string
	if len(metadata["user_id"]) == 0 {
		return "", nil
	}
	if err := json.Unmarshal(metadata["user_id"], &user); err != nil {
		return "", err
	}
	if !strings.HasPrefix(strings.TrimSpace(user), "{") {
		return "", nil
	}
	if err := uniqueJSON([]byte(user)); err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(user), &fields); err != nil {
		return "", err
	}
	var sid string
	if len(fields["session_id"]) > 0 {
		if err := json.Unmarshal(fields["session_id"], &sid); err != nil || !validID(sid) {
			return "", fmt.Errorf("invalid metadata session")
		}
	}
	return sid, nil
}

// Resource references (conversation / previous_response_id) are never rewritten.
func isolateBody(b []byte, secret, site, canonical string) ([]byte, error) {
	if len(b) == 0 {
		return b, nil
	}
	if err := uniqueJSON(b); err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("JSON object required")
	}
	changed := false
	if raw, ok := obj["prompt_cache_key"]; ok && string(raw) != "null" {
		var key string
		if json.Unmarshal(raw, &key) != nil || !validID(key) {
			return nil, fmt.Errorf("invalid prompt_cache_key")
		}
		// Preserve cache grouping semantics within a canonical session, isolate sites.
		obj["prompt_cache_key"], _ = json.Marshal(derive(secret, "cache:v1", site, canonical, key))
		changed = true
	}
	sid, err := metadataSession(obj)
	if err != nil {
		return nil, err
	}
	if sid != "" {
		var metadata map[string]json.RawMessage
		_ = json.Unmarshal(obj["metadata"], &metadata)
		var user string
		_ = json.Unmarshal(metadata["user_id"], &user)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal([]byte(user), &fields)
		fields["session_id"], _ = json.Marshal(derive(secret, "outbound:v1", site, canonical))
		encoded, _ := json.Marshal(fields)
		metadata["user_id"], _ = json.Marshal(string(encoded))
		obj["metadata"], _ = json.Marshal(metadata)
		changed = true
	}
	if !changed {
		return b, nil
	}
	return json.Marshal(obj)
}

// bodySession only recognizes explicit session structures; user IDs are not
// silently promoted to conversation IDs. JSON decoding never rewrites the body.
func bodySession(b []byte) string {
	var obj struct {
		Conversation json.RawMessage `json:"conversation"`
		Metadata     struct {
			UserID string `json:"user_id"`
		} `json:"metadata"`
	}
	if json.Unmarshal(b, &obj) != nil {
		return ""
	}
	var conv string
	if json.Unmarshal(obj.Conversation, &conv) == nil && validID(conv) {
		return conv
	}
	var ref struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(obj.Conversation, &ref) == nil && validID(ref.ID) {
		return ref.ID
	}
	var cc struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal([]byte(obj.Metadata.UserID), &cc) == nil && validID(cc.SessionID) {
		return cc.SessionID
	}
	return ""
}

func canonicalID(s string) bool {
	if !strings.HasPrefix(s, "sa:v1:") || len(s) != 70 {
		return false
	}
	_, err := hex.DecodeString(s[6:])
	return err == nil
}
