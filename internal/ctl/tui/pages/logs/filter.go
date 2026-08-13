package logs

import "unicode/utf8"

// StartFilter enters the filter input state, prefilling the input with the
// current filter so it is editable and can be cleared by deleting to empty
// and pressing enter.
func (m *Model) StartFilter() {
	m.filtering = true
	m.filterInput = m.filter
}

// ToggleFollow flips the follow-at-bottom flag.
func (m *Model) ToggleFollow() {
	m.follow = !m.follow
}

// UpdateFilterKey handles a keystroke while the filter input is active:
// enter applies the filter, esc cancels, backspace deletes, other single
// characters append.
func (m *Model) UpdateFilterKey(key string) {
	switch key {
	case "enter":
		m.SetFilter(m.filterInput)
		m.filterInput = ""
		m.filtering = false
	case "esc":
		m.filterInput = ""
		m.filtering = false
	case "backspace":
		if r := []rune(m.filterInput); len(r) > 0 {
			m.filterInput = string(r[:len(r)-1])
		}
	default:
		if utf8.RuneCountInString(key) == 1 {
			m.filterInput += key
		}
	}
}
