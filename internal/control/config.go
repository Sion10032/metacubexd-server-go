// Active-config routes under /api/control/config*. Mounted by control.New
// when a ProfileStore is wired in. Endpoint set matches GO_SERVER_PLAN.md
// §4 Phase 4:
//
//   GET  /config            active profile's source YAML (text/yaml)
//   PUT  /config            overwrite active profile content + re-activate + restart
//   GET  /config/runtime    the file mihomo actually runs (-f), post-injection
//   GET  /config/section    one top-level key from the active profile (JSON)
//   PUT  /config/section    replace one top-level key + re-activate (optionally restart)
package control

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"metacubexd-server-go/internal/profile"
)

// registerConfigRoutes wires /api/control/config* onto r. Called from
// control.New only when profiles != nil (the routes need an active profile
// to operate on).
func registerConfigRoutes(r Router, mux chi.Router) {
	mux.Get("/api/control/config", func(w http.ResponseWriter, req *http.Request) {
		activeID := r.Profiles.GetActiveID()
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		if activeID == "" {
			return
		}
		content, err := r.Profiles.Read(activeID)
		if err != nil {
			// Stale activeId (profile deleted between calls). Treat as empty
			// rather than 500 — the dashboard shows the empty-config state.
			return
		}
		_, _ = io.WriteString(w, content)
	})

	mux.Put("/api/control/config", func(w http.ResponseWriter, req *http.Request) {
		activeID := r.Profiles.GetActiveID()
		if activeID == "" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "no active profile"})
			return
		}
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if _, err := r.Profiles.Update(activeID, profile.UpdateInput{Content: &body.Content}); err != nil {
			respondProfileErr(w, err)
			return
		}
		// Re-activate so active.yaml is recomposed from the edited content,
		// then restart so the kernel picks it up. safeActivate would also
		// validate — but the TS PUT /config path calls setActive + restart
		// directly without validation; we keep parity so the dashboard's
		// "save raw YAML" doesn't surprise power users with a 400.
		if err := r.Profiles.SetActive(activeID); err != nil {
			respondProfileErr(w, err)
			return
		}
		st, err := r.Supervisor.Restart()
		respondKernel(w, st, err)
	})

	mux.Get("/api/control/config/runtime", func(w http.ResponseWriter, req *http.Request) {
		// This is the actual file mihomo runs (-f). At runtime it carries the
		// supervisor-injected external-controller/secret/mixed-port, so it
		// differs from the active profile served by GET /config. Missing file
		// → empty body (matches the TS ENOENT→'' behavior).
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		b, err := os.ReadFile(r.ActiveConfigPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(b)
	})

	mux.Get("/api/control/config/section", func(w http.ResponseWriter, req *http.Request) {
		key := req.URL.Query().Get("key")
		activeID := r.Profiles.GetActiveID()
		var value any
		if activeID != "" {
			v, err := r.Profiles.GetSection(activeID, key)
			if err == nil {
				value = v
			}
		}
		// Serialize explicitly so an absent section / no-active-profile still
		// yields a JSON `null` body (Go's nil any marshals to null).
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(value)
	})

	mux.Put("/api/control/config/section", func(w http.ResponseWriter, req *http.Request) {
		activeID := r.Profiles.GetActiveID()
		if activeID == "" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "no active profile"})
			return
		}
		var body struct {
			Key     string `json:"key"`
			Value   any    `json:"value"`
			Restart *bool  `json:"restart"` // pointer so we distinguish omit from false
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if body.Key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key required"})
			return
		}
		if err := r.Profiles.SetSection(activeID, body.Key, body.Value); err != nil {
			respondProfileErr(w, err)
			return
		}
		if err := r.Profiles.SetActive(activeID); err != nil {
			respondProfileErr(w, err)
			return
		}
		// restart defaults to true (rule/network editors want one-restart-per-save).
		// The general-config card passes restart=false because it already
		// hot-applies each field via PATCH /configs; it only needs persistence
		// so the change survives the next restart (#2070).
		if body.Restart != nil && !*body.Restart {
			writeJSON(w, http.StatusOK, r.Supervisor.State())
			return
		}
		st, err := r.Supervisor.Restart()
		respondKernel(w, st, err)
	})
}
