package tui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// maxLogLines caps the retained log lines to bound memory; the viewport only
// ever shows a window of them.
const maxLogLines = 1000

// LogsModel renders the realtime kernel log stream in a scrollable viewport.
type LogsModel struct {
	viewport viewport.Model
	allLines []string // full history (capped); filter only affects the view
	filter   string
	follow   bool
}

// NewLogsModel returns a log view with follow-at-bottom enabled. The size is
// a placeholder until WindowSizeMsg sets the real one (1.30).
func NewLogsModel() LogsModel {
	return LogsModel{
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		follow:   true,
	}
}

// SetSize resizes the log viewport. When following, the view re-scrolls to
// the bottom so the latest lines stay visible after a resize — without this,
// the scroll offset computed at the old height leaves blank space below.
func (l *LogsModel) SetSize(width, height int) {
	l.viewport.SetWidth(width)
	l.viewport.SetHeight(height)
	if l.follow {
		l.viewport.GotoBottom()
	}
}

// SetFilter updates the active filter and re-renders the retained history.
// Clearing the filter (empty string) restores the full history.
func (l *LogsModel) SetFilter(filter string) {
	l.filter = filter
	l.render()
}

// Update forwards messages to the viewport.
func (l *LogsModel) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	l.viewport, cmd = l.viewport.Update(msg)
	return cmd
}

// append adds a log line to the history and re-renders the view.
func (l *LogsModel) append(line string) {
	l.allLines = append(l.allLines, line)
	if len(l.allLines) > maxLogLines {
		l.allLines = append([]string(nil), l.allLines[len(l.allLines)-maxLogLines:]...)
	}
	l.render()
}

// render rebuilds the viewport content from the full history, applying the
// active filter. The history itself is never discarded, so clearing the
// filter restores everything.
func (l *LogsModel) render() {
	visible := l.allLines
	if l.filter != "" {
		// Allocate a fresh slice: [:0] would reuse allLines' backing array
		// and clobber the history while filtering.
		var kept []string
		for _, line := range visible {
			if strings.Contains(stripANSI(line), l.filter) {
				kept = append(kept, line)
			}
		}
		visible = kept
	}
	l.viewport.SetContent(strings.Join(visible, "\n"))
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
	if len(l.allLines) == 0 {
		return lipgloss.NewStyle().
			Width(l.viewport.Width()).
			Height(l.viewport.Height()).
			Align(lipgloss.Center, lipgloss.Center).
			Faint(true).
			Render("— waiting for log stream —")
	}
	return l.viewport.View()
}
