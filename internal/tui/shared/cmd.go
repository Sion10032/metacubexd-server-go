package shared

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/tui/client"
)

// RequestBackgroundColor asks the terminal for its background color so the
// theme can adapt to dark/light.
func RequestBackgroundColor() tea.Cmd {
	return func() tea.Msg {
		return tea.RequestBackgroundColor()
	}
}

// FetchStatus fetches the kernel status once.
func FetchStatus(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		st, err := c.KernelStatus()
		if err != nil {
			return StatusErrorMsg{Err: err}
		}
		return StatusLoadedMsg{State: st}
	}
}

// StatusTick schedules the next status refresh one second from now.
func StatusTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return TickMsg{}
	})
}

// ConnectionTick schedules the next connection refresh one second from now.
func ConnectionTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return ConnectionTickMsg{}
	})
}
