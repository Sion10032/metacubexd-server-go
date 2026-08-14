package supervisor

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"metacubexd-server-go/internal/api"
)

var fakeMihomoPath string

func buildFakeMihomo(t *testing.T) string {
	t.Helper()
	if fakeMihomoPath != "" {
		if _, err := os.Stat(fakeMihomoPath); err == nil {
			return fakeMihomoPath
		}
	}
	bin := filepath.Join(t.TempDir(), "fake-mihomo")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/fake-mihomo/")
	cmd.Dir = filepath.Join(getModuleRoot(), "internal", "server", "supervisor")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake-mihomo: %v\n%s", err, out)
	}
	fakeMihomoPath = bin
	return bin
}

func getModuleRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func testOpts(t *testing.T, bin string) Options {
	t.Helper()
	dir := t.TempDir()
	port := freePort(t)
	return Options{
		BinaryPath:         bin,
		HomeDir:            dir,
		ActiveConfigPath:   filepath.Join(dir, "active.yaml"),
		ExternalController: "127.0.0.1:" + itoa(port),
		Secret:             "test-secret",
		MixedPort:          7890,
		StartTimeout:       5 * time.Second,
		StopTimeout:        2 * time.Second,
		ValidateTimeout:    5 * time.Second,
		AutoRestart:        true,
		MaxRestarts:        3,
		RestartBackoff:     100 * time.Millisecond,
		StableRestart:      500 * time.Millisecond,
	}
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
}

func TestStartStop(t *testing.T) {
	bin := buildFakeMihomo(t)
	opts := testOpts(t, bin)
	writeConfig(t, opts.ActiveConfigPath, "mixed-port: 7890\n")
	sup := New(opts)
	defer sup.Dispose()

	state, err := sup.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if state.Status != api.StatusRunning {
		t.Errorf("status = %q, want %q", state.Status, api.StatusRunning)
	}
	if state.Version != "test-v1.0.0" {
		t.Errorf("version = %q, want test-v1.0.0", state.Version)
	}
	state, err = sup.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if state.Status != api.StatusStopped {
		t.Errorf("status after Stop = %q, want %q", state.Status, api.StatusStopped)
	}
}

func TestStartIdempotent(t *testing.T) {
	bin := buildFakeMihomo(t)
	opts := testOpts(t, bin)
	writeConfig(t, opts.ActiveConfigPath, "mixed-port: 7890\n")
	sup := New(opts)
	defer sup.Dispose()

	s1, _ := sup.Start()
	s2, _ := sup.Start()
	if s1.PID == nil || s2.PID == nil {
		t.Fatal("PID is nil")
	}
	if *s1.PID != *s2.PID {
		t.Errorf("PID changed: %d vs %d", *s1.PID, *s2.PID)
	}
}

func TestStopIdempotent(t *testing.T) {
	bin := buildFakeMihomo(t)
	opts := testOpts(t, bin)
	writeConfig(t, opts.ActiveConfigPath, "mixed-port: 7890\n")
	sup := New(opts)
	defer sup.Dispose()

	sup.Start()
	sup.Stop()
	state, err := sup.Stop()
	if err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if state.Status != api.StatusStopped {
		t.Errorf("status = %q, want %q", state.Status, api.StatusStopped)
	}
}

func TestRestart(t *testing.T) {
	bin := buildFakeMihomo(t)
	opts := testOpts(t, bin)
	writeConfig(t, opts.ActiveConfigPath, "mixed-port: 7890\n")
	sup := New(opts)
	defer sup.Dispose()

	sup.Start()
	oldPID := sup.State().PID
	state, err := sup.Restart()
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if state.Status != api.StatusRunning {
		t.Errorf("status after Restart = %q, want %q", state.Status, api.StatusRunning)
	}
	if state.PID == nil {
		t.Fatal("PID nil after Restart")
	}
	if oldPID != nil && *state.PID == *oldPID {
		t.Errorf("PID unchanged after Restart: %d", *state.PID)
	}
}

func TestValidateValid(t *testing.T) {
	bin := buildFakeMihomo(t)
	opts := testOpts(t, bin)
	writeConfig(t, opts.ActiveConfigPath, "mixed-port: 7890\n")
	sup := New(opts)
	defer sup.Dispose()

	result := sup.Validate(opts.ActiveConfigPath)
	if !result.Valid {
		t.Errorf("Validate should succeed, got: %s", result.Message)
	}
}

func TestValidateInvalid(t *testing.T) {
	bin := buildFakeMihomo(t)
	opts := testOpts(t, bin)
	writeConfig(t, opts.ActiveConfigPath, "mixed-port: 7890\n# invalid\n")
	sup := New(opts)
	defer sup.Dispose()

	result := sup.Validate(opts.ActiveConfigPath)
	if result.Valid {
		t.Error("Validate should fail for config with # invalid marker")
	}
}

func TestAutoRestart(t *testing.T) {
	bin := buildFakeMihomo(t)
	opts := testOpts(t, bin)
	writeConfig(t, opts.ActiveConfigPath, "mixed-port: 7890\n")
	sup := New(opts)
	defer sup.Dispose()

	sup.Start()
	firstPID := sup.State().PID
	if firstPID == nil {
		t.Fatal("PID nil after Start")
	}
	killProc(t, *firstPID)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := sup.State()
		if st.Status == api.StatusRunning && st.PID != nil && *st.PID != *firstPID {
			t.Logf("auto-restarted: old=%d new=%d", *firstPID, *st.PID)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("auto-restart did not happen within 3s")
}

func TestMaxRestarts(t *testing.T) {
	bin := buildFakeMihomo(t)
	opts := testOpts(t, bin)
	opts.MaxRestarts = 2
	opts.StableRestart = time.Hour
	opts.RestartBackoff = 50 * time.Millisecond
	writeConfig(t, opts.ActiveConfigPath, "mixed-port: 7890\n")
	sup := New(opts)
	defer sup.Dispose()

	sup.Start()

	// Kill the process 3 times with enough time for each restart to complete.
	// With MaxRestarts=2, the first 2 kills restart; the 3rd should leave errored.
	for i := 0; i < 3; i++ {
		st := sup.State()
		if st.PID == nil {
			t.Fatalf("PID nil before kill %d", i)
		}
		killProc(t, *st.PID)
		// Give enough time for restart or for the supervisor to give up.
		time.Sleep(300 * time.Millisecond)
		t.Logf("after kill %d: status=%s", i, sup.State().Status)
	}

	// After exceeding MaxRestarts, status should be errored.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sup.State().Status == api.StatusErrored {
			t.Logf("correctly reached errored after max restarts")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("expected StatusErrored after MaxRestarts, got %q", sup.State().Status)
}

func TestStableReset(t *testing.T) {
	bin := buildFakeMihomo(t)
	opts := testOpts(t, bin)
	opts.StableRestart = 200 * time.Millisecond
	opts.RestartBackoff = 50 * time.Millisecond
	writeConfig(t, opts.ActiveConfigPath, "mixed-port: 7890\n")
	sup := New(opts)
	defer sup.Dispose()

	sup.Start()

	// Kill once, let it restart.
	st := sup.State()
	killProc(t, *st.PID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sup.State().Status == api.StatusRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for StableRestart to pass (crash counter resets).
	time.Sleep(500 * time.Millisecond)

	// Crash twice more — should still be running (counter was reset).
	for i := 0; i < 2; i++ {
		st = sup.State()
		if st.PID == nil {
			t.Fatalf("PID nil at iteration %d", i)
		}
		killProc(t, *st.PID)
		deadline = time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if sup.State().Status == api.StatusRunning {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	if sup.State().Status != api.StatusRunning {
		t.Errorf("expected running after stable reset, got %q", sup.State().Status)
	}
}

func TestInjectClashConfig(t *testing.T) {
	bin := buildFakeMihomo(t)
	opts := testOpts(t, bin)
	writeConfig(t, opts.ActiveConfigPath, "rules:\n  - MATCH\n")
	sup := New(opts)
	defer sup.Dispose()

	if err := sup.injectClashConfig(); err != nil {
		t.Fatalf("injectClashConfig: %v", err)
	}
	content, err := os.ReadFile(opts.ActiveConfigPath)
	if err != nil {
		t.Fatalf("read active.yaml: %v", err)
	}
	s := string(content)
	for _, want := range []string{"external-controller:", "secret: test-secret", "mixed-port: 7890"} {
		if !strings.Contains(s, want) {
			t.Errorf("active.yaml missing %q\nfull content:\n%s", want, s)
		}
	}
	if !strings.Contains(s, "rules:") {
		t.Error("active.yaml lost original rules")
	}
}

func killProc(t *testing.T, pid int) {
	t.Helper()
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
