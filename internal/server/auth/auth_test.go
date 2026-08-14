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
// password is configured the middleware is a pure pass-through (every request
// reaches downstream, no checks). This is the documented "unset
// CONTROL_TOKEN = unauthenticated" contract.
func TestNewDisabledWhenPasswordEmpty(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	mw := New(Config{ControlToken: ""})(next)

	req := httptest.NewRequest("GET", "/anything", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if !called {
		t.Fatal("downstream not called when auth is disabled")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (pass-through)", w.Code)
	}
}

// TestLoginSuccessForm exercises the form-post flow: POST the password,
// expect a 200 JSON response with {ok:true, redirect:"/"} plus a Set-Cookie
// for the session. The login page's <script> consumes this shape.
func TestLoginSuccessForm(t *testing.T) {
	mw := New(Config{ControlToken: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	mw := New(Config{ControlToken: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	mw := New(Config{ControlToken: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
// handler. The handler must run.
func TestCookieAuth(t *testing.T) {
	called := false
	mw := New(Config{ControlToken: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
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
	// CONTROL_TOKEN, so a valid cookie must let it through.
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
	mw := New(Config{ControlToken: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	mw := New(Config{ControlToken: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	mw := New(Config{ControlToken: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	mw := New(Config{ControlToken: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	mw := New(Config{ControlToken: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	mw := New(Config{ControlToken: "s3cret"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// TestClashAccessControl verifies /api/clash/* accepts three credential
// paths and rejects the rest.
func TestClashAccessControl(t *testing.T) {
	mw := New(Config{
		ControlToken:    "s3cret",
		ClashSecret: "clashpw",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Helper to run one request and check the status.
	run := func(name string, req *http.Request, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != want {
			t.Errorf("%s: status = %d, want %d", name, w.Code, want)
		}
	}

	// Log in to get a session cookie for the cookie test.
	loginReq := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("password=s3cret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginW := httptest.NewRecorder()
	mw.ServeHTTP(loginW, loginReq)
	var session string
	for _, c := range loginW.Result().Cookies() {
		if c.Name == CookieName {
			session = c.Value
		}
	}
	if session == "" {
		t.Fatal("no session cookie issued")
	}

	// 1. Valid login cookie → 200 (same-origin dashboard path).
	cookieReq := httptest.NewRequest("GET", "/api/clash/version", nil)
	cookieReq.AddCookie(&http.Cookie{Name: CookieName, Value: session})
	run("cookie", cookieReq, http.StatusOK)

	// 2. CLASH_SECRET as Bearer → 200 (cross-origin HTTP API path).
	bearerReq := httptest.NewRequest("GET", "/api/clash/version", nil)
	bearerReq.Header.Set("Authorization", "Bearer clashpw")
	run("bearer=clashpw", bearerReq, http.StatusOK)

	// 3. CLASH_SECRET as ?token= → 200 (WS/SSE path, can't set headers).
	tokenReq := httptest.NewRequest("GET", "/api/clash/traffic?token=clashpw", nil)
	run("?token=clashpw", tokenReq, http.StatusOK)

	// 4. Wrong CLASH_SECRET, no cookie → 401.
	wrongReq := httptest.NewRequest("GET", "/api/clash/version", nil)
	wrongReq.Header.Set("Authorization", "Bearer wrong")
	run("wrong secret", wrongReq, http.StatusUnauthorized)

	// 5. No credentials at all → 401.
	noneReq := httptest.NewRequest("GET", "/api/clash/version", nil)
	run("no credentials", noneReq, http.StatusUnauthorized)
}

// TestPublicPathsBypassAuth: PublicPaths are reachable without any
// credentials (capability probes like /api/control/health).
func TestPublicPathsBypassAuth(t *testing.T) {
	called := false
	mw := New(Config{
		ControlToken:    "s3cret",
		PublicPaths: []string{"/api/control/health", "/api/control/info"},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	for _, p := range []string{"/api/control/health", "/api/control/info"} {
		called = false
		req := httptest.NewRequest("GET", p, nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if !called {
			t.Errorf("%s was not let through PublicPaths", p)
		}
	}

	// A non-public control path without credentials must still be rejected.
	called = false
	req := httptest.NewRequest("GET", "/api/control/kernel/status", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if called {
		t.Error("non-public path was let through without credentials")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("non-public path: status = %d, want 401", w.Code)
	}
}
