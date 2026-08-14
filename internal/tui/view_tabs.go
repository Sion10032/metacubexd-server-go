package tui

import (
	"fmt"
	"strings"

	"metacubexd-server-go/internal/tui/shared"
)

// renderTabs renders the tab bar from each page's Title, highlighting the
// active tab.
func renderTabs(tabs []shared.Tab, active int) string {
	var b strings.Builder
	for i, tab := range tabs {
		label := fmt.Sprintf("[%d] %s", i+1, tab.Title())
		if i == active {
			label = shared.TabActiveStyle.Render(label)
		}
		b.WriteString(label)
		b.WriteString("  ")
	}
	return b.String()
}
