package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/pages/logs"
	"metacubexd-server-go/internal/ctl/tui/shared"
)

// TestFilterInput verifies / enters filter mode, typing + enter applies the
// filter and esc cancels without changing it.
func TestFilterInput(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))

	nm, _ := m.Update(keyPress("/"))
	if !nm.(Model).tabs[0].(*logs.Model).Filtering() {
		t.Fatal("filtering should be true after /")
	}

	for _, r := range "err" {
		nm, _ = nm.Update(keyPress(string(r)))
	}
	if got := nm.(Model).tabs[0].(*logs.Model).FilterInput(); got != "err" {
		t.Errorf("filterInput = %q, want err", got)
	}

	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mdl := nm.(Model)
	if mdl.tabs[0].(*logs.Model).Filtering() || mdl.tabs[0].(*logs.Model).Filter() != "err" {
		t.Errorf("after enter: filtering=%v filter=%q", mdl.tabs[0].(*logs.Model).Filtering(), mdl.tabs[0].(*logs.Model).Filter())
	}

	// Re-entering prefills the current filter so it can be edited or cleared.
	nm, _ = mdl.Update(keyPress("/"))
	if got := nm.(Model).tabs[0].(*logs.Model).FilterInput(); got != "err" {
		t.Errorf("filterInput after / = %q, want prefilled err", got)
	}

	// Delete to empty and enter clears the filter.
	for i := 0; i < 3; i++ {
		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mdl = nm.(Model)
	if mdl.tabs[0].(*logs.Model).Filter() != "" {
		t.Errorf("filter after clear = %q, want empty", mdl.tabs[0].(*logs.Model).Filter())
	}
	if mdl.tabs[0].(*logs.Model).Filtering() {
		t.Error("filtering should be false after applying")
	}

	// backspace deletes one character from a fresh input.
	nm, _ = mdl.Update(keyPress("/"))
	nm, _ = nm.Update(keyPress("x"))
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := nm.(Model).tabs[0].(*logs.Model).FilterInput(); got != "" {
		t.Errorf("filterInput after backspace = %q, want empty", got)
	}

	// esc cancels without applying.
	nm, _ = nm.Update(keyPress("z"))
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	mdl = nm.(Model)
	if mdl.tabs[0].(*logs.Model).Filtering() || mdl.tabs[0].(*logs.Model).Filter() != "" {
		t.Errorf("after esc: filtering=%v filter=%q (should keep empty)", mdl.tabs[0].(*logs.Model).Filtering(), mdl.tabs[0].(*logs.Model).Filter())
	}
}

// TestFilterAppliesToHistory verifies setting a filter rebuilds the viewport
// content, filtering lines that arrived before the filter was applied.
func TestFilterAppliesToHistory(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(shared.LogLineMsg{Line: "info: normal line"})
	nm, _ = nm.Update(shared.LogLineMsg{Line: "error: bad thing"})

	// Apply a filter through the input flow.
	nm, _ = nm.Update(keyPress("/"))
	for _, r := range "error" {
		nm, _ = nm.Update(keyPress(string(r)))
	}
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	mdl := nm.(Model)
	if mdl.tabs[0].(*logs.Model).Filter() != "error" {
		t.Fatalf("filter = %q, want error", mdl.tabs[0].(*logs.Model).Filter())
	}
	if mdl.tabs[0].(*logs.Model).LineCount() != 2 {
		t.Errorf("history = %d, want 2 (full history retained)", mdl.tabs[0].(*logs.Model).LineCount())
	}
	if got := mdl.View().Content; strings.Contains(got, "normal line") {
		t.Errorf("View still shows filtered-out history:\n%s", got)
	}
}

// TestMouseWheelScroll verifies wheel events reach the log viewport so the
// terminal buffer is not scrolled instead.
func TestMouseWheelScroll(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(keyPress("f")) // follow off so appends do not re-scroll
	for i := 0; i < 50; i++ {
		nm, _ = nm.Update(shared.LogLineMsg{Line: fmt.Sprintf("scroll line %d", i)})
	}

	nm, _ = nm.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	mdl := nm.(Model)
	if got := mdl.tabs[0].(*logs.Model).YOffset(); got <= 0 {
		t.Errorf("wheel down did not scroll (YOffset=%d)", got)
	}
}

// TestFollowToggle verifies f flips the follow-at-bottom flag.
func TestFollowToggle(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	if !m.tabs[0].(*logs.Model).Following() {
		t.Fatal("follow should default to true")
	}

	nm, _ := m.Update(keyPress("f"))
	if nm.(Model).tabs[0].(*logs.Model).Following() {
		t.Error("follow should be false after f")
	}
	nm, _ = nm.Update(keyPress("f"))
	if !nm.(Model).tabs[0].(*logs.Model).Following() {
		t.Error("follow should be true after f again")
	}
}

// TestFollowIndicator verifies the help line shows the follow state.
func TestFollowIndicator(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	if got := m.View().Content; !strings.Contains(got, "f:follow(ON)") {
		t.Errorf("View = %q, want follow(ON)", got)
	}

	nm, _ := m.Update(keyPress("f"))
	if got := nm.View().Content; !strings.Contains(got, "f:follow(OFF)") {
		t.Errorf("View = %q, want follow(OFF)", got)
	}
}

// TestScrollOnEveryTab verifies PgDn scrolls the active viewport: the log
// viewport on the Logs and Subscriptions tabs, and the config viewport in the
// config viewer modal.
func TestScrollOnEveryTab(t *testing.T) {
	for _, tab := range []string{"1", "2"} {
		t.Run("logs tab "+tab, func(t *testing.T) {
			m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
			nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			nm, _ = nm.Update(keyPress("f")) // follow off
			for i := 0; i < 50; i++ {
				nm, _ = nm.Update(shared.LogLineMsg{Line: fmt.Sprintf("scroll line %d", i)})
			}
			nm, _ = nm.Update(keyPress(tab))

			nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
			mdl := nm.(Model)
			if YOffset := mdl.tabs[0].(*logs.Model).YOffset(); YOffset == 0 {
				t.Errorf("PgDn on tab %s did not scroll the log viewport (YOffset=0)", tab)
			}
		})
	}

	t.Run("config viewer", func(t *testing.T) {
		m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
		nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		nm, _ = nm.Update(configLoadedMsg{mode: configActive, content: strings.Repeat("line\n", 50)})
		mdl := nm.(Model)
		mdl.kernel.viewingConfig = true
		nm = mdl

		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		mdl = nm.(Model)
		if YOffset := mdl.config.viewport.YOffset(); YOffset == 0 {
			t.Errorf("PgDn in config viewer did not scroll the config viewport (YOffset=0)")
		}
	})
}

// TestViewportScroll verifies PgDn is forwarded to the log viewport.
func TestViewportScroll(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(keyPress("f")) // follow off, inspect the scroll position
	for i := 0; i < 50; i++ {
		nm, _ = nm.Update(shared.LogLineMsg{Line: fmt.Sprintf("scroll line %d", i)})
	}

	if got := nm.View().Content; !strings.Contains(got, "scroll line 0") {
		t.Errorf("initial view missing first line:\n%s", got)
	}
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	got := nm.View().Content
	if strings.Contains(got, "scroll line 0") || !strings.Contains(got, "scroll line 17") {
		t.Errorf("PgDn did not scroll the viewport:\n%s", got)
	}
}

// TestViewLogStream drives the model with a log line and verifies it appears
// in the view and the waiting hint disappears.
func TestViewLogStream(t *testing.T) {

	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(shared.LogLineMsg{Line: "hello kernel"})

	got := shared.ANSIRe.ReplaceAllString(nm.View().Content, "")
	if !strings.Contains(got, "hello kernel") {
		t.Errorf("View = %q, missing log line", got)
	}
	if strings.Contains(got, "waiting for log stream") {
		t.Error("View still shows the waiting hint after a log line")
	}
}
