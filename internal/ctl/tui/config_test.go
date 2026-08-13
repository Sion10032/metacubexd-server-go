package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"metacubexd-server-go/internal/ctl"
)

// TestConfigTabLoad verifies entering the Config tab fetches and renders the
// active config.
func TestConfigTabLoad(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		w.Header().Set("Content-Type", "text/yaml")
		fmt.Fprint(w, "mixed-port: 7890\n")
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	nm, cmd := nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if cmd == nil {
		t.Fatal("entering Config tab returned no command")
	}
	msg := cmd()
	if want := "GET /api/control/config"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	if got := msg.(configLoadedMsg); got.mode != configActive || got.content != "mixed-port: 7890\n" {
		t.Fatalf("fetch msg = %+v, want active config", got)
	}

	nm, _ = nm.Update(msg)
	got := ansiRe.ReplaceAllString(nm.View(), "")
	if !strings.Contains(got, "config (active):") {
		t.Errorf("View missing config mode header:\n%s", got)
	}
	if !strings.Contains(got, "mixed-port") {
		t.Errorf("View missing config content:\n%s", got)
	}
}

// TestConfigToggleKey verifies c toggles to the runtime config and fetches it.
func TestConfigToggleKey(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		w.Header().Set("Content-Type", "text/yaml")
		fmt.Fprint(w, "runtime: true\n")
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	nm, _ = nm.Update(configLoadedMsg{mode: configActive, content: "active: true\n"})

	nm, cmd := nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
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
