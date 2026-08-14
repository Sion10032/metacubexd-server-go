// Package components holds reusable UI building blocks shared by the TUI
// pages: modal overlay composition and the generic two-field form. It is a
// leaf package: it depends only on shared, the standard library and
// third-party libraries — never on the root tui package or any page package.
package components

import (
	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/ctl/tui/shared"
)

// OverlayModal centers modal over base using lipgloss's compositor, so the
// styled modal draws on top of the styled frame without mangling ANSI codes.
func OverlayModal(base, modal string, w, h int) string {
	mw, mh := lipgloss.Width(modal), lipgloss.Height(modal)
	x := (w - mw) / 2
	if x < 0 {
		x = 0
	}
	y := (h - mh) / 2
	if y < 0 {
		y = 0
	}
	comp := lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(modal).X(x).Y(y).Z(1),
	)
	return comp.Render()
}

// BorderedModal renders popup content as a rounded, opaque-background box of
// the given content width (plus 2 columns of border), matching the style of
// every TUI modal popup.
func BorderedModal(inner string, contentWidth int) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Background(shared.ModalBackground).
		Width(contentWidth + 2).
		Render(inner)
}
