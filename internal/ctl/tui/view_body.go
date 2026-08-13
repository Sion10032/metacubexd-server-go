package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/ctl/tui/shared"
)

// body renders the active tab's content at the given size, wrapping every
// line with the frame borders. Logs shows the viewport; the other tabs are
// centered placeholders until their phases land.
func (m Model) body(width, height int) string {
	var content string
	switch m.activeTab {
	case 0:
		content = m.tabs[0].View()
	case 1:
		m.tabs[1].SetSize(width, height)
		content = m.tabs[1].View()
	case 2:
		content = lipgloss.NewStyle().Height(height).MaxHeight(height).Render(m.renderKernelTab())
	default:
		content = lipgloss.NewStyle().
			Width(width).Height(height).
			Align(lipgloss.Center, lipgloss.Center).
			Faint(true).
			Render("[" + tabTitles[m.activeTab] + " tab — not implemented yet]")
	}
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		// MaxWidth guards against wide/emoji content overflowing the right
		// border when the computed width and the terminal's rendering differ.
		clipped := lipgloss.NewStyle().MaxWidth(width).Render(l)
		lines[i] = shared.FrameRow(clipped, width)
	}
	return strings.Join(lines, "\n")
}
