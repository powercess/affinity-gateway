package observe

import (
	"sync"
	"time"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/persist"
)

// Record is one request trace entry. The body and headers are captured for
// debugging affinity and are redacted by the proxy before being stored.
type Record struct {
	ID             int64               `json:"id"`
	Time           time.Time           `json:"time"`
	Direction      string              `json:"direction"`
	Route          string              `json:"route"`
	Method         string              `json:"method"`
	Path           string              `json:"path"`
	Model          string              `json:"model"`
	Status         int                 `json:"status"`
	Session        string              `json:"session"`
	SessionSource  string              `json:"session_source"`
	LatencyMS      int64               `json:"latency_ms"`
	Target         string              `json:"target"`
	RequestHeaders map[string][]string `json:"request_headers,omitempty"`
	Error          string              `json:"error,omitempty"`
}

// Recorder keeps the most recent request records, optionally backed by a file.
type Recorder struct {
	mu      sync.Mutex
	max     int
	next    int64
	items   []Record
	persist *persist.Debouncer
}

// NewRecorder returns an in-memory recorder bounded to max entries.
func NewRecorder(max int) *Recorder {
	if max <= 0 {
		max = 200
	}
	return &Recorder{max: max}
}

// NewPersistentRecorder loads any existing records and persists new ones.
func NewPersistentRecorder(path string, max int) (*Recorder, error) {
	recorder := NewRecorder(max)
	var items []Record
	found, err := persist.Load(path, &items)
	if err != nil {
		return nil, err
	}
	if found {
		if len(items) > max {
			items = items[len(items)-max:]
		}
		recorder.items = items
		for _, item := range items {
			if item.ID > recorder.next {
				recorder.next = item.ID
			}
		}
	}
	recorder.persist = persist.NewDebouncer(path, 500*time.Millisecond, recorder.snapshot)
	return recorder, nil
}

// Add appends a record, dropping the oldest when full, and returns the stored
// record including its assigned id.
func (r *Recorder) Add(rec Record) Record {
	r.mu.Lock()
	r.next++
	rec.ID = r.next
	r.items = append(r.items, rec)
	if len(r.items) > r.max {
		r.items = r.items[len(r.items)-r.max:]
	}
	r.mu.Unlock()
	r.persist.Touch()
	return rec
}

// List returns summaries (without headers or body) newest first.
func (r *Recorder) List() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Record, len(r.items))
	for i, rec := range r.items {
		out[len(r.items)-1-i] = rec
	}
	for i := range out {
		out[i].RequestHeaders = nil
	}
	return out
}

// Get returns the full record for id.
func (r *Recorder) Get(id int64) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.items) - 1; i >= 0; i-- {
		if r.items[i].ID == id {
			return r.items[i], true
		}
	}
	return Record{}, false
}

// Flush writes pending state to disk.
func (r *Recorder) Flush() error {
	return r.persist.Flush()
}

func (r *Recorder) snapshot() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Record(nil), r.items...)
}
