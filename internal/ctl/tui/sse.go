package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/supervisor"
)

// subscribeCmd opens the SSE log subscription and hands the stream to the
// model via subscribedMsg.
func subscribeCmd(c *ctl.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := c.SubscribeLogs(ctx)
		if err != nil {
			cancel()
			return statusErrorMsg{err: err}
		}
		return subscribedMsg{ch: ch, cancel: cancel}
	}
}

// forwardEventsCmd pumps one event from the stream into the message loop,
// re-arming itself so the stream keeps flowing.
func forwardEventsCmd(ch <-chan ctl.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return logClosedMsg{}
		}
		if msg := parseLogEvent(ev); msg != nil {
			return msg
		}
		return forwardEventsCmd(ch)()
	}
}

// parseLogEvent decodes an SSE payload into a logMsg or stateMsg, or nil for
// unknown event types.
func parseLogEvent(ev ctl.Event) tea.Msg {
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(ev.Data), &header); err != nil {
		return nil
	}
	switch header.Type {
	case "log":
		var l supervisor.KernelLogLine
		if err := json.Unmarshal([]byte(ev.Data), &l); err != nil {
			return nil
		}
		return logMsg{line: formatLogLine(l)}
	case "state":
		var st supervisor.KernelState
		if err := json.Unmarshal([]byte(ev.Data), &st); err != nil {
			return nil
		}
		return stateMsg{state: st}
	}
	return nil
}

// formatLogLine renders a kernel log line as "2006-01-02 15:04:05 LEVEL  line".
func formatLogLine(l supervisor.KernelLogLine) string {
	level := "INFO "
	if l.Stream == "stderr" {
		level = errorStyle.Render("ERROR")
	}
	ts := time.UnixMilli(l.TS).Format("2006-01-02 15:04:05")
	return fmt.Sprintf("%s %s  %s", ts, level, l.Line)
}
