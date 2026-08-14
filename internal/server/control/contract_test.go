package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"metacubexd-server-go/internal/server/profile"
	"metacubexd-server-go/internal/server/supervisor"
)

// contractRouter builds a minimal Router with real ProfileStore + stub Supervisor.
func contractRouter(t *testing.T) Router {
	t.Helper()
	dir := t.TempDir()
	store := profile.New(profile.Options{
		Dir:              filepath.Join(dir, "profiles"),
		ActiveConfigPath: filepath.Join(dir, "active.yaml"),
	})
	sup := supervisor.New(supervisor.Options{
		BinaryPath:       "/usr/bin/false",
		HomeDir:          dir,
		ActiveConfigPath: filepath.Join(dir, "active.yaml"),
		StartTimeout:     time.Second,
		StopTimeout:      time.Second,
		ValidateTimeout:  time.Second,
	})
	return Router{
		Supervisor:       sup,
		Profiles:         store,
		Token:            "",
		MihomoBin:        "/usr/bin/false",
		MihomoVersion:    "test",
		HomeDir:          dir,
		ActiveConfigPath: filepath.Join(dir, "active.yaml"),
	}
}

// endpoint records one contract test case.
type endpoint struct {
	method  string
	path    string
	body    string
	wantMin int // minimum acceptable status (inclusive); 0 = skip range check
	wantMax int // maximum acceptable status (inclusive)
	desc    string
}

func TestContractEndpoints(t *testing.T) {
	r := contractRouter(t)
	handler := New(r)

	// Create a test profile so GET /profiles/{id} works.
	createResp := doRequest(handler, "POST", "/api/control/profiles",
		`{"name":"c1","content":"a: 1"}`)
	var created map[string]any
	jsonDecode(t, createResp.Body, &created)
	profileID, _ := created["id"].(string)
	if profileID == "" {
		t.Fatalf("failed to create test profile: %v", created)
	}

	endpoints := []endpoint{
		// Health / Info (public, no auth)
		{"GET", "/api/control/health", "", 200, 200, "health check"},
		{"GET", "/api/control/info", "", 200, 200, "server info"},

		// Kernel lifecycle
		{"GET", "/api/control/kernel/status", "", 200, 200, "kernel status"},
		{"POST", "/api/control/kernel/start", "", 200, 503, "start (no real mihomo)"},
		{"POST", "/api/control/kernel/stop", "", 200, 503, "stop"},
		{"POST", "/api/control/kernel/restart", "", 200, 503, "restart"},
		{"POST", "/api/control/kernel/rollback", "", 404, 503, "rollback (no backup)"},
		{"POST", "/api/control/kernel/recover", "", 200, 503, "recover"},

		// Profiles CRUD
		{"GET", "/api/control/profiles", "", 200, 200, "list profiles"},
		{"POST", "/api/control/profiles", `{"name":"test","content":"a: 1"}`, 200, 201, "create profile"},
		{"GET", "/api/control/profiles/" + profileID, "", 200, 200, "get profile"},
		{"PUT", "/api/control/profiles/" + profileID, `{"name":"updated"}`, 200, 200, "update profile"},
		{"POST", "/api/control/profiles/" + profileID + "/duplicate", "", 200, 201, "duplicate profile"},
		{"POST", "/api/control/profiles/" + profileID + "/activate", "", 200, 400, "activate profile"},
		{"POST", "/api/control/profiles/" + profileID + "/validate", "", 200, 503, "validate profile"},
		{"GET", "/api/control/profiles/nonexistent-id", "", 404, 404, "get nonexistent profile"},
		{"DELETE", "/api/control/profiles/nonexistent-id", "", 404, 404, "delete nonexistent profile"},

		// Config
		{"GET", "/api/control/config", "", 200, 200, "get active config"},
		{"GET", "/api/control/config/runtime", "", 200, 200, "get runtime config"},
		{"GET", "/api/control/config/section?key=rules", "", 200, 204, "get config section"},

		// 404 for unknown routes
		{"GET", "/api/control/nonexistent", "", 404, 404, "unknown route"},
		{"POST", "/api/control/unknown/action", "", 404, 404, "unknown action"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			resp := doRequest(handler, ep.method, ep.path, ep.body)
			status := resp.Code

			if ep.wantMin > 0 && ep.wantMax > 0 {
				if status < ep.wantMin || status > ep.wantMax {
					t.Errorf("status %d not in [%d, %d] — %s", status, ep.wantMin, ep.wantMax, ep.desc)
				}
			} else {
				if status >= 500 && status != 503 {
					t.Errorf("unexpected %d — %s", status, ep.desc)
				}
			}

			ct := resp.Header().Get("Content-Type")
			if !strings.Contains(ct, "text/event-stream") && !strings.Contains(ct, "text/yaml") {
				if !strings.Contains(ct, "json") && !strings.Contains(ct, "text/plain") {
					t.Errorf("Content-Type %q should be JSON — %s", ct, ep.desc)
				}
			}
		})
	}
}

func TestContractInfoFeatures(t *testing.T) {
	r := contractRouter(t)
	handler := New(r)

	resp := doRequest(handler, "GET", "/api/control/info", "")
	var info map[string]any
	jsonDecode(t, resp.Body, &info)

	features, ok := info["features"].([]any)
	if !ok {
		t.Fatalf("features not an array: %v", info["features"])
	}

	mustHave := []string{"profiles", "kernel-control", "logs-sse"}
	for _, f := range mustHave {
		found := false
		for _, feat := range features {
			if feat == f {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("feature %q missing from info.features", f)
		}
	}

	mustNotHave := []string{"visual-config-editor", "system-proxy", "kernel-version", "tun"}
	for _, f := range mustNotHave {
		for _, feat := range features {
			if feat == f {
				t.Errorf("feature %q should NOT be in info.features (cut)", f)
			}
		}
	}
}

func TestContractProfileCreateShape(t *testing.T) {
	r := contractRouter(t)
	handler := New(r)

	resp := doRequest(handler, "POST", "/api/control/profiles",
		`{"name":"shape-test","content":"x: 1","type":"local"}`)
	if resp.Code != 200 {
		t.Fatalf("create status = %d, want 200", resp.Code)
	}

	var m map[string]any
	jsonDecode(t, resp.Body, &m)

	for _, field := range []string{"id", "name", "type", "updatedAt"} {
		if _, ok := m[field]; !ok {
			t.Errorf("created profile missing field %q", field)
		}
	}
	if m["name"] != "shape-test" {
		t.Errorf("name = %v, want shape-test", m["name"])
	}
	if m["type"] != "local" {
		t.Errorf("type = %v, want local", m["type"])
	}
}

func doRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func jsonDecode(t *testing.T, buf interface{ Bytes() []byte }, v any) {
	t.Helper()
	if err := json.Unmarshal(buf.Bytes(), v); err != nil {
		t.Fatalf("decode JSON: %v\nbody: %s", err, string(buf.Bytes()))
	}
}
