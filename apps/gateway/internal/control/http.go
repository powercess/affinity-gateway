package control

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/observe"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/plugin"
)

// Handler serves the /api/v1 control plane.
type Handler struct {
	Store    *config.Store
	Metrics  *observe.Metrics
	Recorder *observe.Recorder
	Registry *plugin.Registry
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path, "/api/v1/")
	if len(parts) == 0 {
		http.NotFound(w, r)
		return
	}
	switch parts[0] {
	case "config":
		h.writeJSON(w, h.Store.Snapshot())
	case "status":
		c := h.Store.Snapshot()
		h.writeJSON(w, map[string]any{
			"status":         "ok",
			"listen":         c.Listen,
			"mode":           "soft-affinity",
			"inbound_routes": len(c.Inbound),
			"egress_routes":  len(c.Egress),
			"plugins":        len(c.Plugins),
		})
	case "plugins":
		h.plugins(w, r, parts)
	case "metrics":
		h.writeJSON(w, h.Metrics.Snapshot())
	case "requests":
		h.requests(w, r, parts)
	case config.Inbound, config.Egress:
		h.routes(w, r, parts)
	default:
		http.NotFound(w, r)
	}
}

func (h Handler) plugins(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			h.methodNotAllowed(w)
			return
		}
		h.writeJSON(w, h.Store.Plugins())
		return
	}
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id := parts[1]
	switch r.Method {
	case http.MethodPut, http.MethodPost:
		var p config.Plugin
		if !h.decode(w, r, &p) {
			return
		}
		p.ID = id
		if err := plugin.ValidateSource(p.ID, p.Source); err != nil {
			h.fail(w, err)
			return
		}
		saved, err := h.Store.UpsertPlugin(p)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.reloadPlugins()
		h.writeJSON(w, saved)
	case http.MethodDelete:
		if err := h.Store.DeletePlugin(id); err != nil {
			h.fail(w, err)
			return
		}
		h.reloadPlugins()
		h.writeJSON(w, map[string]string{"deleted": id})
	default:
		h.methodNotAllowed(w)
	}
}

func (h Handler) reloadPlugins() {
	if h.Registry != nil {
		_ = h.Registry.Load(h.Store.Plugins())
	}
}

func (h Handler) requests(w http.ResponseWriter, r *http.Request, parts []string) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w)
		return
	}
	if len(parts) == 1 {
		h.writeJSON(w, h.Recorder.List())
		return
	}
	if len(parts) == 2 {
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			h.fail(w, config.ErrNotFound)
			return
		}
		record, ok := h.Recorder.Get(id)
		if !ok {
			h.fail(w, config.ErrNotFound)
			return
		}
		h.writeJSON(w, record)
		return
	}
	http.NotFound(w, r)
}

func (h Handler) routes(w http.ResponseWriter, r *http.Request, parts []string) {
	direction := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			snapshot := h.Store.Snapshot()
			if direction == config.Inbound {
				h.writeJSON(w, snapshot.Inbound)
			} else {
				h.writeJSON(w, snapshot.Egress)
			}
		case http.MethodPost:
			var route config.Route
			if !h.decode(w, r, &route) {
				return
			}
			created, err := h.Store.Add(direction, route)
			if err != nil {
				h.fail(w, err)
				return
			}
			h.writeJSONStatus(w, http.StatusCreated, created)
		default:
			h.methodNotAllowed(w)
		}
		return
	}
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id := parts[1]
	switch r.Method {
	case http.MethodGet:
		route, err := h.find(direction, id)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.writeJSON(w, route)
	case http.MethodPut, http.MethodPatch:
		var route config.Route
		if !h.decode(w, r, &route) {
			return
		}
		updated, err := h.Store.Update(direction, id, route)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.writeJSON(w, updated)
	case http.MethodDelete:
		if err := h.Store.Delete(direction, id); err != nil {
			h.fail(w, err)
			return
		}
		h.writeJSON(w, map[string]string{"deleted": id})
	default:
		h.methodNotAllowed(w)
	}
}

func (h Handler) find(direction, id string) (config.Route, error) {
	snapshot := h.Store.Snapshot()
	list := snapshot.Inbound
	if direction == config.Egress {
		list = snapshot.Egress
	}
	for _, route := range list {
		if route.ID == id {
			return route, nil
		}
	}
	return config.Route{}, config.ErrNotFound
}

func (h Handler) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		h.fail(w, err)
		return false
	}
	return true
}

func (h Handler) writeJSON(w http.ResponseWriter, v any) {
	h.writeJSONStatus(w, http.StatusOK, v)
}

func (h Handler) writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h Handler) fail(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, config.ErrNotFound) {
		status = http.StatusNotFound
	} else if strings.Contains(err.Error(), "已存在") ||
		strings.Contains(err.Error(), "已被占用") ||
		strings.Contains(err.Error(), "只能") ||
		strings.Contains(err.Error(), "使用") {
		status = http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (h Handler) methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusMethodNotAllowed)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
}

func splitPath(path, prefix string) []string {
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" {
		return nil
	}
	return strings.Split(rest, "/")
}
