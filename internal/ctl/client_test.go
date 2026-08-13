package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// TestKernelOps covers all five kernel POST operations: success decodes the
// returned state, failure surfaces the server-provided lastError.
func TestKernelOps(t *testing.T) {
	ops := []struct {
		name string
		call func(*Client) (supervisor.KernelState, error)
		path string
	}{
		{"start", func(c *Client) (supervisor.KernelState, error) { return c.KernelStart() }, "/api/control/kernel/start"},
		{"stop", func(c *Client) (supervisor.KernelState, error) { return c.KernelStop() }, "/api/control/kernel/stop"},
		{"restart", func(c *Client) (supervisor.KernelState, error) { return c.KernelRestart() }, "/api/control/kernel/restart"},
		{"rollback", func(c *Client) (supervisor.KernelState, error) { return c.KernelRollback() }, "/api/control/kernel/rollback"},
		{"recover", func(c *Client) (supervisor.KernelState, error) { return c.KernelRecover() }, "/api/control/kernel/recover"},
	}

	for _, op := range ops {
		t.Run(op.name+" ok", func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.RequestURI != op.path {
					t.Errorf("unexpected request: %s %s", r.Method, r.RequestURI)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"status":"stopped"}`)
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "", false)
			st, err := op.call(c)
			if err != nil {
				t.Fatalf("%s: %v", op.name, err)
			}
			if st.Status != supervisor.StatusStopped {
				t.Errorf("Status = %q, want %q", st.Status, supervisor.StatusStopped)
			}
		})

		t.Run(op.name+" error", func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"status":"errored","lastError":"binary not found"}`)
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "", false)
			if _, err := op.call(c); err == nil {
				t.Fatalf("%s: want error, got nil", op.name)
			} else if !strings.Contains(err.Error(), "binary not found") {
				t.Errorf("%s: error = %q, want lastError message", op.name, err)
			}
		})
	}
}

// TestUnauthorized verifies a 401 maps to ErrUnauthorized.
func TestUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"unauthorized"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "wrong-token", false)
	if _, err := c.KernelStatus(); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("KernelStatus error = %v, want ErrUnauthorized", err)
	}
}

// TestSubscribeLogs streams SSE events from a fake server, then verifies a
// context cancel closes the channel promptly (no goroutine leak).
func TestSubscribeLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.RequestURI != "/api/control/kernel/logs" {
			t.Errorf("unexpected request: %s %s", r.Method, r.RequestURI)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, "data: {\"type\":\"log\",\"line\":\"line %d\"}\n\n", i)
			flusher.Flush()
		}
		// Keep the stream open until the client disconnects.
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := c.SubscribeLogs(ctx)
	if err != nil {
		t.Fatalf("SubscribeLogs: %v", err)
	}

	for i := 0; i < 5; i++ {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed after %d events", i)
			}
			if !strings.Contains(ev.Data, "line ") {
				t.Errorf("event %d Data = %q, want log line", i, ev.Data)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}

	// Cancelling must close the channel promptly — a leak would block here
	// until the timeout fires.
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel still open after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after cancel — goroutine leak")
	}
}

// TestConfigGet verifies GetConfig and GetRuntimeConfig fetch raw YAML from
// the correct endpoints and surface errors.
func TestConfigGet(t *testing.T) {
	tests := []struct {
		name string
		call func(*Client) (string, error)
		path string
	}{
		{"config", (*Client).GetConfig, "/api/control/config"},
		{"runtime", (*Client).GetRuntimeConfig, "/api/control/config/runtime"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" ok", func(t *testing.T) {
			var gotURI string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotURI = r.Method + " " + r.RequestURI
				w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
				fmt.Fprint(w, "mixed-port: 7890\n")
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "", false)
			got, err := tt.call(c)
			if err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if got != "mixed-port: 7890\n" {
				t.Errorf("%s = %q, want raw YAML body", tt.name, got)
			}
			if want := "GET " + tt.path; gotURI != want {
				t.Errorf("request = %q, want %q", gotURI, want)
			}
		})

		t.Run(tt.name+" error", func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"config exploded"}`)
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "", false)
			if _, err := tt.call(c); err == nil {
				t.Errorf("%s: want error, got nil", tt.name)
			} else if !strings.Contains(err.Error(), "config exploded") {
				t.Errorf("%s: error = %q, want server message", tt.name, err)
			}
		})
	}
}

// TestConfigPutSection verifies PutSection sends key/value/restart — including
// an explicit restart=false — and maps errors.
func TestConfigPutSection(t *testing.T) {
	tests := []struct {
		name    string
		restart bool
	}{
		{"restart", true},
		{"no-restart", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotURI, gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotURI = r.Method + " " + r.RequestURI
				b, _ := io.ReadAll(r.Body)
				gotBody = string(b)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"status":"running"}`)
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "", false)
			if err := c.PutSection("mixed-port", 7890, tt.restart); err != nil {
				t.Fatalf("PutSection: %v", err)
			}
			if want := "PUT /api/control/config/section"; gotURI != want {
				t.Errorf("request = %q, want %q", gotURI, want)
			}
			if !strings.Contains(gotBody, `"key":"mixed-port"`) || !strings.Contains(gotBody, `"value":7890`) {
				t.Errorf("body = %q, want key and value", gotBody)
			}
			wantRestart := fmt.Sprintf(`"restart":%t`, tt.restart)
			if !strings.Contains(gotBody, wantRestart) {
				t.Errorf("body = %q, want %s", gotBody, wantRestart)
			}
		})
	}

	t.Run("error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":"no active profile"}`)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "", false)
		if err := c.PutSection("mixed-port", 7890, true); err == nil {
			t.Error("want error, got nil")
		} else if !strings.Contains(err.Error(), "no active profile") {
			t.Errorf("error = %q, want server message", err)
		}
	})
}

// TestConfigGeoUpdate verifies GeoUpdate hits the geo endpoint and surfaces
// errors.
func TestConfigGeoUpdate(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"files":["geoip.metadb"]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", false)
	if err := c.GeoUpdate(); err != nil {
		t.Fatalf("GeoUpdate: %v", err)
	}
	if want := "POST /api/control/geo/update"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
}

// TestConfigBackupRestore verifies Backup and Restore send the webdav options
// and decode the response.
func TestConfigBackupRestore(t *testing.T) {
	opts := WebdavOptions{URL: "https://dav.example.com", Username: "u", Password: "p", Dir: "/backups"}

	t.Run("backup", func(t *testing.T) {
		var gotURI, gotBody string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotURI = r.Method + " " + r.RequestURI
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true,"path":"/backups/metacubexd-backup.json"}`)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "", false)
		if err := c.Backup(opts); err != nil {
			t.Fatalf("Backup: %v", err)
		}
		if want := "POST /api/control/backup"; gotURI != want {
			t.Errorf("request = %q, want %q", gotURI, want)
		}
		if !strings.Contains(gotBody, `"url":"https://dav.example.com"`) || !strings.Contains(gotBody, `"dir":"/backups"`) {
			t.Errorf("body = %q, want webdav options", gotBody)
		}
	})

	t.Run("restore", func(t *testing.T) {
		var gotURI string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotURI = r.Method + " " + r.RequestURI
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true,"restored":3}`)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "", false)
		n, err := c.Restore(opts)
		if err != nil {
			t.Fatalf("Restore: %v", err)
		}
		if n != 3 {
			t.Errorf("restored = %d, want 3", n)
		}
		if want := "POST /api/control/restore"; gotURI != want {
			t.Errorf("request = %q, want %q", gotURI, want)
		}
	})
}

// TestListProxies verifies ListProxies decodes the proxies response.
func TestListProxies(t *testing.T) {
	const body = `{"proxies":{"G":{"name":"G","type":"Selector","now":"A","all":["A","B"]}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.RequestURI != "/api/clash/proxies" {
			t.Errorf("unexpected request: %s %s", r.Method, r.RequestURI)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", false)
	resp, err := c.ListProxies()
	if err != nil {
		t.Fatalf("ListProxies: %v", err)
	}
	proxy, ok := resp.Proxies["G"]
	if !ok {
		t.Fatal("proxy G not found")
	}
	if proxy.Now != "A" {
		t.Errorf("Now = %q, want A", proxy.Now)
	}
	if len(proxy.All) != 2 || proxy.All[0] != "A" || proxy.All[1] != "B" {
		t.Errorf("All = %v, want [A,B]", proxy.All)
	}
}

// TestSelectProxy verifies SelectProxy sends PUT with the correct body.
func TestSelectProxy(t *testing.T) {
	var gotURI, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", false)
	if err := c.SelectProxy("G", "B"); err != nil {
		t.Fatalf("SelectProxy: %v", err)
	}
	if want := "PUT /api/clash/proxies/G"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.Name != "B" {
		t.Errorf("body.name = %q, want B", body.Name)
	}
}

// TestSelectProxyError verifies error propagation from SelectProxy.
func TestSelectProxyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid group"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", false)
	err := c.SelectProxy("bad", "node")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid group") {
		t.Errorf("error = %q, want server message", err)
	}
}

// TestGetMode verifies GetMode decodes the mode field.
func TestGetMode(t *testing.T) {
	const body = `{"mode":"rule","port":7890}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.RequestURI != "/api/clash/configs" {
			t.Errorf("unexpected request: %s %s", r.Method, r.RequestURI)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", false)
	mode, err := c.GetMode()
	if err != nil {
		t.Fatalf("GetMode: %v", err)
	}
	if mode != "rule" {
		t.Errorf("mode = %q, want rule", mode)
	}
}

// TestSetMode verifies SetMode sends PATCH with the correct body.
func TestSetMode(t *testing.T) {
	var gotURI, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", false)
	if err := c.SetMode("global"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if want := "PATCH /api/clash/configs"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.Mode != "global" {
		t.Errorf("body.mode = %q, want global", body.Mode)
	}
}
