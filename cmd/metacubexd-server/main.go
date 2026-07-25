// Command metacubexd-server is the All-in-One Go binary that replaces the
// upstream TS server (apps/server + packages/agent). It serves the dashboard,
// proxies the Clash API same-origin (HTTP + WebSocket), and supervises the
// mihomo kernel.
//
// Boot sequence mirrors apps/server/plugins/boot-kernel.ts:
//  1. Parse env → ServerEnv
//  2. Ensure DATA_DIR/profiles exists
//  3. Construct Supervisor with env-derived bind/secret/mixedPort
//  4. Auto-start the kernel (header-only active.yaml if no profile yet)
//  5. Mount routes: control > clash > static (catch-all)
//  6. Listen on CONTROL_PORT
//
// Phase 1 scope: HTTP server + supervisor + static + Clash proxy + temp info.
// Profiles, validate, auto-restart, scheduler, webdav, geo — Phases 2-5.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"metacubexd-server-go/internal/clashproxy"
	"metacubexd-server-go/internal/config"
	"metacubexd-server-go/internal/control"
	authmw "metacubexd-server-go/internal/auth"
	"metacubexd-server-go/internal/profile"
	"metacubexd-server-go/internal/scheduler"
	"metacubexd-server-go/internal/static"
	"metacubexd-server-go/internal/supervisor"
)

func main() {
	env := config.FromEnv()

	// Pre-create DATA_DIR/profiles so the supervisor's homeDir exists when it
	// spawns mihomo. The profile store (Phase 2) reads/writes here too.
	if err := os.MkdirAll(env.ProfilesDir(), 0o755); err != nil {
		log.Fatalf("[server] cannot create profiles dir %s: %v", env.ProfilesDir(), err)
	}

	sup := supervisor.New(supervisor.Options{
		BinaryPath:         env.MihomoBin,
		HomeDir:            env.DataDir,
		ActiveConfigPath:   env.ActiveConfigPath(),
		ExternalController: env.ExternalController(),
		Secret:             env.ClashSecret,
		MixedPort:          env.MixedPort,
		// Crash watchdog: the All-in-One server is the only thing keeping the
		// kernel up, so an unexpected crash should self-heal rather than brick
		// the dashboard until someone clicks "Start". maxRestarts bounds a
		// crash loop; stableRestart resets the counter once running.
		AutoRestart: true,
	})

	// Profile store: index.json + <id>.yaml + state.json under
	// <DATA_DIR>/profiles. SetActive composes the chosen profile + its enabled
	// merge overlays into active.yaml, which the supervisor then spawns mihomo
	// against. Initial-state tolerates a missing dir (first run / empty volume).
	profiles := profile.New(profile.Options{
		Dir:              env.ProfilesDir(),
		ActiveConfigPath: env.ActiveConfigPath(),
	})

	// Subscription auto-update scheduler: every 60s, refresh remote profiles
	// whose updateInterval has elapsed. When the refreshed profile IS the
	// active base, re-compose + restart so the new subscription takes effect
	// immediately (matches the TS server's boot-kernel.ts plugin behavior).
	sched := scheduler.New(scheduler.Options{
		Profiles:   profiles,
		Supervisor: sup,
	})
	sched.Start()
	log.Printf("[profiles] auto-update scheduler started (tick=60s)")

	// Auto-start the kernel on boot, fire-and-forget. A slow or failing kernel
	// must not wedge server startup; the dashboard's Start action and (Phase 3)
	// the crash watchdog cover recovery. With no profile yet, the supervisor
	// writes a header-only active.yaml (external-controller/secret/mixed-port)
	// and mihomo runs on its defaults — enough for the dashboard to connect.
	go func() {
		log.Printf("[kernel] starting bundled mihomo (%s) on boot…", env.MihomoBin)
		st, err := sup.Start()
		if err != nil {
			log.Printf("[kernel] failed to start on boot: %v (state=%s)", err, st.Status)
			return
		}
		log.Printf("[kernel] %s", formatKernelState(st))
	}()

	// Top-level dispatcher: Go 1.22's http.ServeMux does longest-prefix
	// matching, which is exactly what we need — no chi.Mount prefix-stripping
	// surprises. Each handler does its own internal routing on the FULL path.
	//
	// Route priority (longest subtree match wins):
	//   /api/control/* — agent API (auth, JSON)  [chi.Router inside]
	//   /api/clash/*   — same-origin Clash proxy  [http.ServeMux inside]
	//   /              — static UI + SPA fallback + /config.js
	mux := http.NewServeMux()

	// control.New returns a chi.Router that registers full /api/control/*
	// paths. ServeMux's subtree pattern forwards every /api/control/** request
	// verbatim (r.URL.Path is unchanged), so chi's internal match works.
	mux.Handle("/api/control/", control.New(control.Router{
		Supervisor:       sup,
		Profiles:         profiles,
		Token:            env.ControlToken,
		MihomoBin:        env.MihomoBin,
		MihomoVersion:    env.MihomoVersion,
		HomeDir:          env.DataDir,
		ActiveConfigPath: env.ActiveConfigPath(),
	}))

	// Clash proxy: stateFunc reads live supervisor state so a kernel restart
	// (which re-injects external-controller/secret) is reflected immediately.
	mux.Handle("/api/clash/", clashproxy.New(func() (upstream, secret string, running bool) {
		st := sup.State()
		return st.ExternalController, st.Secret, st.Status == supervisor.StatusRunning
	}))

	// Static UI. SameOriginClashPath makes /config.js emit
	//   defaultBackendURL = "http://<request host>/api/clash"
	// so the dashboard's transformEndpointURL doesn't prefix `protocol://` onto
	// a path-only string (which would yield `http:///api/clash`). The proxy at
	// /api/clash/* then forwards to mihomo — the core Phase 1 deliverable per
	// GO_SERVER_PLAN.md §5.
	staticHandler, err := static.New(env.UIDist, static.Config{
		ControlToken:        env.ControlToken,
		GitHubToken:         env.GitHubToken,
		SameOriginClashPath: "/api/clash",
	})
	if err != nil {
		log.Fatalf("[server] cannot init static handler: %v", err)
	}
	mux.Handle("/", staticHandler)

	// Auth middleware wraps the entire mux so the login flow (/login,
	// /api/auth/*) is handled before any route, and every other path is
	// gated when CONTROL_TOKEN is set. When CONTROL_TOKEN is empty the
	// middleware is a no-op pass-through, preserving the unauthenticated mode.
	//
	// Order matters: auth must be OUTSIDE mux so /login isn't swallowed by
	// the static SPA fallback. On success auth stamps X-Metacubexd-Authed,
	// which clashproxy and control read to branch on credential handling.
	handler := authmw.New(authmw.Config{Password: env.ControlToken})(mux)

	addr := fmt.Sprintf(":%d", env.ControlPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout — SSE + WS upgrades need to live indefinitely.
		// ReadTimeout is bounded per-request by the underlying conn.
	}

	// Graceful shutdown on SIGINT/SIGTERM. Stops accepting new connections,
	// waits up to 10s for in-flight requests, then disposes the supervisor
	// (SIGTERM → SIGKILL the kernel).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("[server] metacubexd-server listening on %s", addr)
		log.Printf("[server]   UI_DIST     = %s", uiDistDisplay(env.UIDist))
		log.Printf("[server]   DATA_DIR    = %s", absPathDisplay(env.DataDir))
		log.Printf("[server]   Clash API   = %s (internal)", env.ExternalController())
		log.Printf("[server]   Mixed port  = %d", env.MixedPort)
		if env.ControlToken != "" {
			log.Printf("[server]   login auth:  enabled (password = CONTROL_TOKEN)")
		} else {
			log.Printf("[server]   login auth:  DISABLED (set CONTROL_TOKEN to enable login page)")
		}
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[server] listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("[server] shutting down…")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[server] graceful shutdown failed: %v", err)
	}
	sched.Stop()
	if err := sup.Dispose(); err != nil {
		log.Printf("[server] supervisor dispose: %v", err)
	}
	log.Printf("[server] bye")
}

// formatKernelState renders a one-line summary for the boot log.
func formatKernelState(st supervisor.KernelState) string {
	if st.PID != nil {
		return fmt.Sprintf("%s (pid %d)", st.Status, *st.PID)
	}
	return string(st.Status)
}

// uiDistDisplay keeps the log line short by collapsing the empty-embed case.
func uiDistDisplay(uidDist string) string {
	if uidDist == "" {
		return "(embedded, but no UI bundled — set UI_DIST)"
	}
	abs, err := filepath.Abs(uidDist)
	if err != nil {
		return uidDist
	}
	return abs
}

// absPathDisplay resolves a (possibly relative) path against the server's CWD
// for log readability. The default DATA_DIR is "data" (relative), which would
// be ambiguous in startup logs without this.
func absPathDisplay(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
