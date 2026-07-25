// Package clashproxy implements the same-origin Clash API proxy: HTTP requests
// to /api/clash/* are forwarded to mihomo's external-controller, and the four
// server-push WebSocket endpoints (/traffic, /memory, /connections, /logs) are
// bridged so the dashboard never has to know about port 9090.
//
// Why this exists: the upstream TS server cannot proxy the WS endpoints
// because Nitro's routeRules.proxy drops WebSocket upgrades (nitrojs/nitro#2886).
// Go has no such limitation — httputil.ReverseProxy handles HTTP, and
// gorilla/websocket gives us a ~80-line bidirectional bridge for the WS paths.
package clashproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// StateFunc returns the supervisor's current view of the kernel: the upstream
// address to dial and the secret to attach as Authorization. The proxy calls
// it on every request so a kernel restart (which re-injects these into
// active.yaml) is reflected immediately.
type StateFunc func() (upstream string, secret string, running bool)

// wsEndpoints are the dashboard's server-push streams. The proxy bridges them
// at /api/clash/<name>. Anything else under /api/clash/ is plain HTTP.
var wsEndpoints = map[string]struct{}{
	"traffic":     {},
	"memory":      {},
	"connections": {},
	"log":         {}, // mihomo exposes /logs (plural); the UI may use either
	"logs":        {},
}

// New returns an http.Handler that proxies /api/clash/* to mihomo.
//
// The handler expects r.URL.Path to STILL contain the /api/clash prefix — chi
// mounts it without stripping. Internally we strip it when building the
// upstream path so mihomo sees /version, /traffic, etc.
func New(state StateFunc) http.Handler {
	rp := &httputil.ReverseProxy{
		Director:  director(state),
		Transport: http.DefaultTransport,
		//ErrorHandler is invoked when the upstream is unreachable. The default
		//(log + 502) is fine; the dashboard's retry layer handles transient 5xx.
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/clash/", func(w http.ResponseWriter, r *http.Request) {
		upstreamAddr, secret, running := state()
		if !running {
			// Kernel not up: 503 for HTTP, Close(1011) for WS (handled below).
			// The dashboard's 3s reconnect takes over from either signal.
			if isWebSocketUpgrade(r) {
				bridgeUnavailable(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"kernel not running"}`))
			return
		}

		// WS endpoints: bridge with gorilla/websocket.
		sub := stripPrefix(r.URL.Path, "/api/clash")
		if _, ok := wsEndpoints[sub]; ok && isWebSocketUpgrade(r) {
			bridgeWS(w, r, upstreamAddr, secret, sub)
			return
		}

		// HTTP: let the ReverseProxy do its thing. The Director rewrites
		// scheme/host/Authorization and strips /api/clash from the path.
		rp.ServeHTTP(w, r)
	})
	return mux
}

// director returns a Director that:
//   - rewrites the request URL to point at mihomo (rewriting 0.0.0.0/:: to 127.0.0.1)
//   - strips the /api/clash prefix so mihomo sees /version, /configs, ...
//   - injects the supervisor-managed CLASH_SECRET as a Bearer header, dropping
//     any client-supplied Authorization so the browser never forwards its own.
func director(state StateFunc) func(*http.Request) {
	return func(r *http.Request) {
		upstreamAddr, secret, _ := state()

		// upstreamAddr is like "0.0.0.0:9090" or "127.0.0.1:9090"; rewrite
		// wildcard binds to loopback so the client dial succeeds on every
		// host stack (mirrors supervisor.versionURL).
		host := upstreamAddr
		scheme := "http"
		if strings.HasPrefix(host, "http://") {
			host = strings.TrimPrefix(host, "http://")
		} else if strings.HasPrefix(host, "https://") {
			host = strings.TrimPrefix(host, "https://")
			scheme = "https"
		}
		// Split host:port; rewrite the host part if it's a wildcard.
		if h, p, err := net.SplitHostPort(host); err == nil {
			switch h {
			case "0.0.0.0", "::", "":
				host = "127.0.0.1:" + p
			}
		}

		r.URL.Scheme = scheme
		r.URL.Host = host
		r.Host = host

		// Strip /api/clash from the upstream path. mihomo's API is rooted at /.
		r.URL.Path = stripPrefix(r.URL.Path, "/api/clash")
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}

		// Inject the managed CLASH_SECRET so mihomo authenticates the request.
		r.Header.Del("Authorization")
		if secret != "" {
			r.Header.Set("Authorization", "Bearer "+secret)
		}
	}
}

// isWebSocketUpgrade detects a WS handshake. gorilla/websocket uses the same
// check in IsWebSocketUpgrade.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// stripPrefix removes the given prefix and any leftover leading slash so the
// result is a rooted-relative path like "/version" or "" for the prefix itself.
func stripPrefix(p, prefix string) string {
	if !strings.HasPrefix(p, prefix) {
		return p
	}
	rest := strings.TrimPrefix(p, prefix)
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return ""
	}
	return "/" + rest
}

// bridgeWS upgrades the client connection, dials the upstream mihomo WS, and
// copies frames bidirectionally until either side closes. mihomo's endpoints
// are all server-push text frames, so the client→server goroutine rarely fires
// — but we keep it for close/control propagation.
func bridgeWS(w http.ResponseWriter, r *http.Request, upstreamAddr, secret, sub string) {
	// Upstream URL: rewrite wildcard bind host to loopback for the dial.
	upHost := upstreamAddr
	if h, p, err := net.SplitHostPort(upHost); err == nil {
		switch h {
		case "0.0.0.0", "::", "":
			upHost = "127.0.0.1:" + p
		}
	}

	// mihomo's WS path is the sub-endpoint rooted at /.
	u := url.URL{Scheme: "ws", Host: upHost, Path: sub}
	q := r.URL.Query()
	if secret != "" {
		// mihomo accepts the secret as a ?token= query parameter on WS
		// upgrades (Authorization headers are awkward in browser WS).
		q.Set("token", secret)
	}
	u.RawQuery = q.Encode()

	// Dialer with a sane handshake timeout — long enough to cross a slow
	// loopback, short enough that a wedged kernel doesn't dangle the client.
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		// mihomo binds plain ws://, not TLS.
	}

	// Carry through the Origin/headers from the client request minimally —
	// mihomo ignores Origin, but we keep the User-Agent for log readability.
	headers := map[string][]string{}
	if ua := r.Header.Get("User-Agent"); ua != "" {
		headers["User-Agent"] = []string{ua}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	upstream, resp, err := dialer.DialContext(ctx, u.String(), headers)
	if err != nil {
		// If the upstream responded with an HTTP status (e.g. 401 on bad
		// secret), mirror it; otherwise 502.
		status := http.StatusBadGateway
		if resp != nil && resp.StatusCode != 0 {
			status = resp.StatusCode
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"upstream dial failed: %s"}`, err.Error())))
		return
	}
	defer upstream.Close() //nolint:errcheck

	upgrader := websocket.Upgrader{
		// The dashboard is same-origin; mihomo doesn't care. Allow any origin
		// (the proxy is the only dial path).
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	client, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade failed — the response is already written by Upgrader.
		return
	}
	defer client.Close() //nolint:errcheck

	// Two-goroutine bridge. The first to hit an error tears down both sides
	// via closeOnce.
	var closeOnce sync.Once
	shutdown := func() {
		closeOnce.Do(func() {
			_ = client.Close()
			_ = upstream.Close()
		})
	}
	defer shutdown()

	done := make(chan struct{})
	// upstream → client (the hot direction: mihomo pushes traffic/logs/...)
	go func() {
		defer close(done)
		pipeWS(client, upstream, shutdown)
	}()
	// client → upstream (rarely used, but propagates client Close frames)
	go func() {
		pipeWS(upstream, client, shutdown)
	}()
	<-done
}

// pipeWS copies messages from src to dst until either errors. The shutdown
// callback is invoked on the first error so the peer goroutine also exits.
//
// Message type (text vs binary) is preserved across the copy — mihomo emits
// text frames for all four streams, but we don't assume that.
func pipeWS(dst, src *websocket.Conn, shutdown func()) {
	for {
		msgType, msg, err := src.ReadMessage()
		if err != nil {
			// Normal close (1000) or transient network failure — either way,
			// signal the bridge to tear down.
			if isClosedByPeer(err) {
				_ = dst.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			}
			shutdown()
			return
		}
		if err := dst.WriteMessage(msgType, msg); err != nil {
			shutdown()
			return
		}
	}
}

// isClosedByPeer distinguishes an expected close (peer sent a close frame or
// dropped the TCP connection) from a genuine I/O fault, so we can send a clean
// close frame before tearing down.
func isClosedByPeer(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseAbnormalClosure,
	)
}

// bridgeUnavailable handles the "kernel not running" case for WS: the upgrade
// is rejected with a close code that lets the dashboard's reconnect logic
// re-try on its own schedule. We can't actually upgrade and then close in one
// step (the WS handshake requires a 101 response), so we return 503 and let
// the dashboard treat it like any failed upgrade — its retry timer fires.
func bridgeUnavailable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"error":"kernel not running"}`))
}
