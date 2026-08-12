package tui

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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

// Update forwards messages to the viewport.
func (l *LogsModel) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	l.viewport, cmd = l.viewport.Update(msg)
	return cmd
}

// View renders the viewport contents.
func (l LogsModel) View() string {
	return l.viewport.View()
}
