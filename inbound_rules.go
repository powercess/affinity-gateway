package sessionaffinity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type HeaderRule struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Strip   bool   `json:"strip"`
}
type InboundRules struct {
	Profile      string       `json:"profile"`
	Revision     uint64       `json:"revision"`
	Mode         string       `json:"mode"`
	BodyLimit    int64        `json:"body_limit"`
	Metadata     bool         `json:"metadata"`
	Conversation bool         `json:"conversation"`
	CacheKey     bool         `json:"cache_key"`
	Headers      []HeaderRule `json:"headers"`
}
type inboundRegistry struct {
	mu       sync.RWMutex
	path     string
	saved    map[string]InboundRules
	defaults map[string]InboundRules
}

var inboundRules = &inboundRegistry{saved: map[string]InboundRules{}, defaults: map[string]InboundRules{}}

func defaultInboundRules(h Handler) InboundRules {
	r := InboundRules{Profile: h.ObserveID, Mode: "strict", BodyLimit: h.BodyLimit, Metadata: true, Conversation: true, CacheKey: h.CacheKeyAsSession}
	for _, name := range sessionHeaders {
		if name != internalHeader {
			r.Headers = append(r.Headers, HeaderRule{name, true, true})
		}
	}
	// Metadata describes the conversation; harness side-request headers may
	// describe a distinct ephemeral invocation. Ignore them, but still strip them.
	if h.IdentitySource == "metadata" {
		r.Conversation = false
		r.CacheKey = false
		for i := range r.Headers {
			r.Headers[i].Enabled = false
		}
	}
	return r
}
func validateInboundRules(r *InboundRules) error {
	if !validID(r.Profile) || (r.Mode != "strict" && r.Mode != "headers_only") || r.BodyLimit < 1 || r.BodyLimit > 16<<20 || len(r.Headers) > 64 {
		return errors.New("invalid profile, mode, body limit or header count")
	}
	seen := map[string]bool{}
	enabled := false
	for i := range r.Headers {
		h := &r.Headers[i]
		h.Name = http.CanonicalHeaderKey(h.Name)
		lower := strings.ToLower(h.Name)
		if len(h.Name) == 0 || len(h.Name) > 128 || seen[lower] {
			return errors.New("header names must be unique and nonempty")
		}
		for _, c := range h.Name {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
				return errors.New("invalid HTTP header name")
			}
		}
		// Only dedicated identity headers may be configured; never strip framing or credentials.
		switch lower {
		case "authorization", "proxy-authorization", "x-api-key", "api-key", "cookie", "set-cookie", "host", "connection", "content-length", "content-type", "content-encoding", "transfer-encoding", "te", "trailer", "upgrade", "expect", "accept", "accept-encoding", "user-agent", "origin", "referer", "forwarded", "x-session-affinity":
			return fmt.Errorf("reserved header: %s", h.Name)
		}
		if strings.HasPrefix(lower, "x-forwarded-") || strings.HasPrefix(lower, "sec-") {
			return fmt.Errorf("reserved header: %s", h.Name)
		}
		seen[lower] = true
		enabled = enabled || h.Enabled
	}
	if !enabled && (r.Mode == "headers_only" || (!r.Metadata && !r.Conversation && !r.CacheKey)) {
		return errors.New("at least one identity source must be enabled")
	}
	return nil
}
func (s *inboundRegistry) configure(path string) error {
	if path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == path {
		return nil
	}
	if s.path != "" {
		return errors.New("inbound config path differs between modules")
	}
	data, err := os.ReadFile(path)
	next := map[string]InboundRules{}
	if err == nil {
		if err = uniqueJSON(data); err != nil {
			return err
		}
		d := json.NewDecoder(strings.NewReader(string(data)))
		d.DisallowUnknownFields()
		if err = d.Decode(&next); err != nil {
			return err
		}
		for key, r := range next {
			if key != r.Profile {
				return errors.New("invalid inbound profile")
			}
			if err = validateInboundRules(&r); err != nil {
				return err
			}
			next[key] = r
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.path = path
	s.saved = next
	return nil
}
func (s *inboundRegistry) register(h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaults[h.ObserveID] = defaultInboundRules(h)
}
func cloneRules(r InboundRules) InboundRules {
	r.Headers = append([]HeaderRule(nil), r.Headers...)
	return r
}
func (s *inboundRegistry) get(h Handler) InboundRules {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.saved[h.ObserveID]; ok && h.ObserveID != "" {
		return cloneRules(r)
	}
	return defaultInboundRules(h)
}
func (s *inboundRegistry) list() []InboundRules {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := []InboundRules{}
	for id, r := range s.defaults {
		if saved, ok := s.saved[id]; ok {
			r = saved
		}
		rows = append(rows, cloneRules(r))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Profile < rows[j].Profile })
	return rows
}

// response keeps effective rules and their provenance in one registry snapshot.
func (s *inboundRegistry) response() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := []InboundRules{}
	sources := map[string]string{}
	for id, defaults := range s.defaults {
		r := defaults
		sources[id] = "default"
		if saved, ok := s.saved[id]; ok {
			r = saved
			sources[id] = "saved"
		}
		rows = append(rows, cloneRules(r))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Profile < rows[j].Profile })
	return map[string]any{"items": rows, "sources": sources}
}

func (s *inboundRegistry) update(r InboundRules) error {
	if err := validateInboundRules(&r); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.defaults[r.Profile]; !ok {
		return errors.New("unknown inbound profile")
	}
	if s.path == "" {
		return errors.New("persistent rule storage is not configured")
	}
	if s.saved[r.Profile].Revision != r.Revision {
		return errors.New("rules changed; reload before saving")
	}
	r.Revision++
	next := map[string]InboundRules{}
	for k, v := range s.saved {
		next[k] = v
	}
	next[r.Profile] = cloneRules(r)
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".inbound-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	s.saved = next
	return nil
}
func (c *Console) serveInboundRules(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		return json.NewEncoder(w).Encode(inboundRules.response())
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "Method not allowed", 405)
		return nil
	}
	if !sameOrigin(r) {
		http.Error(w, "Invalid origin", 403)
		return nil
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
	if err != nil || uniqueJSON(raw) != nil {
		http.Error(w, "Invalid rules", 400)
		return nil
	}
	var rules InboundRules
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err = d.Decode(&rules); err != nil {
		http.Error(w, "Invalid rules", 400)
		return nil
	}
	if err = inboundRules.update(rules); err != nil {
		http.Error(w, err.Error(), 409)
		return nil
	}
	return json.NewEncoder(w).Encode(inboundRules.response())
}

// Preview evaluates synthetic input without adding observations or storing request values.
func (c *Console) previewInboundRules(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", 405)
		return nil
	}
	if !sameOrigin(r) {
		http.Error(w, "Invalid origin", 403)
		return nil
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil || uniqueJSON(raw) != nil {
		http.Error(w, "Invalid preview", 400)
		return nil
	}
	var input struct {
		Rules   InboundRules      `json:"rules"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err = d.Decode(&input); err != nil {
		http.Error(w, "Invalid preview", 400)
		return nil
	}
	if err = validateInboundRules(&input.Rules); err != nil {
		http.Error(w, err.Error(), 400)
		return nil
	}
	request, _ := http.NewRequest(http.MethodPost, "http://preview/v1/messages", strings.NewReader(input.Body))
	request.Header.Set("Authorization", "preview-only-not-a-credential")
	for _, rule := range input.Rules.Headers {
		for name, value := range input.Headers {
			if strings.EqualFold(name, rule.Name) {
				request.Header.Set(rule.Name, value)
			}
		}
	}
	event := &Observation{}
	request = request.WithContext(context.WithValue(request.Context(), observationKey{}, event))
	h := Handler{Mode: "inbound", BodyLimit: input.Rules.BodyLimit, inbound: &input.Rules, secret: "preview-only-not-a-production-secret"}
	before := h.observationHeaders(request)
	var after map[string]string
	recorder := httptest.NewRecorder()
	err = h.handleHTTP(recorder, request, caddyhttp.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) error {
		after = h.observationHeaders(request)
		return nil
	}))
	if err != nil {
		http.Error(w, "Preview failed", 500)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{"status": recorder.Code, "error": recorder.Header().Get("X-Affinity-Error"), "source": event.Source, "before": before, "after": after})
}
