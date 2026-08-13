package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
)

// TestConfigMenu verifies the Config tab lists the kernel operations plus the
// config viewer entry.
func TestConfigMenu(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(keyPress("3"))

	got := ansiRe.ReplaceAllString(nm.View().Content, "")
	for _, want := range []string{"Start", "Stop", "Restart", "View Config"} {
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

	// Move selection down to the View Config entry (last).
	for range kernelOps {
		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on View Config returned no command")
	}
	if !nm.(Model).viewingConfig {
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
	mdl.viewingConfig = true
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
	mdl.viewingConfig = true
	nm = mdl

	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("esc should not return a command")
	}
	if nm.(Model).viewingConfig {
		t.Fatal("viewingConfig should be false after esc")
	}
}
