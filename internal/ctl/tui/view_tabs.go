package tui

import (
	"fmt"
	"strings"

	"metacubexd-server-go/internal/ctl/tui/shared"
)

// tabTitles are the tab names, one per index.
var tabTitles = []string{"Logs", "Subscriptions", "Config"}

// tabHelp lists the key bindings for a tab; the footer switches with the tab
// so only the relevant operations are shown.
var helpByTab = [][]string{
	// Logs
	{"1-3:tabs", "/:filter", "f:follow", "q:quit"},
	// Profiles
	{"1-3:tabs", "a:activate", "u:refresh", "d:delete", "i:import", "q:quit"},
	// Config
	{"1-3:tabs", "↑↓:select", "enter:run", "q:quit"},
}

func tabHelp(active int) string {
	return strings.Join(helpByTab[active], "  ")
}

// renderTabs renders the tab bar, highlighting the active tab.
func renderTabs(active int) string {
	var b strings.Builder
	for i, title := range tabTitles {
		label := fmt.Sprintf("[%d] %s", i+1, title)
		if i == active {
			label = shared.TabActiveStyle.Render(label)
		}
		b.WriteString(label)
		b.WriteString("  ")
	}
	return b.String()
}
