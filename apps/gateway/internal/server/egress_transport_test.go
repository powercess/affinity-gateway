package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/control"
)

func TestEgressProxyAPIHotApply(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "direct:"+r.URL.Path)
	}))
	defer destination.Close()
	proxyCalls := make(chan string, 10)
	forwardProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls <- r.URL.String()
		if !r.URL.IsAbs() || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-password")) {
			t.Error("HTTP proxy did not receive absolute destination and credentials")
		}
		_, _ = io.WriteString(w, "proxied:"+r.URL.Path)
	}))
	defer forwardProxy.Close()
	otherProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "override:"+r.URL.Path)
	}))
	defer otherProxy.Close()
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := config.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	route, err := store.Add(config.Egress, config.Route{ID: "provider", Target: destination.URL + "/base"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(config.Inbound, "default", config.Route{Target: destination.URL}); err != nil {
		t.Fatal(err)
	}
	api := newConsoleHandler(store, control.Handler{Store: store})
	egress := newEgressHandler(store, nil, nil, nil, nil)
	inbound := newInboundHandler(store, nil, nil, nil, nil)
	requestAPI := func(method, body string, authorized bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1/egress-proxy", strings.NewReader(body))
		if authorized {
			r.Header.Set("Authorization", "Bearer "+store.Token())
		}
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		return w
	}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if w := requestAPI(method, `{"mode":"direct"}`, false); w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s returned %d", method, w.Code)
		}
	}
	if store.Snapshot().EgressProxy.Mode != "environment" {
		t.Fatal("unauthenticated request changed settings")
	}
	setGlobal := func(settings config.ProxySettings) {
		t.Helper()
		body, err := json.Marshal(settings)
		if err != nil {
			t.Fatal(err)
		}
		w := requestAPI(http.MethodPut, string(body), true)
		if w.Code != http.StatusOK {
			t.Fatalf("setting proxy returned %d: %s", w.Code, w.Body.String())
		}
		var saved config.ProxySettings
		if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved != settings {
			t.Fatal("PUT did not return saved settings")
		}
		reloaded, err := config.NewStore(path)
		if err != nil || reloaded.Snapshot().EgressProxy != settings {
			t.Fatal("API settings did not persist")
		}
		w = requestAPI(http.MethodGet, "", true)
		if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved != settings {
			t.Fatal("GET did not return saved settings")
		}
	}
	assertResponse := func(handler http.Handler, path, want string) {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || w.Body.String() != want {
			t.Fatalf("got %d %q, want %q", w.Code, w.Body.String(), want)
		}
	}
	proxyURL, _ := url.Parse(forwardProxy.URL)
	proxyURL.User = url.UserPassword("proxy-user", "proxy-password")
	setGlobal(config.ProxySettings{Mode: "custom", URL: proxyURL.String()})
	assertResponse(egress, "/egress/provider/chat?model=demo", "proxied:/base/chat")
	if got := <-proxyCalls; got != destination.URL+"/base/chat?model=demo" {
		t.Fatalf("proxy received wrong destination: %s", got)
	}
	assertResponse(inbound, "/v1/chat", "direct:/v1/chat")
	route.Proxy = &config.ProxySettings{Mode: "direct"}
	if _, err := store.Update(config.Egress, route.ID, route); err != nil {
		t.Fatal(err)
	}
	assertResponse(egress, "/egress/provider/chat", "direct:/base/chat")
	route.Proxy = &config.ProxySettings{Mode: "custom", URL: otherProxy.URL}
	if _, err := store.Update(config.Egress, route.ID, route); err != nil {
		t.Fatal(err)
	}
	assertResponse(egress, "/egress/provider/chat", "override:/base/chat")
	setGlobal(config.ProxySettings{Mode: "direct"})
	assertResponse(egress, "/egress/provider/chat", "override:/base/chat")
	route.Proxy = &config.ProxySettings{Mode: "inherit"}
	if _, err := store.Update(config.Egress, route.ID, route); err != nil {
		t.Fatal(err)
	}
	assertResponse(egress, "/egress/provider/chat", "direct:/base/chat")
	setGlobal(config.ProxySettings{Mode: "custom", URL: proxyURL.String()})
	assertResponse(egress, "/egress/provider/chat", "proxied:/base/chat")
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if w := requestAPI(http.MethodPut, `{"mode":"direct"}`, true); w.Code != http.StatusInternalServerError {
		t.Fatalf("failed persistence returned %d", w.Code)
	}
	assertResponse(egress, "/egress/provider/chat", "proxied:/base/chat")
}

func TestTransportCacheBoundedAndActiveStreamsSurviveEviction(t *testing.T) {
	finish := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "first\n")
		w.(http.Flusher).Flush()
		<-finish
		_, _ = io.WriteString(w, "last\n")
	}))
	defer proxy.Close()
	defer close(finish)
	cache := newTransportCache()
	settings := config.ProxySettings{Mode: "custom", URL: proxy.URL}
	entry := cache.acquire(settings)
	again := cache.acquire(settings)
	if again != entry {
		t.Fatal("same policy did not reuse transport")
	}
	cache.release(again)
	client := &http.Client{Transport: entry.transport, Timeout: 5 * time.Second}
	resp, err := client.Get("http://provider.invalid/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	defer cache.release(entry)
	reader := bufio.NewReader(resp.Body)
	if first, err := reader.ReadString('\n'); err != nil || first != "first\n" {
		t.Fatalf("stream did not start: %v", err)
	}
	for i := range maxEgressTransports + 10 {
		p := cache.acquire(config.ProxySettings{Mode: "custom", URL: fmt.Sprintf("http://127.0.0.1:%d", 10000+i)})
		cache.release(p)
	}
	if len(cache.entries) != maxEgressTransports || !entry.retired {
		t.Fatal("transport cache did not evict the oldest pool")
	}
	finish <- struct{}{}
	if rest, err := io.ReadAll(reader); err != nil || string(rest) != "last\n" {
		t.Fatalf("eviction interrupted an active stream: %v", err)
	}
	for _, entry := range cache.entries {
		entry.transport.CloseIdleConnections()
	}
}

func TestSOCKSProxyAuthenticationAndRemoteDNS(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() { done <- serveSOCKSRequest(listener) }()
			u := &url.URL{Scheme: scheme, Host: listener.Addr().String(), User: url.UserPassword("proxy-user", "proxy:pass@word")}
			store := newStore(t, nil, []config.Route{{ID: "socks", Target: "http://provider.invalid:8123/base", Proxy: &config.ProxySettings{Mode: "custom", URL: u.String()}}})
			handler := newEgressHandler(store, nil, nil, nil, nil)
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/egress/socks/chat", nil)
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Body.String() != "socks-ok" {
				t.Fatalf("SOCKS forwarding failed: %d %s", w.Code, w.Body.String())
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func serveSOCKSRequest(listener net.Listener) error {
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	read := func(n int) ([]byte, error) {
		b := make([]byte, n)
		_, err := io.ReadFull(conn, b)
		return b, err
	}
	header, err := read(2)
	if err != nil || header[0] != 5 {
		return fmt.Errorf("invalid SOCKS greeting: %v", err)
	}
	methods, err := read(int(header[1]))
	if err != nil || !bytes.Contains(methods, []byte{2}) {
		return fmt.Errorf("SOCKS username/password method missing: %v", err)
	}
	if _, err := conn.Write([]byte{5, 2}); err != nil {
		return err
	}
	header, err = read(2)
	if err != nil || header[0] != 1 {
		return fmt.Errorf("invalid SOCKS authentication: %v", err)
	}
	username, err := read(int(header[1]))
	if err != nil {
		return err
	}
	length, err := read(1)
	if err != nil {
		return err
	}
	password, err := read(int(length[0]))
	if err != nil || string(username) != "proxy-user" || string(password) != "proxy:pass@word" {
		return fmt.Errorf("SOCKS credentials not decoded correctly")
	}
	if _, err := conn.Write([]byte{1, 0}); err != nil {
		return err
	}
	header, err = read(5)
	if err != nil || !bytes.Equal(header[:4], []byte{5, 1, 0, 3}) {
		return fmt.Errorf("SOCKS must send a domain for remote DNS: %v", err)
	}
	host, err := read(int(header[4]))
	if err != nil {
		return err
	}
	port, err := read(2)
	if err != nil || string(host) != "provider.invalid" || binary.BigEndian.Uint16(port) != 8123 {
		return fmt.Errorf("SOCKS destination mismatch")
	}
	if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80}); err != nil {
		return err
	}
	r, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.URL.Path != "/base/chat" || r.Host != "provider.invalid:8123" || r.Header.Get("Proxy-Authorization") != "" {
		return fmt.Errorf("SOCKS tunneled HTTP request mismatch")
	}
	_, err = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 8\r\nConnection: close\r\n\r\nsocks-ok")
	return err
}

// ProxyFromEnvironment caches the process environment, so isolate this test in
// a child test process instead of depending on execution order or host settings.
func TestEnvironmentProxyAndDirectOverride(t *testing.T) {
	if os.Getenv("AFFINITY_PROXY_TEST_CHILD") == "1" {
		// Map only the provider host to a local listener. The environment proxy
		// still receives requests through its own address, with no external DNS.
		target, err := url.Parse(os.Getenv("AFFINITY_PROXY_TEST_TARGET"))
		if err != nil {
			t.Fatal(err)
		}
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		http.DefaultTransport.(*http.Transport).DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			if address == "provider.invalid:80" {
				address = target.Host
			}
			return dialer.DialContext(ctx, network, address)
		}
		store := newStore(t, nil, []config.Route{
			{ID: "inherit", Target: "http://provider.invalid/base"},
			{ID: "direct", Target: "http://provider.invalid", Proxy: &config.ProxySettings{Mode: "direct"}},
		})
		handler := newEgressHandler(store, nil, nil, nil, nil)
		for route, want := range map[string]string{"inherit": "environment", "direct": "direct"} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/egress/"+route+"/chat", nil))
			if w.Code != http.StatusOK || w.Body.String() != want {
				t.Fatalf("%s: got %d %q", route, w.Code, w.Body.String())
			}
		}
		return
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "environment") }))
	defer proxy.Close()
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "direct") }))
	defer direct.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestEnvironmentProxyAndDirectOverride$", "-test.timeout=15s")
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "REQUEST_METHOD", "AFFINITY_PROXY_TEST_CHILD", "AFFINITY_PROXY_TEST_TARGET":
		default:
			cmd.Env = append(cmd.Env, pair)
		}
	}
	cmd.Env = append(cmd.Env, "HTTP_PROXY="+proxy.URL, "NO_PROXY=", "AFFINITY_PROXY_TEST_CHILD=1", "AFFINITY_PROXY_TEST_TARGET="+direct.URL)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("environment proxy child failed: %v\n%s", err, output)
	}
}

// Every egress route to a provider must get its own HTTP/1.1 connection. When
// the cohort shared one multiplexed TLS connection, a connection that died
// without a reset stalled every channel to that provider for minutes.
func TestEgressTransportPinsHTTP11(t *testing.T) {
	type seen struct {
		offered []string
		proto   string
	}
	observed := make(chan seen, 4)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r
	}))
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		observed <- seen{offered: append([]string(nil), hello.SupportedProtos...)}
		return nil, nil
	}}
	server.StartTLS()
	defer server.Close()

	for name, settings := range map[string]config.ProxySettings{
		"environment": {},
		"direct":      {Mode: "direct"},
	} {
		t.Run(name, func(t *testing.T) {
			cache := newTransportCache()
			entry := cache.acquire(settings)
			defer cache.release(entry)
			entry.transport.TLSClientConfig.InsecureSkipVerify = true // httptest certificate
			client := &http.Client{Transport: entry.transport}

			resp, err := client.Get(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			got := <-observed
			for _, proto := range got.offered {
				if proto == "h2" {
					t.Fatalf("client offered ALPN %q; h2 would multiplex every route onto one connection", got.offered)
				}
			}
			if want := []string{egressALPNProtocol}; len(got.offered) != 1 || got.offered[0] != want[0] {
				t.Fatalf("client offered ALPN %q, want %q", got.offered, want)
			}
		})
	}
}

// A provider that accepts the connection but never sends headers must fail the
// request instead of parking it until TCP gives up.
func TestEgressTransportBoundsHeaderWait(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	client := &http.Client{Transport: newEgressTransport(80 * time.Millisecond)}
	start := time.Now()
	_, err := client.Get(server.URL)
	if err == nil {
		t.Fatal("expected a response header timeout")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error = %v, want a response header timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("header wait lasted %s, want it bounded near 80ms", elapsed)
	}
}
