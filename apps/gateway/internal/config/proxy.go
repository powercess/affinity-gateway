package config

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// ProxySettings selects environment, direct or custom forwarding globally.
// A route may instead use inherit, which is stored as a nil Route.Proxy.
// Custom URLs may contain credentials and must never appear in errors or logs.
type ProxySettings struct {
	Mode string `json:"mode"`
	URL  string `json:"url,omitempty"`
}

// ErrPersistence means a mutation was rolled back because saving failed.
var ErrPersistence = errors.New("configuration could not be saved")

func normalizeProxy(p ProxySettings, route bool) (ProxySettings, error) {
	switch p.Mode {
	case "inherit":
		if !route {
			return ProxySettings{}, errors.New("invalid global proxy mode")
		}
		p.URL = ""
	case "environment":
		if route {
			return ProxySettings{}, errors.New("invalid route proxy mode")
		}
		p.URL = ""
	case "direct":
		p.URL = ""
	case "custom":
		p.URL = strings.TrimSpace(p.URL)
		u, err := url.Parse(p.URL)
		if err != nil || u.Hostname() == "" || u.Opaque != "" ||
			(u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
			return ProxySettings{}, errors.New("proxy URL must be an http, https, socks5 or socks5h URL without a path, query or fragment")
		}
		if strings.HasPrefix(u.Host, "[") || strings.Contains(u.Hostname(), ":") {
			ip, err := netip.ParseAddr(u.Hostname())
			if err != nil || !ip.Is6() || !strings.HasPrefix(u.Host, "[") {
				return ProxySettings{}, errors.New("invalid proxy host")
			}
		}
		if port := u.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return ProxySettings{}, errors.New("invalid proxy port")
			}
		} else if strings.HasSuffix(u.Host, ":") {
			return ProxySettings{}, errors.New("invalid proxy port")
		}
	default:
		return ProxySettings{}, errors.New("invalid proxy mode")
	}
	return p, nil
}

func normalizeRouteProxy(direction string, p *ProxySettings) (*ProxySettings, error) {
	if p == nil {
		return nil, nil
	}
	norm, err := normalizeProxy(*p, true)
	if err != nil {
		return nil, err
	}
	if norm.Mode == "inherit" {
		return nil, nil
	}
	if direction != Egress {
		return nil, errors.New("proxy overrides are only available for egress routes")
	}
	return &norm, nil
}

func normalizeConfigProxies(c *Config) (bool, error) {
	original := c.EgressProxy
	if c.EgressProxy == (ProxySettings{}) {
		c.EgressProxy.Mode = "environment"
	}
	p, err := normalizeProxy(c.EgressProxy, false)
	if err != nil {
		return false, err
	}
	c.EgressProxy = p
	changed := original != p
	for _, routes := range []struct {
		direction string
		list      []Route
	}{{Inbound, c.Inbound}, {Egress, c.Egress}} {
		for i := range routes.list {
			original := routes.list[i].Proxy
			p, err := normalizeRouteProxy(routes.direction, routes.list[i].Proxy)
			if err != nil {
				return false, err
			}
			if original != nil && (p == nil || *original != *p) {
				changed = true
			}
			routes.list[i].Proxy = p
		}
	}
	return changed, nil
}

// SetEgressProxy validates and durably saves the policy before publishing it to
// request snapshots. Failed writes leave both the policy and timestamp unchanged.
func (s *Store) SetEgressProxy(p ProxySettings) (ProxySettings, error) {
	p, err := normalizeProxy(p, false)
	if err != nil {
		return ProxySettings{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.cfg
	s.cfg.EgressProxy = p
	if err := s.commitLocked(previous); err != nil {
		return ProxySettings{}, err
	}
	return p, nil
}

func cloneRoute(r Route) Route {
	if r.Plugins != nil {
		r.Plugins = append([]string{}, r.Plugins...)
	}
	if r.Proxy != nil {
		p := *r.Proxy
		r.Proxy = &p
	}
	return r
}
