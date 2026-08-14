package shared

import (
	"strings"
	"testing"
	"time"

	"metacubexd-server-go/internal/tui/client"
	"metacubexd-server-go/internal/api"
)

// TestFormatLogLine verifies the log line rendering: timestamp, INFO level
// and the ERROR style for stderr.
func TestFormatLogLine(t *testing.T) {
	ts := time.Date(2025, 8, 12, 15, 30, 1, 0, time.Local).UnixMilli()

	got := FormatLogLine(api.KernelLogLine{Stream: "stdout", Line: "inbound mixed port 7890 listening", TS: ts})
	want := "2025-08-12 15:30:01 INFO   inbound mixed port 7890 listening"
	if StripANSI(got) != want {
		t.Errorf("FormatLogLine = %q, want %q", StripANSI(got), want)
	}

	got = FormatLogLine(api.KernelLogLine{Stream: "stderr", Line: "boom", TS: ts})
	if !strings.Contains(got, "ERROR") || !strings.Contains(StripANSI(got), "boom") {
		t.Errorf("FormatLogLine(stderr) = %q, want ERROR + line", StripANSI(got))
	}
}

// TestParseLogEvent decodes SSE payloads into log/state messages.
func TestParseLogEvent(t *testing.T) {
	msg := ParseLogEvent(client.Event{Data: `{"type":"log","stream":"stdout","line":"hello","ts":1723469401000}`})
	lm, ok := msg.(LogLineMsg)
	if !ok {
		t.Fatalf("ParseLogEvent(log) = %T, want LogLineMsg", msg)
	}
	if !strings.Contains(StripANSI(lm.Line), "hello") {
		t.Errorf("LogLineMsg.Line = %q, want hello", lm.Line)
	}

	msg = ParseLogEvent(client.Event{Data: `{"type":"state","status":"running","pid":42}`})
	sm, ok := msg.(KernelStateMsg)
	if !ok {
		t.Fatalf("ParseLogEvent(state) = %T, want KernelStateMsg", msg)
	}
	if sm.State.Status != api.StatusRunning {
		t.Errorf("KernelStateMsg status = %q, want running", sm.State.Status)
	}

	if ParseLogEvent(client.Event{Data: `{"type":"nope"}`}) != nil {
		t.Error("unknown event type should be ignored")
	}
	if ParseLogEvent(client.Event{Data: `{bad json`}) != nil {
		t.Error("malformed JSON should be ignored")
	}
}
