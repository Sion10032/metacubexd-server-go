package tui

import (
	"fmt"
	"strconv"

	"metacubexd-server-go/internal/supervisor"
)

// renderStatus renders the kernel status bar line: a colored status dot, the
// state, pid, version and external controller address.
func renderStatus(state *supervisor.KernelState, endpoint string) string {
	if state == nil {
		dot := statusStyle(supervisor.StatusStopped).Render("●")
		return fmt.Sprintf("%s %-8s %-6s %s ec: %s", dot, "unknown", "-", "-", endpoint)
	}
	pid := "-"
	if state.PID != nil {
		pid = strconv.Itoa(*state.PID)
	}
	ec := state.ExternalController
	if ec == "" {
		ec = "-"
	}
	dot := statusStyle(state.Status).Render("●")
	return fmt.Sprintf("%s %-8s pid %-6s %s ec: %s", dot, state.Status, pid, state.Version, ec)
}
