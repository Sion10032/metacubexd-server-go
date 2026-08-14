package auth

import (
	_ "embed"
	"net/http"
)

// loginHTML is the static login page, embedded at build time so the binary
// stays self-contained (no external file dependency). Editing the page is a
// plain HTML/CSS/JS change in login.html — no Go string escaping or template
// syntax to worry about. All login logic (form submit, error display, redirect)
// lives client-side in the page's <script>; the Go side only serves the bytes
// and exposes /api/auth/login as a JSON endpoint.
//
//go:embed login.html
var loginHTML []byte

// serveLoginPage returns the static login page. No template rendering — the
// HTML is byte-for-byte what's in login.html, and all dynamic behavior
// (submit handling, error UI, redirect) is in the page's own <script>.
//
// Styling intentionally mirrors the upstream metacubexd setup.vue (DaisyUI
// semantic palette + the spring-up enter animation) so the login screen feels
// native to the dashboard rather than a tacked-on gate. The page is fully
// self-contained (no external CSS/JS) so it works even when the dashboard
// assets aren't bundled.
func serveLoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Never cache: the page may evolve between releases and a stale cached
	// copy could reference an old /api/auth/* contract.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(loginHTML)
}
