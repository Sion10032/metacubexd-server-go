package tui

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/shared"
	"metacubexd-server-go/internal/supervisor"
)

// TestViewLayout verifies the full layout renders the status bar, the tab bar
// and the help line.
func TestViewLayout(t *testing.T) {

	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	got := shared.ANSIRe.ReplaceAllString(m.View().Content, "")
	for _, want := range []string{"[1] Logs", "[2] Subscriptions", "[3] Config", "/:filter", "f:follow(ON)", "q:quit"} {
		if !strings.Contains(got, want) {
			t.Errorf("View = %q, missing %q", got, want)
		}
	}
}

// TestTabHelp verifies the footer switches with the tab, showing only the
// relevant operations.
func TestTabHelp(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))

	// Logs tab: filter/follow.
	if got := m.View().Content; !strings.Contains(got, "/:filter") {
		t.Errorf("Logs footer missing filter: %q", got)
	}

	// Profiles tab: profile operations.
	nm, _ := m.Update(keyPress("2"))
	got := nm.View().Content
	for _, want := range []string{"a:activate", "u:refresh", "d:delete", "i:import"} {
		if !strings.Contains(got, want) {
			t.Errorf("Profiles footer missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/:filter") {
		t.Error("Profiles footer should not show log filter")
	}

	// Config tab: kernel selection.
	nm, _ = nm.Update(keyPress("3"))
	if got := nm.View().Content; !strings.Contains(got, "enter:run") {
		t.Errorf("Config footer missing enter:run:\n%s", got)
	}
}

// TestTabSwitch verifies the 2/3 keys switch tabs and the placeholder body
// shows for unimplemented tabs.
func TestTabSwitch(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))

	nm, _ := m.Update(keyPress("2"))
	if got := nm.View().Content; !strings.Contains(got, "Name") {
		t.Errorf("View after tab 2 = %q, want profiles table", got)
	}

	nm, _ = nm.Update(keyPress("1"))
	if got := nm.View().Content; !strings.Contains(got, "waiting for log stream") && !strings.Contains(got, "[1] Logs") {
		t.Errorf("View after tab 1 = %q, want log viewport", got)
	}
}

// TestWindowResize verifies the framed layout fills the whole window: title
// border on top, status bar visible, bottom border, and every line at full
// width.
func TestWindowResize(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))

	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	lines := strings.Split(strings.TrimRight(nm.View().Content, "\n"), "\n")
	if len(lines) != 24 {
		t.Errorf("layout = %d lines, want 24 (fills the window)", len(lines))
	}
	if !strings.HasPrefix(lines[0], "┌") || !strings.Contains(lines[0], "mihomo-tui") {
		t.Errorf("line 0 = %q, want top border with title", lines[0])
	}
	if !strings.Contains(lines[1], "unknown") {
		t.Errorf("line 1 = %q, want status bar", lines[1])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "└") {
		t.Errorf("last line = %q, want bottom border", lines[len(lines)-1])
	}
	for i, l := range lines {
		if lipgloss.Width(l) != 80 {
			t.Errorf("line %d width = %d, want 80", i, lipgloss.Width(l))
		}
	}
}

// TestNarrowScreen verifies a terminal under shared.NarrowWidth columns renders the
// bare log stream without the frame or tabs.
func TestNarrowScreen(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	nm, _ = nm.Update(shared.LogLineMsg{Line: "hello narrow"})

	got := nm.View().Content
	if strings.Contains(got, "┌") {
		t.Errorf("narrow view should skip the frame, got: %q", got)
	}
	if !strings.Contains(got, "hello narrow") {
		t.Errorf("narrow view missing log content: %q", got)
	}

	// Wide enough windows keep the frame.
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if got := nm.View().Content; !strings.Contains(got, "┌─ mihomo-tui") {
		t.Errorf("wide view should keep the frame: %q", got)
	}
}

// TestSmokeStartQuit starts the program against a dead endpoint and verifies
// it quits on "q" without hanging (1.17 acceptance).
func TestSmokeStartQuit(t *testing.T) {
	client := ctl.NewClient("http://127.0.0.1:1", "", false) // nothing listens here
	p := tea.NewProgram(New(client),
		tea.WithInput(strings.NewReader("q")),
		tea.WithOutput(io.Discard),
	)

	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		p.Kill()
		t.Fatal("program did not quit within 5s")
	}
}

// TestRenderStatus snapshots the status bar rendering for a running kernel.
func TestRenderStatus(t *testing.T) {
	// lipgloss disables colors when the output is not a TTY; force a color
	// profile so the ANSI assertion below is deterministic in CI.
	pid := 12345
	st := &supervisor.KernelState{
		Status:             supervisor.StatusRunning,
		PID:                &pid,
		Version:            "v1.19.29",
		ExternalController: "127.0.0.1:9090",
	}
	got := shared.RenderStatus(st, "http://127.0.0.1:9097")
	for _, want := range []string{"running", "12345", "v1.19.29", "127.0.0.1:9090"} {
		if !strings.Contains(got, want) {
			t.Errorf("shared.RenderStatus = %q, missing %q", got, want)
		}
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("shared.RenderStatus = %q, want ANSI color on status dot", got)
	}

	// nil state renders an unknown placeholder.
	if got := shared.RenderStatus(nil, "http://127.0.0.1:9097"); !strings.Contains(got, "unknown") {
		t.Errorf("shared.RenderStatus(nil) = %q, want unknown placeholder", got)
	}

	// Stopped kernel renders a grey dot and no pid.
	stopped := &supervisor.KernelState{Status: supervisor.StatusStopped}
	if got := shared.RenderStatus(stopped, "http://x"); !strings.Contains(got, "stopped") {
		t.Errorf("shared.RenderStatus(stopped) = %q, want stopped", got)
	}
}

// TestViewStatusBar drives the model with messages and checks the View shows
// the status bar once a kernel state has been loaded, plus the error line.
func TestViewStatusBar(t *testing.T) {

	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	pid := 42
	nm, _ := m.Update(shared.StatusLoadedMsg{State: supervisor.KernelState{
		Status:             supervisor.StatusRunning,
		PID:                &pid,
		Version:            "v1.19.29",
		ExternalController: "127.0.0.1:9090",
	}})
	got := nm.View().Content
	for _, want := range []string{"running", "42", "v1.19.29"} {
		if !strings.Contains(got, want) {
			t.Errorf("View = %q, missing %q", got, want)
		}
	}

	nm, _ = nm.Update(shared.StatusErrorMsg{Err: errors.New("connection refused")})
	if got := nm.View().Content; !strings.Contains(got, "connection refused") {
		t.Errorf("View with error = %q, want error message", got)
	}

	// A 401 shows the friendly auth hint instead of the raw error.
	nm, _ = nm.Update(shared.StatusErrorMsg{Err: ctl.ErrUnauthorized})
	if got := nm.View().Content; !strings.Contains(got, "认证失败") {
		t.Errorf("View with 401 = %q, want auth hint", got)
	}
}

// keyPress builds a tea.KeyPressMsg for a printable character, mirroring a
// user typing it. Text is set so msg.String() returns the character itself.
func keyPress(s string) tea.KeyPressMsg {
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}
