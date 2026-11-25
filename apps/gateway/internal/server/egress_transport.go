package server

import (
	"crypto/tls"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
)

// Keep at most 32 reusable custom/direct pools, each with at most 32 idle
// connections. Evicted pools remain alive only while serving active requests.
const maxEgressTransports = 32

// Egress upstreams are spoken to over HTTP/1.1 only. HTTP/2 multiplexes every
// request to a host onto one TLS connection, so a connection that goes dark
// without a reset (NAT table drop, half-open proxy hop, silent middlebox) parks
// every request sharing it until TCP gives up. HTTP/1.1 gives each in-flight
// request its own connection, so a dead one only costs the request that reused
// it instead of every route pointed at that provider.
const egressALPNProtocol = "http/1.1"

// Default bound on waiting for upstream response headers. It covers the dead
// connection case above; it never limits streaming bodies, so long SSE
// responses are unaffected. Override with AFFINITY_EGRESS_RESPONSE_HEADER_TIMEOUT
// (Go duration, "0" disables the bound).
const defaultEgressResponseHeaderTimeout = 120 * time.Second

var egressResponseHeaderTimeout = envDuration("AFFINITY_EGRESS_RESPONSE_HEADER_TIMEOUT", defaultEgressResponseHeaderTimeout)

// envDuration reads a Go duration from the environment, keeping the fallback
// when the value is absent, malformed or negative.
func envDuration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		log.Printf("%s: ignoring invalid duration %q, keeping %s", key, raw, fallback)
		return fallback
	}
	return value
}

// newEgressTransport clones the default transport and pins it to HTTP/1.1 with
// bounded pools. Environment proxy settings are inherited from the clone; the
// transport cache overrides them per route.
func newEgressTransport(responseHeaderTimeout time.Duration) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{}
	if transport.TLSClientConfig != nil {
		tlsConfig = transport.TLSClientConfig.Clone()
	}
	// ALPN is what actually decides the protocol: an upstream cannot negotiate
	// h2 without the offer, even if net/http kept an h2 transport registered.
	tlsConfig.NextProtos = []string{egressALPNProtocol}
	transport.TLSClientConfig = tlsConfig
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	transport.MaxIdleConns = 32
	transport.MaxIdleConnsPerHost = 8
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return transport
}

var egressTransports = newTransportCache()

type transportEntry struct {
	transport *http.Transport
	used      uint64
	active    int
	retired   bool
	// shared marks the inherited pool, which is never retired or counted as
	// active and therefore must not be released like a per-route entry.
	shared bool
}

type transportCache struct {
	mu          sync.Mutex
	entries     map[config.ProxySettings]*transportEntry
	clock       uint64
	envOnce     sync.Once
	environment *transportEntry
}

func newTransportCache() *transportCache {
	return &transportCache{entries: make(map[config.ProxySettings]*transportEntry)}
}

// environmentEntry builds the inherited pool on first use, so a caller that
// adjusts http.DefaultTransport before serving is still honoured.
func (c *transportCache) environmentEntry() *transportEntry {
	c.envOnce.Do(func() {
		c.environment = &transportEntry{
			transport: newEgressTransport(egressResponseHeaderTimeout),
			shared:    true,
		}
	})
	return c.environment
}

func (c *transportCache) acquire(settings config.ProxySettings) *transportEntry {
	if settings.Mode == "environment" || settings.Mode == "" {
		return c.environmentEntry()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	if entry := c.entries[settings]; entry != nil {
		entry.used = c.clock
		entry.active++
		return entry
	}
	transport := c.environmentEntry().transport.Clone()
	transport.Proxy = nil
	if settings.Mode == "custom" {
		// Config validates URLs before publishing a snapshot. Go's transport
		// handles HTTP(S), SOCKS5 authentication and SOCKS remote DNS directly.
		proxyURL, _ := url.Parse(settings.URL)
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	if len(c.entries) >= maxEgressTransports {
		var oldestKey config.ProxySettings
		var oldest *transportEntry
		for key, entry := range c.entries {
			if oldest == nil || entry.used < oldest.used {
				oldestKey, oldest = key, entry
			}
		}
		delete(c.entries, oldestKey)
		oldest.retired = true
		oldest.transport.CloseIdleConnections()
	}
	entry := &transportEntry{transport: transport, used: c.clock, active: 1}
	c.entries[settings] = entry
	return entry
}

// Called after ReverseProxy has finished copying the response, including streams.
// Closing again on retirement drains connections returned by in-flight requests
// without ever interrupting them or mutating their transport settings.
func (c *transportCache) release(entry *transportEntry) {
	if entry.shared {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry.active--
	if entry.retired && entry.active == 0 {
		entry.transport.CloseIdleConnections()
	}
}
