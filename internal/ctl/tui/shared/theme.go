// Package shared holds cross-page infrastructure shared by every TUI page:
// theme styles, frame rendering, the status line, global messages/commands,
// the SSE stream layer and the Tab/Modal contracts. It is a leaf package:
// it must not import the root tui package, components or any page package.
package shared

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/supervisor"
)

// Theme styles, rebuilt by SetTheme once the terminal background (dark vs
// light) is known. Until then they default to the dark-terminal scheme.
var (
	// StatusRunningStyle colors a running kernel status.
	StatusRunningStyle lipgloss.Style
	// StatusErroredStyle colors an errored kernel status.
	StatusErroredStyle lipgloss.Style
	// StatusBusyStyle colors a starting/stopping kernel status.
	StatusBusyStyle lipgloss.Style
	// StatusStoppedStyle colors a stopped kernel status.
	StatusStoppedStyle lipgloss.Style

	// ErrorStyle marks inline error messages on the status line.
	ErrorStyle lipgloss.Style

	// TabActiveStyle highlights the active tab in the tab bar.
	TabActiveStyle lipgloss.Style

	// SelectedStyle highlights the selected entry in a list.
	SelectedStyle lipgloss.Style

	// SpinnerStyle colors the operation spinner.
	SpinnerStyle lipgloss.Style

	// ModalBackground is the opaque backdrop of modal popups (config viewer).
	ModalBackground = lipgloss.Color("232")
)

// SetTheme rebuilds every color style for a dark (true) or light (false)
// terminal background. Brighter ANSI shades (8-15) are used on dark
// backgrounds; the standard shades (1-7) on light ones.
func SetTheme(dark bool) {
	ld := lipgloss.LightDark(dark)
	StatusRunningStyle = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("2"), lipgloss.Color("10")))
	StatusErroredStyle = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("1"), lipgloss.Color("9")))
	StatusBusyStyle = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("3"), lipgloss.Color("11")))
	StatusStoppedStyle = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("8"), lipgloss.Color("7")))
	ErrorStyle = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("1"), lipgloss.Color("9")))
	TabActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("4"), lipgloss.Color("12")))
	SelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("4"), lipgloss.Color("12")))
	SpinnerStyle = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("3"), lipgloss.Color("11")))
	if dark {
		ModalBackground = lipgloss.Color("232")
	} else {
		ModalBackground = lipgloss.Color("255")
	}
}

func init() {
	SetTheme(true)
}

// SetModalBackground overrides the modal backdrop with the terminal's actual
// background color once it is detected, so the modal blends seamlessly.
func SetModalBackground(c color.Color) {
	ModalBackground = c
}

// StatusStyle returns the color style for a kernel status.
func StatusStyle(s supervisor.KernelStatus) lipgloss.Style {
	switch s {
	case supervisor.StatusRunning:
		return StatusRunningStyle
	case supervisor.StatusErrored:
		return StatusErroredStyle
	case supervisor.StatusStarting, supervisor.StatusStopping:
		return StatusBusyStyle
	default:
		return StatusStoppedStyle
	}
}
