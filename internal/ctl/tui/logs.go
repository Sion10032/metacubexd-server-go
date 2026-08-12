package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LogsModel renders the realtime kernel log stream in a scrollable viewport.
type LogsModel struct {
	viewport viewport.Model
	lines    []string
	filter   string
	follow   bool
}

// NewLogsModel returns a log view with follow-at-bottom enabled. The size is
// a placeholder until WindowSizeMsg sets the real one (1.30).
func NewLogsModel() LogsModel {
	return LogsModel{
		viewport: viewport.New(80, 20),
		follow:   true,
	}
}

// SetSize resizes the log viewport.
func (l *LogsModel) SetSize(width, height int) {
	l.viewport.Width = width
	l.viewport.Height = height
}

// Update forwards messages to the viewport.
func (l *LogsModel) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	l.viewport, cmd = l.viewport.Update(msg)
	return cmd
}

// append adds a log line, respecting the active filter, and re-renders the
// viewport. When follow is enabled the viewport scrolls to the bottom.
func (l *LogsModel) append(line string) {
	if l.filter != "" && !strings.Contains(stripANSI(line), l.filter) {
		return
	}
	l.lines = append(l.lines, line)
	l.viewport.SetContent(strings.Join(l.lines, "\n"))
	if l.follow {
		l.viewport.GotoBottom()
	}
}

// stripANSI removes SGR escape sequences for plain-text matching.
func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// View renders the viewport contents, or a centered hint filling the whole
// area when no log lines have arrived yet.
func (l LogsModel) View() string {
	if len(l.lines) == 0 {
		return lipgloss.NewStyle().
			Width(l.viewport.Width).
			Height(l.viewport.Height).
			Align(lipgloss.Center, lipgloss.Center).
			Faint(true).
			Render("— waiting for log stream —")
	}
	return l.viewport.View()
}
