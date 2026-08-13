package tui

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/ctl"
)

// frameView renders the framed layout: bordered box with a title, status bar,
// tab bar, active tab body and the key binding help line. The frame fills the
// whole terminal window.
func (m Model) frameView() string {
	w, h := m.width, m.height
	if w < minWidth {
		w = minWidth
	}
	if h < minHeight {
		h = minHeight
	}
	inner := w - 2

	// The log viewport (or placeholder) fills everything between the fixed
	// frame rows. SetSize here is a belt-and-suspenders re-scroll; the size is
	// applied on WindowSizeMsg so the model's viewport is always current.
	if h > frameRows {
		m.logs.SetSize(inner, h-frameRows)
	}

	statusLine := renderStatus(m.state, m.client.Endpoint())
	if m.err != nil {
		statusLine += "  " + errorStyle.Render("⚠ "+m.errText())
	}
	if m.kernel.operating {
		statusLine += "  " + m.spinner.View() + " " + kernelOps[m.kernel.kSelected].label + "…"
	}

	// Second status line: the active profile summary.
	activeLine := m.profiles.ActiveSummary(m.profActive)

	title := " mihomo-tui · " + m.client.Endpoint() + " "
	// TEMP diagnostic: show the resolved window size until the short-window
	// report is confirmed — remove once verified.
	size := fmt.Sprintf(" %dx%d ", w, h)
	help := tabHelp(m.activeTab)
	switch {
	case m.filtering:
		help = "filter: " + m.filterInput + "▌  (enter:apply  esc:cancel)"
	case m.importing:
		help = "import: tab:switch  enter:import  esc:cancel"
	case m.confirmDel:
		help = "⚠ 删除所选 profile? (y 确认 / 其他取消)"
	default:
		// Surface the follow-at-bottom state on the Logs tab only.
		if m.activeTab == 0 {
			flag := "ON"
			if !m.logs.follow {
				flag = "OFF"
			}
			help = strings.Replace(help, "f:follow", "f:follow("+flag+")", 1)
		}
	}
	return strings.Join([]string{
		frameTop(inner, title, size),
		frameRow(statusLine, inner),
		frameRow(activeLine, inner),
		frameRow(renderTabs(m.activeTab), inner),
		frameSep(inner),
		m.body(inner, h-frameRows),
		frameSep(inner),
		frameRow(help, inner),
		frameBottom(inner),
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

// Minimum frame dimensions, the narrow-screen threshold below which the
// frame is dropped in favor of a bare log stream, and the number of fixed
// rows (top + 2 status + tab + sep + body + sep + help + bottom) the log
// viewport must leave room for.
const (
	minWidth    = 40
	minHeight   = 10
	narrowWidth = 60
	frameRows   = 8
)

// frameTop renders the top border with the title embedded on the left and an
// optional right-side label (e.g. window size).
func frameTop(inner int, title, right string) string {
	mid := strings.Repeat("─", max(0, inner-2-lipgloss.Width(title)-lipgloss.Width(right)))
	return "┌─" + title + mid + right + "─┐"
}

// frameSep renders an internal separator line.
func frameSep(inner int) string {
	return "├" + strings.Repeat("─", inner) + "┤"
}

// frameBottom renders the bottom border.
func frameBottom(inner int) string {
	return "└" + strings.Repeat("─", inner) + "┘"
}

// frameRow wraps a content line with the vertical borders, padded to the
// inner width.
func frameRow(content string, inner int) string {
	return "│" + lipgloss.PlaceHorizontal(inner, lipgloss.Left, content) + "│"
}
