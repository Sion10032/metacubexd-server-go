package tui

import (
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl/tui/pages/connections"
	"metacubexd-server-go/internal/ctl/tui/shared"
)

// updateConnectionsMsg routes connection load results and operation outcomes
// to the Connections page.
func (m Model) updateConnectionsMsg(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case connections.ConnectionsLoadedMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		tab, cmd, _ := m.tabs[idxConnection].Update(msg)
		m.tabs[idxConnection] = tab
		return m, cmd
	case connections.ConnectionOpMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		tab, cmd, _ := m.tabs[idxConnection].Update(msg)
		m.tabs[idxConnection] = tab
		return m, cmd
	}
	return m, nil
}

// updateConnectionTick handles the periodic connection refresh tick.
func (m Model) updateConnectionTick(msg tea.Msg) (Model, tea.Cmd) {
	if m.activeTab == idxConnection {
		return m, tea.Batch(
			connections.FetchConnections(m.client),
			shared.ConnectionTick(),
		)
	}
	return m, nil
}

// closeConnectionStream stops the connection refresh tick when leaving the tab.
func (m *Model) closeConnectionStream() {
	shared.CloseStream(m.connCancel)
	m.connCancel = nil
}