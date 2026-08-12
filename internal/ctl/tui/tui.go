// Package tui implements the interactive terminal UI for mihomo-tui.
package tui

import (
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

// Init returns the initial commands: fetch kernel status once; later steps
// add a status poll tick (1.19) and the SSE log subscription (1.22).
func (m Model) Init() tea.Cmd {
	return func() tea.Msg {
		st, err := m.client.KernelStatus()
		if err != nil {
			return statusErrorMsg{err: err}
		}
		return statusLoadedMsg{state: st}
	}
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
	case statusErrorMsg:
		m.err = msg.err
	}
	return m, nil
}

// View renders the UI.
func (m Model) View() string {
	return "mihomo-tui (placeholder)\n"
}
