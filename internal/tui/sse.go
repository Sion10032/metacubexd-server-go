package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/tui/shared"
)

// closeLogStream cancels the SSE subscription so its goroutine and HTTP
// connection are released on exit.
func (m *Model) closeLogStream() {
	shared.CloseStream(m.logCancel)
	m.logCancel = nil
}

// updateStream handles the SSE stream messages: subscription setup, log
// lines, kernel state pushes and stream teardown.
func (m Model) updateStream(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case shared.SubscribedMsg:
		m.logCancel = msg.Cancel
		m.logCh = msg.Ch
		return m, shared.ForwardEvents(msg.Ch)
	case shared.LogLineMsg:
		m.tabs[idxLogs].Update(msg)
		return m, shared.ForwardEvents(m.logCh)
	case shared.KernelStateMsg:
		m.state = &msg.State
		return m, shared.ForwardEvents(m.logCh)
	case shared.LogClosedMsg:
		if !m.quitting {
			m.err = fmt.Errorf("log stream closed")
		}
		return m, nil
	}
	return m, nil
}
