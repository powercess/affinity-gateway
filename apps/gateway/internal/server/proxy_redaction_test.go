package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
	"github.com/powercess/affinity-gateway/apps/gateway/internal/observe"
)

func TestProxyConnectionErrorsDoNotExposeCredentials(t *testing.T) {
	// A CONNECT proxy may put arbitrary text in its status reason. net/http
	// includes that text in the transport error, so even Redacted() is unsafe.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 407 credential-marker\r\nContent-Length: 0\r\n\r\n")
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	u.User = url.UserPassword("private-user", "credential-marker")
	store := newStore(t, nil, []config.Route{{ID: "redaction", Target: "https://provider.invalid", Proxy: &config.ProxySettings{Mode: "custom", URL: u.String()}}})
	recorder := observe.NewRecorder(10)
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	handler := newEgressHandler(store, nil, nil, nil, recorder)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/egress/redaction/chat", nil))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "upstream unavailable") {
		t.Fatalf("expected safe gateway error, got %d", w.Code)
	}
	records, err := json.Marshal(recorder.List())
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{w.Body.String(), logs.String(), string(records)} {
		if strings.Contains(output, "credential-marker") || strings.Contains(output, "private-user") {
			t.Fatal("proxy credentials appeared in response, logs or request history")
		}
	}
}
