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
	"net/url"
	"time"

	"metacubexd-server-go/internal/api"
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
func (c *Client) KernelStatus() (api.KernelState, error) {
	resp, err := c.do(http.MethodGet, "/api/control/kernel/status", nil)
	if err != nil {
		return api.KernelState{}, err
	}
	defer resp.Body.Close()
	return decodeState(resp)
}

// decodeState decodes an api.KernelState and returns an error when the
// response is not 2xx, preferring the server-provided lastError message.
func decodeState(resp *http.Response) (api.KernelState, error) {
	var st api.KernelState
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
func (c *Client) ProfilesList() ([]api.Meta, error) {
	var list []api.Meta
	if err := c.doJSON(http.MethodGet, "/api/control/profiles", nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ProfileImport fetches a remote subscription URL into a new profile.
func (c *Client) ProfileImport(url, name string) (api.Meta, error) {
	var m api.Meta
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
func (c *Client) ProfileRefresh(id string) (api.Meta, error) {
	var m api.Meta
	if err := c.doJSON(http.MethodPost, "/api/control/profiles/"+id+"/refresh", nil, &m); err != nil {
		return m, err
	}
	return m, nil
}

// ProfileRefreshAndActivate refreshes a profile and activates it, restarting
// the kernel.
func (c *Client) ProfileRefreshAndActivate(id string) (api.Meta, error) {
	var out struct {
		Meta api.Meta `json:"meta"`
	}
	if err := c.doJSON(http.MethodPost, "/api/control/profiles/"+id+"/refresh-and-activate", nil, &out); err != nil {
		return api.Meta{}, err
	}
	return out.Meta, nil
}

// ProfileActivate activates a profile and restarts the kernel.
func (c *Client) ProfileActivate(id string) (api.KernelState, error) {
	var st api.KernelState
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
func (c *Client) KernelStart() (api.KernelState, error) {
	return c.postKernel("/api/control/kernel/start")
}

// KernelStop stops the kernel and returns the new state.
func (c *Client) KernelStop() (api.KernelState, error) {
	return c.postKernel("/api/control/kernel/stop")
}

// KernelRestart restarts the kernel and returns the new state.
func (c *Client) KernelRestart() (api.KernelState, error) {
	return c.postKernel("/api/control/kernel/restart")
}

// KernelRollback restores the last-known-good active config and restarts.
func (c *Client) KernelRollback() (api.KernelState, error) {
	return c.postKernel("/api/control/kernel/rollback")
}

// KernelRecover resets the active config to header-only and restarts on
// mihomo defaults — the last-resort escape hatch for a bricked config.
func (c *Client) KernelRecover() (api.KernelState, error) {
	return c.postKernel("/api/control/kernel/recover")
}

// postKernel performs a POST to a kernel control path and decodes the
// resulting kernel state.
func (c *Client) postKernel(path string) (api.KernelState, error) {
	resp, err := c.do(http.MethodPost, path, nil)
	if err != nil {
		return api.KernelState{}, err
	}
	defer resp.Body.Close()
	return decodeState(resp)
}

// Proxy describes a single proxy entry in mihomo's /proxies response.
type Proxy struct {
	Name string   `json:"name"`
	Type string   `json:"type"`          // Selector / URLTest / Fallback / Direct / Reject / ...
	Now  string   `json:"now,omitempty"` // Current node for groups
	All  []string `json:"all,omitempty"` // Member names for groups
}

// ProxiesResponse is the response from GET /api/clash/proxies.
// Order preserves the original key order from the JSON response.
type ProxiesResponse struct {
	Proxies map[string]Proxy `json:"proxies"`
	Order   []string         `json:"-"` // original key order from API
}

// UnmarshalJSON implements json.Unmarshaler to preserve the key order of the
// proxies map. Go's map iteration is random, so without this the proxy-groups
// list would appear in a different order each refresh.
func (r *ProxiesResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Proxies = raw.Proxies

	// Re-extract key order from raw JSON bytes.
	var wrapper struct {
		Proxies json.RawMessage `json:"proxies"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	r.Order = orderedJSONKeys(wrapper.Proxies)
	return nil
}

// orderedJSONKeys extracts object keys in their original order from raw JSON.
func orderedJSONKeys(raw json.RawMessage) []string {
	var keys []string
	dec := json.NewDecoder(bytes.NewReader(raw))
	// read '{'
	if _, err := dec.Token(); err != nil {
		return nil
	}
	for dec.More() {
		// read key string
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if key, ok := tok.(string); ok {
			keys = append(keys, key)
		}
		// skip value by decoding into interface{}
		var skip interface{}
		if err := dec.Decode(&skip); err != nil {
			break
		}
	}
	return keys
}

// ClashConfig is a partial response from GET /api/clash/configs (only mode).
type ClashConfig struct {
	Mode string `json:"mode"` // rule / global / direct
}

// GetConfigs fetches the full running config from mihomo's /configs endpoint
// — the values the kernel actually applies at runtime, including tun. Unlike
// GET /api/control/config/runtime (which reads the on-disk active config),
// this reflects injected, merged and runtime-patched values.
func (c *Client) GetConfigs() (map[string]any, error) {
	var v map[string]any
	if err := c.doJSON(http.MethodGet, "/api/clash/configs", nil, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// ConnectionMetadata is the peer metadata for a single connection.
type ConnectionMetadata struct {
	Network         string `json:"network"`          // tcp / udp
	Type            string `json:"type"`             // HTTP / SOCKS / TCP / ...
	SourceIP        string `json:"sourceIP"`
	DestinationIP   string `json:"destinationIP"`
	SourcePort      string `json:"sourcePort"`
	DestinationPort string `json:"destinationPort"`
	Host            string `json:"host"`             // SNI / Host header (may be empty)
	ProcessPath     string `json:"processPath"`
}

// Connection is a single connection returned by GET /connections.
type Connection struct {
	ID          string             `json:"id"`
	Upload      int64              `json:"upload"`
	Download    int64              `json:"download"`
	Start       string             `json:"start"`     // RFC3339 timestamp
	Chains      []string           `json:"chains"`    // outbound chain: [group, node, ...]
	Rule        string             `json:"rule"`
	RulePayload string             `json:"rulePayload"`
	Metadata    ConnectionMetadata `json:"metadata"`
}

// ConnectionsResponse is the response from GET /connections.
type ConnectionsResponse struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Connections   []Connection `json:"connections"`
}

// ListProxies fetches all proxies from mihomo.
func (c *Client) ListProxies() (ProxiesResponse, error) {
	var resp ProxiesResponse
	if err := c.doJSON(http.MethodGet, "/api/clash/proxies", nil, &resp); err != nil {
		return resp, err
	}
	return resp, nil
}

// SelectProxy switches a Selector group to the given node.
func (c *Client) SelectProxy(group, name string) error {
	body, err := json.Marshal(struct {
		Name string `json:"name"`
	}{name})
	if err != nil {
		return err
	}
	return c.doJSON(http.MethodPut, "/api/clash/proxies/"+url.PathEscape(group), bytes.NewReader(body), nil)
}

// GetMode fetches the current mihomo mode.
func (c *Client) GetMode() (string, error) {
	var cfg ClashConfig
	if err := c.doJSON(http.MethodGet, "/api/clash/configs", nil, &cfg); err != nil {
		return "", err
	}
	return cfg.Mode, nil
}

// SetMode switches the mihomo mode (rule/global/direct).
func (c *Client) SetMode(mode string) error {
	body, err := json.Marshal(struct {
		Mode string `json:"mode"`
	}{mode})
	if err != nil {
		return err
	}
	return c.doJSON(http.MethodPatch, "/api/clash/configs", bytes.NewReader(body), nil)
}

// ListConnections fetches the current connection snapshot.
func (c *Client) ListConnections() (ConnectionsResponse, error) {
	var resp ConnectionsResponse
	if err := c.doJSON(http.MethodGet, "/api/clash/connections", nil, &resp); err != nil {
		return resp, err
	}
	return resp, nil
}

// CloseConnection closes the connection with the given id.
func (c *Client) CloseConnection(id string) error {
	return c.doJSON(http.MethodDelete, "/api/clash/connections/"+url.PathEscape(id), nil, nil)
}

// CloseAllConnections closes all active connections.
func (c *Client) CloseAllConnections() error {
	return c.doJSON(http.MethodDelete, "/api/clash/connections", nil, nil)
}
