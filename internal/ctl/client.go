// Package ctl implements the HTTP client and SSE subscription that mihomo-tui
// uses to talk to a running metacubexd-server (/api/control/*).
package ctl

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"metacubexd-server-go/internal/supervisor"
)

// ErrUnauthorized is returned when the control API rejects the credentials:
// the server has auth enabled but the request carried no or an invalid token.
var ErrUnauthorized = errors.New("unauthorized: token invalid or server requires auth")

// Client talks to the metacubexd-server control API. It is safe for
// concurrent use once created.
type Client struct {
	endpoint string
	token    string
	hc       *http.Client
}

// NewClient returns a Client for the given endpoint. When token is non-empty
// every request carries an Authorization: Bearer <token> header. insecure
// skips TLS certificate verification for https endpoints (explicit user opt-in).
func NewClient(endpoint, token string, insecure bool) *Client {
	transport := &http.Transport{}
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 — explicit user opt-in
	}
	return &Client{
		endpoint: endpoint,
		token:    token,
		hc:       &http.Client{Timeout: 30 * time.Second, Transport: transport},
	}
}

// Endpoint returns the configured server endpoint URL.
func (c *Client) Endpoint() string {
	return c.endpoint
}

// do performs a request against endpoint+path, attaching the bearer token
// (when configured) and requesting JSON. The caller must close the returned
// response body.
func (c *Client) do(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.endpoint+path, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	return c.hc.Do(req)
}

// KernelStatus fetches the current kernel state from the server.
func (c *Client) KernelStatus() (supervisor.KernelState, error) {
	resp, err := c.do(http.MethodGet, "/api/control/kernel/status", nil)
	if err != nil {
		return supervisor.KernelState{}, err
	}
	defer resp.Body.Close()
	return decodeState(resp)
}

// decodeState decodes a supervisor.KernelState and returns an error when the
// response is not 2xx, preferring the server-provided lastError message.
func decodeState(resp *http.Response) (supervisor.KernelState, error) {
	var st supervisor.KernelState
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return st, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized {
			return st, ErrUnauthorized
		}
		msg := st.LastError
		if msg == "" {
			msg = resp.Status
		}
		return st, errors.New(msg)
	}
	return st, nil
}

// KernelStart starts the kernel and returns the new state.
func (c *Client) KernelStart() (supervisor.KernelState, error) {
	return c.postKernel("/api/control/kernel/start")
}

// KernelStop stops the kernel and returns the new state.
func (c *Client) KernelStop() (supervisor.KernelState, error) {
	return c.postKernel("/api/control/kernel/stop")
}

// KernelRestart restarts the kernel and returns the new state.
func (c *Client) KernelRestart() (supervisor.KernelState, error) {
	return c.postKernel("/api/control/kernel/restart")
}

// KernelRollback restores the last-known-good active config and restarts.
func (c *Client) KernelRollback() (supervisor.KernelState, error) {
	return c.postKernel("/api/control/kernel/rollback")
}

// KernelRecover resets the active config to header-only and restarts on
// mihomo defaults — the last-resort escape hatch for a bricked config.
func (c *Client) KernelRecover() (supervisor.KernelState, error) {
	return c.postKernel("/api/control/kernel/recover")
}

// postKernel performs a POST to a kernel control path and decodes the
// resulting kernel state.
func (c *Client) postKernel(path string) (supervisor.KernelState, error) {
	resp, err := c.do(http.MethodPost, path, nil)
	if err != nil {
		return supervisor.KernelState{}, err
	}
	defer resp.Body.Close()
	return decodeState(resp)
}
