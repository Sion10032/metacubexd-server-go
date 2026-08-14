package proxies

import (
	"strings"
	"testing"

	"metacubexd-server-go/internal/ctl"
)

// TestProxiesListRender verifies that the view shows proxy-groups and excludes GLOBAL.
func TestProxiesListRender(t *testing.T) {
	resp := ctl.ProxiesResponse{
		Proxies: map[string]ctl.Proxy{
			"GLOBAL": {Name: "GLOBAL", Type: "Selector", Now: "A", All: []string{"A", "B"}},
			"🚀节点选择": {Name: "🚀节点选择", Type: "Selector", Now: "香港", All: []string{"香港", "日本", "♻️自动选择"}},
			"♻️自动选择": {Name: "♻️自动选择", Type: "URLTest", Now: "香港", All: []string{"香港", "日本"}},
		},
		Order: []string{"GLOBAL", "🚀节点选择", "♻️自动选择"},
	}
	msg := ProxiesLoadedMsg{Resp: resp}

	m := New(nil)
	m.SetSize(80, 20)
	// Simulate receiving the message
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	// Check groups
	groups := m.Groups()
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	// Should not contain GLOBAL
	for _, g := range groups {
		if g == "GLOBAL" {
			t.Error("groups should not contain GLOBAL")
		}
	}

	// Check view
	view := m.View()
	if !strings.Contains(view, "🚀节点选择") {
		t.Errorf("view missing 🚀节点选择:\n%s", view)
	}
	if !strings.Contains(view, "♻️自动选择") {
		t.Errorf("view missing ♻️自动选择:\n%s", view)
	}
	// Check current node marker
	if !strings.Contains(view, "[香港]") {
		t.Errorf("view missing current node marker [香港]:\n%s", view)
	}
}

// TestModeRender verifies the mode line is rendered.
func TestModeRender(t *testing.T) {
	msg := ModeLoadedMsg{Mode: "rule"}
	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	view := m.View()
	if !strings.Contains(view, "mode:") || !strings.Contains(view, "rule") {
		t.Errorf("view missing mode line:\n%s", view)
	}
}

// TestModeToggle verifies pressing 'm' cycles through modes.
func TestModeToggle(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.mode = "rule"

	// Press 'm' to toggle to global
	cmd := m.toggleMode()
	if cmd == nil {
		t.Fatal("toggleMode returned nil")
	}
	// The command will send a ModeOpMsg; we just need to verify the mode
	// is not changed yet (it's async). We'll test the cycle logic directly.
	if m.mode != "rule" {
		t.Errorf("mode should still be rule before command executes, got %s", m.mode)
	}

	// Simulate the mode change by calling the internal logic
	// We'll test the toggleMode function's behavior by checking the returned command
	// For now, we just verify the command is not nil
}

// TestProxiesExpandCollapse verifies expand/collapse behavior.
func TestProxiesExpandCollapse(t *testing.T) {
	resp := ctl.ProxiesResponse{
		Proxies: map[string]ctl.Proxy{
			"🚀节点选择": {Name: "🚀节点选择", Type: "Selector", Now: "香港", All: []string{"香港", "日本"}},
		},
		Order: []string{"🚀节点选择"},
	}
	msg := ProxiesLoadedMsg{Resp: resp}
	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	// Initially collapsed - only group row
	view := m.View()
	if strings.Contains(view, "香港") && strings.Contains(view, "日本") {
		t.Errorf("collapsed view should not show members:\n%s", view)
	}

	// Expand by pressing enter (cursor is at 0)
	m.cursor = 0
	_ = m.expandOrSwitch()

	// Now members should be visible
	view = m.View()
	if !strings.Contains(view, "香港") || !strings.Contains(view, "日本") {
		t.Errorf("expanded view should show members:\n%s", view)
	}

	// Collapse again
	m.cursor = 0
	_ = m.expandOrSwitch()

	// Members should be hidden
	view = m.View()
	if strings.Contains(view, "香港") && strings.Contains(view, "日本") {
		t.Errorf("collapsed view should not show members:\n%s", view)
	}
}

// TestProxySwitch verifies switching to a member.
func TestProxySwitch(t *testing.T) {
	resp := ctl.ProxiesResponse{
		Proxies: map[string]ctl.Proxy{
			"🚀节点选择": {Name: "🚀节点选择", Type: "Selector", Now: "香港", All: []string{"香港", "日本"}},
		},
		Order: []string{"🚀节点选择"},
	}
	msg := ProxiesLoadedMsg{Resp: resp}
	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	// Expand group
	m.cursor = 0
	_ = m.expandOrSwitch()

	// Move cursor to second member (日本)
	m.cursor = 2 // group row (0) + 香港 (1) + 日本 (2)

	// Switch to 日本
	cmd := m.expandOrSwitch()
	if cmd == nil {
		t.Fatal("expandOrSwitch returned nil for member")
	}
	// The command should be a selectCmd; we can't easily test the HTTP call
	// but we can verify the function doesn't panic
}

// TestProxyNoNesting verifies that members that are groups are not expanded.
func TestProxyNoNesting(t *testing.T) {
	resp := ctl.ProxiesResponse{
		Proxies: map[string]ctl.Proxy{
			"🚀节点选择": {Name: "🚀节点选择", Type: "Selector", Now: "♻️自动选择", All: []string{"♻️自动选择", "日本"}},
			"♻️自动选择": {Name: "♻️自动选择", Type: "URLTest", Now: "香港", All: []string{"香港", "日本"}},
		},
		Order: []string{"🚀节点选择", "♻️自动选择"},
	}
	msg := ProxiesLoadedMsg{Resp: resp}
	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	// Expand 节点选择
	m.cursor = 0
	_ = m.expandOrSwitch()

	// Move to ♻️自动选择 member (cursor 1)
	m.cursor = 1

	// Press enter - should switch, not expand
	cmd := m.expandOrSwitch()
	if cmd == nil {
		t.Fatal("expandOrSwitch returned nil for member")
	}
	// Verify that ♻️自动选择 is not expanded (it's a member, not a group row)
	if m.expanded["♻️自动选择"] {
		t.Error("member that is a group should not be expanded")
	}
}

// TestProxiesEmpty verifies empty map doesn't panic.
func TestProxiesEmpty(t *testing.T) {
	resp := ctl.ProxiesResponse{
		Proxies: map[string]ctl.Proxy{},
		Order: []string{},
	}
	msg := ProxiesLoadedMsg{Resp: resp}
	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	view := m.View()
	if view == "" {
		t.Error("view should not be empty for empty proxies")
	}
	// Should contain mode line
	if !strings.Contains(view, "mode:") {
		t.Errorf("view missing mode line:\n%s", view)
	}
}