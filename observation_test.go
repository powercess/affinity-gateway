package sessionaffinity

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func TestObservationRedactionAndStream(t *testing.T) {
	h := configured(t, "inbound")
	h.ObserveID = "test-inbound"
	r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"test-model","messages":[{"content":"SECRET-PROMPT"}]}`))
	r.Header.Set("Authorization", "Bearer SECRET-KEY")
	r.Header.Set("X-Session-Id", "SECRET-SESSION")
	r.Header.Set("Cookie", "SECRET-COOKIE")
	r.Header.Set("X-Custom-Secret", "NEVER-RETAIN")
	w := httptest.NewRecorder()
	err := h.ServeHTTP(w, r, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if _, err := fmt.Fprint(w, "data: first\n\n"); err != nil {
			return err
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			return err
		}
		_, err := fmt.Fprint(w, "data: second\n\n")
		return err
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !w.Flushed || w.Body.String() != "data: first\n\ndata: second\n\n" {
		t.Fatal("stream changed")
	}
	rows, _, _ := observations.snapshot()
	row := rows[0]
	if row.Model != "test-model" || row.Source == "" || row.Session == "" || row.Status != 200 {
		t.Fatalf("incomplete observation: %+v", row)
	}
	encoded, _ := json.Marshal(row)
	for _, secret := range []string{"SECRET-KEY", "SECRET-SESSION", "SECRET-COOKIE", "SECRET-PROMPT", "NEVER-RETAIN"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("secret retained", secret)
		}
	}
	if row.Before["Authorization"] != "[redacted]" {
		t.Fatal("missing redaction")
	}
}
func TestObservationRejection(t *testing.T) {
	h := configured(t, "inbound")
	h.ObserveID = "reject"
	r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer test")
	err := h.ServeHTTP(httptest.NewRecorder(), r, caddyhttp.HandlerFunc(func(http.ResponseWriter, *http.Request) error { t.Fatal("rejected request forwarded"); return nil }))
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _ := observations.snapshot()
	if rows[0].Status != 400 || rows[0].After != nil || rows[0].Session != "" || rows[0].Error != "affinity_identity_required" {
		t.Fatal("incorrect rejection observation")
	}
}

func TestObservedInboundOutboundSession(t *testing.T) {
	inbound := configured(t, "inbound")
	inbound.ObserveID = "paired-in"
	outbound := configured(t, "outbound")
	outbound.ObserveID = "paired-out"
	r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"pair-model"}`))
	r.Header.Set("Authorization", "Bearer client-key")
	r.Header.Set("X-Session-Id", "client-session")
	err := inbound.ServeHTTP(httptest.NewRecorder(), r, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		return outbound.ServeHTTP(w, r, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
			if r.Header.Get(internalHeader) != "" {
				t.Fatal("internal identity escaped")
			}
			w.WriteHeader(200)
			return nil
		}))
	}))
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _ := observations.snapshot()
	if rows[0].Profile != "paired-in" || rows[1].Profile != "paired-out" || rows[0].Session == "" || rows[0].Session != rows[1].Session {
		t.Fatal("session evidence not matched")
	}
	if rows[0].After[internalHeader] == rows[1].After["x-opencode-session"] {
		t.Fatal("outbound identity not isolated")
	}
}
func TestObservationBoundedConcurrentStore(t *testing.T) {
	s := &observationStore{changed: make(chan struct{})}
	_, _, changed := s.snapshot()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s.add(Observation{})
			}
		}()
	}
	wg.Wait()
	rows, rev, _ := s.snapshot()
	if len(rows) != observationCapacity || rev != 1600 {
		t.Fatal(len(rows), rev)
	}
	select {
	case <-changed:
	default:
		t.Fatal("notification missing")
	}
}
func TestConsoleAuthAndSSE(t *testing.T) {
	c := Console{password: strings.Repeat("p", 32)}
	w := httptest.NewRecorder()
	_ = c.ServeHTTP(w, httptest.NewRequest("GET", "/api/observations", nil), nil)
	if w.Code != 401 {
		t.Fatal("unauthenticated API")
	}
	for _, method := range []string{"POST", "DELETE", "PUT"} {
		r := httptest.NewRequest(method, "/api/observations", nil)
		r.SetBasicAuth("admin", c.password)
		w := httptest.NewRecorder()
		_ = c.ServeHTTP(w, r, nil)
		if w.Code != 405 {
			t.Fatal(method, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/observations", nil)
	r.SetBasicAuth("admin", c.password)
	w = httptest.NewRecorder()
	_ = c.ServeHTTP(w, r, nil)
	if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
		t.Fatal("invalid snapshot")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = c.ServeHTTP(w, r, nil) }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/events", nil)
	req.SetBasicAuth("admin", c.password)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "event: snapshot\n" {
		t.Fatal(line, err)
	}
	cancel()
}
