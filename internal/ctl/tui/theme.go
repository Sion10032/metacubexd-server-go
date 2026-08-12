package tui

import (
	"github.com/charmbracelet/lipgloss"

	"metacubexd-server-go/internal/supervisor"
)

// Status styles colored by kernel state: running green, errored red,
// starting/stopping yellow, stopped grey.
var (
	statusRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	statusErrored = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	statusBusy    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	statusStopped = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// statusStyle returns the color style for a kernel status.
func statusStyle(s supervisor.KernelStatus) lipgloss.Style {
	switch s {
	case supervisor.StatusRunning:
		return statusRunning
	case supervisor.StatusErrored:
		return statusErrored
	case supervisor.StatusStarting, supervisor.StatusStopping:
		return statusBusy
	default:
		return statusStopped
	}
}
