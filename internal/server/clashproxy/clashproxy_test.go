package clashproxy

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	authmw "metacubexd-server-go/internal/server/auth"
)

// mockMihomo starts a test HTTP server that mimics mihomo's Clash API.
func mockMihomo(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"version": "test-v1.0.0"})
	})
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"mixed-port": 7890})
	})
	mux.HandleFunc("/traffic", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"upload": 0, "download": 0})
	})
	mux.HandleFunc("/memory", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"inuse": 0})
	})
	mux.HandleFunc("/connections", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"connections": []any{}})
	})
	mux.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"logs": []any{}})
	})
	// WS endpoint for /traffic — server-push text frames.
	mux.HandleFunc("/ws/traffic", func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Send a few frames then close.
		for i := 0; i < 3; i++ {
			conn.WriteMessage(websocket.TextMessage, []byte(`{"upload":0,"download":0}`))
			time.Sleep(10 * time.Millisecond)
		}
	})
	return httptest.NewServer(mux)
}

// mockKernelDown returns a server that always returns 503.
func mockKernelDown(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
}

func TestHTTPProxyVersion(t *testing.T) {
	upstream := mockMihomo(t)
	defer upstream.Close()

	// Extract host:port from the upstream URL.
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))

	handler := New(func() (string, string, bool) {
		return "127.0.0.1:" + port, "test-secret", true
	})

	req := httptest.NewRequest("GET", "/api/clash/version", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["version"] != "test-v1.0.0" {
		t.Errorf("version = %q, want test-v1.0.0", body["version"])
	}
}

func TestHTTPProxyConfigs(t *testing.T) {
	upstream := mockMihomo(t)
	defer upstream.Close()

	_, port, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	handler := New(func() (string, string, bool) {
		return "127.0.0.1:" + port, "", true
	})

	req := httptest.NewRequest("GET", "/api/clash/configs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHTTPProxyTraffic(t *testing.T) {
	upstream := mockMihomo(t)
	defer upstream.Close()

	_, port, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	handler := New(func() (string, string, bool) {
		return "127.0.0.1:" + port, "", true
	})

	req := httptest.NewRequest("GET", "/api/clash/traffic", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestKernelNotRunningReturns503(t *testing.T) {
	handler := New(func() (string, string, bool) {
		return "", "", false
	})

	req := httptest.NewRequest("GET", "/api/clash/version", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "kernel not running") {
		t.Errorf("body should mention kernel not running: %s", w.Body.String())
	}
}

func TestKernelNotRunningWSReturnsClose(t *testing.T) {
	handler := New(func() (string, string, bool) {
		return "", "", false
	})

	// Use a real HTTP test server to test WS upgrade.
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Convert http:// to ws://.
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/api/clash/traffic"
	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err == nil {
		conn.Close()
		t.Fatal("expected WS dial to fail when kernel is down")
	}
	if resp != nil && resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("WS upgrade status = %d, want 503", resp.StatusCode)
	}
}

func TestWSBridgeTraffic(t *testing.T) {
	// Create a mock upstream WS server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for i := 0; i < 3; i++ {
			conn.WriteMessage(websocket.TextMessage, []byte(`{"upload":100,"download":200}`))
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer upstream.Close()

	_, port, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	handler := New(func() (string, string, bool) {
		return "127.0.0.1:" + port, "", true
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/api/clash/traffic"
	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WS dial: %v (resp=%v)", err, resp)
	}
	defer conn.Close()

	// Read 3 frames from the upstream via the bridge.
	for i := 0; i < 3; i++ {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage #%d: %v", i, err)
		}
		if !strings.Contains(string(msg), "upload") {
			t.Errorf("frame %d: %q, want upload field", i, msg)
		}
	}
}

func TestStripPrefix(t *testing.T) {
	tests := []struct {
		path, prefix, want string
	}{
		{"/api/clash/version", "/api/clash", "/version"},
		{"/api/clash/", "/api/clash", ""},
		{"/api/clash", "/api/clash", ""},
		{"/api/clash/traffic", "/api/clash", "/traffic"},
		{"/other/path", "/api/clash", "/other/path"},
	}
	for _, tt := range tests {
		got := stripPrefix(tt.path, tt.prefix)
		if got != tt.want {
			t.Errorf("stripPrefix(%q, %q) = %q, want %q", tt.path, tt.prefix, got, tt.want)
		}
	}
}

// TestHTTPProxyInjectsManagedSecret verifies clashproxy injects the managed
// CLASH_SECRET on every request and drops any client-supplied Authorization.
//
// The mock records the Authorization header it received so we can assert
// the managed secret is attached verbatim.
func TestHTTPProxyInjectsManagedSecret(t *testing.T) {
	type testcase struct {
		name         string
		managedEmpty bool // state returns empty secret (CLASH_SECRET not configured)
		clientAuth   string
		wantAuth     string // Authorization the upstream should observe
	}
	cases := []testcase{
		{
			name:       "client Authorization dropped, managed secret injected",
			clientAuth: "Bearer caller-supplied",
			wantAuth:   "Bearer test-secret",
		},
		{
			name:       "no client Authorization, managed secret injected",
			clientAuth: "",
			wantAuth:   "Bearer test-secret",
		},
		{
			name:         "empty managed secret → no Authorization forwarded",
			managedEmpty: true,
			clientAuth:   "Bearer caller-supplied",
			wantAuth:     "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var observedAuth string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observedAuth = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"version":"ok"}`))
			}))
			defer upstream.Close()

			_, port, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
			managedSecret := "test-secret"
			if tc.managedEmpty {
				managedSecret = ""
			}
			handler := New(func() (string, string, bool) {
				return "127.0.0.1:" + port, managedSecret, true
			})

			req := httptest.NewRequest("GET", "/api/clash/version", nil)
			if tc.clientAuth != "" {
				req.Header.Set("Authorization", tc.clientAuth)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (clashproxy must forward every request)", w.Code)
			}
			if observedAuth != tc.wantAuth {
				t.Errorf("upstream Authorization = %q, want %q", observedAuth, tc.wantAuth)
			}
		})
	}
}

// TestOpenModeAuthMiddlewareLetsClashThrough: in a password-less deploy
// (CONTROL_TOKEN unset), the auth middleware is a pass-through, so requests
// reach clashproxy, which injects the managed CLASH_SECRET.
func TestOpenModeAuthMiddlewareLetsClashThrough(t *testing.T) {
	var observedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"version":"ok"}`))
	}))
	defer upstream.Close()

	_, port, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	clashHandler := New(func() (string, string, bool) {
		return "127.0.0.1:" + port, "managed-secret", true
	})

	// Compose the full chain exactly like main.go does: auth(mux). The auth
	// middleware here is built with an empty password = open mode.
	chain := authmw.New(authmw.Config{ControlToken: ""})(clashHandler)

	// A same-origin dashboard fetch: no cookie, no Authorization, no ?token=.
	// In open mode this must still reach mihomo with the managed secret.
	req := httptest.NewRequest("GET", "/api/clash/version", nil)
	w := httptest.NewRecorder()
	chain.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (open mode should let /api/clash/* through)", w.Code)
	}
	if observedAuth != "Bearer managed-secret" {
		t.Errorf("upstream Authorization = %q, want \"Bearer managed-secret\" (open mode must inject)", observedAuth)
	}
}
