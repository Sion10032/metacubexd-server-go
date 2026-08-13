package tui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
)

// TestConfigMenu verifies the Config tab lists the kernel operations, the
// network fields and the raw YAML viewer entry.
func TestConfigMenu(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(keyPress("3"))

	got := ansiRe.ReplaceAllString(nm.View().Content, "")
	for _, want := range []string{"Start", "Stop", "Restart", "mixed-port", "http-port", "socks-port", "tun-enable", "tun-device", "tun-stack", "View YAML"} {
		if !strings.Contains(got, want) {
			t.Errorf("Config tab = %q, missing %q", got, want)
		}
	}
}

// TestConfigViewerOpen verifies selecting the View Config entry opens the
// modal, fetches the active config, and renders it.
func TestConfigViewerOpen(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		w.Header().Set("Content-Type", "text/yaml")
		fmt.Fprint(w, "mixed-port: 7890\n")
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(keyPress("3")) // Config tab

	// Move selection down to the View YAML entry (last).
	for i := 0; i < len(kernelOps)+len(networkFields); i++ {
		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on View Config returned no command")
	}
	if !nm.(Model).kernel.viewingConfig {
		t.Fatal("viewingConfig should be true")
	}
	msg := cmd()
	if want := "GET /api/control/config"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	if got := msg.(configLoadedMsg); got.mode != configActive || got.content != "mixed-port: 7890\n" {
		t.Fatalf("fetch msg = %+v, want active config", got)
	}

	nm, _ = nm.Update(msg)
	got := ansiRe.ReplaceAllString(nm.View().Content, "")
	if !strings.Contains(got, "View Config (active)") {
		t.Errorf("View missing config modal header:\n%s", got)
	}
	if !strings.Contains(got, "mixed-port") {
		t.Errorf("View missing config content:\n%s", got)
	}
}

// TestConfigViewerToggle verifies c inside the viewer toggles to the runtime
// config and fetches it.
func TestConfigViewerToggle(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		w.Header().Set("Content-Type", "text/yaml")
		fmt.Fprint(w, "runtime: true\n")
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mdl := nm.(Model)
	mdl.kernel.viewingConfig = true
	nm = mdl
	nm, _ = nm.Update(configLoadedMsg{mode: configActive, content: "active: true\n"})

	nm, cmd := nm.Update(keyPress("c"))
	if cmd == nil {
		t.Fatal("c returned no command")
	}
	if got := nm.(Model).config.Mode(); got != "runtime" {
		t.Errorf("mode after c = %q, want runtime", got)
	}
	msg := cmd()
	if want := "GET /api/control/config/runtime"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	if got := msg.(configLoadedMsg); got.mode != configRuntime {
		t.Errorf("fetch mode = %d, want runtime", got.mode)
	}
}

// TestConfigViewerClose verifies esc closes the config viewer modal.
func TestConfigViewerClose(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mdl := nm.(Model)
	mdl.kernel.viewingConfig = true
	nm = mdl

	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("esc should not return a command")
	}
	if nm.(Model).kernel.viewingConfig {
		t.Fatal("viewingConfig should be false after esc")
	}
}

// TestSectionEdit verifies e opens the section editor and enter saves the
// key/value via PutSection.
func TestSectionEdit(t *testing.T) {
	var gotURI, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"running"}`)
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mdl := nm.(Model)
	mdl.kernel.viewingConfig = true
	nm = mdl

	nm, _ = nm.Update(keyPress("e"))
	if !nm.(Model).kernel.editingSection {
		t.Fatal("editingSection should be true after e")
	}

	for _, r := range "mixed-port" {
		nm, _ = nm.Update(keyPress(string(r)))
	}
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	for _, r := range "7890" {
		nm, _ = nm.Update(keyPress(string(r)))
	}

	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	cmd()
	if want := "PUT /api/control/config/section"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	if !strings.Contains(gotBody, `"key":"mixed-port"`) || !strings.Contains(gotBody, `"value":7890`) {
		t.Errorf("body = %q, want key and parsed value", gotBody)
	}
}

// TestNetworkFieldEdit verifies selecting mixed-port opens the editor prefilled
// with the current value, and enter saves via PutSection.
func TestNetworkFieldEdit(t *testing.T) {
	var gotURI, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"running"}`)
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(networkSettingsMsg{settings: networkSettings{
		values: map[string]any{"mixed-port": 7890, "port": 7890, "socks-port": 7891},
		loaded: true,
	}})
	nm, _ = nm.Update(keyPress("3")) // Config tab

	for i := 0; i < len(kernelOps); i++ {
		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !nm.(Model).kernel.editing {
		t.Fatal("editing should be true after enter on mixed-port")
	}
	if got := nm.(Model).kernel.editInput.Value(); got != "7890" {
		t.Errorf("editInput = %q, want 7890", got)
	}

	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	cmd()
	if want := "PUT /api/control/config/section"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	if !strings.Contains(gotBody, `"key":"mixed-port"`) || !strings.Contains(gotBody, `"value":7890`) {
		t.Errorf("body = %q, want key and value", gotBody)
	}
}

// TestTunFieldEdit verifies editing a tun sub-field rebuilds the whole tun
// object and sends it as one section value.
func TestTunFieldEdit(t *testing.T) {
	var gotURI, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"running"}`)
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(networkSettingsMsg{settings: networkSettings{
		values: map[string]any{"tun": map[string]any{"enable": true, "device": "tun0", "stack": "system"}},
		loaded: true,
	}})
	nm, _ = nm.Update(keyPress("3"))

	// Move to tun-enable (index len(kernelOps)+3).
	for i := 0; i < len(kernelOps)+3; i++ {
		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := nm.(Model).kernel.editInput.Value(); got != "true" {
		t.Errorf("editInput = %q, want true", got)
	}

	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	cmd()
	if want := "PUT /api/control/config/section"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	if !strings.Contains(gotBody, `"key":"tun"`) {
		t.Errorf("body = %q, want key tun", gotBody)
	}
	if !strings.Contains(gotBody, `"enable":true`) {
		t.Errorf("body = %q, want enable true", gotBody)
	}
}

// TestParseNetworkSettings verifies the runtime YAML is parsed into the
// editable network fields, including the nested tun object.
func TestParseNetworkSettings(t *testing.T) {
	content := "mixed-port: 7890\nport: 7890\nsocks-port: 7891\ntun:\n  enable: true\n  device: tun0\n  stack: system\n"
	ns := parseNetworkSettings(content)
	if !ns.loaded {
		t.Fatal("loaded should be true")
	}
	for _, f := range networkFields {
		if got := ns.valueOf(f); got == "" {
			t.Errorf("%s value empty, want non-empty", f.label)
		}
	}
	if got := ns.valueOf(networkFields[3]); got != "true" { // tun-enable
		t.Errorf("tun-enable = %q, want true", got)
	}
	if got := ns.valueOf(networkFields[5]); got != "system" { // tun-stack
		t.Errorf("tun-stack = %q, want system", got)
	}
}

// TestTunDefaults verifies tun sub-fields absent from the config fall back to
// mihomo's defaults (device=Mihomo, stack=mixed).
func TestTunDefaults(t *testing.T) {
	ns := parseNetworkSettings("tun:\n  enable: false\n")
	fields := map[string]string{}
	for _, f := range networkFields {
		fields[f.label] = ns.valueOf(f)
	}
	if fields["tun-enable"] != "false" {
		t.Errorf("tun-enable = %q, want false", fields["tun-enable"])
	}
	if fields["tun-device"] != "Mihomo" {
		t.Errorf("tun-device = %q, want Mihomo", fields["tun-device"])
	}
	if fields["tun-stack"] != "mixed" {
		t.Errorf("tun-stack = %q, want mixed", fields["tun-stack"])
	}
}
