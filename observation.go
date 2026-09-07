package sessionaffinity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// Observation records one gateway leg, not an inferred new-api routing decision.
// Headers are a fixed allowlist, and identity values are keyed fingerprints.
type Observation struct {
	ID       string            `json:"id"`
	At       time.Time         `json:"at"`
	Mode     string            `json:"mode"`
	Profile  string            `json:"profile"`
	Model    string            `json:"model,omitempty"`
	Source   string            `json:"source,omitempty"`
	Session  string            `json:"session,omitempty"`
	Policy   string            `json:"policy,omitempty"`
	Scope    string            `json:"scope,omitempty"`
	Status   int               `json:"status,omitempty"`
	Duration int64             `json:"duration"`
	Error    string            `json:"error,omitempty"`
	Before   map[string]string `json:"before"`
	After    map[string]string `json:"after"`
}
type observationKey struct{}
type observationStore struct {
	mu       sync.Mutex
	rows     []Observation
	revision uint64
	changed  chan struct{}
}

var observations = &observationStore{changed: make(chan struct{})}

const observationCapacity = 1000

func (s *observationStore) add(event Observation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.rows) == observationCapacity {
		copy(s.rows, s.rows[1:])
		s.rows = s.rows[:observationCapacity-1]
	}
	s.rows = append(s.rows, event)
	s.revision++
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *observationStore) snapshot() ([]Observation, uint64, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]Observation, len(s.rows))
	for i := range s.rows {
		rows[len(rows)-i-1] = s.rows[i]
	}
	return rows, s.revision, s.changed
}
func fingerprint(secret, value string) string {
	if secret == "" || value == "" {
		return ""
	}
	return derive(secret, "observation:v1", value)
}
func observedHeaders(r *http.Request, secret string) map[string]string {
	result := map[string]string{}
	for _, name := range sessionHeaders {
		if value := r.Header.Get(name); value != "" {
			result[name] = "fp:" + fingerprint(secret, value)
		}
	}
	// Never retain user agents, URLs, cookies, arbitrary headers, keys or bodies.
	for _, name := range []string{"Authorization", "X-Api-Key", "Cookie"} {
		if r.Header.Get(name) != "" {
			result[name] = "[redacted]"
		}
	}
	return result
}
func (h Handler) observeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return h.handleHTTP(w, r, next)
	}
	event := Observation{ID: hex.EncodeToString(id[:]), At: time.Now().UTC(), Mode: h.Mode, Profile: h.ObserveID, Policy: h.Policy, Scope: h.IdentityScope, Before: observedHeaders(r, h.secret)}
	if h.Mode == "inbound" {
		event.Policy = ""
	}
	if h.Mode == "outbound" {
		event.Session = fingerprint(h.secret, r.Header.Get(internalHeader))
	}
	r = r.WithContext(context.WithValue(r.Context(), observationKey{}, &event))
	recorder := caddyhttp.NewResponseRecorder(w, nil, nil)
	// Snapshot after identity processing but before reverse_proxy can change headers.
	downstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		event.After = observedHeaders(r, h.secret)
		if h.Mode == "inbound" {
			event.Session = fingerprint(h.secret, r.Header.Get(internalHeader))
		}
		return next.ServeHTTP(w, r)
	})
	err := h.handleHTTP(recorder, r, downstream)
	event.Duration = time.Since(event.At).Milliseconds()
	event.Status = recorder.Status()
	if err != nil {
		event.Error = "upstream_handler_error"
	} else if event.Status == 0 {
		event.Status = http.StatusOK
	}
	if code := recorder.Header().Get("X-Affinity-Error"); code != "" {
		// Only locally generated, fixed error codes are retained.
		if event.After == nil {
			event.Error = code
		}
	}
	observations.add(event)
	return err
}

// Console serves bounded process-local observations. Deploy on an internal
// listener with TLS at the access boundary. Authentication is mandatory.
type Console struct {
	PasswordEnv string `json:"password_env,omitempty"`
	ConfigEnv   string `json:"config_env,omitempty"`
	password    string
}

func (Console) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.affinity_console", New: func() caddy.Module { return new(Console) }}
}
func init() {
	caddy.RegisterModule(Console{})
	httpcaddyfile.RegisterHandlerDirective("affinity_console", func(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
		m := new(Console)
		return m, m.UnmarshalCaddyfile(h.Dispenser)
	})
}
func (c *Console) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if !d.NextArg() {
			return d.ArgErr()
		}
		c.PasswordEnv = d.Val()
		if d.NextArg() {
			c.ConfigEnv = d.Val()
		}
		if d.NextArg() {
			return d.ArgErr()
		}
		if d.NextBlock(0) {
			return d.Err("blocks are not supported")
		}
	}
	return nil
}
func (c *Console) Provision(caddy.Context) error {
	c.password = os.Getenv(c.PasswordEnv)
	if len(c.password) < 32 {
		return fmt.Errorf("affinity_console password env must contain at least 32 bytes")
	}
	if c.ConfigEnv != "" {
		if err := suppliers.configure(os.Getenv(c.ConfigEnv)); err != nil {
			return err
		}
	}
	return nil
}
func (c *Console) ServeHTTP(w http.ResponseWriter, r *http.Request, _ caddyhttp.Handler) error {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	user, password, ok := r.BasicAuth()
	provided, expected := sha256.Sum256([]byte(password)), sha256.Sum256([]byte(c.password))
	if !ok || user != "admin" || subtle.ConstantTimeCompare(provided[:], expected[:]) != 1 || len(c.password) < 32 {
		w.Header().Set("WWW-Authenticate", `Basic realm="Affinity", charset="UTF-8"`)
		http.Error(w, "Unauthorized", 401)
		return nil
	}
	switch r.URL.Path {
	case "/api/observations":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", 405)
			return nil
		}
		rows, rev, _ := observations.snapshot()
		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(map[string]any{"items": rows, "revision": rev, "capacity": observationCapacity, "retention": "process"})
	case "/api/events":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", 405)
			return nil
		}
		w.Header().Set("Content-Type", "text/event-stream")
		controller := http.NewResponseController(w)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			_, rev, changed := observations.snapshot()
			// Invalidations always cause a full snapshot fetch, including reconnects.
			if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil && err != http.ErrNotSupported {
				return err
			}
			if _, err := fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", strconv.FormatUint(rev, 10)); err != nil {
				return err
			}
			if err := controller.Flush(); err != nil {
				return err
			}
			select {
			case <-r.Context().Done():
				return nil
			case <-changed:
			case <-ticker.C:
			}
		}
	case "/api/suppliers":
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			return json.NewEncoder(w).Encode(map[string]any{"items": suppliers.list()})
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "Method not allowed", 405)
			return nil
		}
		if !sameOrigin(r) {
			http.Error(w, "Invalid origin", 403)
			return nil
		}
		row, err := decodeSupplier(r)
		if err != nil {
			http.Error(w, "Invalid supplier", 400)
			return nil
		}
		if err = suppliers.add(row); err != nil {
			http.Error(w, err.Error(), 409)
			return nil
		}
		w.WriteHeader(http.StatusCreated)
		return json.NewEncoder(w).Encode(map[string]any{"items": suppliers.list()})
	default:
		if strings.HasPrefix(r.URL.Path, "/api/suppliers/") {
			if r.Method != http.MethodDelete {
				w.Header().Set("Allow", "DELETE")
				http.Error(w, "Method not allowed", 405)
				return nil
			}
			if !sameOrigin(r) {
				http.Error(w, "Invalid origin", 403)
				return nil
			}
			id := strings.TrimPrefix(r.URL.Path, "/api/suppliers/")
			if !validSupplierID(id) {
				http.NotFound(w, r)
				return nil
			}
			if err := suppliers.remove(id); errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
				return nil
			} else if err != nil {
				http.Error(w, "Cannot persist supplier", 500)
				return nil
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}
		http.NotFound(w, r)
		return nil
	}
}
