// Package config loads, validates and persists inbound and egress route
// configuration together with Lua plugin definitions. The persisted form is a
// single JSON document; the runtime only ever reads immutable snapshots.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Route states.
const (
	StateActive   = "active"
	StateInactive = "inactive"
)

// Directions.
const (
	Inbound = "inbound"
	Egress  = "egress"
	Both    = "both"
)

// Route is one inbound listener mapping or one egress provider mapping.
type Route struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Target  string   `json:"target"`
	State   string   `json:"state"`
	Plugins []string `json:"plugins"`
}

// Plugin is a Lua request transformation definition.
type Plugin struct {
	ID          string `json:"id"`
	Direction   string `json:"direction"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// Config is the complete persisted control-plane state.
type Config struct {
	Version   int       `json:"version"`
	Listen    string    `json:"listen"`
	Inbound   []Route   `json:"inbound"`
	Egress    []Route   `json:"egress"`
	Plugins   []Plugin  `json:"plugins"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store owns the in-memory config and its persistence file.
type Store struct {
	mu     sync.RWMutex
	path   string
	secret string
	token  string
	cfg    Config
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

// ErrNotFound reports that a route or plugin id does not exist.
var ErrNotFound = errors.New("资源不存在")

func defaultPlugins() []Plugin {
	return []Plugin{
		{
			ID:          "opencode.session",
			Direction:   Egress,
			Description: "把 x-affinity-session-id 映射为 OpenCode 需要的 x-opencode-session",
			Source: `-- 出站插件：把软亲和会话标识派生为 OpenCode 需要的会话 ID。
function handle(ctx)
  if ctx.session == "" then
    return "continue"
  end
  ctx:set_header("x-opencode-session", session.opencode(ctx.session, ctx.route_id))
  return "continue"
end
`,
		},
	}
}

// Default returns the first-run configuration.
func Default() Config {
	return Config{
		Version: 1,
		Listen:  ":8236",
		Inbound: []Route{{
			ID:      "default",
			Name:    "默认入站",
			Path:    "",
			Target:  "http://127.0.0.1:9000",
			State:   StateActive,
			Plugins: []string{},
		}},
		Egress: []Route{{
			ID:      "opencode",
			Name:    "OpenCode",
			Target:  "https://opencode.ai/zen/go",
			State:   StateActive,
			Plugins: []string{"opencode.session"},
		}},
		Plugins:   defaultPlugins(),
		UpdatedAt: time.Now().UTC(),
	}
}

// NewStore opens (or creates) the config file at path.
func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.cfg = Default()
		if err := s.persistLocked(); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if err := json.Unmarshal(raw, &s.cfg); err != nil {
		return err
	}
	if s.cfg.Listen == "" {
		s.cfg.Listen = ":8236"
	}
	if s.cfg.Version == 0 {
		s.cfg.Version = 1
	}
	if migrate(&s.cfg) {
		if err := s.persistLocked(); err != nil {
			return err
		}
	}
	if len(s.cfg.Plugins) == 0 {
		s.cfg.Plugins = defaultPlugins()
		if err := s.persistLocked(); err != nil {
			return err
		}
	}
	secret, err := loadSecret(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	s.secret = secret
	token, err := loadToken(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	s.token = token
	return nil
}

func loadSecret(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "secret")
	if raw, err := os.ReadFile(path); err == nil {
		if value := strings.TrimSpace(string(raw)); value != "" {
			return value, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	value := hex.EncodeToString(buf[:])
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return "", err
	}
	return value, nil
}

func loadToken(dir string) (string, error) {
	if env := strings.TrimSpace(os.Getenv("AFFINITY_TOKEN")); env != "" {
		return env, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "token")
	if raw, err := os.ReadFile(path); err == nil {
		if value := strings.TrimSpace(string(raw)); value != "" {
			return value, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	value := hex.EncodeToString(buf[:])
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return "", err
	}
	return value, nil
}

// deprecatedPlugins were bundled once but are no longer needed: internal header
// stripping and session propagation are handled by the Go core / headers.
var deprecatedPlugins = []string{"strip.internal", "session.inject-metadata"}

// migrate applies one-time config cleanups.
func migrate(c *Config) bool {
	changed := false
	for _, dep := range deprecatedPlugins {
		for i := range c.Inbound {
			if removeString(&c.Inbound[i].Plugins, dep) {
				changed = true
			}
		}
		for i := range c.Egress {
			if removeString(&c.Egress[i].Plugins, dep) {
				changed = true
			}
		}
	}
	kept := make([]Plugin, 0, len(c.Plugins))
	for _, p := range c.Plugins {
		if contains(deprecatedPlugins, p.ID) {
			changed = true
			continue
		}
		kept = append(kept, p)
	}
	if changed {
		c.Plugins = kept
	}
	return changed
}

func removeString(list *[]string, value string) bool {
	src := *list
	out := src[:0]
	removed := false
	for _, item := range src {
		if item == value {
			removed = true
			continue
		}
		out = append(out, item)
	}
	if removed {
		*list = out
	}
	return removed
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(raw, '\n'), 0o600)
}

// Snapshot returns a copy that callers may read without locking.
func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.cfg)
}

// Listen returns the configured listener address.
func (s *Store) Listen() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Listen == "" {
		return ":8236"
	}
	return s.cfg.Listen
}

// Secret returns the gateway secret used to derive provider session ids.
func (s *Store) Secret() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.secret
}

// Token returns the control-plane access token.
func (s *Store) Token() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token
}

// Plugins returns the configured plugin definitions.
func (s *Store) Plugins() []Plugin {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Plugin(nil), s.cfg.Plugins...)
}

func (s *Store) pluginByID(id string) (Plugin, bool) {
	for _, p := range s.cfg.Plugins {
		if p.ID == id {
			return p, true
		}
	}
	return Plugin{}, false
}

func (s *Store) listLocked(direction string) (*[]Route, error) {
	switch direction {
	case Inbound:
		return &s.cfg.Inbound, nil
	case Egress:
		return &s.cfg.Egress, nil
	default:
		return nil, fmt.Errorf("unknown direction %q", direction)
	}
}

// Add validates and appends a route.
func (s *Store) Add(direction string, route Route) (Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.listLocked(direction)
	if err != nil {
		return Route{}, err
	}
	norm, err := normalizeRoute(direction, route)
	if err != nil {
		return Route{}, err
	}
	if err := s.validateRoutePluginsLocked(direction, norm.Plugins); err != nil {
		return Route{}, err
	}
	if err := checkUnique(*list, norm, ""); err != nil {
		return Route{}, err
	}
	if err := checkSinglePrefix(*list, norm, ""); err != nil {
		return Route{}, err
	}
	*list = append(*list, norm)
	return norm, s.commitLocked()
}

// Update replaces the route identified by id.
func (s *Store) Update(direction, id string, route Route) (Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.listLocked(direction)
	if err != nil {
		return Route{}, err
	}
	index := -1
	for i, r := range *list {
		if r.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return Route{}, ErrNotFound
	}
	route.ID = id
	norm, err := normalizeRoute(direction, route)
	if err != nil {
		return Route{}, err
	}
	if err := s.validateRoutePluginsLocked(direction, norm.Plugins); err != nil {
		return Route{}, err
	}
	if err := checkUnique(*list, norm, id); err != nil {
		return Route{}, err
	}
	if err := checkSinglePrefix(*list, norm, id); err != nil {
		return Route{}, err
	}
	(*list)[index] = norm
	return norm, s.commitLocked()
}

// Delete removes the route identified by id.
func (s *Store) Delete(direction, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.listLocked(direction)
	if err != nil {
		return err
	}
	for i, r := range *list {
		if r.ID == id {
			*list = append((*list)[:i], (*list)[i+1:]...)
			return s.commitLocked()
		}
	}
	return ErrNotFound
}

// UpsertPlugin creates or replaces a plugin definition.
func (s *Store) UpsertPlugin(p Plugin) (Plugin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	norm, err := normalizePlugin(p)
	if err != nil {
		return Plugin{}, err
	}
	for i, existing := range s.cfg.Plugins {
		if existing.ID == norm.ID {
			s.cfg.Plugins[i] = norm
			return norm, s.commitLocked()
		}
	}
	s.cfg.Plugins = append(s.cfg.Plugins, norm)
	return norm, s.commitLocked()
}

// DeletePlugin removes a plugin that is not referenced by any route.
func (s *Store) DeletePlugin(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pluginByID(id); !ok {
		return ErrNotFound
	}
	for _, r := range s.cfg.Inbound {
		if contains(r.Plugins, id) {
			return fmt.Errorf("插件 %q 仍被入站路由 %q 使用", id, r.ID)
		}
	}
	for _, r := range s.cfg.Egress {
		if contains(r.Plugins, id) {
			return fmt.Errorf("插件 %q 仍被出站路由 %q 使用", id, r.ID)
		}
	}
	for i, p := range s.cfg.Plugins {
		if p.ID == id {
			s.cfg.Plugins = append(s.cfg.Plugins[:i], s.cfg.Plugins[i+1:]...)
			break
		}
	}
	return s.commitLocked()
}

func (s *Store) validateRoutePluginsLocked(direction string, ids []string) error {
	for _, id := range ids {
		p, ok := s.pluginByID(id)
		if !ok {
			return fmt.Errorf("未知插件 %q", id)
		}
		if p.Direction != Both && p.Direction != direction {
			return fmt.Errorf("插件 %q 不适用于%s路由", id, direction)
		}
	}
	return nil
}

func (s *Store) commitLocked() error {
	s.cfg.UpdatedAt = time.Now().UTC()
	return s.persistLocked()
}

func checkUnique(list []Route, candidate Route, skipID string) error {
	for _, r := range list {
		if r.ID == skipID {
			continue
		}
		if r.ID == candidate.ID {
			return fmt.Errorf("路由 ID %q 已存在", candidate.ID)
		}
		if candidate.Path != "" && r.Path == candidate.Path {
			return fmt.Errorf("路由路径 %q 已被占用", candidate.Path)
		}
	}
	return nil
}

// checkSinglePrefix enforces that at most one active inbound route has no
// prefix (the global catch-all).
func checkSinglePrefix(list []Route, candidate Route, skipID string) error {
	if candidate.Path != "" || candidate.State != StateActive {
		return nil
	}
	for _, r := range list {
		if r.ID == skipID {
			continue
		}
		if r.Path == "" && r.State == StateActive {
			return errors.New("无前缀入站路由只能启用一个")
		}
	}
	return nil
}

func normalizeRoute(direction string, route Route) (Route, error) {
	route.ID = strings.TrimSpace(route.ID)
	route.Name = strings.TrimSpace(route.Name)
	route.Target = strings.TrimRight(strings.TrimSpace(route.Target), "/")
	route.State = strings.TrimSpace(route.State)

	if route.ID == "" {
		route.ID = slug(route.Name)
	}
	if route.ID == "" {
		route.ID = "route-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	if !idPattern.MatchString(route.ID) {
		return Route{}, errors.New("路由 ID 只能包含小写字母、数字、点和连字符")
	}
	if route.Name == "" {
		route.Name = route.ID
	}
	if route.State == "" {
		route.State = StateActive
	}
	if route.State != StateActive && route.State != StateInactive {
		return Route{}, fmt.Errorf("无效状态 %q", route.State)
	}
	if route.Target == "" {
		return Route{}, errors.New("目标地址不能为空")
	}
	u, err := url.Parse(route.Target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Route{}, fmt.Errorf("目标地址必须是 http(s) URL: %q", route.Target)
	}
	route.Plugins = cleanStrings(route.Plugins)
	if direction == Inbound {
		route.Path = strings.TrimSpace(route.Path)
		if route.Path != "" && !strings.HasPrefix(route.Path, "/") {
			return Route{}, errors.New("入站前缀必须以 / 开头（留空表示无前缀）")
		}
	} else {
		route.Path = "/egress/" + route.ID
	}
	return route, nil
}

func normalizePlugin(p Plugin) (Plugin, error) {
	p.ID = strings.TrimSpace(p.ID)
	p.Direction = strings.TrimSpace(p.Direction)
	p.Description = strings.TrimSpace(p.Description)
	if p.ID == "" {
		return Plugin{}, errors.New("插件 ID 不能为空")
	}
	if !idPattern.MatchString(p.ID) {
		return Plugin{}, errors.New("插件 ID 只能包含小写字母、数字、点和连字符")
	}
	if p.Direction != Inbound && p.Direction != Egress && p.Direction != Both {
		return Plugin{}, fmt.Errorf("无效插件方向 %q", p.Direction)
	}
	if strings.TrimSpace(p.Source) == "" {
		return Plugin{}, errors.New("插件源码不能为空")
	}
	return p, nil
}

func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func clone(c Config) Config {
	if c.Inbound != nil {
		c.Inbound = append([]Route{}, c.Inbound...)
	}
	if c.Egress != nil {
		c.Egress = append([]Route{}, c.Egress...)
	}
	if c.Plugins != nil {
		c.Plugins = append([]Plugin{}, c.Plugins...)
	}
	for i := range c.Inbound {
		if c.Inbound[i].Plugins != nil {
			c.Inbound[i].Plugins = append([]string{}, c.Inbound[i].Plugins...)
		}
	}
	for i := range c.Egress {
		if c.Egress[i].Plugins != nil {
			c.Egress[i].Plugins = append([]string{}, c.Egress[i].Plugins...)
		}
	}
	return c
}
