package tui

import (
	"regexp"

	"github.com/charmbracelet/lipgloss"

	"metacubexd-server-go/internal/supervisor"
)

// ansiRe matches SGR escape sequences; used to strip colors for plain-text
// matching and test assertions.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Status styles colored by kernel state: running green, errored red,
// starting/stopping yellow, stopped grey.
var (
	statusRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	statusErrored = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	statusBusy    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	statusStopped = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// errorStyle marks inline error messages on the status line.
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	// tabActiveStyle highlights the active tab in the tab bar.
	tabActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))

	// selectedStyle highlights the selected entry in a list.
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
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
