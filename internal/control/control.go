// Package control implements the /api/control/* HTTP routes that drive the
// dashboard's agent-side surface (kernel lifecycle, profiles, info, SSE logs).
//
// Phase 1 scope (this file):
//   - Auth middleware mirroring apps/server/middleware/auth.ts
//   - GET  /health, /info
//   - GET  /kernel/status
//   - POST /kernel/start | /stop | /restart
//   - GET  /kernel/logs (SSE: state + log lines)
//   - GET  /profiles (empty list stub — Phase 2 fills in the full store)
//
// Everything else under /api/control/* returns 404 in Phase 1 so the dashboard
// shows a clean error instead of an opaque 500. Phase 2 adds /profiles/* and
// the config/section routes; Phase 3 adds /kernel/rollback + recover + validate.
package control

import (
	"encoding/json"
	"net/http"
	"runtime"

	"github.com/go-chi/chi/v5"

	"metacubexd-server-go/internal/profile"
	"metacubexd-server-go/internal/sse"
	"metacubexd-server-go/internal/supervisor"
)

// Info describes the server to the dashboard. The shape mirrors the TS
// AgentInfo interface so the dashboard's feature-flag logic works unchanged.
//
// Notably absent from Features (vs the TS server):
//   - "visual-config-editor" — the {} button is intentionally hidden
//   - "system-proxy" / "kernel-version" / "tun" — desktop-only capabilities
type Info struct {
	HasAgent bool          `json:"hasAgent"`
	Version  string        `json:"version"`
	Platform PlatformInfo  `json:"platform"`
	Kernel   KernelInfo    `json:"kernel"`
	Features []string      `json:"features"`
}

type PlatformInfo struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type KernelInfo struct {
	Bundled bool   `json:"bundled"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

// Router bundles the dependencies every control handler needs. main.go
// constructs one and passes it to chi mounting.
type Router struct {
	Supervisor    *supervisor.Supervisor
	Profiles      *profile.Store // nil = Phase 1 stub (no /profiles/* routes)
	Token         string         // "" = no auth (matches the in-process Electron case)
	MihomoBin     string         // absolute path to the kernel binary (for info.kernel.path)
	MihomoVersion string         // build-time pinned tag (for info.kernel.version fallback)
	// HomeDir is mihomo's -d working dir. Used to materialize validate
	// candidate files alongside the live active.yaml so the validator resolves
	// geo/cache paths the same way.
	HomeDir string
	// ActiveConfigPath is the file the kernel runs with -f. safeActivate runs
	// `mihomo -t` against this path AFTER SetActive writes it.
	ActiveConfigPath string
}

// AgentVersion is the agent version reported to the dashboard. The TS server
// uses "0.0.0"; we keep parity so version-comparison logic in the UI behaves
// the same way.
const AgentVersion = "0.0.0"

// Features is the static capability list advertised to the dashboard. Order
// matches the TS createAgent so diffs in DevTools are obvious.
var Features = []string{
	"profiles",
	"logs-sse",
	"kernel-control",
	"geo-assets",
	"webdav-backup",
	"runtime-config",
	"config-sections",
	// Deliberately omitted: "visual-config-editor", "system-proxy",
	// "kernel-version", "tun".
}

// New builds a chi.Router with all Phase 1 routes registered. Auth is applied
// to everything except /health and /info (matches the TS PUBLIC_CONTROL_PATHS
// semantics — the dashboard probes /info on boot before it has a token).
func New(r Router) chi.Router {
	mux := chi.NewRouter()

	mux.Get("/api/control/health", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.Get("/api/control/info", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, r.info())
	})

	mux.Get("/api/control/kernel/status", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, r.Supervisor.State())
	})
	mux.Post("/api/control/kernel/start", func(w http.ResponseWriter, req *http.Request) {
		st, err := r.Supervisor.Start()
		respondKernel(w, st, err)
	})
	mux.Post("/api/control/kernel/stop", func(w http.ResponseWriter, req *http.Request) {
		st, err := r.Supervisor.Stop()
		respondKernel(w, st, err)
	})
	mux.Post("/api/control/kernel/restart", func(w http.ResponseWriter, req *http.Request) {
		st, err := r.Supervisor.Restart()
		respondKernel(w, st, err)
	})
	// Restore the last-known-good active.yaml from the .bak snapshot written
	// by SetActive, then restart. Escape hatch for a config that bricks the
	// kernel on boot (#2109). 404 when no backup exists.
	mux.Post("/api/control/kernel/rollback", func(w http.ResponseWriter, req *http.Request) {
		if r.Profiles == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "profile store unavailable"})
			return
		}
		if !r.Profiles.Rollback() {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no backup config to roll back to"})
			return
		}
		st, err := r.Supervisor.Restart()
		respondKernel(w, st, err)
	})
	// Reset active.yaml to header-only and drop activeId, then restart on
	// mihomo defaults. Last-resort recovery when even the .bak is bad — the
	// dashboard reconnects and the user can re-import a profile.
	mux.Post("/api/control/kernel/recover", func(w http.ResponseWriter, req *http.Request) {
		if r.Profiles == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "profile store unavailable"})
			return
		}
		if err := r.Profiles.ResetActive(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		st, err := r.Supervisor.Restart()
		respondKernel(w, st, err)
	})
	mux.Get("/api/control/kernel/logs", r.handleKernelLogs)

	// Profile routes. When no ProfileStore is wired in, /profiles stays at
	// the empty-list stub so the dashboard's sidebar still renders. With a
	// store, the full Phase 2 endpoint set is registered.
	if r.Profiles != nil {
		registerProfileRoutes(mux, &r)
		registerConfigRoutes(r, mux)
		registerBackupRoutes(r, mux)
	} else {
		mux.Get("/api/control/profiles", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, []any{})
		})
	}
	// Everything else: explicit 404 JSON so the dashboard sees a stable error
	// shape rather than chi's HTML 404.
	mux.NotFound(func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	mux.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	})
	return mux
}

// info builds the AgentInfo snapshot. The kernel.version field falls back to
// the build-time MihomoVersion when the supervisor hasn't observed a running
// kernel yet (mirrors the TS fallback comment in index.ts).
func (r Router) info() Info {
	st := r.Supervisor.State()
	version := st.Version
	if version == "" {
		version = r.MihomoVersion
	}
	return Info{
		HasAgent: true,
		Version:  AgentVersion,
		Platform: PlatformInfo{OS: runtime.GOOS, Arch: runtime.GOARCH},
		Kernel: KernelInfo{
			Bundled: true,
			Path:    r.MihomoBin,
			Version: version,
		},
		Features: Features,
	}
}

// handleKernelLogs streams kernel log + state events as SSE. The handler
// subscribes to the supervisor, seeds the current state, then blocks on the
// request context until the client disconnects. On disconnect the deferred
// Off* calls detach the callbacks so a reconnecting EventSource can't leak
// closures into the supervisor's callback map (the same bug the TS off()
// symmetric API exists to prevent).
func (r Router) handleKernelLogs(w http.ResponseWriter, req *http.Request) {
	sw, err := sse.New(w)
	if err != nil {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Buffered channels so a chatty kernel can't block the supervisor's mutex
	// when the SSE client is slow; overflow drops log lines (acceptable: the
	// dashboard isn't a durable log store).
	logCh := make(chan supervisor.KernelLogLine, 64)
	stateCh := make(chan supervisor.KernelState, 16)

	logID := r.Supervisor.OnLog(func(l supervisor.KernelLogLine) {
		select {
		case logCh <- l:
		default:
		}
	})
	stateID := r.Supervisor.OnState(func(s supervisor.KernelState) {
		select {
		case stateCh <- s:
		default:
		}
	})
	defer func() {
		r.Supervisor.OffLog(logID)
		r.Supervisor.OffState(stateID)
		sw.Close()
	}()

	// Seed with current state so a late subscriber knows where things stand
	// (matches the TS seed in /kernel/logs).
	if err := sw.Push(stateEvent{Type: "state", KernelState: r.Supervisor.State()}); err != nil {
		return
	}

	ctx := req.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case l := <-logCh:
			if err := sw.Push(logEvent{Type: "log", KernelLogLine: l}); err != nil {
				return
			}
		case s := <-stateCh:
			if err := sw.Push(stateEvent{Type: "state", KernelState: s}); err != nil {
				return
			}
		}
	}
}

// stateEvent / logEvent match the TS { type, ...payload } wire shape: the
// supervisor types are embedded so their fields appear at the top level of
// the JSON object.
type stateEvent struct {
	Type string `json:"type"` // "state"
	supervisor.KernelState
}

type logEvent struct {
	Type string `json:"type"` // "log"
	supervisor.KernelLogLine
}

// respondKernel writes a kernel state snapshot, or a 500 with lastError if the
// lifecycle op failed. Lifecycle ops don't have a meaningful error body beyond
// state.LastError, so we fold the two together.
func respondKernel(w http.ResponseWriter, st supervisor.KernelState, err error) {
	if err != nil {
		// The state already carries lastError; surface it as 500 for clients
		// that distinguish HTTP status, plus the body for diagnostics.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(st)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// writeJSON sets the content type and encodes the payload. On marshal error
// (which should be impossible for our types) it falls back to a 500.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
