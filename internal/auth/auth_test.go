package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNewDisabledWhenPasswordEmpty guards the open-mode contract: when no
// password is configured the middleware passes every request through AND
// stamps X-Authed, so downstream handlers (clashproxy) treat them as trusted
// and inject CLASH_SECRET on their behalf. Without the stamp, a password-less
// LAN deploy couldn't reach /api/clash/* at all.
func TestNewDisabledWhenPasswordEmpty(t *testing.T) {
	called := false
	var sawAuthed bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		sawAuthed = IsAuthed(r)
	})
	mw := New(Config{Password: ""})(next)

	req := httptest.NewRequest("GET", "/anything", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if !called {
		t.Fatal("downstream not called when auth is disabled")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (pass-through)", w.Code)
	}
	if !sawAuthed {
		t.Error("open mode did not stamp X-Authed; /api/clash/* would 401 in a password-less deploy")
	}
}

// TestLoginSuccessForm exercises the form-post flow: POST the password,
// expect a 200 JSON response with {ok:true, redirect:"/"} plus a Set-Cookie
// for the session. The login page's <script> consumes this shape.
func TestLoginSuccessForm(t *testing.T) {
	mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be hit during login")
	}))

	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("password=s3cret"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"ok":true`) {
		t.Errorf("body = %q, want ok:true", body)
	}
	if !strings.Contains(body, `"redirect":"/"`) {
		t.Errorf("body = %q, want redirect:/", body)
	}
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == CookieName {
			found = true
			if !c.HttpOnly {
				t.Error("cookie not HttpOnly (XSS risk)")
			}
			if c.SameSite != http.SameSiteStrictMode {
				t.Errorf("SameSite = %v, want Strict", c.SameSite)
			}
			if c.Path != "/" {
				t.Errorf("Path = %q, want /", c.Path)
			}
		}
		if c.Name == "metacubexd_next" {
			if c.MaxAge >= 0 {
				t.Error("metacubexd_next not cleared on login")
			}
		}
	}
	if !found {
		t.Error("session cookie not set")
	}
}

// TestLoginWrongPassword verifies a bad password yields 401 and no cookie.
func TestLoginWrongPassword(t *testing.T) {
	mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be hit")
	}))

	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("password=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			t.Fatal("session cookie set on failed login")
		}
	}
}

// TestLoginJSON verifies the JSON API path accepts {"password":"..."}.
func TestLoginJSON(t *testing.T) {
	mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be hit")
	}))

	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"s3cret"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Errorf("body = %q, want JSON ok", w.Body.String())
	}
}

// TestCookieAuth: log in, then use the issued cookie to reach a downstream
// handler. The handler must run AND see the X-Metacubexd-Authed marker.
func TestCookieAuth(t *testing.T) {
	called := false
	mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if !IsAuthed(r) {
			t.Error("IsAuthed false inside downstream after cookie auth")
		}
	}))

	// Step 1: log in to get the cookie.
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("password=s3cret"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	var session string
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			session = c.Value
		}
	}
	if session == "" {
		t.Fatal("no session cookie issued")
	}

	// Step 2: use the cookie on a real path. /api/control/* is gated by
	// CONTROL_TOKEN, so a valid cookie must let it through AND set the marker.
	req2 := httptest.NewRequest("GET", "/api/control/kernel/status", nil)
	req2.AddCookie(&http.Cookie{Name: CookieName, Value: session})
	w2 := httptest.NewRecorder()
	mw.ServeHTTP(w2, req2)

	if !called {
		t.Fatal("downstream not called with valid cookie")
	}
	if w2.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w2.Code)
	}
}

// TestBearerAuth: a Bearer token equal to the password authenticates without
// a cookie. This is the cross-origin API/panel-client path.
func TestBearerAuth(t *testing.T) {
	called := false
	mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("GET", "/api/control/kernel/status", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if !called {
		t.Fatal("downstream not called with valid Bearer")
	}
}

// TestUnauthedSameOriginGETRedirects: a same-origin browser navigation
// (Accept: text/html) without a cookie should 302 to /login (the smooth
// UX path) and stash the destination in metacubexd_next.
func TestUnauthedSameOriginGETRedirects(t *testing.T) {
	mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be hit")
	}))

	req := httptest.NewRequest("GET", "/overview", nil)
	// Real browser navigations always send Accept: text/html.
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	// No Origin/Referer → treated as same-origin (direct address-bar visit).
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if w.Header().Get("Location") != "/login" {
		t.Errorf("Location = %q, want /login", w.Header().Get("Location"))
	}
	// Verify the destination was stashed for post-login redirect.
	var next string
	for _, c := range w.Result().Cookies() {
		if c.Name == "metacubexd_next" {
			next = c.Value
		}
	}
	if next != "/overview" {
		t.Errorf("metacubexd_next = %q, want /overview", next)
	}
}

// TestFaviconDoesNotStealNextPath: browsers auto-request /favicon.ico with
// Accept: image/* on every page load. If we treated that as a navigation and
// stashed it in metacubexd_next, the cookie would get overwritten and the
// user would be sent to /favicon.ico after login. Verify favicon gets 401
// (not a redirect) and does NOT set the next cookie.
func TestFaviconDoesNotStealNextPath(t *testing.T) {
	mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be hit")
	}))

	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (favicon is not a navigation)", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "metacubexd_next" {
			t.Errorf("favicon request set metacubexd_next = %q (would hijack post-login redirect)", c.Value)
		}
	}
}

// TestUnauthedCrossOriginReturns401: a cross-origin request (different Host
// in Origin) without credentials should get 401, not a redirect.
func TestUnauthedCrossOriginReturns401(t *testing.T) {
	mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be hit")
	}))

	req := httptest.NewRequest("GET", "/api/control/kernel/status", nil)
	req.Host = "my-server:8080"
	req.Header.Set("Origin", "https://metacubexd.pages.dev")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// TestLoginRoutesBypassAuth: /login, /api/auth/login, /api/auth/logout must be
// reachable WITHOUT a cookie — otherwise users could never log in.
func TestLoginRoutesBypassAuth(t *testing.T) {
	mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be hit for login routes")
	}))

	paths := []struct {
		method string
		path   string
		want   int
	}{
		{"GET", "/login", 200}, // serves static page
		// /api/auth/login POST is exercised elsewhere; here just verify it
		// doesn't 401 before processing (a wrong password gives 401, which is
		// the handler's own response, not the middleware's).
		{"GET", "/api/auth/logout", 200}, // clears cookie, returns JSON
	}
	for _, p := range paths {
		req := httptest.NewRequest(p.method, p.path, nil)
		req.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != p.want {
			t.Errorf("%s %s: status = %d, want %d", p.method, p.path, w.Code, p.want)
		}
	}
}

// TestCookieTampering: flipping one byte in the cookie value must fail auth.
// Guards against forgery.
func TestCookieTampering(t *testing.T) {
	key := signingKey("s3cret")
	good := issueCookie(key)
	tampered := good[:len(good)-2] + "ff" // corrupt the last hex byte of tag
	if validCookie(tampered, key) {
		t.Error("validCookie accepted a tampered cookie")
	}
}

// TestCookieExpiry: a cookie older than sessionTTL must be rejected.
func TestCookieExpiry(t *testing.T) {
	key := signingKey("s3cret")
	// Build a cookie whose iat is well past TTL. We can't call issueCookie with
	// a custom time, so reconstruct the payload manually.
	oldIAT := time.Now().Add(-(sessionTTL + time.Hour)).Unix()
	nonce := "deadbeefdeadbeefdeadbeefdeadbeef"
	body := payload + "." + nonce + "." + itoa(oldIAT)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(body))
	tag := strings.Builder{}
	for _, b := range mac.Sum(nil) {
		tag.WriteByte(hexNibble(b >> 4))
		tag.WriteByte(hexNibble(b))
	}
	expired := body + "." + tag.String()
	if validCookie(expired, key) {
		t.Error("validCookie accepted an expired cookie")
	}
}

// TestSafePath guards the open-redirect defense for the next-path cookie.
func TestSafePath(t *testing.T) {
	cases := map[string]string{
		"/":              "/",
		"/overview":      "/overview",
		"":               "",
		"//evil.com":     "", // protocol-relative URL
		"/\\evil":        "/\\evil",
		"http://x/":      "",
		"https://evil/":  "",
		"relative":       "", // doesn't start with /
	}
	for in, want := range cases {
		got := safePath(in)
		if got != want {
			t.Errorf("safePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// hexNibble is a test-only helper to format a 0-15 nibble as a hex char.
func hexNibble(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'a' + n - 10
}

// TestLoginPageRenders: /login returns the static HTML with the form and
// the JS hook in place. The Go side no longer renders anything — all
// dynamic behavior is client-side — so we only assert the page contains the
// elements the <script> relies on.
func TestLoginPageRenders(t *testing.T) {
	mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be hit")
	}))

	req := httptest.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	// The form is JS-driven now (no action attr); assert the ids the <script>
	// hooks into are present.
	for _, want := range []string{
		`id="login-form"`,
		`id="password"`,
		`id="submit"`,
		`name="password"`,
		`/api/auth/login`, // referenced in the fetch call
	} {
		if !strings.Contains(body, want) {
			t.Errorf("login page missing %q", want)
		}
	}
}

// TestInboundAuthedHeaderStripped: an attacker sending X-Metacubexd-Authed
// from the outside must NOT bypass auth. The middleware must delete it.
func TestInboundAuthedHeaderStripped(t *testing.T) {
	called := false
	mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("GET", "/api/control/kernel/status", nil)
	req.Header.Set("X-Metacubexd-Authed", "1") // spoofed
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if called {
		t.Fatal("spoofed X-Metacubexd-Authed bypassed auth")
	}
	// Same-origin unauthenticated GET redirects to /login; cross-origin or
	// non-GET would 401. Either way, NOT 200 (downstream must not run).
	if w.Code == http.StatusOK {
		t.Errorf("status = 200; downstream ran with spoofed header")
	}
}

// TestClashPathBypassesControlAuth: /api/clash/* authenticates with
// CLASH_SECRET (handled by clashproxy), NOT with CONTROL_TOKEN. The auth
// middleware must let these requests through to clashproxy even when the
// caller carries no CONTROL_TOKEN cookie/Bearer — otherwise cross-origin
// panel clients (which only know CLASH_SECRET) could never reach the proxy.
//
// We verify:
//   1. An unauthenticated /api/clash/* request reaches downstream (no 401,
//      no redirect). clashproxy will then do its own CLASH_SECRET check.
//   2. A same-origin request with a valid cookie still gets the X-Authed
//      marker set, so clashproxy knows to inject CLASH_SECRET rather than
//      demand it from the caller.
func TestClashPathBypassesControlAuth(t *testing.T) {
	t.Run("unauthenticated clash request reaches downstream", func(t *testing.T) {
		hit := false
		mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hit = true
			if IsAuthed(r) {
				t.Error("IsAuthed should be false for an unauthenticated clash request")
			}
		}))

		req := httptest.NewRequest("GET", "/api/clash/version", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if !hit {
			t.Fatal("downstream not reached; /api/clash/* must bypass CONTROL_TOKEN auth")
		}
	})

	t.Run("authenticated clash request keeps X-Authed marker", func(t *testing.T) {
		// Log in first to obtain a cookie.
		mw := New(Config{Password: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !IsAuthed(r) {
				t.Error("IsAuthed false for authenticated clash request")
			}
		}))
		req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("password=s3cret"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		var session string
		for _, c := range w.Result().Cookies() {
			if c.Name == CookieName {
				session = c.Value
			}
		}
		if session == "" {
			t.Fatal("no session cookie issued")
		}

		req2 := httptest.NewRequest("GET", "/api/clash/version", nil)
		req2.AddCookie(&http.Cookie{Name: CookieName, Value: session})
		w2 := httptest.NewRecorder()
		mw.ServeHTTP(w2, req2)
	})
}
