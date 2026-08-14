package connections

import (
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/tui/client"
)

// FetchConnections returns a command that fetches all connections.
func FetchConnections(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.ListConnections()
		return ConnectionsLoadedMsg{Resp: resp, Err: err}
	}
}

// closeCmd returns a command that closes a specific connection.
func closeCmd(c *client.Client, id string) tea.Cmd {
	return func() tea.Msg {
		err := c.CloseConnection(id)
		return ConnectionOpMsg{Err: err, All: false}
	}
}

// closeAllCmd returns a command that closes all connections.
func closeAllCmd(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		err := c.CloseAllConnections()
		return ConnectionOpMsg{Err: err, All: true}
	}
}