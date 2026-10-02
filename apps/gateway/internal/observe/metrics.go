package observe

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/persist"
)

// maxTrackedSessions bounds how many session ids are remembered for dedup.
const maxTrackedSessions = 10000

// Metrics holds process counters exposed to the console. They can be persisted
// so they survive a gateway restart.
type Metrics struct {
	requests      atomic.Int64
	egress        atomic.Int64
	egressSuccess atomic.Int64
	affinity      atomic.Int64

	mu       sync.Mutex
	sessions map[string]struct{}
	order    []string
	persist  *persist.Debouncer
}

// Counters is the JSON view returned by the control API.
type Counters struct {
	Requests         int64 `json:"requests"`
	AffinitySessions int64 `json:"affinity_sessions"`
	EgressRequests   int64 `json:"egress_requests"`
	EgressSuccess    int64 `json:"egress_success"`
}

type metricsState struct {
	Requests         int64    `json:"requests"`
	AffinitySessions int64    `json:"affinity_sessions"`
	EgressRequests   int64    `json:"egress_requests"`
	EgressSuccess    int64    `json:"egress_success"`
	Sessions         []string `json:"sessions,omitempty"`
}

// NewPersistentMetrics loads any existing counters and persists changes.
func NewPersistentMetrics(path string) (*Metrics, error) {
	m := &Metrics{}
	var state metricsState
	found, err := persist.Load(path, &state)
	if err != nil {
		return nil, err
	}
	if found {
		m.requests.Store(state.Requests)
		m.egress.Store(state.EgressRequests)
		m.egressSuccess.Store(state.EgressSuccess)
		m.affinity.Store(state.AffinitySessions)
		m.sessions = make(map[string]struct{}, len(state.Sessions))
		for _, id := range state.Sessions {
			if id == "" {
				continue
			}
			m.sessions[id] = struct{}{}
			m.order = append(m.order, id)
		}
	}
	m.persist = persist.NewDebouncer(path, 500*time.Millisecond, m.snapshot)
	return m, nil
}

// IncRequests counts one inbound or egress proxied request.
func (m *Metrics) IncRequests() {
	m.requests.Add(1)
	m.persist.Touch()
}

// IncEgress counts one egress proxied request.
func (m *Metrics) IncEgress() {
	m.egress.Add(1)
	m.persist.Touch()
}

// IncEgressSuccess counts one successful egress response.
func (m *Metrics) IncEgressSuccess() {
	m.egressSuccess.Add(1)
	m.persist.Touch()
}

// ObserveSession records a session id, counting each unique id once.
func (m *Metrics) ObserveSession(id string) {
	if id == "" {
		return
	}
	m.mu.Lock()
	if m.sessions == nil {
		m.sessions = make(map[string]struct{})
	}
	if _, seen := m.sessions[id]; !seen {
		m.sessions[id] = struct{}{}
		m.order = append(m.order, id)
		m.affinity.Add(1)
		for len(m.order) > maxTrackedSessions {
			oldest := m.order[0]
			m.order = m.order[1:]
			delete(m.sessions, oldest)
		}
		m.mu.Unlock()
		m.persist.Touch()
		return
	}
	m.mu.Unlock()
}

// Snapshot returns the current counters.
func (m *Metrics) Snapshot() Counters {
	return Counters{
		Requests:         m.requests.Load(),
		AffinitySessions: m.affinity.Load(),
		EgressRequests:   m.egress.Load(),
		EgressSuccess:    m.egressSuccess.Load(),
	}
}

// Flush writes pending state to disk.
func (m *Metrics) Flush() error {
	return m.persist.Flush()
}

func (m *Metrics) snapshot() any {
	m.mu.Lock()
	sessions := append([]string(nil), m.order...)
	m.mu.Unlock()
	return metricsState{
		Requests:         m.requests.Load(),
		AffinitySessions: m.affinity.Load(),
		EgressRequests:   m.egress.Load(),
		EgressSuccess:    m.egressSuccess.Load(),
		Sessions:         sessions,
	}
}
