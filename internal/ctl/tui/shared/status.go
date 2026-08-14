package shared

import (
	"fmt"
	"strconv"

	"metacubexd-server-go/internal/supervisor"
)

// RenderStatus renders the kernel status bar line: a colored status dot, the
// state, pid, version and external controller address.
func RenderStatus(state *supervisor.KernelState, endpoint string) string {
	if state == nil {
		dot := StatusStyle(supervisor.StatusStopped).Render("●")
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
	dot := StatusStyle(state.Status).Render("●")
	return fmt.Sprintf("%s %-8s pid %-6s %s ec: %s", dot, state.Status, pid, state.Version, ec)
}
