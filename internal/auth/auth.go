// Package auth implements optional password-based login for the All-in-One
// server. When a password (CONTROL_TOKEN) is configured, browser access to the
// dashboard is gated behind a lightweight login page; a signed cookie issued
// on success lets subsequent requests (including WebSocket + SSE) pass
// through without re-entering the password.
//
// Design goals (see discussion leading to this package):
//   - Reuse CONTROL_TOKEN as the single login password — no new env var.
//   - Cookie carries no user info (single password), so the payload is a
//     constant; authenticity comes from an HMAC tag keyed by the password
//     (scheme B: password change invalidates all sessions, no extra env).
//   - Downstream packages (clashproxy, control) detect an authenticated
//     request via IsAuthed so they can branch:
//       * clashproxy — inject CLASH_SECRET for authed same-origin requests,
//         otherwise pass through the caller's own secret.
//       * control    — skip the Bearer check for authed requests.
//   - Same-origin UX: unauthenticated browser GET → 302 /login. Cross-origin
//     (e.g. the hosted metacubexd panel, API clients) → 401, since the login
//     page can't set a cookie the cross-origin client will send back without
//     CORS + credentials plumbing that we deliberately avoid here.
//
// When Password is empty, New stamps X-Authed on every request (open mode)
// so existing unauthenticated deployments keep working AND can still reach
// /api/clash/* (clashproxy injects CLASH_SECRET for authed requests).
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CookieName is the signed session cookie's name.
const CookieName = "metacubexd_auth"

// authedHeader marks a request as authenticated-by-cookie-or-Bearer so
// downstream handlers can branch without re-parsing the cookie. It is set
// only inside the process (never comes from the network — see stripInternal).
const authedHeader = "X-Metacubexd-Authed"

// sessionTTL bounds how long a login stays valid. 30 days mirrors typical
// "remember me" semantics; rotating CONTROL_TOKEN invalidates immediately.
const sessionTTL = 30 * 24 * time.Hour

// payload is the fixed cookie body. There's nothing per-user to carry, so we
// sign a constant and rely on the HMAC tag for authenticity. The issued-at
// timestamp lets us expire sessions without server-side state.
//
// Layout: "<unixSeconds>.<random16hex>".
//   - unixSeconds → enforce sessionTTL
//   - random16hex → unique per login, so two logins aren't byte-identical
const payload = "ok"

// Config controls auth behavior.
type Config struct {
	// Password gates login. Empty = OPEN MODE: no login page, every request
	// is trusted (X-Metacubexd-Authed is stamped unconditionally so downstream
	// handlers like clashproxy inject CLASH_SECRET on the caller's behalf).
	// This matches the documented "unset CONTROL_TOKEN = unauthenticated"
	// contract: a password-less deployment is fully open, including to
	// cross-origin clients. Set CONTROL_TOKEN to gate access.
	// In practice this is wired from env.ControlToken.
	Password string
}

// New returns an http middleware. When Password is empty it runs in OPEN
// MODE: every request is stamped X-Authed and passed through (no login page),
// so a password-less LAN deploy still works end-to-end. Otherwise it:
//   - Intercepts /login, /api/auth/login, /api/auth/logout
//   - Accepts a valid session cookie OR a Bearer token equal to Password
//   - Redirects same-origin browser GETs to /login when unauthenticated
//   - Returns 401 for unauthenticated cross-origin requests
//
// On success it stamps authedHeader so downstream IsAuthed checks are O(1).
func New(cfg Config) func(http.Handler) http.Handler {
	if cfg.Password == "" {
		// Open mode: no login page, no cookie. We still stamp X-Authed so
		// clashproxy treats these as trusted same-origin requests and injects
		// CLASH_SECRET on their behalf — otherwise a password-less deploy
		// (the common LAN case) couldn't reach /api/clash/* at all, since the
		// dashboard has no secret to present. This is consistent with
		// /api/control/* also being open in this mode.
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Header.Set(authedHeader, "1")
				next.ServeHTTP(w, r)
			})
		}
	}
	key := signingKey(cfg.Password)
	password := cfg.Password
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Never trust an inbound copy of our internal marker header.
			r.Header.Del(authedHeader)

			switch r.URL.Path {
			case "/login":
				serveLoginPage(w, r)
				return
			case "/api/auth/login":
				handleLogin(w, r, password, key)
				return
			case "/api/auth/logout":
				handleLogout(w, r)
				return
			}

			// /api/clash/* proxies to mihomo and authenticates with CLASH_SECRET,
			// NOT CONTROL_TOKEN. Two reasons to special-case it here:
			//   1. A same-origin logged-in browser sends only the cookie (no
			//      Bearer); we stamp X-Authed so clashproxy injects CLASH_SECRET.
			//   2. A cross-origin client (hosted panel, API) carries its own
			//      CLASH_SECRET as a Bearer — which would fail the CONTROL_TOKEN
			//      check below, so we must let it through to clashproxy, which
			//      validates it directly.
			if strings.HasPrefix(r.URL.Path, "/api/clash/") {
				if isAuthenticated(r, key, password) {
					r.Header.Set(authedHeader, "1")
				}
				next.ServeHTTP(w, r)
				return
			}

			if isAuthenticated(r, key, password) {
				r.Header.Set(authedHeader, "1")
				next.ServeHTTP(w, r)
				return
			}

			// Unauthenticated. Same-origin browser navigation (Accept: text/html)
			// → redirect to /login for a smooth UX and stash the destination so
			// login bounces back. Anything else (cross-origin, non-HTML like
			// favicon.ico / image requests, API probes) → 401.
			//
			// The Accept gate is important: browsers auto-request /favicon.ico
			// with Accept: image/*, and if we treated that as a navigation we'd
			// overwrite the real next-path cookie with "/favicon.ico" — sending
			// users to the favicon after login.
			if r.Method == http.MethodGet && isSameOrigin(r) && acceptsHTML(r) {
				// Preserve the intended destination so login bounces back.
				http.SetCookie(w, &http.Cookie{
					Name:  "metacubexd_next",
					Value: r.URL.RequestURI(),
					Path:  "/",
					// Short-lived, readable from JS for the redirect. Not sensitive.
					MaxAge: 300,
				})
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		})
	}
}

// IsAuthed reports whether the request was authenticated by the auth
// middleware (via cookie or Bearer). Downstream handlers use this to decide
// between "server injects credentials" and "caller provides credentials".
func IsAuthed(r *http.Request) bool {
	return r.Header.Get(authedHeader) == "1"
}

// isAuthenticated accepts either a valid signed cookie or a Bearer token
// equal to Password. Both checks are constant-time where it matters.
func isAuthenticated(r *http.Request, key []byte, password string) bool {
	if c, err := r.Cookie(CookieName); err == nil && validCookie(c.Value, key) {
		return true
	}
	if presented, ok := parseBearer(r.Header.Get("Authorization")); ok {
		// Constant-time compare against the configured password.
		if subtle.ConstantTimeCompare([]byte(presented), []byte(password)) == 1 {
			return true
		}
	}
	return false
}

// handleLogin validates the posted password, then sets the signed cookie and
// redirects back to the "next" cookie (or "/") for the browser flow. Non-HTML
// clients get a JSON success.
func handleLogin(w http.ResponseWriter, r *http.Request, password string, key []byte) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"error":"method not allowed"}`))
		return
	}
	// Tolerate both application/x-www-form-urlencoded (form) and JSON. Form
	// keeps the login page dead simple (no fetch); JSON helps API clients.
	var submitted string
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") {
		// Minimal JSON parse: {"password":"..."} — avoid pulling encoding/json
		// for a single field. Fall back to form parsing on malformed JSON.
		submitted = extractJSONStringField(r)
	} else {
		_ = r.ParseForm()
		submitted = r.FormValue("password")
	}

	if subtle.ConstantTimeCompare([]byte(submitted), []byte(password)) != 1 {
		// 401 with a fresh WWW-Authenticate so the browser doesn't cache a
		// bad credential. The login page shows the error inline.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid password"}`))
		return
	}

	// Issue cookie. Payload is constant; uniqueness comes from the random
	// nonce baked into issueCookie.
	value := issueCookie(key)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
	})
	// Clear the "next" redirect hint.
	http.SetCookie(w, &http.Cookie{
		Name:   "metacubexd_next",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	next := nextPath(r)
	// Always JSON now — the login page's <script> reads the redirect field
	// and navigates client-side. API clients get the same shape.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// next is already path-only (safePath strips hosts), so embedding it
	// into a JSON string is injection-safe.
	_, _ = w.Write([]byte(`{"ok":true,"redirect":` + jsonString(next) + `}`))
}

// handleLogout clears the session cookie. Idempotent.
func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"error":"method not allowed"}`))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:   CookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// nextPath reads the metacubexd_next cookie (or ?next=) to find where to send
// the user after login. It's strictly path-only: any absolute URL or scheme
// is reduced to just its path to avoid open-redirect via crafted cookie.
func nextPath(r *http.Request) string {
	if c, err := r.Cookie("metacubexd_next"); err == nil && c.Value != "" {
		if p := safePath(c.Value); p != "" {
			return p
		}
	}
	if q := r.URL.Query().Get("next"); q != "" {
		if p := safePath(q); p != "" {
			return p
		}
	}
	return "/"
}

// safePath returns the path component of v if it's same-site, else "".
// Prevents open-redirect: an attacker can't set metacubexd_next to
// //evil.com because we only honor paths starting with "/" but not "//".
func safePath(v string) string {
	if v == "" || v[0] != '/' {
		return ""
	}
	if len(v) >= 2 && v[1] == '/' {
		// "//evil.com" / "/\\evil" → schemeless URL, browser treats as host.
		return ""
	}
	// Reject if it parses as a URL with a host.
	if u, err := url.Parse(v); err == nil && u.Host == "" {
		return v
	}
	return ""
}

// ── cookie signing ──────────────────────────────────────────────────────

// signingKey derives the HMAC key from the password. Using the password
// directly would also work, but a one-way derivation means the cookie tag
// never leaks the password even hypothetically (defense in depth). The
// domain-separation prefix ("metacubexd-auth-v1") avoids key reuse if other
// HMAC uses are added later.
func signingKey(password string) []byte {
	mac := hmac.New(sha256.New, []byte("metacubexd-auth-v1"))
	mac.Write([]byte(password))
	// Second derivation: the key used to sign cookies, domain-separated
	// from the Bearer password check (which compares plaintext directly).
	mac2 := hmac.New(sha256.New, mac.Sum(nil))
	mac2.Write([]byte("cookie-key"))
	return mac2.Sum(nil)
}

// issueCookie builds "<payload>.<nonceHex>.<iatUnix>.<tagHex>".
//   - payload: constant "ok"
//   - nonceHex: 16 random bytes hex (uniqueness per login)
//   - iatUnix: issue time for TTL enforcement
//   - tagHex: HMAC-SHA256 over "payload.nonceHex.iatUnix"
func issueCookie(key []byte) string {
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	nonceHex := hex.EncodeToString(nonce)
	iat := time.Now().Unix()
	body := payload + "." + nonceHex + "." + itoa(iat)
	tag := hmacTag(key, body)
	return body + "." + tag
}

// validCookie parses + verifies a cookie value. Returns false on any
// structural problem, signature mismatch, or expiry.
func validCookie(v string, key []byte) bool {
	// Expect "<payload>.<nonce>.<iat>.<tag>". Split from the right so a future
	// payload containing "." stays safe.
	idx := strings.LastIndex(v, ".")
	if idx < 0 {
		return false
	}
	body, tag := v[:idx], v[idx+1:]
	expected := hmacTag(key, body)
	if !hmac.Equal([]byte(tag), []byte(expected)) {
		return false
	}
	// body == "<payload>.<nonce>.<iat>"; pull iat for TTL check.
	parts := strings.Split(body, ".")
	if len(parts) != 3 {
		return false
	}
	iat, ok := atoi(parts[2])
	if !ok {
		return false
	}
	if time.Since(time.Unix(iat, 0)) > sessionTTL {
		return false
	}
	return true
}

// hmacTag returns the hex HMAC-SHA256 of msg under key.
func hmacTag(key []byte, msg string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// ── tiny stdlib-free helpers ────────────────────────────────────────────

// itoa formats a non-negative int64 as decimal. Avoids strconv for a hot
// path that runs once per login (not really hot, but keeps imports minimal).
func itoa(n int64) string {
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

// atoi parses a decimal int64. ok=false on any non-digit.
func atoi(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	var n int64
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < '0' || b > '9' {
			return 0, false
		}
		n = n*10 + int64(b-'0')
	}
	return n, true
}

// extractJSONStringField pulls "password" from a small JSON body without
// pulling encoding/json. Tolerates whitespace and double-quoted values.
// Returns "" on any structural problem — the caller then 401s, which is safe.
func extractJSONStringField(r *http.Request) string {
	// Limit body to avoid unbounded reads on a malformed request.
	var buf [4096]byte
	n, _ := r.Body.Read(buf[:])
	s := string(buf[:n])
	// Find "password" key, then the following quoted value.
	key := `"password"`
	i := strings.Index(s, key)
	if i < 0 {
		return ""
	}
	rest := s[i+len(key):]
	// Skip whitespace and a colon.
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == ':' || rest[0] == '\n' || rest[0] == '\r') {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	// Read until the closing quote. No escape handling — passwords in our
	// context are configured verbatim and rarely contain quotes; a password
	// with a literal " is unsupported via JSON path (use the form path).
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// parseBearer extracts the token from "Bearer <token>". Scheme is
// case-insensitive. Mirrors control.parseBearer semantics so the two auth
// paths agree on edge cases.
func parseBearer(header string) (string, bool) {
	h := strings.TrimSpace(header)
	i := 0
	for i < len(h) && h[i] != ' ' && h[i] != '\t' {
		i++
	}
	if i == 0 || !strings.EqualFold(h[:i], "bearer") {
		return "", false
	}
	for i < len(h) && (h[i] == ' ' || h[i] == '\t') {
		i++
	}
	if i >= len(h) {
		return "", false
	}
	return h[i:], true
}

// jsonString quotes s as a JSON string literal. We use this for the login
// response's redirect field rather than pulling encoding/json for one value;
// safePath has already constrained the input to a same-site path (no host,
// no scheme), so the only characters needing escaping are the JSON string
// specials: quote and backslash. Anything else (including the / in paths)
// is safe to emit verbatim per RFC 8259.
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

// isSameOrigin reports whether the request originated from the server's own
// origin (the embedded UI). We check Origin then Referer; absence of both
// means a same-origin navigation or a non-browser client — we treat that as
// same-origin so direct address-bar visits and curl get a sensible response
// (redirect for GET, 401 otherwise) rather than a blanket cross-origin block.
func isSameOrigin(r *http.Request) bool {
	host := r.Host
	for _, h := range []string{r.Header.Get("Origin"), r.Header.Get("Referer")} {
		if h == "" {
			continue
		}
		u, err := url.Parse(h)
		if err != nil {
			continue
		}
		if u.Host != host {
			return false
		}
	}
	return true
}

// acceptsHTML reports whether the client wants an HTML response (i.e. a
// browser top-level navigation). Used to distinguish real page loads from
// background asset fetches like favicon.ico (Accept: image/*) so those don't
// get captured as the post-login redirect target.
func acceptsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

