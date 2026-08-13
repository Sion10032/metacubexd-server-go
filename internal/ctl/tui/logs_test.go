package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/supervisor"
)

// TestFormatLogLine verifies the log line rendering: timestamp, INFO level
// and the ERROR style for stderr.
func TestFormatLogLine(t *testing.T) {
	ts := time.Date(2025, 8, 12, 15, 30, 1, 0, time.Local).UnixMilli()

	got := formatLogLine(supervisor.KernelLogLine{Stream: "stdout", Line: "inbound mixed port 7890 listening", TS: ts})
	want := "2025-08-12 15:30:01 INFO   inbound mixed port 7890 listening"
	if stripANSI(got) != want {
		t.Errorf("formatLogLine = %q, want %q", stripANSI(got), want)
	}

	got = formatLogLine(supervisor.KernelLogLine{Stream: "stderr", Line: "boom", TS: ts})
	if !strings.Contains(got, "ERROR") || !strings.Contains(stripANSI(got), "boom") {
		t.Errorf("formatLogLine(stderr) = %q, want ERROR + line", stripANSI(got))
	}
}

// TestParseLogEvent decodes SSE payloads into log/state messages.
func TestParseLogEvent(t *testing.T) {
	msg := parseLogEvent(ctl.Event{Data: `{"type":"log","stream":"stdout","line":"hello","ts":1723469401000}`})
	lm, ok := msg.(logMsg)
	if !ok {
		t.Fatalf("parseLogEvent(log) = %T, want logMsg", msg)
	}
	if !strings.Contains(stripANSI(lm.line), "hello") {
		t.Errorf("logMsg.line = %q, want hello", lm.line)
	}

	msg = parseLogEvent(ctl.Event{Data: `{"type":"state","status":"running","pid":42}`})
	sm, ok := msg.(stateMsg)
	if !ok {
		t.Fatalf("parseLogEvent(state) = %T, want stateMsg", msg)
	}
	if sm.state.Status != supervisor.StatusRunning {
		t.Errorf("stateMsg status = %q, want running", sm.state.Status)
	}

	if parseLogEvent(ctl.Event{Data: `{"type":"nope"}`}) != nil {
		t.Error("unknown event type should be ignored")
	}
	if parseLogEvent(ctl.Event{Data: `{bad json`}) != nil {
		t.Error("malformed JSON should be ignored")
	}
}

// TestLogsAppend verifies lines accumulate in the viewport and the filter
// drops non-matching lines.
func TestLogsAppend(t *testing.T) {
	l := NewLogsModel()
	l.SetSize(80, 10)

	l.append("line one")
	l.append("line two")
	if len(l.allLines) != 2 {
		t.Fatalf("lines = %d, want 2", len(l.allLines))
	}
	if got := l.View(); !strings.Contains(got, "line two") {
		t.Errorf("viewport view = %q, missing line two", got)
	}

	// Filtering only affects the view, never the history.
	l.SetFilter("two")
	l.append("line three")
	if len(l.allLines) != 3 {
		t.Errorf("history = %d, want 3 (filter never drops lines)", len(l.allLines))
	}
	if got := l.View(); strings.Contains(got, "line one") {
		t.Errorf("filtered view still shows non-matching line: %q", got)
	}
}

// TestFilterClearRestores verifies clearing the filter brings back the full
// history that was hidden while the filter was active.
func TestFilterClearRestores(t *testing.T) {
	l := NewLogsModel()
	l.SetSize(80, 10)
	l.append("info: one")
	l.append("error: two")
	l.append("info: three")

	l.SetFilter("error")
	if got := l.View(); strings.Contains(got, "info:") {
		t.Errorf("filtered view shows info lines: %q", got)
	}

	l.SetFilter("")
	if len(l.allLines) != 3 {
		t.Errorf("history = %d, want 3 after clear", len(l.allLines))
	}
	got := l.View()
	for _, want := range []string{"info: one", "error: two", "info: three"} {
		if !strings.Contains(got, want) {
			t.Errorf("view after clear missing %q:\n%s", want, got)
		}
	}
}

// TestLogsCap verifies the retained lines are capped to bound memory.
func TestLogsCap(t *testing.T) {
	l := NewLogsModel()
	for i := 0; i < maxLogLines+50; i++ {
		l.append(fmt.Sprintf("line %d", i))
	}
	if len(l.allLines) != maxLogLines {
		t.Errorf("lines = %d, want cap %d", len(l.allLines), maxLogLines)
	}
	if l.allLines[0] != "line 50" || l.allLines[len(l.allLines)-1] != fmt.Sprintf("line %d", maxLogLines+49) {
		t.Errorf("kept window wrong: first=%q last=%q", l.allLines[0], l.allLines[len(l.allLines)-1])
	}
}

// TestFilterInput verifies / enters filter mode, typing + enter applies the
// filter and esc cancels without changing it.
func TestFilterInput(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))

	nm, _ := m.Update(keyPress("/"))
	if !nm.(Model).filtering {
		t.Fatal("filtering should be true after /")
	}

	for _, r := range "err" {
		nm, _ = nm.Update(keyPress(string(r)))
	}
	if got := nm.(Model).filterInput; got != "err" {
		t.Errorf("filterInput = %q, want err", got)
	}

	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mdl := nm.(Model)
	if mdl.filtering || mdl.logs.filter != "err" {
		t.Errorf("after enter: filtering=%v filter=%q", mdl.filtering, mdl.logs.filter)
	}

	// Re-entering prefills the current filter so it can be edited or cleared.
	nm, _ = mdl.Update(keyPress("/"))
	if got := nm.(Model).filterInput; got != "err" {
		t.Errorf("filterInput after / = %q, want prefilled err", got)
	}

	// Delete to empty and enter clears the filter.
	for i := 0; i < 3; i++ {
		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mdl = nm.(Model)
	if mdl.logs.filter != "" {
		t.Errorf("filter after clear = %q, want empty", mdl.logs.filter)
	}
	if mdl.filtering {
		t.Error("filtering should be false after applying")
	}

	// backspace deletes one character from a fresh input.
	nm, _ = mdl.Update(keyPress("/"))
	nm, _ = nm.Update(keyPress("x"))
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := nm.(Model).filterInput; got != "" {
		t.Errorf("filterInput after backspace = %q, want empty", got)
	}

	// esc cancels without applying.
	nm, _ = nm.Update(keyPress("z"))
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	mdl = nm.(Model)
	if mdl.filtering || mdl.logs.filter != "" {
		t.Errorf("after esc: filtering=%v filter=%q (should keep empty)", mdl.filtering, mdl.logs.filter)
	}
}

// TestFilterAppliesToHistory verifies setting a filter rebuilds the viewport
// content, filtering lines that arrived before the filter was applied.
func TestFilterAppliesToHistory(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(logMsg{line: "info: normal line"})
	nm, _ = nm.Update(logMsg{line: "error: bad thing"})

	// Apply a filter through the input flow.
	nm, _ = nm.Update(keyPress("/"))
	for _, r := range "error" {
		nm, _ = nm.Update(keyPress(string(r)))
	}
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	mdl := nm.(Model)
	if mdl.logs.filter != "error" {
		t.Fatalf("filter = %q, want error", mdl.logs.filter)
	}
	if len(mdl.logs.allLines) != 2 {
		t.Errorf("history = %d, want 2 (full history retained)", len(mdl.logs.allLines))
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
	mdl := nm.(Model)
	mdl.logs.follow = false
	nm = mdl
	for i := 0; i < 50; i++ {
		nm, _ = nm.Update(logMsg{line: fmt.Sprintf("scroll line %d", i)})
	}

	nm, _ = nm.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	mdl = nm.(Model)
	if got := mdl.logs.viewport.YOffset(); got <= 0 {
		t.Errorf("wheel down did not scroll (YOffset=%d)", got)
	}
}

// TestFollowToggle verifies f flips the follow-at-bottom flag.
func TestFollowToggle(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	if !m.logs.follow {
		t.Fatal("follow should default to true")
	}

	nm, _ := m.Update(keyPress("f"))
	if nm.(Model).logs.follow {
		t.Error("follow should be false after f")
	}
	nm, _ = nm.Update(keyPress("f"))
	if !nm.(Model).logs.follow {
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
// viewport on the Logs and Subscriptions tabs, and the config viewport on the
// Config tab.
func TestScrollOnEveryTab(t *testing.T) {
	for _, tab := range []string{"1", "2"} {
		t.Run("logs tab "+tab, func(t *testing.T) {
			m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
			nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			mdl := nm.(Model)
			mdl.logs.follow = false
			nm = mdl
			for i := 0; i < 50; i++ {
				nm, _ = nm.Update(logMsg{line: fmt.Sprintf("scroll line %d", i)})
			}
			nm, _ = nm.Update(keyPress(tab))

			nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
			mdl = nm.(Model)
			if YOffset := mdl.logs.viewport.YOffset(); YOffset == 0 {
				t.Errorf("PgDn on tab %s did not scroll the log viewport (YOffset=0)", tab)
			}
		})
	}

	t.Run("config tab 3", func(t *testing.T) {
		m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
		nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		nm, _ = nm.Update(configLoadedMsg{mode: configActive, content: strings.Repeat("line\n", 50)})
		nm, _ = nm.Update(keyPress("3"))

		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		mdl := nm.(Model)
		if YOffset := mdl.config.viewport.YOffset(); YOffset == 0 {
			t.Errorf("PgDn on tab 3 did not scroll the config viewport (YOffset=0)")
		}
	})
}

// TestViewportScroll verifies PgDn is forwarded to the log viewport.
func TestViewportScroll(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mdl := nm.(Model)
	mdl.logs.follow = false // inspect the scroll position
	nm = mdl
	for i := 0; i < 50; i++ {
		nm, _ = nm.Update(logMsg{line: fmt.Sprintf("scroll line %d", i)})
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

// TestSetSizeFollowsBottom reproduces the short-window bug: lines appended
// while the viewport is at its default height leave blank space below after a
// resize. Resizing while following must re-scroll to the bottom.
func TestSetSizeFollowsBottom(t *testing.T) {
	l := NewLogsModel() // default viewport height 20
	for i := 0; i < 100; i++ {
		l.append(fmt.Sprintf("line %d", i))
	}

	l.SetSize(80, 32) // resize like View() does with a 39-row window
	lines := strings.Split(strings.TrimRight(l.View(), "\n"), "\n")
	if len(lines) != 32 {
		t.Fatalf("viewport rendered %d lines, want 32", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "line 99") {
		t.Errorf("bottom line = %q, want line 99", lines[len(lines)-1])
	}
	if strings.TrimSpace(lines[0]) == "" {
		t.Error("first visible line is blank — view is not at the bottom")
	}
}

// TestViewLogStream drives the model with a log line and verifies it appears
// in the view and the waiting hint disappears.
func TestViewLogStream(t *testing.T) {

	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(logMsg{line: "hello kernel"})

	got := ansiRe.ReplaceAllString(nm.View().Content, "")
	if !strings.Contains(got, "hello kernel") {
		t.Errorf("View = %q, missing log line", got)
	}
	if strings.Contains(got, "waiting for log stream") {
		t.Error("View still shows the waiting hint after a log line")
	}
}
