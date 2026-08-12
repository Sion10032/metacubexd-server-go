package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"metacubexd-server-go/internal/ctl"
)

// ansiRe strips SGR escape sequences so layout assertions ignore colors.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// TestViewLayout verifies the full layout renders the status bar, the tab bar
// and the help line.
func TestViewLayout(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	got := ansiRe.ReplaceAllString(m.View(), "")
	for _, want := range []string{"[1] Logs", "[2] Profiles", "[3] Config", "s:start", "q:quit"} {
		if !strings.Contains(got, want) {
			t.Errorf("View = %q, missing %q", got, want)
		}
	}
}

// TestTabSwitch verifies the 2/3 keys switch tabs and the placeholder body
// shows for unimplemented tabs.
func TestTabSwitch(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if got := nm.View(); !strings.Contains(got, "Profiles tab — not implemented yet") {
		t.Errorf("View after tab 2 = %q, want Profiles placeholder", got)
	}

	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if got := nm.View(); strings.Contains(got, "not implemented yet") {
		t.Errorf("View after tab 1 = %q, want log viewport", got)
	}
}

// TestWindowResize verifies the framed layout fills the whole window: title
// border on top, status bar visible, bottom border, and every line at full
// width.
func TestWindowResize(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))

	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	lines := strings.Split(strings.TrimRight(nm.View(), "\n"), "\n")
	if len(lines) != 24 {
		t.Errorf("layout = %d lines, want 24 (fills the window)", len(lines))
	}
	if !strings.HasPrefix(lines[0], "┌") || !strings.Contains(lines[0], "mihomo-tui") {
		t.Errorf("line 0 = %q, want top border with title", lines[0])
	}
	if !strings.Contains(lines[1], "unknown") {
		t.Errorf("line 1 = %q, want status bar", lines[1])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "└") {
		t.Errorf("last line = %q, want bottom border", lines[len(lines)-1])
	}
	for i, l := range lines {
		if lipgloss.Width(l) != 80 {
			t.Errorf("line %d width = %d, want 80", i, lipgloss.Width(l))
		}
	}
}
