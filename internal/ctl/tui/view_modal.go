package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// overlayModal centers modal over base using lipgloss's compositor, so the
// styled modal draws on top of the styled frame without mangling ANSI codes.
func overlayModal(base, modal string, w, h int) string {
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

// configModal renders the bordered config viewer modal for the given terminal
// size: a bold header, the scrollable config viewport, and a key-hint footer.
func (m Model) configModal(w, h int) string {
	cw := w - 8
	if cw < 24 {
		cw = 24
	}
	if cw > 80 {
		cw = 80
	}
	title := "View Config (" + m.config.Mode() + ")"
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render(title)
	sep := strings.Repeat("─", cw)
	footer := lipgloss.NewStyle().Width(cw).Align(lipgloss.Center).Render("c:active/runtime  e:edit  ↑↓:scroll  esc:close")
	viewHeight := h - 10
	if viewHeight < 1 {
		viewHeight = 1
	}
	m.config.SetSize(cw, viewHeight)
	inner := strings.Join([]string{header, sep, m.config.View(), sep, footer}, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Background(modalBackground).
		Width(cw + 2).
		Render(inner)
}

// sectionFormView renders the section editor popup: key and value textinputs
// with a header and a key-hint footer.
func (m Model) sectionFormView() string {
	const cw = 60
	title := "Edit Section"
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render(title)
	sep := strings.Repeat("─", cw)
	footer := lipgloss.NewStyle().Width(cw).Align(lipgloss.Center).Render("tab:switch  enter:save  esc:cancel")
	inner := strings.Join([]string{
		header,
		sep,
		m.sectionForm.key.View(),
		m.sectionForm.value.View(),
		sep,
		footer,
	}, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Background(modalBackground).
		Width(cw + 2).
		Render(inner)
}

// editInputView renders the network field editor popup: a single prefilled
// textinput with a header and a key-hint footer.
func (m Model) editInputView() string {
	const cw = 40
	f := networkFields[m.editField]
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render("Edit " + f.label)
	sep := strings.Repeat("─", cw)
	footer := lipgloss.NewStyle().Width(cw).Align(lipgloss.Center).Render("enter:save  esc:cancel")
	inner := strings.Join([]string{header, sep, m.editInput.View(), sep, footer}, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Background(modalBackground).
		Width(cw + 2).
		Render(inner)
}
