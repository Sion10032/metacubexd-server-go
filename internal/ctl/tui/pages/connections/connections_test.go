package connections

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
)

// TestConnectionsView verifies that the view shows connections and summary.
func TestConnectionsView(t *testing.T) {
	msg := ConnectionsLoadedMsg{
		Resp: ctl.ConnectionsResponse{
			DownloadTotal: 1024,
			UploadTotal:   512,
			Connections: []ctl.Connection{
				{
					ID:       "conn1",
					Upload:   100,
					Download: 200,
					Chains:   []string{"GLOBAL", "node1"},
					Metadata: ctl.ConnectionMetadata{
						Network:         "tcp",
						Type:            "HTTP",
						SourceIP:        "127.0.0.1",
						DestinationIP:   "93.184.216.34",
						SourcePort:      "12345",
						DestinationPort: "80",
						Host:            "example.com",
					},
				},
			},
		},
	}

	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	view := m.View()
	if !strings.Contains(view, "example.com") {
		t.Errorf("view missing host:\n%s", view)
	}
	if !strings.Contains(view, "↓1.0KB ↑512B") {
		t.Errorf("view missing summary:\n%s", view)
	}
	if !strings.Contains(view, "GLOBAL → node1") {
		t.Errorf("view missing chains:\n%s", view)
	}
}

// TestConnectionClose verifies that pressing 'x' closes the selected connection.
func TestConnectionClose(t *testing.T) {
	msg := ConnectionsLoadedMsg{
		Resp: ctl.ConnectionsResponse{
			Connections: []ctl.Connection{
				{ID: "conn1", Metadata: ctl.ConnectionMetadata{Host: "example.com"}},
			},
		},
	}

	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	// Press 'x' to close the connection
	tab, cmd, handled := m.Update(ctlKeyPress("x"))
	m = tab.(*Model)
	if !handled {
		t.Fatal("x should be handled")
	}
	if cmd == nil {
		t.Fatal("x should return a command")
	}
}

// TestConnectionCloseEmpty verifies that pressing 'x' on empty list does nothing.
func TestConnectionCloseEmpty(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)

	// Press 'x' on empty list
	tab, cmd, handled := m.Update(ctlKeyPress("x"))
	m = tab.(*Model)
	if !handled {
		t.Fatal("x should be handled")
	}
	if cmd != nil {
		t.Fatal("x on empty list should return nil command")
	}
}

// TestCloseAllConfirm verifies that pressing 'X' enters confirmation state.
func TestCloseAllConfirm(t *testing.T) {
	msg := ConnectionsLoadedMsg{
		Resp: ctl.ConnectionsResponse{
			Connections: []ctl.Connection{
				{ID: "conn1", Metadata: ctl.ConnectionMetadata{Host: "example.com"}},
			},
		},
	}

	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	// Press 'X' to close all
	tab, _, handled := m.Update(ctlKeyPress("X"))
	m = tab.(*Model)
	if !handled {
		t.Fatal("X should be handled")
	}
	if !m.ConfirmAll() {
		t.Fatal("confirmAll should be true after X")
	}
	if !strings.Contains(m.Help(), "关闭全部连接") {
		t.Errorf("help should show confirmation prompt:\n%s", m.Help())
	}

	// Press 'y' to confirm
	tab, cmd, handled := m.Update(ctlKeyPress("y"))
	m = tab.(*Model)
	if !handled {
		t.Fatal("y should be handled")
	}
	if cmd == nil {
		t.Fatal("y should return a command")
	}
	if m.ConfirmAll() {
		t.Fatal("confirmAll should be false after y")
	}

	// Press 'n' to cancel
	m.confirmAll = true
	tab, _, handled = m.Update(ctlKeyPress("n"))
	m = tab.(*Model)
	if !handled {
		t.Fatal("n should be handled")
	}
	if m.ConfirmAll() {
		t.Fatal("confirmAll should be false after n")
	}
}

// TestEmptyHostFallback verifies that Host falls back to destinationIP:port.
func TestEmptyHostFallback(t *testing.T) {
	msg := ConnectionsLoadedMsg{
		Resp: ctl.ConnectionsResponse{
			Connections: []ctl.Connection{
				{
					ID:       "conn1",
					Metadata: ctl.ConnectionMetadata{DestinationIP: "93.184.216.34", DestinationPort: "80"},
				},
			},
		},
	}

	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	view := m.View()
	if !strings.Contains(view, "93.184.216.34:80") {
		t.Errorf("view should fallback to destinationIP:port:\n%s", view)
	}
}

// TestConnectionsCursor verifies cursor movement.
func TestConnectionsCursor(t *testing.T) {
	msg := ConnectionsLoadedMsg{
		Resp: ctl.ConnectionsResponse{
			Connections: []ctl.Connection{
				{ID: "conn1", Metadata: ctl.ConnectionMetadata{Host: "example.com"}},
				{ID: "conn2", Metadata: ctl.ConnectionMetadata{Host: "google.com"}},
			},
		},
	}

	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	if m.Cursor() != 0 {
		t.Errorf("cursor should start at 0, got %d", m.Cursor())
	}

	// Move down
	tab, _, _ = m.Update(ctlKeyPress("down"))
	m = tab.(*Model)
	if m.Cursor() != 1 {
		t.Errorf("cursor should be 1 after down, got %d", m.Cursor())
	}

	// Move up
	tab, _, _ = m.Update(ctlKeyPress("up"))
	m = tab.(*Model)
	if m.Cursor() != 0 {
		t.Errorf("cursor should be 0 after up, got %d", m.Cursor())
	}
}

// TestFormatBytes verifies byte formatting.
func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0B"},
		{100, "100B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
		{1073741824, "1.0GB"},
	}

	for _, tt := range tests {
		got := formatBytes(tt.bytes)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

// Helper function to create a key press message.
func ctlKeyPress(key string) tea.KeyPressMsg {
	r := []rune(key)[0]
	return tea.KeyPressMsg{Code: r, Text: key}
}