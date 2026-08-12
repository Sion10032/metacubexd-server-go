// Package ctl implements the HTTP client and SSE subscription that mihomo-tui
// uses to talk to a running metacubexd-server (/api/control/*).
package ctl

import (
	"crypto/tls"
	"net/http"
	"time"
)

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
