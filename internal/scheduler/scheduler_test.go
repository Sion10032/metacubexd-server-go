package scheduler

import (
	"testing"
	"time"
)

// TestStopReturnsInTime is the critical regression test for the defer Lock/Unlock
// typo that once deadlocked Stop(), hanging Ctrl-C indefinitely.
func TestStopReturnsInTime(t *testing.T) {
	s := New(Options{Tick: time.Hour})
	s.Start()

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop() deadlocked (regression: defer Lock/Unlock typo)")
	}
}

// TestStopIdempotent verifies calling Stop() twice doesn't panic or block.
func TestStopIdempotent(t *testing.T) {
	s := New(Options{Tick: time.Hour})
	s.Start()
	s.Stop()

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second Stop() blocked — must be idempotent")
	}
}

// TestStartIdempotent verifies calling Start() twice doesn't double-arm.
func TestStartIdempotent(t *testing.T) {
	s := New(Options{Tick: time.Hour})
	s.Start()
	s.Start()
	defer s.Stop()
}

// TestStartStopCycle verifies repeated Start/Stop cycling works.
func TestStartStopCycle(t *testing.T) {
	s := New(Options{Tick: time.Hour})
	for i := 0; i < 5; i++ {
		s.Start()
		s.Stop()
	}
}

// TestNewDefaultsTickTo60s verifies the default tick duration.
func TestNewDefaultsTickTo60s(t *testing.T) {
	s := New(Options{})
	if s.tick != 60*time.Second {
		t.Errorf("default tick = %v, want 60s", s.tick)
	}
}

// TestNewCustomTick verifies custom tick is honoured.
func TestNewCustomTick(t *testing.T) {
	s := New(Options{Tick: 5 * time.Second})
	if s.tick != 5*time.Second {
		t.Errorf("tick = %v, want 5s", s.tick)
	}
}

// TestNewDefaultsNow verifies a non-nil Now function is injected.
func TestNewDefaultsNow(t *testing.T) {
	s := New(Options{})
	if s.now == nil {
		t.Fatal("Now function not injected")
	}
	if s.now() == 0 {
		t.Error("Now() returned 0")
	}
}
