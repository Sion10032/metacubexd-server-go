// Package logs implements the Logs tab: the realtime kernel log stream in a
// scrollable viewport, with an inline filter and follow-at-bottom control.
// It consumes the neutral shared.LogLineMsg and renders its own body.
package logs

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/tui/client"
	"metacubexd-server-go/internal/tui/shared"
)

// maxLogLines caps the retained log lines to bound memory; the viewport only
// ever shows a window of them.
const maxLogLines = 1000

// baseHelp is the Logs tab footer; the follow flag is spliced in on render.
const baseHelp = "1-3:tabs  /:filter  f:follow  q:quit"

// Model renders the realtime kernel log stream in a scrollable viewport. It
// owns the filter input state (filtering/filterInput) and the follow flag.
type Model struct {
	viewport    viewport.Model
	allLines    []string // full history (capped); filter only affects the view
	filter      string
	filtering   bool
	filterInput string
	follow      bool
}

// New returns the Logs tab. The client is unused by this page; it is part of
// the constructor signature so every page shares the same New(client) shape.
func New(client *client.Client) *Model {
	return &Model{
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		follow:   true,
	}
}

// Title implements shared.Tab.
func (m *Model) Title() string { return "Logs" }

// Help implements shared.Tab. It reflects the current state: the inline
// filter input while filtering, otherwise the follow-at-bottom flag.
func (m *Model) Help() string {
	if m.filtering {
		return "filter: " + m.filterInput + "▌  (enter:apply  esc:cancel)"
	}
	flag := "ON"
	if !m.follow {
		flag = "OFF"
	}
	return strings.Replace(baseHelp, "f:follow", "f:follow("+flag+")", 1)
}

// Busy implements shared.Tab; logs has no in-flight operations.
func (m *Model) Busy() bool { return false }

// Overlay implements shared.Tab; logs has no popups.
func (m *Model) Overlay() shared.Modal { return nil }

// SetSize resizes the log viewport. When following, the view re-scrolls to
// the bottom so the latest lines stay visible after a resize — without this,
// the scroll offset computed at the old height leaves blank space below.
func (m *Model) SetSize(width, height int) {
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(height)
	if m.follow {
		m.viewport.GotoBottom()
	}
}

// SetFilter updates the active filter and re-renders the retained history.
// Clearing the filter (empty string) restores the full history.
func (m *Model) SetFilter(filter string) {
	m.filter = filter
	m.render()
}

// Update implements shared.Tab: filter keystrokes while the filter input is
// active, "/" to start filtering, "f" to toggle follow, scroll/wheel
// messages for the viewport, and shared.LogLineMsg to append a line.
func (m *Model) Update(msg tea.Msg) (shared.Tab, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()
		if m.filtering {
			m.UpdateFilterKey(key)
			return m, nil, true
		}
		switch key {
		case "/":
			m.StartFilter()
			return m, nil, true
		case "f":
			m.ToggleFollow()
			return m, nil, true
		default:
			if shared.IsScrollKey(key) {
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd, true
			}
			return m, nil, false
		}
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd, true
	case shared.LogLineMsg:
		m.append(msg.Line)
		return m, nil, true
	}
	return m, nil, false
}

// Filtering reports whether the filter input is active.
func (m *Model) Filtering() bool { return m.filtering }

// Filter returns the active filter; empty means no filtering.
func (m *Model) Filter() string { return m.filter }

// FilterInput returns the filter text currently being typed.
func (m *Model) FilterInput() string { return m.filterInput }

// Following reports the follow-at-bottom flag.
func (m *Model) Following() bool { return m.follow }

// LineCount returns the number of retained log lines.
func (m *Model) LineCount() int { return len(m.allLines) }

// YOffset reports the viewport scroll offset.
func (m *Model) YOffset() int { return m.viewport.YOffset() }

// append adds a log line to the history and re-renders the view.
func (m *Model) append(line string) {
	m.allLines = append(m.allLines, line)
	if len(m.allLines) > maxLogLines {
		m.allLines = append([]string(nil), m.allLines[len(m.allLines)-maxLogLines:]...)
	}
	m.render()
}

// render rebuilds the viewport content from the full history, applying the
// active filter. The history itself is never discarded, so clearing the
// filter restores everything.
func (m *Model) render() {
	visible := m.allLines
	if m.filter != "" {
		// Allocate a fresh slice: [:0] would reuse allLines' backing array
		// and clobber the history while filtering.
		var kept []string
		for _, line := range visible {
			if strings.Contains(shared.StripANSI(line), m.filter) {
				kept = append(kept, line)
			}
		}
		visible = kept
	}
	m.viewport.SetContent(strings.Join(visible, "\n"))
	if m.follow {
		m.viewport.GotoBottom()
	}
}

// View implements shared.Tab, rendering the viewport contents, or a centered
// hint filling the whole area when no log lines have arrived yet.
func (m *Model) View() string {
	if len(m.allLines) == 0 {
		return lipgloss.NewStyle().
			Width(m.viewport.Width()).
			Height(m.viewport.Height()).
			Align(lipgloss.Center, lipgloss.Center).
			Faint(true).
			Render("— waiting for log stream —")
	}
	return m.viewport.View()
}
