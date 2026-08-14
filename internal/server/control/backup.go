// Backup / restore / geo routes under /api/control. Mounted by control.New
// when a ProfileStore is wired in.
//
//   POST /backup       push a JSON bundle of every profile (meta + content)
//                      plus optional uiSettings to a WebDAV server
//   POST /restore      pull the bundle back and recreate every profile with
//                      fresh ids (preserving merge/script types; remote → local)
//   POST /geo/update   download mihomo's geoip/geosite/country.mmdb into homeDir
package control

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"metacubexd-server-go/internal/server/kernel"
	"metacubexd-server-go/internal/server/profile"
	"metacubexd-server-go/internal/server/webdav"
)

// backupFilename is the bundle's name on the WebDAV server. Same as the TS
// server's BACKUP_FILENAME so a backup written by one binary can be restored
// by the other (mixed deployment migration).
const backupFilename = "metacubexd-backup.json"

// webdavTimeout is long enough to upload a large bundle on a slow link, short
// enough that a wedged server doesn't dangle the dashboard's spinner forever.
const webdavTimeout = 60 * time.Second

// backupBundleEntry is one entry in the bundle's profiles list.
type backupBundleEntry struct {
	Meta struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Type          string `json:"type"`
		BaseProfileID string `json:"baseProfileId"`
		ManagedBy     string `json:"managedBy"`
		EditorStatus  string `json:"editorStatus"`
	} `json:"meta"`
	Content string `json:"content"`
}

// backupBundle is the on-disk JSON layout (mirrors the TS server's bundle v1).
type backupBundle struct {
	Version    int                  `json:"version"`
	Profiles   []backupBundleEntry  `json:"profiles"`
	UISettings any                  `json:"uiSettings,omitempty"`
}

// registerBackupRoutes wires /backup, /restore, /geo/update onto mux.
func registerBackupRoutes(r Router, mux chi.Router) {
	mux.Post("/api/control/backup", handleBackup(r))
	mux.Post("/api/control/restore", handleRestore(r))
	mux.Post("/api/control/geo/update", handleGeoUpdate(r))
}

func handleBackup(r Router) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Webdav struct {
				URL      string `json:"url"`
				Username string `json:"username"`
				Password string `json:"password"`
				Dir      string `json:"dir"`
			} `json:"webdav"`
			UISettings any `json:"uiSettings,omitempty"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if body.Webdav.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "webdav.url required"})
			return
		}

		// Build a bundle of every profile (meta + raw content). Read errors
		// for an individual profile shouldn't abort the whole backup — skip
		// and continue so one corrupted file doesn't make the whole backup
		// unusable.
		metas := r.Profiles.List()
		bundle := backupBundle{Version: 1, Profiles: make([]backupBundleEntry, 0, len(metas))}
		for _, m := range metas {
			content, err := r.Profiles.Read(m.ID)
			if err != nil {
				continue
			}
			entry := backupBundleEntry{Content: content}
			entry.Meta.ID = m.ID
			entry.Meta.Name = m.Name
			entry.Meta.Type = m.Type
			entry.Meta.BaseProfileID = m.BaseProfileID
			entry.Meta.ManagedBy = m.ManagedBy
			entry.Meta.EditorStatus = m.EditorStatus
			bundle.Profiles = append(bundle.Profiles, entry)
		}
		if body.UISettings != nil {
			bundle.UISettings = body.UISettings
		}
		bundleBytes, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		client := webdav.New(webdav.Options{
			URL:      body.Webdav.URL,
			Username: body.Webdav.Username,
			Password: body.Webdav.Password,
		})
		ctx, cancel := context.WithTimeout(req.Context(), webdavTimeout)
		defer cancel()
		// Best-effort directory creation — ignore "already exists" / transient
		// errors so an existing collection doesn't fail the upload.
		if body.Webdav.Dir != "" {
			_ = client.Mkcol(ctx, body.Webdav.Dir)
		}
		path := backupPath(body.Webdav.Dir)
		if err := client.Put(ctx, path, string(bundleBytes)); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": path})
	}
}

func handleRestore(r Router) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Webdav struct {
				URL      string `json:"url"`
				Username string `json:"username"`
				Password string `json:"password"`
				Dir      string `json:"dir"`
			} `json:"webdav"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if body.Webdav.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "webdav.url required"})
			return
		}
		client := webdav.New(webdav.Options{
			URL:      body.Webdav.URL,
			Username: body.Webdav.Username,
			Password: body.Webdav.Password,
		})
		ctx, cancel := context.WithTimeout(req.Context(), webdavTimeout)
		defer cancel()
		raw, err := client.Get(ctx, backupPath(body.Webdav.Dir))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		var bundle backupBundle
		if err := json.Unmarshal([]byte(raw), &bundle); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "parse bundle: " + err.Error()})
			return
		}

		// Recreate each profile via profiles.Create so it gets a fresh id
		// (avoids clashing with any existing local id). Ordinary profiles
		// first, managed overlays last, so the overlay's baseProfileId can
		// be remapped to the base's NEW id.
		ordinary, managed := splitBackupEntries(bundle.Profiles)
		restoredIDs := map[string]string{} // old id → new id
		restored := 0
		for _, p := range append(ordinary, managed...) {
			// Script profiles: this build doesn't run them. We could persist
			// them as inert (compose would skip), but Create() refuses
			// script → so silently DROP them and don't count. Matches the
			// plan §2 砍项: "数据照常存" only applied to migrated in-place
			// index.json; restore from a TS-server backup similarly drops.
			if p.Meta.Type == profile.TypeScript {
				continue
			}
			// Preserve composition types (merge) so restored overlays still
			// apply; 'remote' is intentionally restored as 'local' so we
			// don't re-fetch at restore time (the user may not have network
			// or the URL may have expired).
			t := profile.TypeLocal
			if p.Meta.Type == profile.TypeMerge {
				t = profile.TypeMerge
			}
			in := profile.CreateInput{
				Name:         p.Meta.Name,
				Content:      p.Content,
				Type:         t,
				ManagedBy:    p.Meta.ManagedBy,
				EditorStatus: p.Meta.EditorStatus,
			}
			if p.Meta.BaseProfileID != "" {
				if newID, ok := restoredIDs[p.Meta.BaseProfileID]; ok {
					in.BaseProfileID = newID
				}
			}
			created, err := r.Profiles.Create(in)
			if err != nil {
				// Don't abort the whole restore on one failure — but report
				// the count honestly so the user knows something was skipped.
				continue
			}
			if p.Meta.ID != "" {
				restoredIDs[p.Meta.ID] = created.ID
			}
			restored++
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"restored":   restored,
			"uiSettings": bundle.UISettings,
		})
	}
}

func handleGeoUpdate(r Router) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// Geo download can take a while on a slow link; give it room but cap
		// so a hung connection doesn't wedge the dashboard's spinner forever.
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Minute)
		defer cancel()
		files, err := kernel.FetchGeoAssets(ctx, r.HomeDir, nil)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "files": files})
	}
}

// backupPath renders the bundle's path on the WebDAV server. Empty dir →
// bare filename at the server's base; otherwise <dir>/filename with exactly
// one slash between them.
func backupPath(dir string) string {
	if dir == "" {
		return backupFilename
	}
	end := len(dir)
	for end > 0 && dir[end-1] == '/' {
		end--
	}
	return dir[:end] + "/" + backupFilename
}

// splitBackupEntries partitions entries into ordinary (first) + managed
// (second) so ordinary bases are created before the overlays that reference
// them.
func splitBackupEntries(entries []backupBundleEntry) (ordinary, managed []backupBundleEntry) {
	for _, e := range entries {
		if e.Meta.ManagedBy == profile.ManagedByVisualEditor {
			managed = append(managed, e)
		} else {
			ordinary = append(ordinary, e)
		}
	}
	return
}
