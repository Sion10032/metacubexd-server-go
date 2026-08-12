package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
	if len(l.lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(l.lines))
	}
	if got := l.View(); !strings.Contains(got, "line two") {
		t.Errorf("viewport view = %q, missing line two", got)
	}

	l.filter = "two"
	l.append("line three")
	if len(l.lines) != 2 {
		t.Errorf("lines after filter = %d, want 2 (filtered line dropped)", len(l.lines))
	}
}

// TestViewLogStream drives the model with a log line and verifies it appears
// in the view and the waiting hint disappears.
func TestViewLogStream(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(logMsg{line: "hello kernel"})

	got := ansiRe.ReplaceAllString(nm.View(), "")
	if !strings.Contains(got, "hello kernel") {
		t.Errorf("View = %q, missing log line", got)
	}
	if strings.Contains(got, "waiting for log stream") {
		t.Error("View still shows the waiting hint after a log line")
	}
}
