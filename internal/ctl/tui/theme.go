package tui

import (
	"image/color"
	"regexp"

	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/supervisor"
)

// ansiRe matches SGR escape sequences; used to strip colors for plain-text
// matching and test assertions.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Theme styles, rebuilt by setTheme once the terminal background (dark vs
// light) is known. Until then they default to the dark-terminal scheme.
var (
	statusRunning lipgloss.Style
	statusErrored lipgloss.Style
	statusBusy    lipgloss.Style
	statusStopped lipgloss.Style

	// errorStyle marks inline error messages on the status line.
	errorStyle lipgloss.Style

	// tabActiveStyle highlights the active tab in the tab bar.
	tabActiveStyle lipgloss.Style

	// selectedStyle highlights the selected entry in a list.
	selectedStyle lipgloss.Style

	// spinnerStyle colors the operation spinner.
	spinnerStyle lipgloss.Style

	// modalBackground is the opaque backdrop of modal popups (config viewer).
	modalBackground = lipgloss.Color("232")
)

// setTheme rebuilds every color style for a dark (true) or light (false)
// terminal background. Brighter ANSI shades (8-15) are used on dark
// backgrounds; the standard shades (1-7) on light ones.
func setTheme(dark bool) {
	ld := lipgloss.LightDark(dark)
	statusRunning = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("2"), lipgloss.Color("10")))
	statusErrored = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("1"), lipgloss.Color("9")))
	statusBusy = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("3"), lipgloss.Color("11")))
	statusStopped = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("8"), lipgloss.Color("7")))
	errorStyle = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("1"), lipgloss.Color("9")))
	tabActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("4"), lipgloss.Color("12")))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("4"), lipgloss.Color("12")))
	spinnerStyle = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("3"), lipgloss.Color("11")))
	if dark {
		modalBackground = lipgloss.Color("232")
	} else {
		modalBackground = lipgloss.Color("255")
	}
}

func init() {
	setTheme(true)
}

// setModalBackground overrides the modal backdrop with the terminal's actual
// background color once it is detected, so the modal blends seamlessly.
func setModalBackground(c color.Color) {
	modalBackground = c
}

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
