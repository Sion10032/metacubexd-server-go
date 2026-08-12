package ctl

import (
	"bufio"
	"reflect"
	"strings"
	"testing"
)

// TestSSEParse covers the SSE framing edge cases: single/multi-line data,
// blank-line separation, comments, event type and unterminated final events.
func TestSSEParse(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		events []Event
	}{
		{
			name:   "single data line",
			input:  "data: hello\n\n",
			events: []Event{{Data: "hello"}},
		},
		{
			name:   "multi-line data joined with newline",
			input:  "data: line1\ndata: line2\n\n",
			events: []Event{{Data: "line1\nline2"}},
		},
		{
			name:   "blank line separation",
			input:  "data: a\n\n\n\ndata: b\n\n",
			events: []Event{{Data: "a"}, {Data: "b"}},
		},
		{
			name:   "comment lines ignored",
			input:  ": keep-alive\ndata: a\n\n",
			events: []Event{{Data: "a"}},
		},
		{
			name:   "event type",
			input:  "event: log\ndata: x\n\n",
			events: []Event{{Type: "log", Data: "x"}},
		},
		{
			name:   "no trailing blank line",
			input:  "data: a\ndata: b",
			events: []Event{{Data: "a\nb"}},
		},
		{
			name:   "no space after colon",
			input:  "data:hello\n\nevent:state\ndata:{\"a\":1}\n\n",
			events: []Event{{Data: "hello"}, {Type: "state", Data: "{\"a\":1}"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []Event
			parseStream(bufio.NewReader(strings.NewReader(tc.input)), func(ev Event) {
				got = append(got, ev)
			})
			if !reflect.DeepEqual(got, tc.events) {
				t.Errorf("events = %+v, want %+v", got, tc.events)
			}
		})
	}
}
