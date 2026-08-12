package ctl

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"metacubexd-server-go/internal/supervisor"
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

// TestClientPath verifies the request URL is exactly endpoint+path, including
// query strings.
func TestClientPath(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", false)
	for _, path := range []string{
		"/api/control/kernel/status",
		"/api/control/kernel/logs?follow=1",
	} {
		mustDo(t, c, http.MethodGet, path)
		if gotURI != path {
			t.Errorf("request target = %q, want %q", gotURI, path)
		}
	}
}

// TestKernelStatus verifies KernelState JSON decodes into the shared
// supervisor contract type.
func TestKernelStatus(t *testing.T) {
	const body = `{"status":"running","pid":12345,"startedAt":1723456789,"version":"v1.19.29","externalController":"127.0.0.1:9090","secret":"s3cret","lastExitCode":0}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.RequestURI != "/api/control/kernel/status" {
			t.Errorf("unexpected request: %s %s", r.Method, r.RequestURI)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", false)
	st, err := c.KernelStatus()
	if err != nil {
		t.Fatalf("KernelStatus: %v", err)
	}
	if st.Status != supervisor.StatusRunning {
		t.Errorf("Status = %q, want %q", st.Status, supervisor.StatusRunning)
	}
	if st.PID == nil || *st.PID != 12345 {
		t.Errorf("PID = %v, want 12345", st.PID)
	}
	if st.Version != "v1.19.29" {
		t.Errorf("Version = %q, want v1.19.29", st.Version)
	}
	if st.ExternalController != "127.0.0.1:9090" {
		t.Errorf("ExternalController = %q, want 127.0.0.1:9090", st.ExternalController)
	}
}
