package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/profile"
)

// TestProfilesSetRows verifies SetProfiles builds rows and marks the active
// profile, and SelectedID follows the cursor.
func TestProfilesSetRows(t *testing.T) {
	p := NewProfilesModel()
	list := []profile.Meta{
		{ID: "a", Name: "base", Type: "local", UpdatedAt: 1723456789000},
		{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1723456789000},
	}
	p.SetProfiles(list, "b")
	got := ansiRe.ReplaceAllString(p.View(), "")
	if !strings.Contains(got, "base") || !strings.Contains(got, "sub") {
		t.Errorf("table view missing profile names:\n%s", got)
	}
	if !strings.Contains(got, "●") {
		t.Errorf("table view missing active marker:\n%s", got)
	}
	if id := p.SelectedID(); id != "a" {
		t.Errorf("SelectedID = %q, want a (first row)", id)
	}

	// Empty list renders empty and SelectedID is empty.
	p.SetProfiles(nil, "")
	if id := p.SelectedID(); id != "" {
		t.Errorf("SelectedID on empty list = %q, want empty", id)
	}
}

// TestProfilesTabLoaded verifies loading profiles renders the table and the
// status bar's second line shows the active profile summary.
func TestProfilesTabLoaded(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(profilesLoadedMsg{list: []profile.Meta{
		{ID: "a", Name: "base", Type: "local", UpdatedAt: 1723456789000},
		{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1723456789000},
	}})
	mdl := nm.(Model)
	mdl.profActive = "b"
	nm = mdl

	if got := nm.View(); !strings.Contains(got, "active: sub (remote)") {
		t.Errorf("View missing active profile summary:\n%s", got)
	}

	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	got := ansiRe.ReplaceAllString(nm.View(), "")
	if !strings.Contains(got, "sub") || !strings.Contains(got, "●") {
		t.Errorf("profiles tab missing table content:\n%s", got)
	}
}
