package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/affinity"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/observe"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/plugin"
)

// maxInspectBytes bounds how much request body the gateway reads for session,
// model and plugin metadata. Anything larger is streamed through untouched.
const maxInspectBytes = 1 << 20

type proxyHandler struct {
	store    *config.Store
	plugins  *plugin.Registry
	resolver *affinity.Resolver
	egress   bool
	metrics  *observe.Metrics
	recorder *observe.Recorder
}

func (p proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if p.metrics != nil {
		p.metrics.IncRequests()
		if p.egress {
			p.metrics.IncEgress()
		}
	}

	rawBody := readJSONBody(r)
	identity, hasIdentity := affinity.FromHeaders(r.Header)
	if !hasIdentity {
		if id, ok := affinity.FromMetadata(rawBody); ok {
			identity, hasIdentity = id, true
		}
	}
	if !hasIdentity && !p.egress && p.resolver != nil {
		if id, ok := p.resolver.FromHistory(r.Header.Get("Authorization"), rawBody); ok {
			identity, hasIdentity = id, true
		}
	}
	if !hasIdentity && !p.egress {
		identity = affinity.Generate()
		hasIdentity = true
	}
	if hasIdentity && p.metrics != nil {
		p.metrics.ObserveSession(identity.ID)
	}

	cfg := p.store.Snapshot()
	var (
		route config.Route
		rest  string
		ok    bool
	)
	if p.egress {
		route, rest, ok = resolveEgress(cfg, r.URL.Path)
	} else {
		// Inbound paths are matched only to pick a route; the request path is
		// always forwarded unchanged (transparent pass-through).
		route, ok = resolveInbound(cfg, r.URL.Path)
		rest = r.URL.Path
	}

	record := func(status int, errorMessage string) {
		if p.recorder == nil {
			return
		}
		p.recorder.Add(observe.Record{
			Time:           start,
			Direction:      directionOf(p.egress),
			Route:          route.ID,
			Method:         r.Method,
			Path:           r.URL.Path,
			Model:          extractModel(rawBody),
			Status:         status,
			Session:        identity.ID,
			SessionSource:  identity.Source,
			LatencyMS:      time.Since(start).Milliseconds(),
			Target:         route.Target,
			RequestHeaders: redactHeaders(r.Header),
			Error:          errorMessage,
		})
	}

	if !ok {
		record(http.StatusNotFound, "route not configured")
		writeProxyError(w, http.StatusNotFound, "route not configured")
		return
	}

	u, err := url.Parse(route.Target)
	if err != nil || u.Scheme == "" || u.Host == "" {
		record(http.StatusBadGateway, "invalid route target")
		writeProxyError(w, http.StatusBadGateway, "invalid route target")
		return
	}

	if p.plugins != nil && len(route.Plugins) > 0 {
		rc := &plugin.Context{
			Direction:     directionOf(p.egress),
			RouteID:       route.ID,
			Session:       identity.ID,
			SessionSource: identity.Source,
			Method:        r.Method,
			Path:          r.URL.Path,
			Query:         queryMap(r.URL.Query()),
			Headers:       headerMap(r.Header),
			Body:          rawBody,
		}
		if !p.egress && hasIdentity {
			rc.SetHeader("x-affinity-session-id", identity.ID)
		}
		result, runErr := p.plugins.RunChain(r.Context(), route.Plugins, rc, p.store.Secret())
		if runErr != nil {
			record(http.StatusInternalServerError, runErr.Error())
			writeProxyError(w, http.StatusInternalServerError, runErr.Error())
			return
		}
		if result.Action == "reject" {
			status := result.Status
			if status == 0 {
				status = http.StatusForbidden
			}
			message := result.Message
			if message == "" {
				message = "request rejected by plugin"
			}
			record(status, message)
			writeProxyError(w, status, message)
			return
		}
		applyPluginContext(r, rawBody, rc)
	} else if !p.egress && hasIdentity {
		r.Header.Set("X-Affinity-Session-Id", identity.ID)
	}

	// Gateway-internal headers must never reach a provider.
	if p.egress {
		stripInternalHeaders(r.Header)
	}

	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = u.Scheme
		req.URL.Host = u.Host
		req.URL.Path = joinPath(u.Path, rest)
		req.URL.RawPath = ""
		req.Host = u.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, e error) {
		writeProxyError(w, http.StatusBadGateway, fmt.Sprintf("upstream unavailable: %v", e))
	}

	cw := &countWriter{ResponseWriter: w}
	proxy.ServeHTTP(cw, r)

	status := cw.status
	if status == 0 {
		status = http.StatusOK
	}
	record(status, "")
	if p.metrics != nil && p.egress && status >= 200 && status < 400 {
		p.metrics.IncEgressSuccess()
	}
}

// applyPluginContext writes plugin mutations back onto the outgoing request.
func applyPluginContext(r *http.Request, original []byte, rc *plugin.Context) {
	headers := make(http.Header, len(rc.Headers))
	for name, value := range rc.Headers {
		headers.Set(name, value)
	}
	r.Header = headers
	if !bytes.Equal(rc.Body, original) {
		r.Body = io.NopCloser(bytes.NewReader(rc.Body))
		r.ContentLength = int64(len(rc.Body))
	}
}

func directionOf(egress bool) string {
	if egress {
		return config.Egress
	}
	return config.Inbound
}

func headerMap(header http.Header) map[string]string {
	out := make(map[string]string, len(header))
	for name, values := range header {
		if len(values) > 0 {
			out[strings.ToLower(name)] = values[0]
		}
	}
	return out
}

func queryMap(values url.Values) map[string]string {
	out := make(map[string]string, len(values))
	for name, items := range values {
		if len(items) > 0 {
			out[name] = items[0]
		}
	}
	return out
}

var internalHeaders = []string{"X-Affinity-Session-Id"}

func stripInternalHeaders(header http.Header) {
	for _, name := range internalHeaders {
		header.Del(name)
	}
}

var redactedHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"x-api-key":           true,
	"api-key":             true,
	"cookie":              true,
	"set-cookie":          true,
}

func redactHeaders(header http.Header) map[string][]string {
	out := make(map[string][]string, len(header))
	for name, values := range header {
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if redactedHeaders[strings.ToLower(name)] {
			out[canonical] = []string{"***"}
			continue
		}
		out[canonical] = append([]string(nil), values...)
	}
	return out
}

func resolveInbound(c config.Config, path string) (config.Route, bool) {
	var best config.Route
	found := false
	for _, route := range c.Inbound {
		if route.State != config.StateActive {
			continue
		}
		if !pathMatches(route.Path, path) {
			continue
		}
		if !found || len(route.Path) > len(best.Path) {
			best = route
			found = true
		}
	}
	return best, found
}

func resolveEgress(c config.Config, path string) (config.Route, string, bool) {
	rest := strings.TrimPrefix(path, "/egress/")
	parts := strings.SplitN(rest, "/", 2)
	if parts[0] == "" {
		return config.Route{}, "", false
	}
	id := parts[0]
	remainder := "/"
	if len(parts) == 2 {
		remainder = "/" + parts[1]
	}
	for _, route := range c.Egress {
		if route.ID == id && route.State == config.StateActive {
			return route, remainder, true
		}
	}
	return config.Route{}, "", false
}

func pathMatches(routePath, requestPath string) bool {
	if routePath == "" {
		// No prefix: the global catch-all matches everything.
		return true
	}
	if routePath == requestPath {
		return true
	}
	base := strings.TrimRight(routePath, "/")
	return strings.HasPrefix(requestPath, base+"/")
}

func joinPath(base, rest string) string {
	if base == "/" {
		base = ""
	}
	if rest == "" {
		rest = "/"
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(rest, "/")
}

// readJSONBody buffers a JSON request body so the gateway can inspect session,
// model and plugin metadata, then restores it for the upstream proxy.
func readJSONBody(r *http.Request) []byte {
	if r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		return nil
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, maxInspectBytes))
	if err != nil {
		return nil
	}
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), r.Body))
	return buf
}

func extractModel(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return payload.Model
}

func writeProxyError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

type countWriter struct {
	http.ResponseWriter
	status int
}

var (
	_ http.ResponseWriter = (*countWriter)(nil)
	_ http.Flusher        = (*countWriter)(nil)
)

func (w *countWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *countWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Flush keeps streaming responses (for example SSE) working through the wrapper.
func (w *countWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *countWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
