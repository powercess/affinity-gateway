package server

import (
	"net/http"
	"net/url"
	"sync"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
)

// Keep at most 32 reusable custom/direct pools, each with at most 32 idle
// connections. Evicted pools remain alive only while serving active requests.
const maxEgressTransports = 32

var egressTransports = newTransportCache()

type transportEntry struct {
	transport *http.Transport
	used      uint64
	active    int
	retired   bool
}

type transportCache struct {
	mu          sync.Mutex
	entries     map[config.ProxySettings]*transportEntry
	clock       uint64
	environment transportEntry
}

func newTransportCache() *transportCache {
	return &transportCache{
		entries:     make(map[config.ProxySettings]*transportEntry),
		environment: transportEntry{transport: http.DefaultTransport.(*http.Transport)},
	}
}

func (c *transportCache) acquire(settings config.ProxySettings) *transportEntry {
	if settings.Mode == "environment" || settings.Mode == "" {
		return &c.environment
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	if entry := c.entries[settings]; entry != nil {
		entry.used = c.clock
		entry.active++
		return entry
	}
	transport := c.environment.transport.Clone()
	transport.Proxy = nil
	transport.MaxIdleConns = 32
	transport.MaxIdleConnsPerHost = 8
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
	if entry == &c.environment {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry.active--
	if entry.retired && entry.active == 0 {
		entry.transport.CloseIdleConnections()
	}
}
