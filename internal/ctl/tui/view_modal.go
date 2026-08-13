package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/ctl/tui/components"
)

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
	return components.BorderedModal(inner, cw)
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
		m.kernel.sectionForm.Fields[0].View(),
		m.kernel.sectionForm.Fields[1].View(),
		sep,
		footer,
	}, "\n")
	return components.BorderedModal(inner, cw)
}

// editInputView renders the network field editor popup: a single prefilled
// textinput with a header and a key-hint footer.
func (m Model) editInputView() string {
	const cw = 40
	f := networkFields[m.kernel.editField]
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render("Edit " + f.label)
	sep := strings.Repeat("─", cw)
	footer := lipgloss.NewStyle().Width(cw).Align(lipgloss.Center).Render("enter:save  esc:cancel")
	inner := strings.Join([]string{header, sep, m.kernel.editInput.View(), sep, footer}, "\n")
	return components.BorderedModal(inner, cw)
}
