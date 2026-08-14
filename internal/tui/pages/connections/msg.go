package connections

import "metacubexd-server-go/internal/tui/client"

// ConnectionsLoadedMsg is sent when connections are fetched.
type ConnectionsLoadedMsg struct {
	Resp client.ConnectionsResponse
	Err  error
}

// ConnectionOpMsg is sent after a connection close operation.
type ConnectionOpMsg struct {
	Err error
	All bool // true if CloseAllConnections was called
}