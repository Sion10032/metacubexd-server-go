package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/shared"
)

// TestSSEToView runs the real subscribe → event pump → model chain against a
// fake server streaming SSE log events, verifying the lines reach the model
// and the subscription is torn down cleanly.
func TestSSEToView(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/control/kernel/status":
			fmt.Fprint(w, `{"status":"running","pid":42,"version":"v1.19.29","externalController":"127.0.0.1:9090"}`)
		case "/api/control/kernel/logs":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			for i := 0; i < 3; i++ {
				fmt.Fprintf(w, "data: {\"type\":\"log\",\"stream\":\"stdout\",\"line\":\"line %d\",\"ts\":%d}\n\n",
					i, time.Now().UnixMilli())
				flusher.Flush()
			}
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	client := ctl.NewClient(srv.URL, "", false)
	m := New(client)

	// Subscribe over real HTTP.
	msg := shared.Subscribe(client)()
	sm, ok := msg.(shared.SubscribedMsg)
	if !ok {
		t.Fatalf("Subscribe returned %T, want shared.SubscribedMsg", msg)
	}
	defer sm.Cancel()

	// Simulate the window size arriving before events (as in a real terminal).
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Pump events through the model until all three log lines arrive.
	nm, cmd := nm.Update(sm)
	var got []string
	for i := 0; i < 10 && len(got) < 3; i++ {
		msg = cmd()
		switch mm := msg.(type) {
		case shared.LogLineMsg:
			nm, cmd = nm.Update(mm)
			got = append(got, shared.StripANSI(mm.Line))
		case shared.KernelStateMsg:
			nm, cmd = nm.Update(mm)
		default:
			nm, cmd = nm.Update(msg)
		}
	}
	if len(got) != 3 {
		t.Fatalf("received %d log lines %v, want 3", len(got), got)
	}
	for i, want := range []string{"line 0", "line 1", "line 2"} {
		if !strings.Contains(got[i], want) {
			t.Errorf("log[%d] = %q, missing %q", i, got[i], want)
		}
	}

	// The view renders the received lines, scrolled to the bottom.
	if v := nm.View().Content; !strings.Contains(v, "line 2") {
		t.Errorf("View missing last line:\n%s", v)
	}

	// Cancelling closes the stream: the next pump yields shared.LogClosedMsg.
	sm.Cancel()
	msg = cmd()
	if _, ok := msg.(shared.LogClosedMsg); !ok {
		t.Errorf("after cancel, pump = %T, want shared.LogClosedMsg", msg)
	}
}
