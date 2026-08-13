package tui

import (
	"errors"
	"fmt"
	"strings"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/shared"
)

// frameView renders the framed layout: bordered box with a title, status bar,
// tab bar, active tab body and the key binding help line. The frame fills the
// whole terminal window.
func (m Model) frameView() string {
	w, h := m.width, m.height
	if w < shared.MinWidth {
		w = shared.MinWidth
	}
	if h < shared.MinHeight {
		h = shared.MinHeight
	}
	inner := w - 2

	// The log viewport (or placeholder) fills everything between the fixed
	// frame rows. SetSize here is a belt-and-suspenders re-scroll; the size is
	// applied on WindowSizeMsg so the model's viewport is always current.
	if h > shared.FrameRows {
		m.tabs[0].SetSize(inner, h-shared.FrameRows)
	}

	statusLine := shared.RenderStatus(m.state, m.client.Endpoint())
	if m.err != nil {
		statusLine += "  " + shared.ErrorStyle.Render("⚠ "+m.errText())
	}
	if m.kernel.operating {
		statusLine += "  " + m.spinner.View() + " " + kernelOps[m.kernel.kSelected].label + "…"
	}

	// Second status line: the active profile summary.
	activeLine := m.profilesPage().ActiveSummary()

	title := " mihomo-tui · " + m.client.Endpoint() + " "
	// TEMP diagnostic: show the resolved window size until the short-window
	// report is confirmed — remove once verified.
	size := fmt.Sprintf(" %dx%d ", w, h)
	logTab := m.logsPage()
	help := tabHelp(m.activeTab)
	switch {
	case logTab.Filtering():
		help = logTab.Help()
	case m.activeTab == 1:
		// The Profiles page owns its footer (import form / delete confirm
		// hints).
		help = m.profilesPage().Help()
	default:
		// The Logs page owns its footer (follow state, filter input).
		if m.activeTab == 0 {
			help = logTab.Help()
		}
	}
	return strings.Join([]string{
		shared.FrameTop(inner, title, size),
		shared.FrameRow(statusLine, inner),
		shared.FrameRow(activeLine, inner),
		shared.FrameRow(renderTabs(m.activeTab), inner),
		shared.FrameSep(inner),
		m.body(inner, h-shared.FrameRows),
		shared.FrameSep(inner),
		shared.FrameRow(help, inner),
		shared.FrameBottom(inner),
	}, "\n")
}

// errText maps low-level errors to friendly status-bar text.
func (m Model) errText() string {
	if m.err == nil {
		return ""
	}
	if errors.Is(m.err, ctl.ErrUnauthorized) {
		return "认证失败：token 无效或 server 开启了认证"
	}
	return m.err.Error()
}
