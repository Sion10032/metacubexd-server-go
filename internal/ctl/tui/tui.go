// Package tui implements the interactive terminal UI for mihomo-tui.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/supervisor"
)

// Model is the top-level Bubble Tea model. It owns the kernel state, the log
// viewport and the current tab.
type Model struct {
	client    *ctl.Client
	state     *supervisor.KernelState
	err       error
	logs      LogsModel
	activeTab int
	width     int
	height    int
	quitting  bool
}

// New returns a Model for the given control API client.
func New(client *ctl.Client) Model {
	return Model{client: client, logs: NewLogsModel()}
}

// statusLoadedMsg carries a fresh kernel state from the control API.
type statusLoadedMsg struct {
	state supervisor.KernelState
}

// statusErrorMsg carries a control API failure (connection, auth, ...).
type statusErrorMsg struct {
	err error
}

// tickMsg fires once per second to refresh the kernel status.
type tickMsg struct{}

// Init returns the initial commands: fetch kernel status once, then poll it
// every second; the SSE log subscription lands in a later step (1.22).
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		fetchStatusCmd(m.client),
		statusTick(),
	)
}

// fetchStatusCmd fetches the kernel status once.
func fetchStatusCmd(c *ctl.Client) tea.Cmd {
	return func() tea.Msg {
		st, err := c.KernelStatus()
		if err != nil {
			return statusErrorMsg{err: err}
		}
		return statusLoadedMsg{state: st}
	}
}

// statusTick schedules the next status refresh one second from now.
func statusTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

// Update handles messages and key presses.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "1", "2", "3":
			m.activeTab = int(msg.String()[0] - '1')
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.QuitMsg:
		m.quitting = true
		return m, nil
	case statusLoadedMsg:
		m.state = &msg.state
		return m, nil
	case statusErrorMsg:
		m.err = msg.err
		return m, nil
	case tickMsg:
		return m, tea.Batch(fetchStatusCmd(m.client), statusTick())
	}
	return m, nil
}

// View renders the framed layout: bordered box with a title, status bar, tab
// bar, active tab body and the key binding help line. The frame fills the
// whole terminal window.
func (m Model) View() string {
	w, h := m.width, m.height
	if w < minWidth {
		w = minWidth
	}
	if h < minHeight {
		h = minHeight
	}
	inner := w - 2

	// The log viewport (or placeholder) fills everything between the fixed
	// frame rows: top + status + sep + tab + sep + body + sep + help + bottom.
	const frameRows = 8
	if h > frameRows {
		m.logs.SetSize(inner, h-frameRows)
	}

	statusLine := renderStatus(m.state, m.client.Endpoint())
	if m.err != nil {
		statusLine += "  " + errorStyle.Render("⚠ "+m.err.Error())
	}

	title := " mihomo-tui · " + m.client.Endpoint() + " "
	return strings.Join([]string{
		frameTop(inner, title),
		frameRow(statusLine, inner),
		frameSep(inner),
		frameRow(renderTabs(m.activeTab), inner),
		frameSep(inner),
		m.body(inner, h-frameRows),
		frameSep(inner),
		frameRow(helpLine, inner),
		frameBottom(inner),
	}, "\n")
}

// body renders the active tab's content at the given size, wrapping every
// line with the frame borders. Logs shows the viewport; the other tabs are
// centered placeholders until their phases land.
func (m Model) body(width, height int) string {
	var content string
	switch m.activeTab {
	case 0:
		content = m.logs.View()
	default:
		content = lipgloss.NewStyle().
			Width(width).Height(height).
			Align(lipgloss.Center, lipgloss.Center).
			Faint(true).
			Render("[" + tabTitles[m.activeTab] + " tab — not implemented yet]")
	}
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = frameRow(l, width)
	}
	return strings.Join(lines, "\n")
}

// Minimum frame dimensions.
const (
	minWidth  = 40
	minHeight = 10
)

// frameTop renders the top border with the title embedded on the left.
func frameTop(inner int, title string) string {
	mid := strings.Repeat("─", max(0, inner-2-lipgloss.Width(title)))
	return "┌─" + title + mid + "─┐"
}

// frameSep renders an internal separator line.
func frameSep(inner int) string {
	return "├" + strings.Repeat("─", inner) + "┤"
}

// frameBottom renders the bottom border.
func frameBottom(inner int) string {
	return "└" + strings.Repeat("─", inner) + "┘"
}

// frameRow wraps a content line with the vertical borders, padded to the
// inner width.
func frameRow(content string, inner int) string {
	return "│" + lipgloss.PlaceHorizontal(inner, lipgloss.Left, content) + "│"
}

// tabTitles are the tab names, one per index.
var tabTitles = []string{"Logs", "Profiles", "Config"}

// helpLine lists the key bindings.
const helpLine = "s:start  S:stop  r:restart  R:rollback  c:recover  /:filter  f:follow  q:quit"

// renderTabs renders the tab bar, highlighting the active tab.
func renderTabs(active int) string {
	var b strings.Builder
	for i, title := range tabTitles {
		label := fmt.Sprintf("[%d] %s", i+1, title)
		if i == active {
			label = tabActiveStyle.Render(label)
		}
		b.WriteString(label)
		b.WriteString("  ")
	}
	return b.String()
}
