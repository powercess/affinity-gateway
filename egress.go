package sessionaffinity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

type Supplier struct {
	ID        string    `json:"id"`
	Origin    string    `json:"origin"`
	CreatedAt time.Time `json:"created_at"`
}

type supplierView struct {
	Supplier
	InternalBaseURL string `json:"internal_base_url"`
}

type supplierRegistry struct {
	mu   sync.RWMutex
	path string
	rows map[string]Supplier
}

var suppliers = &supplierRegistry{rows: map[string]Supplier{}}

func validSupplierID(s string) bool {
	if len(s) == 0 || len(s) > 48 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

func normalizeOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("origin must be an HTTPS origin without credentials, path, query or fragment")
	}
	return "https://" + strings.ToLower(u.Host), nil
}

func (s *supplierRegistry) configure(path string) error {
	if path == "" {
		return errors.New("supplier config path is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "" && s.path != path {
		return errors.New("supplier config path differs between gateway modules")
	}
	s.path = path
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.rows = map[string]Supplier{}
		return nil
	}
	if err != nil {
		return err
	}
	var rows []Supplier
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&rows); err != nil {
		return fmt.Errorf("load suppliers: %w", err)
	}
	next := make(map[string]Supplier, len(rows))
	for _, row := range rows {
		origin, err := normalizeOrigin(row.Origin)
		if !validSupplierID(row.ID) || err != nil || next[row.ID].ID != "" {
			return fmt.Errorf("invalid supplier %q", row.ID)
		}
		row.Origin = origin
		next[row.ID] = row
	}
	s.rows = next
	return nil
}

func (s *supplierRegistry) persistLocked() error {
	rows := make([]Supplier, 0, len(s.rows))
	for _, row := range s.rows {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".suppliers-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(raw, '\n'))
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

func (s *supplierRegistry) list() []supplierView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := make([]supplierView, 0, len(s.rows))
	for _, row := range s.rows {
		rows = append(rows, supplierView{Supplier: row, InternalBaseURL: "http://affinity-gateway:8237/r/" + row.ID})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func (s *supplierRegistry) add(row Supplier) error {
	if !validSupplierID(row.ID) {
		return errors.New("id must start with a lowercase letter and contain only lowercase letters, numbers or hyphens")
	}
	origin, err := normalizeOrigin(row.Origin)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[row.ID]; ok {
		return errors.New("supplier id already exists; create a new id when changing origin")
	}
	row.Origin, row.CreatedAt = origin, time.Now().UTC()
	s.rows[row.ID] = row
	if err := s.persistLocked(); err != nil {
		delete(s.rows, row.ID)
		return err
	}
	return nil
}

func (s *supplierRegistry) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok {
		return os.ErrNotExist
	}
	delete(s.rows, id)
	if err := s.persistLocked(); err != nil {
		s.rows[id] = row
		return err
	}
	return nil
}

// Egress resolves /r/{supplier-id}/... and transparently proxies to the stored origin.
type Egress struct {
	ConfigEnv string `json:"config_env,omitempty"`
	SecretEnv string `json:"secret_env,omitempty"`
	secret    string
}

func (Egress) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.affinity_egress", New: func() caddy.Module { return new(Egress) }}
}
func init() {
	caddy.RegisterModule(Egress{})
	httpcaddyfile.RegisterHandlerDirective("affinity_egress", func(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
		m := new(Egress)
		return m, m.UnmarshalCaddyfile(h.Dispenser)
	})
	httpcaddyfile.RegisterDirectiveOrder("affinity_egress", "before", "reverse_proxy")
}
func (e *Egress) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if !d.NextArg() {
			return d.ArgErr()
		}
		e.ConfigEnv = d.Val()
		if d.NextArg() {
			e.SecretEnv = d.Val()
		}
		if d.NextArg() {
			return d.ArgErr()
		}
	}
	return nil
}
func (e *Egress) Provision(caddy.Context) error {
	e.secret = os.Getenv(e.SecretEnv)
	return suppliers.configure(os.Getenv(e.ConfigEnv))
}
func (e *Egress) ServeHTTP(w http.ResponseWriter, r *http.Request, _ caddyhttp.Handler) error {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/r/"), "/", 2)
	if len(parts) != 2 || !validSupplierID(parts[0]) {
		http.NotFound(w, r)
		return nil
	}
	suppliers.mu.RLock()
	supplier, ok := suppliers.rows[parts[0]]
	suppliers.mu.RUnlock()
	if !ok {
		http.Error(w, "Unknown supplier", http.StatusBadGateway)
		return nil
	}
	target, _ := url.Parse(supplier.Origin)
	var randomID [16]byte
	_, _ = rand.Read(randomID[:])
	event := Observation{ID: hex.EncodeToString(randomID[:]), At: time.Now().UTC(), Mode: "outbound", Profile: supplier.ID, Policy: "transparent", Session: fingerprint(e.secret, r.Header.Get(internalHeader)), Before: observedHeaders(r, e.secret)}
	event.After = observedHeaders(r, e.secret)
	recorder := caddyhttp.NewResponseRecorder(w, nil, nil)
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(req *http.Request) { req.URL.Path = "/" + parts[1]; director(req); req.Host = target.Host }
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Supplier unavailable", http.StatusBadGateway)
	}
	proxy.ServeHTTP(recorder, r)
	event.Duration = time.Since(event.At).Milliseconds()
	event.Status = recorder.Status()
	if event.Status == 0 {
		event.Status = http.StatusOK
	}
	observations.add(event)
	return nil
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && subtle.ConstantTimeCompare([]byte(strings.ToLower(u.Host)), []byte(strings.ToLower(r.Host))) == 1
}

func decodeSupplier(r *http.Request) (Supplier, error) {
	var row Supplier
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return row, errors.New("content type must be application/json")
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(&row); err != nil {
		return row, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return row, errors.New("expected one JSON object")
	}
	return row, nil
}
