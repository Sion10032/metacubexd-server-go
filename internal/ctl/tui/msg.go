package tui

import (
	"context"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/profile"
	"metacubexd-server-go/internal/supervisor"
)

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

// subscribedMsg carries the live SSE log stream and its cancellation func.
type subscribedMsg struct {
	ch     <-chan ctl.Event
	cancel context.CancelFunc
}

// logMsg carries one formatted kernel log line.
type logMsg struct {
	line string
}

// stateMsg carries a kernel state pushed over SSE.
type stateMsg struct {
	state supervisor.KernelState
}

// logClosedMsg fires when the SSE log stream ends.
type logClosedMsg struct{}

// profilesLoadedMsg carries the fetched profile list.
type profilesLoadedMsg struct {
	list []profile.Meta
	err  error
}

// profileOpMsg carries the result of a profile operation; a nil err means the
// lists and kernel status should be refreshed.
type profileOpMsg struct {
	err error
}

// configLoadedMsg carries a fetched config body (active or runtime).
type configLoadedMsg struct {
	mode    int
	content string
	err     error
}

// sectionEditMsg carries the result of a config section edit.
type sectionEditMsg struct {
	err error
}

// networkSettingsMsg carries the fetched network settings of the active
// config.
type networkSettingsMsg struct {
	settings networkSettings
	err      error
}
