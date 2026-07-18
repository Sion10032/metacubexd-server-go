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

	"github.com/go-chi/chi/v5"

	"metacubexd-server-go/internal/profile"
	"metacubexd-server-go/internal/supervisor"
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

	// Phase 3 will replace this with `mihomo -t -f <candidate>`. For now the
	// route exists so the dashboard's "Validate" button doesn't 404, and
	// reports success optimistically so activation isn't blocked.
	r.Post("/api/control/profiles/{id}/validate", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"valid":   false,
			"message": "validate not implemented in this build (Phase 3)",
		})
	})
}

// safeActivate composes the profile into active.yaml and restarts the kernel.
// Phase 3 will add `mihomo -t` validation + rollback-on-failure here; for now
// the operation is: setActive → restart.
//
// The previousActiveId is captured (but unused in Phase 2) so the rollback
// path lands cleanly in Phase 3 without an API change.
func safeActivate(deps *Router, id string) (supervisor.KernelState, error) {
	prev := deps.Profiles.GetActiveID()
	_ = prev // Phase 3 rollback hook
	if err := deps.Profiles.SetActive(id); err != nil {
		return supervisor.KernelState{}, err
	}
	return deps.Supervisor.Restart()
}

// findMeta scans the index for one meta. We don't expose a public Get on the
// store because list+filter is what the TS does and the index always fits in
// memory.
func findMeta(p *profile.Store, id string) (profile.Meta, error) {
	for _, m := range p.List() {
		if m.ID == id {
			return m, nil
		}
	}
	return profile.Meta{}, profile.ErrNotFound
}

// respondProfileResult writes a created/updated meta, or maps the error to an
// HTTP status if present.
func respondProfileResult(w http.ResponseWriter, m profile.Meta, err error) {
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
