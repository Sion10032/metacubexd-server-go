// Package tui implements the interactive terminal UI for mihomo-tui.
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/supervisor"
)

// Model is the top-level Bubble Tea model. It owns the kernel state, the log
// viewport and the current tab.
type Model struct {
	client   *ctl.Client
	state    *supervisor.KernelState
	err      error
	width    int
	height   int
	quitting bool
}

// New returns a Model for the given control API client.
func New(client *ctl.Client) Model {
	return Model{client: client}
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
		}
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

// View renders the status bar on top; the full layout lands in 1.24.
func (m Model) View() string {
	line := renderStatus(m.state, m.client.Endpoint())
	if m.err != nil {
		line += "\n  " + m.err.Error()
	}
	return line + "\n"
}
