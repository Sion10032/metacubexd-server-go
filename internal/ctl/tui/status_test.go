package tui

import (
	"errors"
	"strings"
	"testing"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/supervisor"
)

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
	got := renderStatus(st, "http://127.0.0.1:9097")
	for _, want := range []string{"running", "12345", "v1.19.29", "127.0.0.1:9090"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderStatus = %q, missing %q", got, want)
		}
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("renderStatus = %q, want ANSI color on status dot", got)
	}

	// nil state renders an unknown placeholder.
	if got := renderStatus(nil, "http://127.0.0.1:9097"); !strings.Contains(got, "unknown") {
		t.Errorf("renderStatus(nil) = %q, want unknown placeholder", got)
	}

	// Stopped kernel renders a grey dot and no pid.
	stopped := &supervisor.KernelState{Status: supervisor.StatusStopped}
	if got := renderStatus(stopped, "http://x"); !strings.Contains(got, "stopped") {
		t.Errorf("renderStatus(stopped) = %q, want stopped", got)
	}
}

// TestViewStatusBar drives the model with messages and checks the View shows
// the status bar once a kernel state has been loaded, plus the error line.
func TestViewStatusBar(t *testing.T) {

	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	pid := 42
	nm, _ := m.Update(statusLoadedMsg{state: supervisor.KernelState{
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

	nm, _ = nm.Update(statusErrorMsg{err: errors.New("connection refused")})
	if got := nm.View().Content; !strings.Contains(got, "connection refused") {
		t.Errorf("View with error = %q, want error message", got)
	}

	// A 401 shows the friendly auth hint instead of the raw error.
	nm, _ = nm.Update(statusErrorMsg{err: ctl.ErrUnauthorized})
	if got := nm.View().Content; !strings.Contains(got, "认证失败") {
		t.Errorf("View with 401 = %q, want auth hint", got)
	}
}
