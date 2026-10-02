// Package persist provides tiny atomic JSON file storage with debounced writes,
// used for the gateway's local runtime state (request records, metrics,
// conversation index).
package persist

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Load unmarshals path into v. It reports whether the file existed.
func Load(path string, v any) (bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(raw) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return true, err
	}
	return true, nil
}

// Save writes v atomically (temp file + rename).
func Save(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Debouncer coalesces rapid state changes into a single periodic write.
type Debouncer struct {
	mu       sync.Mutex
	path     string
	delay    time.Duration
	snapshot func() any
	timer    *time.Timer
}

// NewDebouncer returns a debouncer that serializes snapshot() to path.
func NewDebouncer(path string, delay time.Duration, snapshot func() any) *Debouncer {
	return &Debouncer{path: path, delay: delay, snapshot: snapshot}
}

// Touch schedules a write soon unless one is already pending.
func (d *Debouncer) Touch() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.timer == nil {
		d.timer = time.AfterFunc(d.delay, func() { _ = d.Flush() })
	}
	d.mu.Unlock()
}

// Flush writes the current snapshot immediately.
func (d *Debouncer) Flush() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.mu.Unlock()
	return Save(d.path, d.snapshot())
}
