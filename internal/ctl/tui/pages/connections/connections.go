// Package connections implements the Connections tab: a table of active
// connections with close and close-all operations.
package connections

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/shared"
)

// Model holds the state for the Connections tab.
type Model struct {
	client    *ctl.Client
	conns     []ctl.Connection
	dlTotal   int64
	ulTotal   int64
	cursor    int
	confirmAll bool
	width     int
	height    int
}

// New returns the Connections tab.
func New(client *ctl.Client) *Model {
	return &Model{client: client}
}

// Title implements shared.Tab.
func (m *Model) Title() string { return "Connections" }

// Help implements shared.Tab.
func (m *Model) Help() string {
	if m.confirmAll {
		return "⚠ 关闭全部连接? (y 确认 / 其他取消)"
	}
	return "1-5:tabs  ↑↓:select  x:close  X:close all  r:refresh  q:quit"
}

// Busy implements shared.Tab.
func (m *Model) Busy() bool { return false }

// Overlay implements shared.Tab.
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
		return m.updateKey(msg)
	case ConnectionsLoadedMsg:
		if msg.Err != nil {
			return m, nil, true
		}
		m.conns = msg.Resp.Connections
		m.dlTotal = msg.Resp.DownloadTotal
		m.ulTotal = msg.Resp.UploadTotal
		// Clamp cursor
		if m.cursor >= len(m.conns) {
			m.cursor = max(0, len(m.conns)-1)
		}
		return m, nil, true
	case ConnectionOpMsg:
		if msg.Err != nil {
			return m, nil, true
		}
		// Refresh connections after close operation
		return m, FetchConnections(m.client), true
	}
	return m, nil, false
}

// updateKey handles key presses in the Connections tab.
func (m *Model) updateKey(msg tea.Msg) (shared.Tab, tea.Cmd, bool) {
	key := msg.(tea.KeyPressMsg).String()

	// Handle confirmation state
	if m.confirmAll {
		m.confirmAll = false
		if key == "y" || key == "Y" {
			return m, closeAllCmd(m.client), true
		}
		return m, nil, true
	}

	switch key {
	case "up", "k":
		m.moveCursor(-1)
		return m, nil, true
	case "down", "j":
		m.moveCursor(1)
		return m, nil, true
	case "x":
		return m, m.closeCurrent(), true
	case "X":
		m.confirmAll = true
		return m, nil, true
	case "r":
		return m, FetchConnections(m.client), true
	}
	return m, nil, false
}

// moveCursor moves the cursor by delta, clamping to valid range.
func (m *Model) moveCursor(delta int) {
	if len(m.conns) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.conns) {
		m.cursor = len(m.conns) - 1
	}
}

// closeCurrent returns a command to close the currently selected connection.
func (m *Model) closeCurrent() tea.Cmd {
	if len(m.conns) == 0 {
		return nil
	}
	id := m.conns[m.cursor].ID
	return closeCmd(m.client, id)
}

// View implements shared.Tab.
func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	var b strings.Builder

	// Summary line
	b.WriteString(fmt.Sprintf("↓%s ↑%s", formatBytes(m.dlTotal), formatBytes(m.ulTotal)))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	// Table header
	header := fmt.Sprintf("%-20s %-8s %-20s %10s %10s", "Host", "Network", "Chains", "DL", "UL")
	b.WriteString(shared.SelectedStyle.Render(header))
	b.WriteString("\n")

	// Table rows
	for i, conn := range m.conns {
		host := conn.Metadata.Host
		if host == "" {
			host = conn.Metadata.DestinationIP + ":" + conn.Metadata.DestinationPort
		}
		if len(host) > 20 {
			host = host[:17] + "..."
		}

		chains := strings.Join(conn.Chains, " → ")
		if len(chains) > 20 {
			chains = chains[:17] + "..."
		}

		row := fmt.Sprintf("%-20s %-8s %-20s %10s %10s",
			host,
			conn.Metadata.Network,
			chains,
			formatBytes(conn.Download),
			formatBytes(conn.Upload),
		)

		if i == m.cursor {
			row = shared.SelectedStyle.Render(row)
		}
		b.WriteString(row)
		b.WriteString("\n")
	}

	return lipgloss.NewStyle().Height(m.height).MaxHeight(m.height).Render(b.String())
}

// formatBytes formats bytes as human-readable string.
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1fKB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// Connections returns the current connections (for testing).
func (m *Model) Connections() []ctl.Connection {
	return m.conns
}

// Cursor returns the cursor position (for testing).
func (m *Model) Cursor() int {
	return m.cursor
}

// ConfirmAll returns whether the close-all confirmation is active (for testing).
func (m *Model) ConfirmAll() bool {
	return m.confirmAll
}