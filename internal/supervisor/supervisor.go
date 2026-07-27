// Package supervisor manages the mihomo kernel subprocess: spawn, readiness
// poll, stop, and state broadcasting. It is a direct port of
// packages/agent/src/supervisor.ts in the upstream TS server, trimmed to
// Phase 1 scope (no validate, no auto-restart watchdog — those land in Phase 3).
//
// Lifecycle is serialized by lifeMu so two tabs cannot double-spawn. State
// changes are broadcast to registered callbacks (used by the SSE log handler).
package supervisor

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// KernelStatus mirrors the TS KernelStatus union. JSON-marshals as a string so
// the UI's endpoint store decodes it without any custom logic.
type KernelStatus string

const (
	StatusStopped  KernelStatus = "stopped"
	StatusStarting KernelStatus = "starting"
	StatusRunning  KernelStatus = "running"
	StatusStopping KernelStatus = "stopping"
	StatusErrored  KernelStatus = "errored"
)

// KernelState is the JSON shape served by GET /api/control/kernel/status. It
// matches the TS KernelState interface field-for-field so the dashboard's
// endpoint store reads it without translation.
type KernelState struct {
	Status             KernelStatus `json:"status"`
	PID                *int         `json:"pid,omitempty"`
	StartedAt          *int64       `json:"startedAt,omitempty"`
	Version            string       `json:"version,omitempty"`
	ExternalController string       `json:"externalController"`
	Secret             string       `json:"secret"`
	LastExitCode       *int         `json:"lastExitCode,omitempty"`
	LastError          string       `json:"lastError,omitempty"`
}

// KernelLogLine is one line emitted by mihomo on stdout/stderr.
type KernelLogLine struct {
	Stream string `json:"stream"` // "stdout" | "stderr"
	Line   string `json:"line"`
	TS     int64  `json:"ts"`
}

// LogCallback / StateCallback are invoked on each log line / state transition.
// OnLog returns an ID to pass to OffLog; the same pattern applies to OnState.
// Callbacks must not block — they run under the supervisor mutex.
type LogCallback func(line KernelLogLine)
type StateCallback func(state KernelState)

// Options configures a Supervisor. ExternalController/Secret default to
// "127.0.0.1:9090" / random hex when empty; callers from the server layer
// pass the env-derived values.
type Options struct {
	BinaryPath         string
	HomeDir            string
	ActiveConfigPath   string
	ExternalController string
	Secret             string
	MixedPort          int  // 0 = don't inject mixed-port into active.yaml
	StartTimeout       time.Duration
	StopTimeout        time.Duration

	// ValidateTimeout caps a single `mihomo -t` run. Default 5min — mihomo
	// synchronously downloads missing GEO data on a fresh homeDir and gives
	// each download up to 90s; a too-tight watchdog kills every first
	// validation that references GEOIP/fallback-filter (TS #2118, #2121).
	ValidateTimeout time.Duration

	// AutoRestart arms the crash watchdog: an unexpected proc exit (not a
	// user Stop) is retried after RestartBackoff, up to MaxRestarts
	// consecutive attempts. Default true.
	AutoRestart bool
	// MaxRestarts caps consecutive auto-restarts before the supervisor gives
	// up and leaves status=errored. Default 3.
	MaxRestarts int
	// RestartBackoff is the delay before each auto-restart attempt. Default 1s.
	RestartBackoff time.Duration
	// StableRestart is how long the kernel must stay running before the
	// consecutive-crash counter resets. Default 30s — a kernel that reaches
	// running and immediately dies still counts toward MaxRestarts.
	StableRestart time.Duration
}

// Supervisor manages one mihomo subprocess at a time. Concurrent Start/Stop/
// Restart calls are serialized; State() is always non-blocking.
type Supervisor struct {
	opts Options

	binaryPath string

	mu       sync.Mutex // protects state, child, callbacks, flags, timers
	state    KernelState
	child    *childProc
	intentionalStop bool

	// Crash watchdog bookkeeping. All guarded by mu so timer callbacks can
	// read/inspect them without racing lifecycle ops.
	restartCount int
	restartTimer *time.Timer
	stableTimer  *time.Timer

	logCbs   map[int]LogCallback
	stateCbs map[int]StateCallback
	nextCbID int

	lifeMu sync.Mutex // serializes Start/Stop/Restart
}

// childProc bundles an exec.Cmd with a completion channel so doStop can wait
// for the single watchExit goroutine that owns cmd.Wait().
type childProc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

// New constructs a Supervisor. Does not spawn anything — call Start.
func New(opts Options) *Supervisor {
	secret := opts.Secret
	if secret == "" {
		// Match the TS randomBytes(16).toString('hex') — 32 hex chars.
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		secret = hex.EncodeToString(b)
	}
	ec := opts.ExternalController
	if ec == "" {
		ec = "127.0.0.1:9090"
	}
	if opts.StartTimeout == 0 {
		opts.StartTimeout = 10 * time.Second
	}
	if opts.StopTimeout == 0 {
		opts.StopTimeout = 5 * time.Second
	}
	if opts.ValidateTimeout == 0 {
		opts.ValidateTimeout = 5 * time.Minute
	}
	// AutoRestart is a plain bool so tests can disable the watchdog by
	// passing false; the All-in-One server sets it true explicitly in
	// main.go. Zero-value MaxRestarts/RestartBackoff/StableRestart get useful
	// defaults because their zero values aren't meaningful.
	if opts.MaxRestarts == 0 {
		opts.MaxRestarts = 3
	}
	if opts.RestartBackoff == 0 {
		opts.RestartBackoff = 1 * time.Second
	}
	if opts.StableRestart == 0 {
		opts.StableRestart = 30 * time.Second
	}
	opts.ExternalController = ec
	opts.Secret = secret
	return &Supervisor{
		opts:      opts,
		binaryPath: opts.BinaryPath,
		state: KernelState{
			Status:             StatusStopped,
			ExternalController: ec,
			Secret:             secret,
		},
		logCbs:   make(map[int]LogCallback),
		stateCbs: make(map[int]StateCallback),
	}
}

// State returns a snapshot of the current kernel state. Safe for concurrent use.
func (s *Supervisor) State() KernelState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// SetBinaryPath swaps the kernel binary path; takes effect on the next Start.
// Mirrors the TS setBinaryPath.
func (s *Supervisor) SetBinaryPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binaryPath = path
}

// Start spawns the kernel (or returns the current state if already up). It is
// serialized with Stop/Restart via lifeMu.
func (s *Supervisor) Start() (KernelState, error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	s.mu.Lock()
	if s.state.Status == StatusRunning || s.state.Status == StatusStarting {
		st := s.state
		s.mu.Unlock()
		return st, nil
	}
	s.mu.Unlock()

	return s.doStart()
}

// Stop terminates the kernel: SIGTERM, wait StopTimeout, then SIGKILL.
func (s *Supervisor) Stop() (KernelState, error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	s.mu.Lock()
	s.intentionalStop = true
	s.restartCount = 0
	s.cancelRestartTimerLocked()
	s.cancelStabilityResetLocked()
	c := s.child
	if c == nil {
		s.state.Status = StatusStopped
		s.state.PID = nil
		snapshot := s.snapshotLocked()
		s.mu.Unlock()
		return snapshot, nil
	}
	s.state.Status = StatusStopping
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	_ = killProcGroup(c.cmd, syscall.SIGTERM)

	timer := time.NewTimer(s.opts.StopTimeout)
	defer timer.Stop()
	select {
	case <-c.done:
		// exited cleanly via SIGTERM
	case <-timer.C:
		// escalate
		_ = killProcGroup(c.cmd, syscall.SIGKILL)
		<-c.done
	}

	s.mu.Lock()
	s.child = nil
	s.state.Status = StatusStopped
	s.state.PID = nil
	snapshot = s.snapshotLocked()
	s.mu.Unlock()
	return snapshot, nil
}

// Restart is Stop followed by Start, both under lifeMu.
func (s *Supervisor) Restart() (KernelState, error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	if _, err := s.doStop(); err != nil {
		return s.State(), err
	}
	return s.doStart()
}

// Dispose stops the kernel and detaches all callbacks. Idempotent.
func (s *Supervisor) Dispose() error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	_, err := s.doStop()
	s.mu.Lock()
	// Tear down watchdog timers so a pending backoff can't respawn after
	// dispose (mirrors the TS dispose).
	s.cancelRestartTimerLocked()
	s.cancelStabilityResetLocked()
	s.logCbs = make(map[int]LogCallback)
	s.stateCbs = make(map[int]StateCallback)
	s.mu.Unlock()
	return err
}

// OnLog registers a log callback. Returns an ID for OffLog.
func (s *Supervisor) OnLog(cb LogCallback) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextCbID
	s.nextCbID++
	s.logCbs[id] = cb
	return id
}

// OffLog unregisters a log callback by ID.
func (s *Supervisor) OffLog(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.logCbs, id)
}

// OnState registers a state callback. Returns an ID for OffState.
func (s *Supervisor) OnState(cb StateCallback) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextCbID
	s.nextCbID++
	s.stateCbs[id] = cb
	return id
}

// OffState unregisters a state callback by ID.
func (s *Supervisor) OffState(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.stateCbs, id)
}

// --- internals ---

// doStart assumes lifeMu is held.
func (s *Supervisor) doStart() (KernelState, error) {
	s.mu.Lock()
	// A (re)start clears the intentional-stop flag so a later crash is detectable.
	s.intentionalStop = false
	s.state = KernelState{
		Status:             StatusStarting,
		ExternalController: s.opts.ExternalController,
		Secret:             s.opts.Secret,
	}
	snapshot := s.snapshotLocked()
	s.broadcastStateLocked(snapshot)
	s.mu.Unlock()

	if err := s.injectClashConfig(); err != nil {
		return s.failStart("inject config: " + err.Error())
	}

	bin := s.binaryPath
	cmd := exec.Command(bin, "-d", s.opts.HomeDir, "-f", s.opts.ActiveConfigPath)
	// Put mihomo in its own process group so we can tree-kill (SIGTERM/SIGKILL
	// the whole group) without orphaning any helper goroutines mihomo spawns.
	// Linux-only simplification per GO_SERVER_PLAN.md §2 (砍项: Windows tree-kill).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return s.failStart("stdout pipe: " + err.Error())
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return s.failStart("stderr pipe: " + err.Error())
	}
	if err := cmd.Start(); err != nil {
		return s.failStart("spawn: " + err.Error())
	}

	c := &childProc{cmd: cmd, done: make(chan struct{})}
	pid := cmd.Process.Pid
	now := time.Now().UnixMilli()

	s.mu.Lock()
	s.child = c
	s.state.PID = &pid
	s.state.StartedAt = &now
	snapshot = s.snapshotLocked()
	s.broadcastStateLocked(snapshot)
	s.mu.Unlock()

	go s.readStream(stdout, "stdout")
	go s.readStream(stderr, "stderr")
	go s.watchExit(c)

	ready := s.pollReady(time.Now().Add(s.opts.StartTimeout))

	s.mu.Lock()
	if !ready {
		// If the proc already exited and watchExit marked it errored, keep that.
		if s.state.Status != StatusErrored {
			s.state.Status = StatusErrored
			s.state.LastError = "ready timeout"
			snapshot = s.snapshotLocked()
			s.broadcastStateLocked(snapshot)
		} else {
			snapshot = s.state
		}
		s.mu.Unlock()
		// Best-effort cleanup of a process that never became ready.
		_ = killProcGroup(cmd, syscall.SIGKILL)
		<-c.done
	} else {
		snapshot = s.state
		s.mu.Unlock()
	}
	return snapshot, nil
}

// failStart transitions to errored and returns the snapshot + error.
func (s *Supervisor) failStart(msg string) (KernelState, error) {
	s.mu.Lock()
	s.state.Status = StatusErrored
	s.state.LastError = msg
	snapshot := s.snapshotLocked()
	s.broadcastStateLocked(snapshot)
	s.mu.Unlock()
	return snapshot, errors.New(msg)
}

// doStop assumes lifeMu is held. Separate from Stop so Restart can call it
// without re-acquiring lifeMu.
func (s *Supervisor) doStop() (KernelState, error) {
	s.mu.Lock()
	// Mark user-initiated so the proc 'exit' handler does not auto-restart,
	// and cancel any pending backoff restart a prior crash may have armed.
	s.intentionalStop = true
	s.restartCount = 0
	s.cancelRestartTimerLocked()
	s.cancelStabilityResetLocked()
	c := s.child
	if c == nil {
		s.state.Status = StatusStopped
		s.state.PID = nil
		snapshot := s.snapshotLocked()
		s.mu.Unlock()
		return snapshot, nil
	}
	s.state.Status = StatusStopping
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	_ = killProcGroup(c.cmd, syscall.SIGTERM)
	timer := time.NewTimer(s.opts.StopTimeout)
	defer timer.Stop()
	select {
	case <-c.done:
	case <-timer.C:
		_ = killProcGroup(c.cmd, syscall.SIGKILL)
		<-c.done
	}

	s.mu.Lock()
	s.child = nil
	s.state.Status = StatusStopped
	s.state.PID = nil
	snapshot = s.snapshotLocked()
	s.mu.Unlock()
	return snapshot, nil
}

// watchExit is the single owner of cmd.Wait(). It runs in its own goroutine
// per spawn and updates state when the process exits. An unexpected exit
// (not user-initiated) arms the auto-restart watchdog.
func (s *Supervisor) watchExit(c *childProc) {
	_ = c.cmd.Wait()
	var exitCode *int
	if ps := c.cmd.ProcessState; ps != nil {
		// -1 (signal kill) maps to nil so the JSON shape matches the TS union
		// (number | null); the dashboard treats both as "exited, no clean code".
		if code := ps.ExitCode(); code >= 0 {
			exitCode = &code
		}
	}

	needRestart := false
	s.mu.Lock()
	// Late exit from a superseded child — ignore. (Shouldn't happen since lifeMu
	// serializes lifecycle ops, but guard anyway.)
	if s.child != c {
		s.mu.Unlock()
		close(c.done)
		return
	}
		s.child = nil
	// This run is over: any stability timer armed for it must not fire later.
	s.cancelStabilityResetLocked()

	wasActive := s.state.Status == StatusStarting ||
		s.state.Status == StatusRunning ||
		s.state.Status == StatusStopping
	if wasActive {
		if s.intentionalStop {
			// User-initiated stop/restart: clean transition to stopped, no retry.
			s.state.Status = StatusStopped
		} else {
			// Unexpected crash: mark errored, then maybe arm the watchdog.
			s.state.Status = StatusErrored
			if s.opts.AutoRestart && s.restartCount < s.opts.MaxRestarts {
				s.restartCount++
				needRestart = true
			}
		}
	} else {
		s.state.Status = StatusStopped
	}
	s.state.LastExitCode = exitCode
	s.state.PID = nil
	snapshot := s.snapshotLocked()
	s.broadcastStateLocked(snapshot)
	s.mu.Unlock()

	// scheduleRestart sets up its own timer goroutine; doing it outside s.mu
	// avoids holding the lock across time.AfterFunc bookkeeping.
	if needRestart {
		s.scheduleRestart()
	}
	close(c.done)
}

// readStream pipes a mihomo stdout/stderr pipe into log callbacks, line by line.
func (s *Supervisor) readStream(r io.Reader, stream string) {
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			// mihomo logs are LF-terminated; trim the trailing newline to match
			// the TS split('\n') which yields lines without the separator.
			line = strings.TrimRight(line, "\n")
			if line != "" {
				ln := KernelLogLine{Stream: stream, Line: line, TS: time.Now().UnixMilli()}
				s.mu.Lock()
				for _, cb := range s.logCbs {
					cb(ln)
				}
				s.mu.Unlock()
			}
		}
		if err != nil {
			return
		}
	}
}

// pollReady GETs /version on the Clash API until it responds 200 or the
// deadline elapses. Rewrites wildcard bind hosts to 127.0.0.1 (client-side).
func (s *Supervisor) pollReady(deadline time.Time) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		s.mu.Lock()
		status := s.state.Status
		ec := s.state.ExternalController
		secret := s.state.Secret
		s.mu.Unlock()
		if status == StatusErrored {
			return false
		}

		req, err := http.NewRequest(http.MethodGet, versionURL(ec), nil)
		if err == nil {
			if secret != "" {
				req.Header.Set("Authorization", "Bearer "+secret)
			}
			resp, err := client.Do(req)
			if err == nil {
				if resp.StatusCode == http.StatusOK {
					var body struct {
						Version string `json:"version"`
					}
					_ = json.NewDecoder(resp.Body).Decode(&body)
					_ = resp.Body.Close()
					s.mu.Lock()
					s.state.Status = StatusRunning
					if body.Version != "" {
						s.state.Version = body.Version
					}
					snapshot := s.snapshotLocked()
					s.broadcastStateLocked(snapshot)
					s.mu.Unlock()
					// Reached running: arm the stability window. Only if it stays
					// running for StableRestart do we treat it as recovered and
					// clear the crash counter. A kernel that reaches running and
					// dies again immediately still counts toward MaxRestarts.
					s.armStabilityReset()
					return true
				}
				_ = resp.Body.Close()
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// versionURL builds http://<host>/version, rewriting wildcard bind hosts to
// 127.0.0.1 so the client socket is routable on every host stack.
func versionURL(ec string) string {
	addr := ec
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return addr + "/version"
	}
	switch u.Hostname() {
	case "0.0.0.0", "::", "":
		host := "127.0.0.1"
		if p := u.Port(); p != "" {
			host = host + ":" + p
		}
		u.Host = host
	}
	return u.Scheme + "://" + u.Host + "/version"
}

// injectClashConfig rewrites active.yaml in place: strip any user-authored
// external-controller/secret/mixed-port (and listener ports clashing with
// mixed-port), then prepend the supervisor-managed values. When MIXED_PORT
// env is non-zero, the supervisor locks mixed-port; when 0 (default),
// mixed-port is left untouched so the mihomo dashboard can edit it.
func (s *Supervisor) injectClashConfig() error {
	var existing string
	if b, err := os.ReadFile(s.opts.ActiveConfigPath); err == nil {
		existing = string(b)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	mixedPort := s.opts.MixedPort
	hasMixed := mixedPort > 0

	var kept strings.Builder
	if existing != "" {
		for _, line := range strings.Split(existing, "\n") {
			if shouldStripTopLevelKey(line, mixedPort, hasMixed) {
				continue
			}
			kept.WriteString(line)
			kept.WriteByte('\n')
		}
	}

	var header string
	if hasMixed {
		header = fmt.Sprintf(
			"external-controller: %s\nsecret: %s\nmixed-port: %d\n\n",
			s.opts.ExternalController, s.opts.Secret, mixedPort,
		)
	} else {
		header = fmt.Sprintf(
			"external-controller: %s\nsecret: %s\n\n",
			s.opts.ExternalController, s.opts.Secret,
		)
	}

	// 0644: mihomo only reads the file; no group/other write needed.
	return os.WriteFile(s.opts.ActiveConfigPath, []byte(header+kept.String()), 0o644)
}

// shouldStripTopLevelKey reproduces the TS shouldStripTopLevelKey: managed
// keys (external-controller, secret) are always stripped; mixed-port is
// stripped only when MIXED_PORT env is non-zero; listener-port keys are
// stripped only when mixed-port is active (to avoid double-binding).
func shouldStripTopLevelKey(line string, mixedPort int, hasMixed bool) bool {
	sep := strings.IndexByte(line, ':')
	if sep == -1 {
		return false
	}
	// trimEnd preserves leading indentation: nested keys with these names
	// (e.g. `  port:` under a `tun:` block) keep their leading spaces and so
	// never match.
	key := strings.TrimRight(line[:sep], " \t")
	switch key {
	case "external-controller", "secret":
		return true
	case "mixed-port":
		return hasMixed
	case "port", "socks-port", "redir-port", "tproxy-port":
		if !hasMixed {
			return false
		}
		// yaml.Unmarshal tolerates leading space + YAML scalar grammar; a
		// non-numeric value (string, inline map) returns an error and is left
		// untouched for mihomo's own validator to report.
		var v int
		if err := yaml.Unmarshal([]byte(line[sep+1:]), &v); err == nil {
			return v == mixedPort
		}
		return false
	}
	return false
}

// snapshotLocked returns a state copy. Caller must hold s.mu.
func (s *Supervisor) snapshotLocked() KernelState {
	return s.state
}

// broadcastStateLocked invokes every state callback. Caller must hold s.mu.
// Callbacks run inline; keep them cheap (the SSE handler just writes to a
// channel).
func (s *Supervisor) broadcastStateLocked(snapshot KernelState) {
	for _, cb := range s.stateCbs {
		cb(snapshot)
	}
}

// killProcGroup sends a signal to the whole process group of cmd. Falls back
// to a direct signal if Getpgid fails (e.g. process already reaped).
func killProcGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return cmd.Process.Signal(sig)
	}
	return syscall.Kill(-pgid, sig)
}

// --- auto-restart watchdog ---

// scheduleRestart arms the backoff timer, then (re)spawns via doStart under
// lifeMu when it fires. The timer is owned under s.mu so cancel can race the
// fire safely; the actual doStart runs without holding s.mu (doStart takes it
// briefly itself).
func (s *Supervisor) scheduleRestart() {
	s.mu.Lock()
	if s.restartTimer != nil {
		s.restartTimer.Stop()
	}
	s.restartTimer = time.AfterFunc(s.opts.RestartBackoff, func() {
		s.lifeMu.Lock()
		defer s.lifeMu.Unlock()
		s.mu.Lock()
		// Clear under the same lock so a concurrent doStop sees "no timer"
		// and a subsequent scheduleRestart starts a fresh one.
		s.restartTimer = nil
		// Bail if the user stopped us between the fire and the lock — doStart
		// would reset intentionalStop otherwise.
		if s.intentionalStop {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		_, _ = s.doStart()
	})
	s.mu.Unlock()
}

// cancelRestartTimerLocked stops the pending backoff. Caller must hold s.mu.
func (s *Supervisor) cancelRestartTimerLocked() {
	if s.restartTimer != nil {
		s.restartTimer.Stop()
		s.restartTimer = nil
	}
}

// armStabilityReset arms the stable-window timer. When it fires (without being
// cancelled by an exit), the consecutive-crash counter resets — a kernel that
// reached running and stayed up for StableRestart is considered recovered.
//
// Only armed when AutoRestart is on; a kernel run without the watchdog doesn't
// need stability bookkeeping.
func (s *Supervisor) armStabilityReset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opts.AutoRestart {
		return
	}
	s.cancelStabilityResetLocked()
	s.stableTimer = time.AfterFunc(s.opts.StableRestart, func() {
		s.mu.Lock()
		s.stableTimer = nil
		if s.state.Status == StatusRunning {
			s.restartCount = 0
		}
		s.mu.Unlock()
	})
}

// cancelStabilityResetLocked stops the pending stable-window timer. Caller
// must hold s.mu.
func (s *Supervisor) cancelStabilityResetLocked() {
	if s.stableTimer != nil {
		s.stableTimer.Stop()
		s.stableTimer = nil
	}
}

// --- validate ---

// ValidationResult mirrors the TS validate return shape: {valid, message}.
// message is the combined stdout+stderr the validator emitted (truncated for
// display by the caller if needed).
type ValidationResult struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message"`
}

// Validate runs `mihomo -t -d <homeDir> -f <configPath>` with the configured
// ValidateTimeout. The validator's combined stdout+stderr is captured as the
// message regardless of outcome so a failure surfaces a useful diagnostic
// instead of just exit code != 0.
//
// On timeout the process is SIGKILLed and given a bounded grace window to
// report exit; if even that elapses, we return the timeout verdict without
// hanging the caller (mirrors the TS VALIDATE_KILL_GRACE_MS=5s path).
//
// Validate does NOT serialize on lifeMu — it runs the binary in `-t` test
// mode and never affects the live kernel state. Multiple concurrent validates
// against different candidate files are fine.
func (s *Supervisor) Validate(configPath string) ValidationResult {
	bin := func() string {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.binaryPath
	}()

	// Combined output buffer, written from both pipes. bytes.Buffer is
	// concurrency-safe for our two-writer pattern only if we hold a lock; use a
	// mutex to be safe.
	var outMu sync.Mutex
	var out strings.Builder
	appendOut := func(b []byte) {
		outMu.Lock()
		out.Write(b)
		outMu.Unlock()
	}

	cmd := exec.Command(bin, "-t", "-d", s.opts.HomeDir, "-f", configPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ValidationResult{Valid: false, Message: "stdout pipe: " + err.Error()}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ValidationResult{Valid: false, Message: "stderr pipe: " + err.Error()}
	}
	if err := cmd.Start(); err != nil {
		return ValidationResult{Valid: false, Message: "spawn: " + err.Error()}
	}

	// Copy pipes into out, then wait for both to close. io.Copy returns once
	// the reader hits EOF, which happens when the writer side (the process)
	// exits — so joining these goroutines implies the process has finished
	// writing.
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})
	go func() { _, _ = io.Copy(&writerFunc{appendOut}, stdout); close(stdoutDone) }()
	go func() { _, _ = io.Copy(&writerFunc{appendOut}, stderr); close(stderrDone) }()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	timer := time.NewTimer(s.opts.ValidateTimeout)
	defer timer.Stop()

	var timedOut bool
	select {
	case err := <-waitCh:
		// Normal exit. Drain pipes so we capture any final output.
		<-stdoutDone
		<-stderrDone
		if err != nil {
			// ProcessState may be nil if Start failed earlier — but Start already
			// returned, so we have a ProcessState. ExitCode() returns -1 for
			// signal kills, which we surface as invalid (not a clean validate).
			return ValidationResult{Valid: false, Message: out.String()}
		}
		// Exit code 0 = config valid. mihomo's validator can return 0 with
		// warnings on stderr; the message carries them for the UI to show.
		return ValidationResult{Valid: true, Message: out.String()}
	case <-timer.C:
		timedOut = true
		// Fall through to the kill path. Set BEFORE sending the signal: a
		// platform wrapper might emit exit synchronously, and that exit must
		// still report timeout rather than turn SIGKILL into a verdict.
		_ = killProcGroup(cmd, syscall.SIGKILL)
	}

	// Bounded grace window for the kill to take effect. If the proc still
	// doesn't exit, give up and return the timeout verdict so the caller's
	// goroutine doesn't leak.
	grace := time.NewTimer(5 * time.Second)
	defer grace.Stop()
	select {
	case <-waitCh:
		<-stdoutDone
		<-stderrDone
	case <-grace.C:
		// Process didn't exit after SIGKILL; leak it (the OS will reap when it
		// eventually dies) and return the timeout verdict.
	}

	if timedOut {
		msg := out.String()
		return ValidationResult{
			Valid:   false,
			Message: fmt.Sprintf("validate timeout after %s\n%s", s.opts.ValidateTimeout, msg),
		}
	}
	// Unreachable in practice (we only get here via the timer.C branch).
	return ValidationResult{Valid: false, Message: out.String()}
}

// writerFunc adapts an append func into an io.Writer so io.Copy can stream
// into our mutex-guarded buffer.
type writerFunc struct {
	append func([]byte)
}

func (w *writerFunc) Write(p []byte) (int, error) {
	// Copy p so the underlying slice isn't aliased to io.Copy's reuse buffer.
	dup := make([]byte, len(p))
	copy(dup, p)
	w.append(dup)
	return len(p), nil
}
