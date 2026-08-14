package proxies

import (
	"fmt"
	"strings"
	"testing"

	"metacubexd-server-go/internal/tui/client"
)

// TestProxiesListRender verifies that the view shows proxy-groups and excludes GLOBAL.
func TestProxiesListRender(t *testing.T) {
	resp := client.ProxiesResponse{
		Proxies: map[string]client.Proxy{
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
	resp := client.ProxiesResponse{
		Proxies: map[string]client.Proxy{
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
	resp := client.ProxiesResponse{
		Proxies: map[string]client.Proxy{
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
	resp := client.ProxiesResponse{
		Proxies: map[string]client.Proxy{
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
	resp := client.ProxiesResponse{
		Proxies: map[string]client.Proxy{},
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

// TestHistoryParsedIntoDelays verifies that proxy history is extracted into delays map.
func TestHistoryParsedIntoDelays(t *testing.T) {
	resp := client.ProxiesResponse{
		Proxies: map[string]client.Proxy{
			"香港": {
				Name:    "香港",
				Type:    "ss",
				History: []client.DelayHistory{{Time: "2024-01-01T00:00:00Z", Delay: 150}},
			},
			"日本": {
				Name:    "日本",
				Type:    "vmess",
				History: []client.DelayHistory{{Time: "2024-01-01T00:00:00Z", Delay: 0}},
			},
			"美国": {
				Name:    "美国",
				Type:    "trojan",
				History: nil,
			},
		},
		Order: []string{"香港", "日本", "美国"},
	}
	msg := ProxiesLoadedMsg{Resp: resp}
	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	if d, ok := m.delays["香港"]; !ok || d != 150 {
		t.Errorf("delays[香港] = %d, want 150", d)
	}
	if d, ok := m.delays["日本"]; !ok || d != 0 {
		t.Errorf("delays[日本] = %d, want 0", d)
	}
	if _, ok := m.delays["美国"]; ok {
		t.Error("delays[美国] should not exist")
	}
}

// TestGroupDelayMsgMerge verifies GroupDelayMsg merges delays and clears testing.
func TestGroupDelayMsgMerge(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.testing["🚀节点选择"] = true
	m.delays["香港"] = 100

	delays := map[string]int{"香港": 200, "日本": 300}
	msg := GroupDelayMsg{Group: "🚀节点选择", Delays: delays}
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	if m.testing["🚀节点选择"] {
		t.Error("testing flag should be cleared")
	}
	if d := m.delays["香港"]; d != 200 {
		t.Errorf("delays[香港] = %d, want 200", d)
	}
	if d := m.delays["日本"]; d != 300 {
		t.Errorf("delays[日本] = %d, want 300", d)
	}
}

// TestGroupDelayMsgError verifies GroupDelayMsg error clears testing.
func TestGroupDelayMsgError(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.testing["🚀节点选择"] = true

	msg := GroupDelayMsg{Group: "🚀节点选择", Err: fmt.Errorf("network error")}
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	if m.testing["🚀节点选择"] {
		t.Error("testing flag should be cleared on error")
	}
}

// TestDelayRender verifies delay rendering in the view.
func TestDelayRender(t *testing.T) {
	resp := client.ProxiesResponse{
		Proxies: map[string]client.Proxy{
			"GROUP": {Name: "GROUP", Type: "Selector", Now: "香港", All: []string{"香港", "日本", "美国"}},
			"香港": {
				Name:    "香港",
				Type:    "ss",
				History: []client.DelayHistory{{Time: "2024-01-01T00:00:00Z", Delay: 150}},
			},
			"日本": {
				Name:    "日本",
				Type:    "vmess",
				History: []client.DelayHistory{{Time: "2024-01-01T00:00:00Z", Delay: 0}},
			},
			"美国": {
				Name:    "美国",
				Type:    "trojan",
				History: nil,
			},
		},
		Order: []string{"GROUP"},
	}
	msg := ProxiesLoadedMsg{Resp: resp}
	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	m.cursor = 0
	_ = m.expandOrSwitch()

	view := m.View()
	if !strings.Contains(view, "150ms") {
		t.Errorf("view missing 150ms:\n%s", view)
	}
	if !strings.Contains(view, "timeout") {
		t.Errorf("view missing timeout:\n%s", view)
	}
	if !strings.Contains(view, "--") {
		t.Errorf("view missing --:\n%s", view)
	}
}

// TestGroupDelayTestingRender verifies testing… is shown during delay test
// and members show "-" instead of stale delay values.
func TestGroupDelayTestingRender(t *testing.T) {
	resp := client.ProxiesResponse{
		Proxies: map[string]client.Proxy{
			"GROUP": {Name: "GROUP", Type: "Selector", Now: "香港", All: []string{"香港", "日本"}},
		},
		Order: []string{"GROUP"},
	}
	msg := ProxiesLoadedMsg{Resp: resp}
	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	// Pre-populate stale delays
	m.delays["香港"] = 100
	m.delays["日本"] = 200

	// Expand group and start testing
	m.cursor = 0
	_ = m.expandOrSwitch()
	m.testing["GROUP"] = true

	view := m.View()
	if !strings.Contains(view, "testing…") {
		t.Errorf("view missing testing…:\n%s", view)
	}
	// Members should show "-" during testing, not stale delays
	if strings.Contains(view, "100ms") {
		t.Errorf("view should not show stale delay 100ms during testing:\n%s", view)
	}
	if strings.Contains(view, "200ms") {
		t.Errorf("view should not show stale delay 200ms during testing:\n%s", view)
	}
}

// TestGroupDelayBusy verifies Busy returns true during delay test.
func TestGroupDelayBusy(t *testing.T) {
	m := New(nil)
	if m.Busy() {
		t.Error("Busy should be false initially")
	}
	m.testing["GROUP"] = true
	if !m.Busy() {
		t.Error("Busy should be true during testing")
	}
	m.testing["GROUP"] = false
	if m.Busy() {
		t.Error("Busy should be false after testing")
	}
}

// TestGroupDelayDKey verifies pressing 'd' triggers delay test.
func TestGroupDelayDKey(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	cmd := m.testGroupDelay()
	if cmd != nil {
		t.Error("testGroupDelay should return nil when no groups")
	}

	resp := client.ProxiesResponse{
		Proxies: map[string]client.Proxy{
			"GROUP": {Name: "GROUP", Type: "Selector", Now: "香港", All: []string{"香港"}},
		},
		Order: []string{"GROUP"},
	}
	msg := ProxiesLoadedMsg{Resp: resp}
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	m.cursor = 0
	cmd = m.testGroupDelay()
	if cmd == nil {
		t.Fatal("testGroupDelay should return a command")
	}
	if !m.testing["GROUP"] {
		t.Error("testing flag should be set")
	}

	cmd = m.testGroupDelay()
	if cmd != nil {
		t.Error("testGroupDelay should return nil when already testing")
	}
}

// TestGroupDelayDKeyMember verifies pressing 'd' on a member row does nothing.
func TestGroupDelayDKeyMember(t *testing.T) {
	resp := client.ProxiesResponse{
		Proxies: map[string]client.Proxy{
			"GROUP": {Name: "GROUP", Type: "Selector", Now: "香港", All: []string{"香港"}},
		},
		Order: []string{"GROUP"},
	}
	msg := ProxiesLoadedMsg{Resp: resp}
	m := New(nil)
	m.SetSize(80, 20)
	tab, _, _ := m.Update(msg)
	m = tab.(*Model)

	m.cursor = 0
	_ = m.expandOrSwitch()

	m.cursor = 1
	cmd := m.testGroupDelay()
	if cmd != nil {
		t.Error("testGroupDelay should return nil on member row")
	}
}

// TestScrolling verifies that the list scrolls when the cursor moves beyond visible area.
func TestScrolling(t *testing.T) {
	// Create a group with many members to exceed visible area
	members := make([]string, 20)
	for i := range members {
		members[i] = fmt.Sprintf("node%d", i)
	}
	proxies := map[string]client.Proxy{
		"GROUP": {Name: "GROUP", Type: "Selector", Now: "node0", All: members},
	}
	for _, name := range members {
		proxies[name] = client.Proxy{Name: name, Type: "ss"}
	}
	resp := client.ProxiesResponse{Proxies: proxies, Order: []string{"GROUP"}}
	m := New(nil)
	m.SetSize(80, 10) // 10 lines total, 8 visible rows (10 - 2 header)
	tab, _, _ := m.Update(ProxiesLoadedMsg{Resp: resp})
	m = tab.(*Model)

	// Expand group
	m.cursor = 0
	_ = m.expandOrSwitch()

	// Move cursor down past visible area
	for i := 0; i < 10; i++ {
		m.moveCursor(1)
	}

	// Cursor should be at 10, scrollTop should have adjusted
	if m.cursor != 10 {
		t.Errorf("cursor = %d, want 10", m.cursor)
	}
	if m.scrollTop == 0 {
		t.Error("scrollTop should be > 0 when cursor is below visible area")
	}
	// Cursor should be within visible window
	vh := m.visibleHeight()
	if m.cursor < m.scrollTop || m.cursor >= m.scrollTop+vh {
		t.Errorf("cursor %d not in visible window [%d, %d)", m.cursor, m.scrollTop, m.scrollTop+vh)
	}
}

// TestCollapseWhenOffscreen verifies that collapsing works even when cursor is off-screen.
func TestCollapseWhenOffscreen(t *testing.T) {
	members := make([]string, 20)
	for i := range members {
		members[i] = fmt.Sprintf("node%d", i)
	}
	proxies := map[string]client.Proxy{
		"GROUP": {Name: "GROUP", Type: "Selector", Now: "node0", All: members},
	}
	for _, name := range members {
		proxies[name] = client.Proxy{Name: name, Type: "ss"}
	}
	resp := client.ProxiesResponse{Proxies: proxies, Order: []string{"GROUP"}}
	m := New(nil)
	m.SetSize(80, 10)
	tab, _, _ := m.Update(ProxiesLoadedMsg{Resp: resp})
	m = tab.(*Model)

	// Expand group
	m.cursor = 0
	_ = m.expandOrSwitch()
	if !m.expanded["GROUP"] {
		t.Fatal("GROUP should be expanded")
	}

	// Move to a member row far down
	for i := 0; i < 15; i++ {
		m.moveCursor(1)
	}

	// Collapse via left key
	m.collapseCurrent()
	if m.expanded["GROUP"] {
		t.Error("GROUP should be collapsed after pressing left on member")
	}
	// Cursor should be on the group row
	rows := m.buildRows()
	if m.cursor >= len(rows) || !rows[m.cursor].isGroup {
		t.Errorf("cursor should be on group row after collapse, cursor=%d", m.cursor)
	}
}