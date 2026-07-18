// Package sse is a tiny Server-Sent Events writer: it frames events as
// "data: <json>\n\n" over a long-lived HTTP/1.1 response and flushes after
// every push. It exists because the dashboard's /api/control/kernel/logs
// endpoint streams kernel log/state events; net/http doesn't ship an SSE
// helper, and pulling in a third-party dep for ~30 lines isn't worth it.
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Writer wraps an http.ResponseWriter in SSE framing. Call Push to send an
// event; Close (or letting the request handler return) to terminate.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
	closed  bool
}

// New prepares the response for streaming and returns a Writer. The caller
// must keep the handler goroutine alive until the client disconnects (use
// r.Context().Done()).
func New(w http.ResponseWriter) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("sse: response writer is not a flusher")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Disable proxy buffering (nginx etc.) so events reach the browser
	// immediately. The All-in-One server is the only proxy in front of us,
	// so these are belt-and-suspenders.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &Writer{w: w, flusher: flusher}, nil
}

// Push marshals payload as JSON and writes it as one SSE "data:" line. A nil
// or marshalling error is returned without writing anything.
func (sw *Writer) Push(payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.closed {
		return fmt.Errorf("sse: writer closed")
	}
	// SSE frames are "data: <text>\n\n". Splitting on embedded newlines keeps
	// each frame atomic — a multi-line payload becomes multiple "data:" lines
	// that the browser reassembles with \n joins. We just JSON-encode (which
	// has no raw newlines) so a single "data:" line suffices.
	if _, err := fmt.Fprintf(sw.w, "data: %s\n\n", b); err != nil {
		return err
	}
	sw.flusher.Flush()
	return nil
}

// Close marks the writer as closed so further Push calls are no-ops. The
// underlying response is finalized by net/http when the handler returns.
func (sw *Writer) Close() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.closed = true
}

// Await blocks until the request context is cancelled (client disconnect,
// server shutdown, or timeout). SSE handlers use this to keep the response
// open between event pushes; a disconnect unblocks the handler so its
// goroutine and any registered callbacks can be cleaned up.
func Await(ctx context.Context) {
	<-ctx.Done()
}
