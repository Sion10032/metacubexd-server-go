package shared

import (
	"context"

	"metacubexd-server-go/internal/tui/client"
	"metacubexd-server-go/internal/api"
)

// StatusLoadedMsg carries a fresh kernel state from the control API.
type StatusLoadedMsg struct {
	State api.KernelState
}

// StatusErrorMsg carries a control API failure (connection, auth, ...).
type StatusErrorMsg struct {
	Err error
}

// TickMsg fires once per second to refresh the kernel status.
type TickMsg struct{}

// SubscribedMsg carries the live SSE log stream and its cancellation func.
type SubscribedMsg struct {
	Ch     <-chan client.Event
	Cancel context.CancelFunc
}

// LogLineMsg carries one formatted kernel log line.
type LogLineMsg struct {
	Line string
}

// KernelStateMsg carries a kernel state pushed over SSE.
type KernelStateMsg struct {
	State api.KernelState
}

// LogClosedMsg fires when the SSE log stream ends.
type LogClosedMsg struct{}

// ConnectionTickMsg fires periodically to refresh the connections list.
type ConnectionTickMsg struct{}
