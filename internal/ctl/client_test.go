package ctl

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// mustDo performs a request via the client and fails the test on error. The
// response body is closed via t.Cleanup.
func mustDo(t *testing.T, c *Client, method, path string) *http.Response {
	t.Helper()
	resp, err := c.do(method, path, nil)
	if err != nil {
		t.Fatalf("do(%s %s): %v", method, path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestClientAuth verifies the Authorization header is attached only when a
// token is configured.
func TestClientAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "sekrit", false)
	mustDo(t, c, http.MethodGet, "/api/control/health")
	if gotAuth != "Bearer sekrit" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sekrit")
	}

	c = NewClient(srv.URL, "", false)
	mustDo(t, c, http.MethodGet, "/api/control/health")
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty", gotAuth)
	}
}

// TestClientAcceptJSON verifies every request asks for JSON.
func TestClientAcceptJSON(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", false)
	mustDo(t, c, http.MethodGet, "/api/control/health")
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
}
