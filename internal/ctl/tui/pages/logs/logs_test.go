package logs

import (
	"fmt"
	"strings"
	"testing"
)

// TestLogsAppend verifies lines accumulate in the viewport and the filter
// drops non-matching lines.
func TestLogsAppend(t *testing.T) {
	l := New(nil)
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
	l := New(nil)
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
	l := New(nil)
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

// TestSetSizeFollowsBottom reproduces the short-window bug: lines appended
// while the viewport is at its default height leave blank space below after a
// resize. Resizing while following must re-scroll to the bottom.
func TestSetSizeFollowsBottom(t *testing.T) {
	l := New(nil) // default viewport height 20
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
