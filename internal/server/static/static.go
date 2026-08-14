// Package static serves the embedded (or external) dashboard UI: hashed
// assets, index.html with SPA fallback, and /config.js with runtime token
// injection. This is the Go counterpart to apps/server/routes/[...].ts +
// routes/config.js.ts in the upstream TS server.
//
// Two serving modes:
//   - UIDist == "" → serve from the embedded web/ FS (go:embed)
//   - UIDist != "" → serve from that directory on disk (UI_DIST override)
//
// SPA fallback: any GET that doesn't match a real file falls through to
// index.html so client-side routes (/proxies, /overview, ...) work on refresh.
package static

import (
	"bytes"
	"encoding/json"
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// webFS holds the dashboard assets bundled at build time. Empty by default —
// operators either populate internal/static/web/ at build time or set UI_DIST
// at runtime to point at the unpacked tarball.
//
//go:embed all:web
var webFS embed.FS

// Config carries the runtime values injected into /config.js.
type Config struct {
	// ControlToken gates /api/control/** auth. Empty = no token required.
	ControlToken string
	// GitHubToken is forwarded to the UI for authenticated GitHub API limits.
	// Empty = omitted.
	GitHubToken string
	// DefaultBackendURL is served verbatim in /config.js when SameOriginClashPath
	// is unset. Use this for a fixed backend address (e.g. an external mihomo).
	// Empty = the dashboard falls back to its build-time default
	// (http://127.0.0.1:9090).
	DefaultBackendURL string
	// SameOriginClashPath, when non-empty, makes /config.js compute
	//   defaultBackendURL = <request scheme>://<request host> + SameOriginClashPath
	// per request. This is what the All-in-One server wants: the dashboard
	// points at its own origin's /api/clash proxy, so the browser never needs
	// to know about port 9090 and the WS upgrade reaches the proxy correctly.
	//
	// Why dynamic: the dashboard's transformEndpointURL prepends `protocol://`
	// to any URL that lacks a scheme, so a path-only value like "/api/clash"
	// becomes the malformed `http:///api/clash` (empty host). The Host header
	// is the only way to know the address the browser used to reach us.
	// Overrides DefaultBackendURL when both are set.
	SameOriginClashPath string
}

// Handler serves the UI. The returned http.Handler covers:
//   - GET /config.js          → runtime JS injection (no-store)
//   - GET <real asset>        → file with MIME + 1y immutable cache
//   - GET <SPA route>         → index.html
//   - non-GET                 → 405
//
// It deliberately does NOT handle /api/** paths; mount it under "/" with the
// API routes registered at higher priority.
type Handler struct {
	root     string // absolute filesystem path ("" when embedded)
	embed    fs.FS  // embedded FS (nil when root != "")
	config   Config
	fileSrv  http.Handler
	indexHTML []byte
}

// New constructs a Handler. If uidDist is "", the embedded web/ tree is used;
// otherwise the on-disk directory takes precedence (UI_DIST override).
func New(uidDist string, cfg Config) (*Handler, error) {
	h := &Handler{config: cfg}

	if uidDist != "" {
		abs, err := filepath.Abs(uidDist)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, &os.PathError{Op: "stat", Path: abs, Err: os.ErrInvalid}
		}
		h.root = abs
		h.fileSrv = http.FileServer(http.Dir(abs))
	} else {
		sub, err := fs.Sub(webFS, "web")
		if err != nil {
			return nil, err
		}
		h.embed = sub
		h.fileSrv = http.FileServer(http.FS(sub))
	}

	if err := h.preloadIndex(); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *Handler) preloadIndex() error {
	b, err := h.readFile("index.html")
	if err != nil {
		// The dashboard isn't bundled in this build — fallback to a stub so
		// /config.js still works and the operator sees a clear error page
		// instead of a 500 on every route. Phase 1 ships without an embedded
		// UI; operators point UI_DIST at the unpacked tarball.
		h.indexHTML = []byte(stubIndexHTML)
		return nil
	}
	h.indexHTML = b
	return nil
}

// readFile fetches a file's contents from whichever source is active.
func (h *Handler) readFile(name string) ([]byte, error) {
	if h.root != "" {
		return os.ReadFile(filepath.Join(h.root, filepath.FromSlash(name)))
	}
	return fs.ReadFile(h.embed, name)
}

// statFile returns the os.FileInfo (disk) or fs.FileInfo (embed) for name,
// or os.ErrNotExist if absent.
func (h *Handler) statFile(name string) (fs.FileInfo, error) {
	if h.root != "" {
		return os.Stat(filepath.Join(h.root, filepath.FromSlash(name)))
	}
	return fs.Stat(h.embed, name)
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Never swallow API routes — those are mounted at higher priority by main,
	// but defend against accidental catch-all here too.
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// /config.js is synthesized at request time so token changes take effect
	// across restarts without rebuilding.
	if r.URL.Path == "/config.js" {
		h.serveConfigJS(w, r)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		h.serveIndex(w, r)
		return
	}

	// Reject obvious directory traversals. http.FileServer already defends
	// against ../, but we serve SPA fallback for unknown paths, so an
	// attacker-controlled path must not leak filesystem metadata.
	clean := path.Clean("/" + name)
	if strings.Contains(clean, "..") {
		http.NotFound(w, r)
		return
	}

	if info, err := h.statFile(name); err == nil && !info.IsDir() {
		// Real file — let http.FileServer set headers (Content-Type, ETag, ...).
		// Hashed assets under _nuxt/ are immutable; the UI's build emits a
		// long max-age via cache-control headers in its asset manifest, but
		// for non-hashed files (config.js handled above, index.html, manifest)
		// we want no-cache. http.FileServer doesn't add Cache-Control by
		// default, which is correct for the safe-default case.
		h.fileSrv.ServeHTTP(w, r)
		return
	}

	// SPA fallback: anything else is a client-side route.
	h.serveIndex(w, r)
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// SPA shell must never be cached or a stale asset hash can wedge the UI
	// after an upgrade (#2063-style bug).
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(h.indexHTML)
}

func (h *Handler) serveConfigJS(w http.ResponseWriter, r *http.Request) {
	cfg := map[string]any{}
	// defaultBackendURL resolution mirrors the priority order the dashboard's
	// useConnect.ts expects: an explicit absolute URL wins; otherwise we let
	// the field be absent so the UI falls back to its build-time default.
	if h.config.SameOriginClashPath != "" {
		cfg["defaultBackendURL"] = originURL(r) + h.config.SameOriginClashPath
	} else if h.config.DefaultBackendURL != "" {
		cfg["defaultBackendURL"] = h.config.DefaultBackendURL
	}
	if h.config.ControlToken != "" {
		cfg["controlToken"] = h.config.ControlToken
	}
	if h.config.GitHubToken != "" {
		cfg["githubToken"] = h.config.GitHubToken
	}

	// Marshal manually so the output is stable and human-readable (matches the
	// TS `JSON.stringify` output, which the dashboard parses via a <script> tag).
	buf, err := json.Marshal(cfg)
	if err != nil {
		http.Error(w, "config marshal failed", http.StatusInternalServerError)
		return
	}

	// Indent for debuggability in DevTools; the TS server emits a single-line
	// assignment which is fine too — indented just makes operator inspection
	// easier.
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, buf, "", "  "); err != nil {
		pretty.Write(buf)
	}

	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	// Never cache: tokens must track env across restarts.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("window.__METACUBEXD_CONFIG__ = "))
	_, _ = w.Write(pretty.Bytes())
	_, _ = w.Write([]byte("\n"))
}

// originURL returns the scheme://host the client used to reach this server,
// honoring X-Forwarded-Proto for TLS-terminating reverse proxies. r.Host is
// authoritative (Go populates it from the Host header, falling back to the
// listen address if the client sent none) so we don't need to splice the
// CONTROL_PORT ourselves.
func originURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// A reverse proxy in front of us (nginx, Caddy, Cloudflare) terminates TLS
	// and forwards over plain HTTP; without this, /config.js would emit http://
	// and the browser would block mixed-content WS upgrades (wss:// expected).
	if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "" {
		// Strip any trailing list ("https, http") — take the first hop.
		if i := indexByte(xfp, ','); i >= 0 {
			xfp = xfp[:i]
		}
		scheme = trimASCIISpace(xfp)
	}
	return scheme + "://" + r.Host
}

// indexByte / trimASCIISpace avoid pulling strings + bytes for one call each.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trimASCIISpace(s string) string {
	start, end := 0, len(s)
	for start < end && isASCIISpaceByte(s[start]) {
		start++
	}
	for end > start && isASCIISpaceByte(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isASCIISpaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// stubIndexHTML is shown when no UI is bundled and UI_DIST is not set. It
// renders a minimal error page so operators know where to put the tarball.
const stubIndexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>metacubexd server (no UI)</title>
<style>body{font:14px/1.5 -apple-system,system-ui,sans-serif;margin:2rem;max-width:48rem;color:#333}code{background:#f4f4f4;padding:.15rem .3rem;border-radius:3px}</style>
</head>
<body>
<h1>metacubexd server is running — but no dashboard is bundled</h1>
<p>This binary was built without an embedded UI, and <code>UI_DIST</code> is not set.</p>
<p>To serve the dashboard, either:</p>
<ul>
<li>Set <code>UI_DIST=/path/to/unpacked/tarball</code> pointing at the
<code>compressed-dist.tgz</code> contents from
<a href="https://github.com/metacubex/metacubexd/releases">metacubexd releases</a>, or</li>
<li>Rebuild with <code>internal/static/web/</code> populated (go:embed).</li>
</ul>
<p>The control + Clash proxy APIs are still reachable under <code>/api/</code>.</p>
</body>
</html>
`
