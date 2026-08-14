package sse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// flushRecorder is an httptest.ResponseRecorder that also implements http.Flusher.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushCount int
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (f *flushRecorder) Flush() { f.flushCount++ }

// Verify flushRecorder satisfies http.Flusher at compile time.
var _ http.Flusher = (*flushRecorder)(nil)

func TestPushWritesSSEFrame(t *testing.T) {
	w := newFlushRecorder()
	sw, err := New(w)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sw.Close()

	if err := sw.Push(map[string]string{"event": "state", "status": "running"}); err != nil {
		t.Fatalf("Push: %v", err)
	}

	body := w.Body.String()
	if !strings.HasPrefix(body, "data: ") {
		t.Errorf("body should start with 'data: ', got: %q", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("body should end with double newline, got: %q", body)
	}
	if !strings.Contains(body, `"status":"running"`) {
		t.Errorf("JSON payload missing in body: %q", body)
	}
}

func TestPushFlushesImmediately(t *testing.T) {
	w := newFlushRecorder()
	sw, err := New(w)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sw.Close()

	if w.flushCount != 1 {
		t.Fatalf("flushCount before Push = %d, want 1 (New calls Flush once)", w.flushCount)
	}
	sw.Push("ping")
	if w.flushCount != 2 {
		t.Errorf("flushCount after Push = %d, want 2", w.flushCount)
	}
}

func TestPushReturnsErrorOnClosedWriter(t *testing.T) {
	w := newFlushRecorder()
	sw, err := New(w)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sw.Close()

	if err := sw.Push("should-fail"); err == nil {
		t.Fatal("expected error from Push after Close")
	}
}

func TestPushMultipleTimes(t *testing.T) {
	w := newFlushRecorder()
	sw, err := New(w)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sw.Close()

	for i := 0; i < 5; i++ {
		if err := sw.Push(i); err != nil {
			t.Fatalf("Push #%d: %v", i, err)
		}
	}
	// Each push = 1 Flush (plus 1 from New).
	if w.flushCount != 6 {
		t.Errorf("flushCount = %d, want 6 (1 from New + 5 from Push)", w.flushCount)
	}
	frames := strings.Split(strings.TrimSpace(w.Body.String()), "\n\n")
	if len(frames) != 5 {
		t.Errorf("got %d frames, want 5", len(frames))
	}
}

func TestPushConcurrent(t *testing.T) {
	w := newFlushRecorder()
	sw, err := New(w)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sw.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sw.Push(n)
		}(i)
	}
	wg.Wait()
	if w.flushCount != 21 {
		t.Errorf("flushCount = %d, want 21", w.flushCount)
	}
}

func TestCloseIdempotent(t *testing.T) {
	w := newFlushRecorder()
	sw, err := New(w)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sw.Close()
	sw.Close()
	sw.Close()
}

func TestNewSetsHeaders(t *testing.T) {
	w := newFlushRecorder()
	sw, err := New(w)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sw.Close()

	h := w.Header()
	if ct := h.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := h.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	if conn := h.Get("Connection"); conn != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", conn)
	}
	if ab := h.Get("X-Accel-Buffering"); ab != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", ab)
	}
}

func TestAwaitReturnsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Await(ctx)
		close(done)
	}()

	// Not yet cancelled — goroutine should be blocked.
	select {
	case <-done:
		t.Fatal("Await returned before context was cancelled")
	default:
	}

	cancel()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Await did not return after context cancellation")
	}
}
