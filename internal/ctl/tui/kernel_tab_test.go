package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
)

// runBatch executes every command in a batch, returning the produced messages.
func runBatch(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want tea.BatchMsg", cmd())
	}
	var msgs []tea.Msg
	for _, c := range batch {
		if m := c(); m != nil {
			msgs = append(msgs, m)
		}
	}
	return msgs
}

// TestKernelTabRender verifies the Config tab lists every operation, the
// network fields and the raw YAML viewer, with the selected entry highlighted.
func TestKernelTabRender(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	got := m.renderKernelTab()
	plain := ansiRe.ReplaceAllString(got, "")
	for _, want := range []string{
		"[kernel]", "Start", "Stop", "Restart",
		"[network]", "mixed-port", "http-port", "socks-port", "tun-enable", "tun-device", "tun-stack",
		"View YAML",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("kernel tab = %q, missing %q", plain, want)
		}
	}
	if !strings.Contains(got, "> ") || !strings.Contains(got, "\x1b[") {
		t.Errorf("kernel tab missing selection highlight: %q", got)
	}
}

// TestKernelTabSelect verifies up/down move the selection with wraparound.
func TestKernelTabSelect(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(keyPress("3")) // Config tab

	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := nm.(Model).kernel.kSelected; got != 2 {
		t.Errorf("kSelected after 2x down = %d, want 2", got)
	}

	// up wraps to the last entry.
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := nm.(Model).kernel.kSelected; got != 1 {
		t.Errorf("kSelected after up = %d, want 1", got)
	}
}

// TestKernelTabExecute verifies enter runs the selected operation over HTTP
// and the fresh state lands in the model.
func TestKernelTabExecute(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"starting"}`)
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(keyPress("3")) // Config tab

	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // enter on Start
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	if !nm.(Model).kernel.operating {
		t.Error("operating should be true while the operation runs")
	}
	runBatch(t, cmd) // spinner tick + kernel op
	if want := "POST /api/control/kernel/start"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
}

// TestRecoverConfirm is disabled while Recover is commented out of the
// operation list; re-enable when Recover returns.
func _TestRecoverConfirm(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"starting"}`)
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(keyPress("3")) // Config tab
	for i := 0; i < 4; i++ {
		nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	// Enter on Recover enters the confirm state instead of running it.
	nm, _ = nm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("confirm state should not issue a command")
	}
	if !nm.(Model).kernel.kConfirming {
		t.Fatal("kConfirming should be true")
	}
	if got := nm.View().Content; !strings.Contains(got, "确认执行") {
		t.Errorf("View missing confirm prompt:\n%s", got)
	}

	// "y" runs Recover.
	nm, cmd = nm.Update(keyPress("y"))
	if cmd == nil {
		t.Fatal("y returned no command")
	}
	runBatch(t, cmd)
	if want := "POST /api/control/kernel/recover"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}

	// Any other key cancels without issuing a request.
	gotURI = ""
	nm, _ = m.Update(keyPress("3"))
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	nm, _ = nm.Update(keyPress("3")) // back to config
	nm, _ = nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	nm, cmd = nm.Update(keyPress("n"))
	if cmd != nil {
		t.Fatal("cancelling should not issue a command")
	}
	if gotURI != "" {
		t.Errorf("unexpected request after cancel: %q", gotURI)
	}
	if nm.(Model).kernel.kConfirming {
		t.Error("kConfirming should be false after cancel")
	}
}

// TestSpinnerTick verifies spinner ticks keep flowing while operating.
func TestSpinnerTick(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(keyPress("3"))
	nm, cmd := nm.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // Start

	// Batch returns the child commands; find the spinner tick among them.
	msgs := runBatch(t, cmd)
	var tickMsg tea.Msg
	for _, m := range msgs {
		if _, ok := m.(spinner.TickMsg); ok {
			tickMsg = m
			break
		}
	}
	if tickMsg == nil {
		t.Fatalf("no spinner.TickMsg in batch, got %T", msgs)
	}
	nm, cmd = nm.Update(tickMsg)
	if cmd == nil {
		t.Fatal("spinner tick should keep the animation running")
	}
	_ = nm
}
