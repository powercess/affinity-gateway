// Package server runs the three gateway listeners:
//
//   - inbound: public AI entry point (/v1/ and, for pass-through, any path)
//   - egress:  provider entry point for downstream relays (/egress/{id}/*)
//   - console: the web UI and the token-protected control API (/ui, /api/v1)
//
// Keeping them on separate ports means the public proxy port never exposes the
// console or the control plane.
package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/affinity"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/control"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/observe"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/plugin"
)

func configPath() string {
	if p := os.Getenv("AFFINITY_CONFIG_FILE"); p != "" {
		return p
	}
	return filepath.Join(".data", "config.json")
}

func dataDir() string {
	return filepath.Dir(configPath())
}

// resolveConsoleDir finds the built console assets. AFFINITY_CONSOLE_DIR wins,
// otherwise the usual workspace locations are probed.
func resolveConsoleDir() string {
	if dir := os.Getenv("AFFINITY_CONSOLE_DIR"); dir != "" {
		if isDir(dir) {
			return dir
		}
	}
	candidates := []string{
		filepath.Join("..", "console", "dist"), // run from apps/gateway
		filepath.Join("apps", "console", "dist"),
		filepath.Join("..", "..", "apps", "console", "dist"),
	}
	for _, dir := range candidates {
		if isDir(dir) {
			return dir
		}
	}
	return ""
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

type gateway struct {
	store    *config.Store
	recorder *observe.Recorder
	metrics  *observe.Metrics
	resolver *affinity.Resolver

	inbound http.Handler
	egress  http.Handler
	console http.Handler
}

// Flush persists pending runtime state.
func (g *gateway) Flush() {
	for _, flush := range []func() error{g.recorder.Flush, g.metrics.Flush, g.resolver.Flush} {
		if err := flush(); err != nil {
			log.Printf("state flush warning: %v", err)
		}
	}
}

// New builds the gateway handlers. The inbound handler is returned for tests.
func New() (http.Handler, error) {
	g, err := build()
	if err != nil {
		return nil, err
	}
	return g.inbound, nil
}

func build() (*gateway, error) {
	store, err := config.NewStore(configPath())
	if err != nil {
		return nil, err
	}

	dir := dataDir()
	recorder, err := observe.NewPersistentRecorder(filepath.Join(dir, "requests.json"), 200)
	if err != nil {
		return nil, err
	}
	metrics, err := observe.NewPersistentMetrics(filepath.Join(dir, "metrics.json"))
	if err != nil {
		return nil, err
	}
	resolver, err := affinity.NewPersistentResolver(filepath.Join(dir, "affinity.json"))
	if err != nil {
		return nil, err
	}

	registry := plugin.NewRegistry()
	if err := registry.Load(store.Plugins()); err != nil {
		log.Printf("plugin load warning: %v", err)
	}
	api := control.Handler{Store: store, Metrics: metrics, Recorder: recorder, Registry: registry}

	g := &gateway{store: store, recorder: recorder, metrics: metrics, resolver: resolver}
	g.inbound = newInboundHandler(store, registry, resolver, metrics, recorder)
	g.egress = newEgressHandler(store, registry, resolver, metrics, recorder)
	g.console = newConsoleHandler(store, api)
	return g, nil
}

// newInboundHandler proxies inbound traffic to the single inbound target. Every
// path is forwarded unchanged (plain pass-through).
func newInboundHandler(store *config.Store, registry *plugin.Registry, resolver *affinity.Resolver, metrics *observe.Metrics, recorder *observe.Recorder) http.Handler {
	proxy := proxyHandler{store: store, plugins: registry, resolver: resolver, metrics: metrics, recorder: recorder}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("/", proxy)
	return mux
}

// newEgressHandler serves only /egress/{route-id}/*.
func newEgressHandler(store *config.Store, registry *plugin.Registry, resolver *affinity.Resolver, metrics *observe.Metrics, recorder *observe.Recorder) http.Handler {
	proxy := proxyHandler{store: store, plugins: registry, resolver: resolver, egress: true, metrics: metrics, recorder: recorder}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("/egress/", proxy)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeProxyError(w, http.StatusNotFound, "affinity-gateway egress expects /egress/{route-id}/...")
	})
	return mux
}

// newConsoleHandler serves the web UI and the token-protected control API.
func newConsoleHandler(store *config.Store, api control.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("/api/v1/", requireToken(store, api))
	if dir := resolveConsoleDir(); dir != "" {
		log.Printf("serving console at /ui from %s", dir)
		mux.Handle("/ui/", consoleHandler(dir))
		mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
		})
	} else {
		log.Print("console assets not found; /ui is unavailable")
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// requireToken protects the control plane with the gateway access token.
func requireToken(store *config.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := store.Token()
		if token == "" || tokenAuthorized(r, token) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="affinity-gateway"`)
		writeProxyError(w, http.StatusUnauthorized, "unauthorized")
	})
}

func tokenAuthorized(r *http.Request, token string) bool {
	if header := r.Header.Get("Authorization"); header != "" {
		const prefix = "Bearer "
		if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
			return constantEqual(strings.TrimSpace(header[len(prefix):]), token)
		}
	}
	if header := strings.TrimSpace(r.Header.Get("X-Affinity-Token")); header != "" {
		return constantEqual(header, token)
	}
	return false
}

func constantEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// consoleHandler serves the built console under the /ui prefix, with SPA
// fallback for extensionless paths.
func consoleHandler(consoleDir string) http.Handler {
	index := filepath.Join(consoleDir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/ui"))
		if clean == "/" {
			http.ServeFile(w, r, index)
			return
		}
		file := filepath.Join(consoleDir, filepath.FromSlash(clean))
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			http.ServeFile(w, r, file)
			return
		}
		// Unknown asset paths must 404; other paths fall back to the SPA.
		if path.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}

// envAddr lets tests/containers override a listener address.
func envAddr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// ListenAndServe starts the three listeners.
func ListenAndServe() error {
	g, err := build()
	if err != nil {
		return err
	}
	l := g.store.Listeners()
	inboundAddr := envAddr("AFFINITY_LISTEN", l.Inbound)
	egressAddr := envAddr("AFFINITY_EGRESS_LISTEN", l.Egress)
	consoleAddr := envAddr("AFFINITY_CONSOLE_LISTEN", l.Console)

	servers := []*http.Server{
		{Addr: inboundAddr, Handler: g.inbound},
		{Addr: egressAddr, Handler: g.egress},
		{Addr: consoleAddr, Handler: g.console},
	}
	log.Printf("inbound listening on %s", inboundAddr)
	log.Printf("egress  listening on %s", egressAddr)
	log.Printf("console listening on %s (control API: %s/ui, token required)", consoleAddr, consoleAddr)

	errCh := make(chan error, len(servers))
	var wg sync.WaitGroup
	for _, srv := range servers {
		wg.Add(1)
		go func(s *http.Server) {
			defer wg.Done()
			if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}(srv)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	var serveErr error
	select {
	case serveErr = <-errCh:
	case <-stop:
		log.Print("shutting down")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, srv := range servers {
		_ = srv.Shutdown(ctx)
	}
	wg.Wait()
	g.Flush()
	return serveErr
}
