package shared

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Minimum frame dimensions, the narrow-screen threshold below which the
// frame is dropped in favor of a bare log stream, and the number of fixed
// rows (top + 2 status + tab + sep + body + sep + help + bottom) the log
// viewport must leave room for.
const (
	MinWidth    = 40
	MinHeight   = 10
	NarrowWidth = 60
	FrameRows   = 8
)

// FrameTop renders the top border with the title embedded on the left and an
// optional right-side label (e.g. window size).
func FrameTop(inner int, title, right string) string {
	mid := strings.Repeat("─", max(0, inner-2-lipgloss.Width(title)-lipgloss.Width(right)))
	return "┌─" + title + mid + right + "─┐"
}

// FrameSep renders an internal separator line.
func FrameSep(inner int) string {
	return "├" + strings.Repeat("─", inner) + "┤"
}

// FrameBottom renders the bottom border.
func FrameBottom(inner int) string {
	return "└" + strings.Repeat("─", inner) + "┘"
}

// FrameRow wraps a content line with the vertical borders, padded to the
// inner width.
func FrameRow(content string, inner int) string {
	return "│" + lipgloss.PlaceHorizontal(inner, lipgloss.Left, content) + "│"
}
