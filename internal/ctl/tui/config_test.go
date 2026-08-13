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
	"metacubexd-server-go/internal/ctl/tui/pages/kernel"
	"metacubexd-server-go/internal/ctl/tui/shared"
)

// TestConfigMenu verifies the Config tab lists the kernel operations, the
// network fields and the raw YAML viewer entry.
func TestConfigMenu(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(keyPress("3"))

	got := shared.ANSIRe.ReplaceAllString(nm.View().Content, "")
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
	for i := 0; i < kernel.ConfigMenuLen()-1; i++ {
		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on View Config returned no command")
	}
	if !nm.(Model).tabs[2].(*kernel.Model).ViewingConfig() {
		t.Fatal("viewingConfig should be true")
	}
	msg := cmd()
	if want := "GET /api/control/config"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	if got := msg.(kernel.ConfigLoadedMsg); got.Mode != kernel.ConfigActive || got.Content != "mixed-port: 7890\n" {
		t.Fatalf("fetch msg = %+v, want active config", got)
	}

	nm, _ = nm.Update(msg)
	got := shared.ANSIRe.ReplaceAllString(nm.View().Content, "")
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
	mdl.tabs[2].(*kernel.Model).SetViewingConfig(true)
	nm = mdl
	nm, _ = nm.Update(kernel.ConfigLoadedMsg{Mode: kernel.ConfigActive, Content: "active: true\n"})

	nm, cmd := nm.Update(keyPress("c"))
	if cmd == nil {
		t.Fatal("c returned no command")
	}
	if got := nm.(Model).tabs[2].(*kernel.Model).ConfigMode(); got != "runtime" {
		t.Errorf("mode after c = %q, want runtime", got)
	}
	msg := cmd()
	if want := "GET /api/control/config/runtime"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	if got := msg.(kernel.ConfigLoadedMsg); got.Mode != kernel.ConfigRuntime {
		t.Errorf("fetch mode = %d, want runtime", got.Mode)
	}
}

// TestConfigViewerClose verifies esc closes the config viewer modal.
func TestConfigViewerClose(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mdl := nm.(Model)
	mdl.tabs[2].(*kernel.Model).SetViewingConfig(true)
	nm = mdl

	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("esc should not return a command")
	}
	if nm.(Model).tabs[2].(*kernel.Model).ViewingConfig() {
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
	mdl.tabs[2].(*kernel.Model).SetViewingConfig(true)
	nm = mdl

	nm, _ = nm.Update(keyPress("e"))
	if !nm.(Model).tabs[2].(*kernel.Model).EditingSection() {
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
	nm, _ = nm.Update(kernel.NetworkSettingsMsg{Settings: kernel.NetworkSettings{
		Values: map[string]any{"mixed-port": 7890, "port": 7890, "socks-port": 7891},
		Loaded: true,
	}})
	nm, _ = nm.Update(keyPress("3")) // Config tab

	for i := 0; i < kernel.OpCount(); i++ {
		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !nm.(Model).tabs[2].(*kernel.Model).Editing() {
		t.Fatal("editing should be true after enter on mixed-port")
	}
	if got := nm.(Model).tabs[2].(*kernel.Model).EditFieldValue(); got != "7890" {
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
	nm, _ = nm.Update(kernel.NetworkSettingsMsg{Settings: kernel.NetworkSettings{
		Values: map[string]any{"tun": map[string]any{"enable": true, "device": "tun0", "stack": "system"}},
		Loaded: true,
	}})
	nm, _ = nm.Update(keyPress("3"))

	// Move to tun-enable (index len(kernelOps)+3).
	for i := 0; i < kernel.OpCount()+3; i++ {
		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := nm.(Model).tabs[2].(*kernel.Model).EditFieldValue(); got != "true" {
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
