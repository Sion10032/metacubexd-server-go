package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Event is one parsed SSE event. Type comes from an "event:" field (empty
// when absent); Data holds the "data:" field values.
type Event struct {
	Type string
	Data string
}

// parseStream reads a text/event-stream from r, invoking emit for each
// complete event (terminated by a blank line). Per the SSE spec: "data:"
// fields are joined with "\n", a single space after the colon is dropped,
// lines starting with ":" are comments, and an event not terminated by a
// trailing blank line is still emitted on EOF.
func parseStream(r *bufio.Reader, emit func(Event)) {
	var (
		typ  string
		data []string
	)
	flush := func() {
		if len(data) > 0 {
			emit(Event{Type: typ, Data: strings.Join(data, "\n")})
		}
		typ = ""
		data = nil
	}

	for {
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")

		switch {
		case line == "":
			flush()
		case line[0] == ':':
			// comment — ignore
		case strings.HasPrefix(line, "data:"):
			data = append(data, fieldValue(line[len("data:"):]))
		case strings.HasPrefix(line, "event:"):
			typ = fieldValue(line[len("event:"):])
		}

		if err != nil {
			// EOF: emit any event not terminated by a blank line.
			if err == io.EOF && len(data) > 0 {
				emit(Event{Type: typ, Data: strings.Join(data, "\n")})
			}
			return
		}
	}
}

// fieldValue drops a single leading space from an SSE field value.
func fieldValue(v string) string {
	if strings.HasPrefix(v, " ") {
		return v[1:]
	}
	return v
}

// SubscribeLogs opens a streaming SSE connection to /api/control/kernel/logs
// and returns a channel of raw events. The goroutine exits when ctx is
// cancelled or the stream ends, closing the channel; events carry the JSON
// payload in Data (Type is only set when the server sends "event:" lines).
func (c *Client) SubscribeLogs(ctx context.Context) (<-chan Event, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/api/control/kernel/logs", nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "text/event-stream")

	// The shared hc carries a 30s timeout that would kill a long-lived
	// stream; subscribe with its transport but no overall deadline.
	streamClient := &http.Client{Transport: c.hc.Transport}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, fmt.Errorf("subscribe logs: %s", resp.Status)
	}

	ch := make(chan Event)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		parseStream(bufio.NewReader(resp.Body), func(ev Event) {
			select {
			case ch <- ev:
			case <-ctx.Done():
			}
		})
	}()
	return ch, nil
}
