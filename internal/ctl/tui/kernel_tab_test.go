package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"metacubexd-server-go/internal/ctl"
)

// TestKernelTabRender verifies the kernel tab lists every operation with the
// selected entry highlighted.
func TestKernelTabRender(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	got := renderKernelTab(nil, 0, nil)
	plain := ansiRe.ReplaceAllString(got, "")
	for _, want := range []string{"Start", "Stop", "Restart", "Rollback", "Recover"} {
		if !strings.Contains(plain, want) {
			t.Errorf("kernel tab = %q, missing %q", plain, want)
		}
	}
	if !strings.Contains(got, "> ") || !strings.Contains(got, "\x1b[") {
		t.Errorf("kernel tab missing selection highlight: %q", got)
	}
}

// TestKernelTabSelect verifies up/down move the selection with wraparound.
func TestKernelTabSelect(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")}) // Config tab

	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyDown})
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := nm.(Model).kSelected; got != 2 {
		t.Errorf("kSelected after 2x down = %d, want 2", got)
	}

	// up wraps to the last entry.
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := nm.(Model).kSelected; got != 1 {
		t.Errorf("kSelected after up = %d, want 1", got)
	}
}

// TestKernelTabExecute verifies enter runs the selected operation over HTTP
// and the fresh state lands in the model.
func TestKernelTabExecute(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"starting"}`)
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")}) // Config tab

	nm, cmd := nm.Update(tea.KeyMsg{Type: tea.KeyEnter}) // enter on Start
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	if _, ok := cmd().(statusLoadedMsg); !ok {
		t.Fatalf("cmd returned %T, want statusLoadedMsg", cmd())
	}
	if want := "POST /api/control/kernel/start"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	_ = nm
}
