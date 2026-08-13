package tui

import tea "charm.land/bubbletea/v2"

// keyPress builds a tea.KeyPressMsg for a printable character, mirroring a
// user typing it. Text is set so msg.String() returns the character itself.
func keyPress(s string) tea.KeyPressMsg {
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}
