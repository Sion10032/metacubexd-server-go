// Package scheduler drives subscription auto-update: on every tick (60s by
// default) it refreshes every remote profile whose updateInterval has elapsed,
// and when a refresh hits the ACTIVE base it re-composes active.yaml +
// restarts the kernel so the new subscription takes effect immediately
// (instead of waiting for the user to click "activate" manually).
//
// Direct port of packages/agent/src/scheduler.ts + refresh-apply.ts.
package scheduler

import (
	"sync"
	"sync/atomic"
	"time"

	"metacubexd-server-go/internal/profile"
	"metacubexd-server-go/internal/supervisor"
)

// Options configures the scheduler. ProfileStore + Supervisor are required.
type Options struct {
	Profiles *profile.Store
	Supervisor *supervisor.Supervisor
	// Tick defaults to 60s. Tests inject a smaller value.
	Tick time.Duration
	// Now returns Unix-millis; tests inject a fake clock so they can drive
	// ticks deterministically (mirrors the TS now dep).
	Now func() int64
}

// Scheduler arm/disarm is idempotent. At most one tick runs at a time.
type Scheduler struct {
	profiles *profile.Store
	sup      *supervisor.Supervisor
	tick     time.Duration
	now      func() int64

	mu      sync.Mutex
	running atomic.Bool // separate from mu so tick() can read it without holding the lock
	timer   *time.Timer
}

// New constructs a Scheduler. Does not start until Start is called.
func New(opts Options) *Scheduler {
	if opts.Tick == 0 {
		opts.Tick = 60 * time.Second
	}
	if opts.Now == nil {
		opts.Now = func() int64 { return time.Now().UnixMilli() }
	}
	return &Scheduler{
		profiles: opts.Profiles,
		sup:      opts.Supervisor,
		tick:     opts.Tick,
		now:      opts.Now,
	}
}

// Start arms the timer if not already running. Idempotent.
func (s *Scheduler) Start() {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	s.arm()
}

// Stop cancels the pending tick and waits for an in-flight tickOnce to finish
// (it holds s.mu). Idempotent.
func (s *Scheduler) Stop() {
	if !s.running.CompareAndSwap(true, false) {
		return
	}
	s.mu.Lock()
	defer s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
}

// arm schedules the next tick. Caller must hold s.mu.
func (s *Scheduler) arm() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running.Load() {
		return
	}
	// time.AfterFunc fires in its own goroutine — we don't need to spawn one.
	// The callback runs tickOnce (which locks s.mu itself, but AfterFunc has
	// already released the caller's lock by the time it fires), then arms the
	// next tick on success/failure alike so the loop is self-perpetuating.
	s.timer = time.AfterFunc(s.tick, func() {
		s.tickOnce()
		s.arm()
	})
}

// tickOnce refreshes every remote profile whose updateInterval has elapsed,
// best-effort: a failing refresh must not abort the rest of the loop, and the
// active re-compose+restart is fire-and-forget so a kernel hiccup can't wedge
// the scheduler.
func (s *Scheduler) tickOnce() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running.Load() {
		return
	}
	now := s.now()
	for _, m := range s.profiles.List() {
		if m.Type != profile.TypeRemote {
			continue
		}
		// updateInterval is *int (pointer so we can distinguish "unset" from
		// "0 = disabled"). Nil or <=0 means auto-update is off.
		if m.UpdateInterval == nil || *m.UpdateInterval <= 0 {
			continue
		}
		// interval is in minutes; updatedAt in millis. Multiply once, compare once.
		deadline := int64(*m.UpdateInterval) * 60 * 1000
		if now-m.UpdatedAt < deadline {
			continue
		}
		// Refresh the profile in storage. A failing refresh is swallowed —
		// next tick retries. We do NOT trigger the active re-compose on failure
		// (matches the TS onResult guard).
		if _, err := s.profiles.Refresh(m.ID); err != nil {
			continue
		}
		// If this profile is the active base, re-compose + restart so the new
		// subscription actually takes effect (#2107). Best-effort: a failing
		// apply must not wedge the tick loop; the next tick retries.
		if s.profiles.GetActiveID() == m.ID {
			applyActiveRefresh(s.profiles, s.sup, m.ID)
		}
	}
}

// applyActiveRefresh re-activates the profile (which re-composes active.yaml)
// and restarts the kernel so the refreshed subscription routes traffic. A
// no-op when id isn't the active one — guards against a race where the user
// switched profiles between Refresh and this call.
func applyActiveRefresh(p *profile.Store, sup *supervisor.Supervisor, id string) {
	if p.GetActiveID() != id {
		return
	}
	if err := p.SetActive(id); err != nil {
		return
	}
	_, _ = sup.Restart()
}
