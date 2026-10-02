package affinity

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/persist"
)

// maxTrim is how many trailing messages may be dropped when looking for a
// previously seen conversation prefix. A normal turn appends one or two
// messages, so a small window covers the common flows.
const maxTrim = 8

type convEntry struct {
	session string
	expires time.Time
}

type storedConv struct {
	Key     string    `json:"key"`
	Session string    `json:"session"`
	Expires time.Time `json:"expires"`
}

// Resolver rebuilds a session id from chat history for clients that carry no
// session marker at all. It keeps a bounded index of conversation fingerprints,
// one current key per conversation, optionally persisted across restarts.
type Resolver struct {
	mu      sync.Mutex
	entries map[string]*convEntry
	order   []string
	max     int
	ttl     time.Duration
	persist *persist.Debouncer
}

// NewResolver returns an empty in-memory resolver.
func NewResolver() *Resolver {
	return &Resolver{
		entries: map[string]*convEntry{},
		max:     4096,
		ttl:     2 * time.Hour,
	}
}

// NewPersistentResolver loads any existing conversation index and persists
// changes.
func NewPersistentResolver(path string) (*Resolver, error) {
	resolver := NewResolver()
	var stored []storedConv
	found, err := persist.Load(path, &stored)
	if err != nil {
		return nil, err
	}
	if found {
		now := time.Now()
		for _, item := range stored {
			if item.Key == "" || item.Session == "" || now.After(item.Expires) {
				continue
			}
			resolver.entries[item.Key] = &convEntry{session: item.Session, expires: item.Expires}
			resolver.order = append(resolver.order, item.Key)
		}
	}
	resolver.persist = persist.NewDebouncer(path, 500*time.Millisecond, resolver.snapshot)
	return resolver, nil
}

// FromHistory derives (or recalls) a session id from the request body chat
// history. salt usually contains the client credential so different credentials
// never share a rebuilt session. It returns false when the body has no usable
// messages.
func (r *Resolver) FromHistory(salt string, body []byte) (Identity, bool) {
	messages := extractMessages(body)
	if len(messages) == 0 {
		return Identity{}, false
	}
	normalized := normalizeMessages(messages)
	if len(normalized) == 0 {
		return Identity{}, false
	}
	saltHash := hashString(salt)
	fullKey := fingerprint(saltHash, normalized)

	if session, ok := r.lookup(fullKey); ok {
		return Identity{ID: session, Source: "history:exact"}, true
	}

	for trim := 1; trim <= maxTrim && trim < len(normalized); trim++ {
		key := fingerprint(saltHash, normalized[:len(normalized)-trim])
		if session, ok := r.lookup(key); ok {
			r.remember(fullKey, session, key)
			return Identity{ID: session, Source: fmt.Sprintf("history:prefix-%d", trim)}, true
		}
	}

	generated := Generate()
	r.remember(fullKey, generated.ID, "")
	return Identity{ID: generated.ID, Source: "history:new"}, true
}

// Flush writes pending state to disk.
func (r *Resolver) Flush() error {
	return r.persist.Flush()
}

func (r *Resolver) lookup(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expires) {
		delete(r.entries, key)
		return "", false
	}
	return entry.session, true
}

func (r *Resolver) remember(key, session, drop string) {
	r.mu.Lock()
	if drop != "" && drop != key {
		delete(r.entries, drop)
	}
	r.entries[key] = &convEntry{session: session, expires: time.Now().Add(r.ttl)}
	r.order = append(r.order, key)
	for len(r.entries) > r.max && len(r.order) > 0 {
		oldest := r.order[0]
		r.order = r.order[1:]
		if _, ok := r.entries[oldest]; ok {
			delete(r.entries, oldest)
		}
	}
	r.mu.Unlock()
	r.persist.Touch()
}

func (r *Resolver) snapshot() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]storedConv, 0, len(r.entries))
	for key, entry := range r.entries {
		out = append(out, storedConv{Key: key, Session: entry.session, Expires: entry.expires})
	}
	return out
}

func extractMessages(body []byte) []any {
	if len(body) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	messages, _ := payload["messages"].([]any)
	return messages
}

func normalizeMessages(messages []any) []string {
	out := make([]string, 0, len(messages))
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			out = append(out, mustJSON(raw))
			continue
		}
		var builder strings.Builder
		role, _ := message["role"].(string)
		builder.WriteString(role)
		builder.WriteByte(0)
		builder.WriteString(normalizeContent(message["content"]))
		if toolCalls, ok := message["tool_calls"]; ok {
			builder.WriteString("|tc=")
			builder.WriteString(mustJSON(toolCalls))
		}
		if name, ok := message["name"]; ok {
			builder.WriteString("|name=")
			builder.WriteString(fmt.Sprint(name))
		}
		if toolCallID, ok := message["tool_call_id"]; ok {
			builder.WriteString("|tcid=")
			builder.WriteString(fmt.Sprint(toolCallID))
		}
		out = append(out, builder.String())
	}
	return out
}

func normalizeContent(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, normalizeContent(item))
		}
		return strings.Join(parts, "\x01")
	case map[string]any:
		if text, ok := typed["text"].(string); ok {
			return text
		}
		return mustJSON(typed)
	default:
		return mustJSON(typed)
	}
}

func fingerprint(saltHash string, messages []string) string {
	hasher := sha256.New()
	hasher.Write([]byte(saltHash))
	hasher.Write([]byte{0})
	var length [4]byte
	for _, message := range messages {
		binary.BigEndian.PutUint32(length[:], uint32(len(message)))
		hasher.Write(length[:])
		hasher.Write([]byte(message))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}
