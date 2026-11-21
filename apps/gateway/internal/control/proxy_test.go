package control

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyAPIValidationAndRouteModes(t *testing.T) {
	h := newHandler(t)
	for _, tc := range []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodPut, "/api/v1/egress-proxy", `{"mode":"custom","url":"http://user:{{PASSWORD_0iyr1epn}}@proxy.example/path"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/egress-proxy", `{"mode":"credential-marker"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/egress-proxy", `{"mode":{},"url":"credential-marker"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/egress-proxy", `{"mode":"inherit"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/egress-proxy", `{"mode":"direct"}`, http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/egress-proxy/extra", "", http.StatusNotFound},
		{http.MethodPut, "/api/v1/inbound/default", `{"target":"http://relay.example","proxy":{"mode":"direct"}}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/inbound/default", `{"target":"http://relay.example","proxy":{"mode":"inherit","url":"credential-marker"}}`, http.StatusOK},
		{http.MethodPost, "/api/v1/egress", `{"id":"proxy-api","target":"http://provider.example","proxy":{"mode":"direct","url":"credential-marker"}}`, http.StatusCreated},
		{http.MethodPut, "/api/v1/egress/proxy-api", `{"target":"http://provider.example","proxy":{"mode":"custom","url":"http://user:{{PASSWORD_0iyr1epn}}@proxy.example:99999"}}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/egress/proxy-api", `{"target":"http://provider.example","proxy":{"mode":"environment"}}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/egress/proxy-api", `{"target":"http://provider.example","proxy":null}`, http.StatusOK},
		{http.MethodPut, "/api/v1/egress-proxy", `{"mode":"environment","url":"credential-marker"}`, http.StatusOK},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatalf("%s %s: got %d, want %d", tc.method, tc.path, w.Code, tc.status)
		}
		if strings.Contains(w.Body.String(), "credential-marker") {
			t.Fatal("API exposed rejected or cleared credentials")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/config", nil))
	if !strings.Contains(w.Body.String(), `"egress_proxy":{"mode":"environment"}`) {
		t.Fatal("config API did not include global proxy")
	}
}
