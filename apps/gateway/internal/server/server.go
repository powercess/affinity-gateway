// Package server wires the shared :8236 listener: the console, inbound
// proxying, egress proxying and the control API.
package server

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
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
	handler  http.Handler
	store    *config.Store
	recorder *observe.Recorder
	metrics  *observe.Metrics
	resolver *affinity.Resolver
}

// Flush persists pending runtime state.
func (g *gateway) Flush() {
	for _, flush := range []func() error{g.recorder.Flush, g.metrics.Flush, g.resolver.Flush} {
		if err := flush(); err != nil {
			log.Printf("state flush warning: %v", err)
		}
	}
}

// New builds the gateway handler. The returned handler owns config, metrics and
// the persisted request recorder.
func New() (http.Handler, error) {
	g, err := build()
	if err != nil {
		return nil, err
	}
	return g.handler, nil
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

	inbound := proxyHandler{store: store, plugins: registry, resolver: resolver, metrics: metrics, recorder: recorder}
	egress := proxyHandler{store: store, plugins: registry, resolver: resolver, egress: true, metrics: metrics, recorder: recorder}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/api/v1/", requireToken(store, api))
	mux.Handle("/egress/", egress)
	if dir := resolveConsoleDir(); dir != "" {
		log.Printf("serving console at /ui from %s", dir)
		mux.Handle("/ui/", consoleHandler(dir))
		mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
		})
	} else {
		log.Print("console assets not found; /ui is unavailable")
	}
	// Only /v1/ is proxied on ingress. The gateway is an AI endpoint, not a web
	// reverse proxy: anything else is rejected so it can never forward normal
	// HTTP traffic (and never loop back into a downstream site).
	mux.Handle(config.InboundPrefix, inbound)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeProxyError(w, http.StatusNotFound, "affinity-gateway only proxies /v1/")
	})

	return &gateway{handler: mux, store: store, recorder: recorder, metrics: metrics, resolver: resolver}, nil
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

// ListenAndServe starts the gateway on the configured address. AFFINITY_LISTEN
// overrides the persisted value, which is useful for tests and containers.
func ListenAndServe() error {
	g, err := build()
	if err != nil {
		return err
	}
	addr := os.Getenv("AFFINITY_LISTEN")
	if addr == "" {
		addr = g.store.Listen()
	}

	server := &http.Server{Addr: addr, Handler: g.handler}
	log.Print("control API authentication is enabled")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		log.Print("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	log.Printf("affinity gateway listening on %s", addr)
	serveErr := server.ListenAndServe()
	// Flush on the main path so the state is written before the process exits.
	g.Flush()
	if serveErr != nil && serveErr != http.ErrServerClosed {
		return serveErr
	}
	return nil
}
