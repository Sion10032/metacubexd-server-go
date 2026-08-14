// Profile routes under /api/control/profiles/**. Mounted by control.New when
// a ProfileStore is wired in; without one, /profiles stays at the Phase 1 empty
// stub (useful for spinning up the server before the store is ready).
//
// Endpoint set matches GO_SERVER_PLAN.md §4 Phase 2:
//   GET    /profiles                       list
//   POST   /profiles                       create (script → 501)
//   GET    /profiles/{id}                  read meta + content
//   PUT    /profiles/{id}                  update name/content/enabled/...
//   DELETE /profiles/{id}                  remove (cascades scoped overlays)
//   POST   /profiles/{id}/duplicate        copy content to a new local profile
//   POST   /profiles/import                fetch URL with UA clash.meta
//   POST   /profiles/{id}/refresh          re-fetch in place
//   POST   /profiles/{id}/refresh-and-activate  refresh + activate + restart
//   POST   /profiles/{id}/activate         compose + setActive + restart
//   POST   /profiles/{id}/validate         501 until Phase 3 wires mihomo -t
package control

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"metacubexd-server-go/internal/api"
	"metacubexd-server-go/internal/server/profile"
)

// registerProfileRoutes wires every /profiles/** route onto r. Called from
// control.New only when profiles != nil.
func registerProfileRoutes(r chi.Router, deps *Router) {
	p := deps.Profiles

	r.Get("/api/control/profiles", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, p.List())
	})

	r.Post("/api/control/profiles", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Name          string `json:"name"`
			Content       string `json:"content"`
			Type          string `json:"type"`
			BaseProfileID string `json:"baseProfileId"`
			ManagedBy     string `json:"managedBy"`
			EditorStatus  string `json:"editorStatus"`
		}
		if err := decodeJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		m, err := p.Create(profile.CreateInput{
			Name:          body.Name,
			Content:       body.Content,
			Type:          body.Type,
			BaseProfileID: body.BaseProfileID,
			ManagedBy:     body.ManagedBy,
			EditorStatus:  body.EditorStatus,
		})
		respondProfileResult(w, m, err)
	})

	r.Get("/api/control/profiles/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		meta, err := findMeta(p, id)
		if err != nil {
			respondProfileErr(w, err)
			return
		}
		content, err := p.Read(id)
		if err != nil {
			respondProfileErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"meta":    meta,
			"content": content,
		})
	})

	r.Put("/api/control/profiles/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		var body struct {
			Name           *string `json:"name"`
			Content        *string `json:"content"`
			Enabled        *bool   `json:"enabled"`
			UpdateInterval *int    `json:"updateInterval"`
			EditorStatus   *string `json:"editorStatus"`
		}
		if err := decodeJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// managedBy === 'visual-editor' overlays are read-only via this route.
		if m, err := findMeta(p, id); err == nil && m.ManagedBy == profile.ManagedByVisualEditor {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "managed visual editor overlays are read-only",
			})
			return
		}
		m, err := p.Update(id, profile.UpdateInput{
			Name:           body.Name,
			Content:        body.Content,
			Enabled:        body.Enabled,
			UpdateInterval: body.UpdateInterval,
			EditorStatus:   body.EditorStatus,
		})
		respondProfileResult(w, m, err)
	})

	r.Delete("/api/control/profiles/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		if err := p.Delete(id); err != nil {
			respondProfileErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	r.Post("/api/control/profiles/{id}/duplicate", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		// Body is optional {name}; missing name → "<src> copy".
		var body struct {
			Name string `json:"name"`
		}
		_ = decodeJSON(req, &body) // tolerate empty/invalid body
		m, err := p.Duplicate(id, body.Name)
		respondProfileResult(w, m, err)
	})

	r.Post("/api/control/profiles/import", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			URL  string `json:"url"`
			Name string `json:"name"`
		}
		if err := decodeJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if body.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url required"})
			return
		}
		m, err := p.ImportFromURL(body.URL, body.Name)
		respondProfileResult(w, m, err)
	})

	r.Post("/api/control/profiles/{id}/refresh", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		// Pure refresh: re-fetch + return updated meta. Does NOT activate —
		// pair with /activate, or use /refresh-and-activate for the combined
		// apply (#2108).
		m, err := p.Refresh(id)
		respondProfileResult(w, m, err)
	})

	r.Post("/api/control/profiles/{id}/refresh-and-activate", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		meta, err := p.Refresh(id)
		if err != nil {
			respondProfileErr(w, err)
			return
		}
		kernel, err := safeActivate(deps, id)
		if err != nil {
			respondProfileErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"meta": meta, "kernel": kernel})
	})

	r.Post("/api/control/profiles/{id}/activate", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		kernel, err := safeActivate(deps, id)
		if err != nil {
			respondProfileErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, kernel)
	})

	// Phase 3: real validate. Materialize the profile to a candidate file
	// (independent of active.yaml so the running kernel is untouched) and ask
	// the supervisor to run `mihomo -t` on it. The 5min timeout tolerates a
	// first-validate GEO download.
	r.Post("/api/control/profiles/{id}/validate", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		content, err := p.Read(id)
		if err != nil {
			respondProfileErr(w, err)
			return
		}
		// Candidate file alongside active.yaml so the supervisor's homeDir -d
		// argument resolves geo/cache paths the same way the live config does.
		candidate := deps.HomeDir + "/.validate-" + id + ".yaml"
		if err := os.WriteFile(candidate, []byte(content), 0o644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer os.Remove(candidate)
		result := deps.Supervisor.Validate(candidate)
		writeJSON(w, http.StatusOK, result)
	})
}

// safeActivate composes the profile into active.yaml, validates it with
// `mihomo -t`, and restarts the kernel only if validation passes. On failure
// it restores the previous active config so a bad subscription can't brick
// the running kernel across restarts (#2109):
//
//   - If there was a different prior active profile, re-activate it (re-compose
//	     its overlays). This is the common case (user toggles between profiles).
//   - If there was no prior active profile (first activation) OR re-activating
//	     the same id, restore the pre-activation file from the .bak snapshot
//			written by SetActive.
//
// Either rollback path is best-effort — a failure must not mask the original
// validation error, so we swallow rollback errors.
func safeActivate(deps *Router, id string) (api.KernelState, error) {
	prevID := deps.Profiles.GetActiveID()
	if err := deps.Profiles.SetActive(id); err != nil {
		return api.KernelState{}, err
	}

	result := deps.Supervisor.Validate(deps.ActiveConfigPath)
	if result.Valid {
		return deps.Supervisor.Restart()
	}

	// Validation failed: roll back to the prior good config so the running
	// kernel doesn't pick up the bad one on next restart.
	if prevID != "" && prevID != id {
		_ = deps.Profiles.SetActive(prevID)
	} else {
		_ = deps.Profiles.Rollback()
	}

	// Surface a clean 400 carrying the validator's message. The caller maps
	// this to the HTTP response.
	return api.KernelState{}, &ValidateError{Message: result.Message}
}

// ValidateError is surfaced by safeActivate when `mihomo -t` rejects the
// candidate. The control layer maps it to HTTP 400 with the validator's
// message so the dashboard can show the diagnostic.
type ValidateError struct {
	Message string
}

func (e *ValidateError) Error() string { return "profile validation failed" }

// findMeta scans the index for one meta. We don't expose a public Get on the
// store because list+filter is what the TS does and the index always fits in
// memory.
func findMeta(p *profile.Store, id string) (api.Meta, error) {
	for _, m := range p.List() {
		if m.ID == id {
			return m, nil
		}
	}
	return api.Meta{}, profile.ErrNotFound
}

// respondProfileResult writes a created/updated meta, or maps the error to an
// HTTP status if present.
func respondProfileResult(w http.ResponseWriter, m api.Meta, err error) {
	if err != nil {
		respondProfileErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// respondProfileErr maps profile package errors to HTTP statuses. The contract
// matches the TS control layer:
//   - ErrNotFound              → 404
//   - ErrScriptNotSupported    → 501 (砍项 surfaced to UI as a toast)
//   - ErrIsOverlay             → 400 (activate on merge/script)
//   - ErrNotRemote             → 400 (refresh on local)
//   - *SubscriptionFetchError  → echo upstream status (preserves 401/403/404)
//   - anything else            → 500
func respondProfileErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, profile.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, profile.ErrScriptNotSupported):
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": err.Error()})
	case errors.Is(err, profile.ErrIsOverlay):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, profile.ErrNotRemote):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, profile.ErrMultipleOverlays):
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	default:
		// ValidateError is a fresh allocation per call (not a sentinel), so we
		// use errors.As rather than errors.Is.
		var ve *ValidateError
		if errors.As(err, &ve) {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "profile validation failed",
				"detail": ve.Message,
			})
			return
		}
		var subErr *profile.SubscriptionFetchError
		if errors.As(err, &subErr) {
			// Echo the provider's HTTP status verbatim — a 401 from a credentialed
			// URL surfaces as 401 here, not a generic 502.
			writeJSON(w, subErr.UpstreamStatus, map[string]any{
				"error":          subErr.Error(),
				"upstreamStatus": subErr.UpstreamStatus,
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

// decodeJSON reads a JSON body into dst. An empty body is tolerated (leaves
// dst at zero values) so optional-body routes like /duplicate don't 400.
func decodeJSON(req *http.Request, dst any) error {
	if req.Body == nil {
		return nil
	}
	dec := json.NewDecoder(req.Body)
	return dec.Decode(dst)
}
