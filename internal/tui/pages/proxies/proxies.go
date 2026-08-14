// Package proxies implements the Proxy tab: mode toggle and proxy-groups list.
package proxies

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/tui/client"
	"metacubexd-server-go/internal/tui/shared"
)

// Model holds the state for the Proxy tab.
type Model struct {
	client   *client.Client
	resp     client.ProxiesResponse
	mode     string
	groups   []string          // filtered proxy-groups (excluding GLOBAL)
	expanded map[string]bool   // group name -> expanded
	cursor   int               // cursor in visible rows
	width    int
	height   int
	delays   map[string]int    // node name -> delay in ms (0 = timeout)
	testing  map[string]bool   // group name -> testing in progress
}

// New returns the Proxy tab.
func New(client *client.Client) *Model {
	return &Model{
		client:   client,
		expanded: make(map[string]bool),
		delays:   make(map[string]int),
		testing:  make(map[string]bool),
	}
}

// Title implements shared.Tab.
func (m *Model) Title() string { return "Proxy" }

// Help implements shared.Tab.
func (m *Model) Help() string {
	return "1-5:tabs  m:mode  ↑↓:move  →/enter:expand or switch  ←:collapse  r:refresh  d:delay  q:quit"
}

// Busy implements shared.Tab; returns true when a group delay test is in progress.
func (m *Model) Busy() bool {
	for _, v := range m.testing {
		if v {
			return true
		}
	}
	return false
}

// Overlay implements shared.Tab; no popups.
func (m *Model) Overlay() shared.Modal { return nil }

// SetSize implements shared.Tab.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Update implements shared.Tab.
func (m *Model) Update(msg tea.Msg) (shared.Tab, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()
		switch key {
		case "m":
			return m, m.toggleMode(), true
		case "up", "k":
			m.moveCursor(-1)
			return m, nil, true
		case "down", "j":
			m.moveCursor(1)
			return m, nil, true
		case "right", "l", "enter":
			return m, m.expandOrSwitch(), true
		case "left", "h":
			m.collapseCurrent()
			return m, nil, true
		case "r":
			return m, m.refresh(), true
		case "d":
			return m, m.testGroupDelay(), true
		}
	case ProxiesLoadedMsg:
		if msg.Err != nil {
			// TODO: handle error
			return m, nil, true
		}
		m.resp = msg.Resp
		m.rebuildGroups()
		// Extract delay history for each node
		for name, proxy := range m.resp.Proxies {
			if len(proxy.History) > 0 {
				last := proxy.History[len(proxy.History)-1]
				m.delays[name] = last.Delay
			}
		}
		return m, nil, true
	case ModeLoadedMsg:
		if msg.Err != nil {
			// TODO: handle error
			return m, nil, true
		}
		m.mode = msg.Mode
		return m, nil, true
	case ProxyOpMsg:
		// After proxy switch, refresh proxies
		return m, FetchProxies(m.client), true
	case ModeOpMsg:
		// After mode switch, refresh mode
		return m, FetchMode(m.client), true
	case GroupDelayMsg:
		// Clear testing flag for this group (even on error)
		m.testing[msg.Group] = false
		if msg.Err != nil {
			// TODO: surface error to status line
			return m, nil, true
		}
		// Merge delays into m.delays
		for name, delay := range msg.Delays {
			m.delays[name] = delay
		}
		return m, nil, true
	}
	return m, nil, false
}

// View implements shared.Tab.
func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	var b strings.Builder

	// Mode toggle line
	modeStyle := lipgloss.NewStyle().Bold(true)
	b.WriteString("mode: ")
	b.WriteString(modeStyle.Render(m.mode))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	// Build visible rows
	rows := m.buildRows()
	for i, row := range rows {
		isSelected := i == m.cursor

		var line string
		if row.isGroup {
			// Group line
			expandIcon := "▸"
			if m.expanded[row.group] {
				expandIcon = "▾"
			}
			groupType := row.typ
			now := ""
			if row.nowName != "" {
				now = " [" + row.nowName + "]"
			}
			line = fmt.Sprintf("%s %s (%s)%s", expandIcon, row.name, groupType, now)
			if m.testing[row.group] {
				line += " " + shared.SpinnerStyle.Render("testing…")
			}
		} else {
			// Member line
			indicator := "○"
			if row.isNow {
				indicator = "●"
			}
			line = fmt.Sprintf("   %s %s %s", indicator, row.name, row.typ)
			// Append delay information
			if m.testing[row.group] {
				line += " " + shared.DelayNoneStyle.Render("-")
			} else if delay, ok := m.delays[row.member]; ok {
				var delayStr string
				var delayStyle lipgloss.Style
				if delay == 0 {
					delayStr = "timeout"
					delayStyle = shared.DelayBadStyle
				} else {
					delayStr = fmt.Sprintf("%dms", delay)
					delayStyle = shared.DelayStyle(delay)
				}
				line += " " + delayStyle.Render(delayStr)
			} else {
				line += " " + shared.DelayNoneStyle.Render("--")
			}
		}
		if isSelected {
			line = shared.SelectedStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return lipgloss.NewStyle().Height(m.height).MaxHeight(m.height).Render(b.String())
}

// row describes a visible line in the proxy-groups list.
type row struct {
	group   string // parent group name
	member  string // member name (empty for group rows)
	isGroup bool
	name    string // display name
	typ     string
	nowName string // for group rows: current node name
	isNow   bool   // for member rows: whether it's the current node
}

// buildRows constructs the flat list of visible rows.
func (m *Model) buildRows() []row {
	var rows []row
	for _, groupName := range m.groups {
		proxy := m.resp.Proxies[groupName]
		rows = append(rows, row{
			group:   groupName,
			isGroup: true,
			name:    groupName,
			typ:     proxy.Type,
			nowName: proxy.Now,
		})
		if m.expanded[groupName] {
			for _, memberName := range proxy.All {
				memberProxy := m.resp.Proxies[memberName]
				isNow := proxy.Now == memberName
				rows = append(rows, row{
					group:   groupName,
					member:  memberName,
					isGroup: false,
					name:    memberName,
					typ:     memberProxy.Type,
					isNow:   isNow,
				})
			}
		}
	}
	return rows
}

// rebuildGroups filters proxy-groups from the response.
// Order follows GLOBAL.all (same as metacubexd), with GLOBAL excluded.
func (m *Model) rebuildGroups() {
	m.groups = nil
	seen := make(map[string]bool)

	// Build the desired order from GLOBAL.all, like metacubexd does.
	var order []string
	if g, ok := m.resp.Proxies["GLOBAL"]; ok {
		order = g.All
	}
	// Append any groups not in GLOBAL.all as fallback.
	for _, name := range m.resp.Order {
		if !seen[name] {
			order = append(order, name)
		}
	}

	for _, name := range order {
		if seen[name] {
			continue
		}
		seen[name] = true
		proxy, ok := m.resp.Proxies[name]
		if !ok {
			continue
		}
		// Skip GLOBAL, DIRECT, REJECT and non-group types
		if name == "GLOBAL" || name == "DIRECT" || name == "REJECT" {
			continue
		}
		switch proxy.Type {
		case "Selector", "URLTest", "Fallback", "LoadBalance", "Relay":
			m.groups = append(m.groups, name)
		}
	}
	// Reset cursor if out of bounds
	rows := m.buildRows()
	if m.cursor >= len(rows) {
		m.cursor = 0
	}
}

// moveCursor moves the cursor by delta, clamping to valid range.
func (m *Model) moveCursor(delta int) {
	rows := m.buildRows()
	if len(rows) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
}

// expandOrSwitch expands/collapses a group or switches to a member.
func (m *Model) expandOrSwitch() tea.Cmd {
	rows := m.buildRows()
	if m.cursor >= len(rows) {
		return nil
	}
	row := rows[m.cursor]
	if row.isGroup {
		// Toggle expansion
		m.expanded[row.group] = !m.expanded[row.group]
		return nil
	}
	// Switch to member
	return selectCmd(m.client, row.group, row.member)
}

// collapseCurrent collapses the current group if cursor is on a group row.
func (m *Model) collapseCurrent() {
	rows := m.buildRows()
	if m.cursor >= len(rows) {
		return
	}
	row := rows[m.cursor]
	if row.isGroup {
		// On a group row: just collapse it
		m.expanded[row.group] = false
	} else {
		// On a member row: collapse the parent group and move cursor to it
		m.expanded[row.group] = false
		for i, r := range rows {
			if r.isGroup && r.group == row.group {
				m.cursor = i
				break
			}
		}
	}
}

// toggleMode cycles through rule -> global -> direct -> rule.
func (m *Model) toggleMode() tea.Cmd {
	var next string
	switch m.mode {
	case "rule":
		next = "global"
	case "global":
		next = "direct"
	case "direct":
		next = "rule"
	default:
		next = "rule"
	}
	return setModeCmd(m.client, next)
}

// refresh triggers a full refresh of proxies and mode.
func (m *Model) refresh() tea.Cmd {
	return tea.Batch(FetchProxies(m.client), FetchMode(m.client))
}

// Mode returns the current mode (for testing).
func (m *Model) Mode() string {
	return m.mode
}

// Groups returns the filtered group names (for testing).
func (m *Model) Groups() []string {
	return m.groups
}

// Expanded returns the expansion state (for testing).
func (m *Model) Expanded() map[string]bool {
	return m.expanded
}

// Cursor returns the cursor position (for testing).
func (m *Model) Cursor() int {
	return m.cursor
}

// testGroupDelay returns a command that tests the delay of the group at the current cursor position.
// If the cursor is not on a group row or the group is already being tested, it returns nil.
func (m *Model) testGroupDelay() tea.Cmd {
	rows := m.buildRows()
	if m.cursor >= len(rows) {
		return nil
	}
	row := rows[m.cursor]
	if !row.isGroup {
		return nil
	}
	group := row.group
	if m.testing[group] {
		return nil
	}
	m.testing[group] = true
	return GroupDelayCmd(m.client, group)
}