package tui

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
)

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
