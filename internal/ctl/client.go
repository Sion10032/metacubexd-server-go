// Package ctl implements the HTTP client and SSE subscription that mihomo-tui
// uses to talk to a running metacubexd-server (/api/control/*).
package ctl

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"metacubexd-server-go/internal/profile"
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

// doJSON performs a request and decodes a JSON response, surfacing the
// {"error": ...} message on non-2xx responses.
func (c *Client) doJSON(method, path string, body io.Reader, out any) error {
	resp, err := c.do(method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if derr := json.NewDecoder(resp.Body).Decode(&e); derr == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return errors.New(resp.Status)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// doText performs a request and returns the raw response body as a string,
// surfacing the {"error": ...} message on non-2xx responses.
func (c *Client) doText(method, path string, body io.Reader) (string, error) {
	resp, err := c.do(method, path, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if derr := json.NewDecoder(resp.Body).Decode(&e); derr == nil && e.Error != "" {
			return "", errors.New(e.Error)
		}
		return "", errors.New(resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ProfilesList fetches all profiles.
func (c *Client) ProfilesList() ([]profile.Meta, error) {
	var list []profile.Meta
	if err := c.doJSON(http.MethodGet, "/api/control/profiles", nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ProfileImport fetches a remote subscription URL into a new profile.
func (c *Client) ProfileImport(url, name string) (profile.Meta, error) {
	var m profile.Meta
	body, err := json.Marshal(struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}{url, name})
	if err != nil {
		return m, err
	}
	if err := c.doJSON(http.MethodPost, "/api/control/profiles/import", bytes.NewReader(body), &m); err != nil {
		return m, err
	}
	return m, nil
}

// ProfileRefresh re-fetches a subscription profile in place.
func (c *Client) ProfileRefresh(id string) (profile.Meta, error) {
	var m profile.Meta
	if err := c.doJSON(http.MethodPost, "/api/control/profiles/"+id+"/refresh", nil, &m); err != nil {
		return m, err
	}
	return m, nil
}

// ProfileRefreshAndActivate refreshes a profile and activates it, restarting
// the kernel.
func (c *Client) ProfileRefreshAndActivate(id string) (profile.Meta, error) {
	var out struct {
		Meta profile.Meta `json:"meta"`
	}
	if err := c.doJSON(http.MethodPost, "/api/control/profiles/"+id+"/refresh-and-activate", nil, &out); err != nil {
		return profile.Meta{}, err
	}
	return out.Meta, nil
}

// ProfileActivate activates a profile and restarts the kernel.
func (c *Client) ProfileActivate(id string) (supervisor.KernelState, error) {
	var st supervisor.KernelState
	if err := c.doJSON(http.MethodPost, "/api/control/profiles/"+id+"/activate", nil, &st); err != nil {
		return st, err
	}
	return st, nil
}

// ProfileDelete removes a profile.
func (c *Client) ProfileDelete(id string) error {
	return c.doJSON(http.MethodDelete, "/api/control/profiles/"+id, nil, nil)
}

// WebdavOptions configures backup/restore against a WebDAV server. Field
// names mirror the server's /api/control/backup + /restore request body.
type WebdavOptions struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	Dir      string `json:"dir"`
}

// GetConfig fetches the active profile's source YAML.
func (c *Client) GetConfig() (string, error) {
	return c.doText(http.MethodGet, "/api/control/config", nil)
}

// GetRuntimeConfig fetches the runtime config — the file mihomo actually
// runs (post-injection).
func (c *Client) GetRuntimeConfig() (string, error) {
	return c.doText(http.MethodGet, "/api/control/config/runtime", nil)
}

// PutSection replaces one top-level key in the active config. When restart is
// false the change is persisted without restarting the kernel.
func (c *Client) PutSection(key string, value any, restart bool) error {
	body, err := json.Marshal(struct {
		Key     string `json:"key"`
		Value   any    `json:"value"`
		Restart *bool  `json:"restart"`
	}{key, value, &restart})
	if err != nil {
		return err
	}
	return c.doJSON(http.MethodPut, "/api/control/config/section", bytes.NewReader(body), nil)
}

// GeoUpdate downloads mihomo's geoip/geosite/country.mmdb assets into the
// server's home dir.
func (c *Client) GeoUpdate() error {
	return c.doJSON(http.MethodPost, "/api/control/geo/update", nil, nil)
}

// Backup pushes every profile to a WebDAV server.
func (c *Client) Backup(opts WebdavOptions) error {
	body, err := json.Marshal(struct {
		Webdav WebdavOptions `json:"webdav"`
	}{opts})
	if err != nil {
		return err
	}
	return c.doJSON(http.MethodPost, "/api/control/backup", bytes.NewReader(body), nil)
}

// Restore pulls the profile bundle back from WebDAV, returning the number of
// profiles restored.
func (c *Client) Restore(opts WebdavOptions) (int, error) {
	body, err := json.Marshal(struct {
		Webdav WebdavOptions `json:"webdav"`
	}{opts})
	if err != nil {
		return 0, err
	}
	var out struct {
		Restored int `json:"restored"`
	}
	if err := c.doJSON(http.MethodPost, "/api/control/restore", bytes.NewReader(body), &out); err != nil {
		return 0, err
	}
	return out.Restored, nil
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
