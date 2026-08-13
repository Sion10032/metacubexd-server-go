package shared

import (
	"context"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/supervisor"
)

// StatusLoadedMsg carries a fresh kernel state from the control API.
type StatusLoadedMsg struct {
	State supervisor.KernelState
}

// StatusErrorMsg carries a control API failure (connection, auth, ...).
type StatusErrorMsg struct {
	Err error
}

// TickMsg fires once per second to refresh the kernel status.
type TickMsg struct{}

// SubscribedMsg carries the live SSE log stream and its cancellation func.
type SubscribedMsg struct {
	Ch     <-chan ctl.Event
	Cancel context.CancelFunc
}

// LogLineMsg carries one formatted kernel log line.
type LogLineMsg struct {
	Line string
}

// KernelStateMsg carries a kernel state pushed over SSE.
type KernelStateMsg struct {
	State supervisor.KernelState
}

// LogClosedMsg fires when the SSE log stream ends.
type LogClosedMsg struct{}
