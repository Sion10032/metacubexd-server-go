// Package webdav is a minimal WebDAV client over net/http: PUT, GET, MKCOL.
// It exists for the backup/restore flow (POST /api/control/backup and /restore)
// — those routes need to push and pull a single JSON bundle file from the
// user's WebDAV server, and pulling in a full WebDAV library for three verbs
// would be overkill.
//
// Direct port of packages/agent/src/webdav.ts.
package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is the three-method WebDAV client the backup/restore routes need.
type Client interface {
	Put(ctx context.Context, path, body string) error
	Get(ctx context.Context, path string) (string, error)
	// Mkcol creates a collection (directory). Implementations tolerate the
	// "already exists" responses (405 / 301) so a prior collection doesn't
	// fail the call.
	Mkcol(ctx context.Context, dir string) error
}

// Options configures a Client. URL is the WebDAV base (e.g.
// "https://host/remote.php/dav/files/user"); per-request paths are joined to
// it. HTTPClient is injectable for tests; nil → a 30s default.
type Options struct {
	URL      string
	Username string
	Password string
	HTTPClient *http.Client
}

// New returns a Client that sends HTTP Basic auth on every request, matching
// the TS implementation's behavior (mihomo's documented WebDAV servers all
// accept Basic).
func New(opts Options) Client {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &client{
		base:    opts.URL,
		auth:    "Basic " + base64.StdEncoding.EncodeToString([]byte(opts.Username+":"+opts.Password)),
		http:    hc,
	}
}

type client struct {
	base string
	auth string
	http *http.Client
}

func (c *client) Put(ctx context.Context, path, body string) error {
	resp, err := c.do(ctx, http.MethodPut, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webdav PUT failed %d %s for %s", resp.StatusCode, resp.Status, path)
	}
	return nil
}

func (c *client) Get(ctx context.Context, path string) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, path, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("webdav GET failed %d %s for %s", resp.StatusCode, resp.Status, path)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *client) Mkcol(ctx context.Context, dir string) error {
	resp, err := c.do(ctx, "MKCOL", dir, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 405 (Method Not Allowed) and 301 (Moved Permanently) are the typical
	// responses when the collection already exists — treat as success.
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent,
		http.StatusMethodNotAllowed, http.StatusMovedPermanently:
		return nil
	}
	return fmt.Errorf("webdav MKCOL failed %d %s for %s", resp.StatusCode, resp.Status, dir)
}

func (c *client) do(ctx context.Context, method, path, body string) (*http.Response, error) {
	if c.base == "" {
		return nil, errors.New("webdav: base URL is empty")
	}
	req, err := http.NewRequestWithContext(ctx, method, joinURL(c.base, path), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.auth)
	if body != "" {
		req.Body = io.NopCloser(strings.NewReader(body))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body)), nil }
		// Some WebDAV servers reject PUTs without a Content-Type; the TS impl
		// doesn't set one and relies on sniffing. Match that to stay compatible.
		req.ContentLength = int64(len(body))
	}
	// bytes.NewReader is unused at this point but kept in scope as a cheap
	// alternative if we need to retry a body (GetBody exists for that).
	_ = bytes.NewReader
	return c.http.Do(req)
}

// joinURL joins base + path with exactly one slash between them, regardless of
// trailing/leading slashes on either side. Mirrors the TS joinUrl helper.
func joinURL(base, path string) string {
	baseEnd := len(base)
	for baseEnd > 0 && base[baseEnd-1] == '/' {
		baseEnd--
	}
	pathStart := 0
	for pathStart < len(path) && path[pathStart] == '/' {
		pathStart++
	}
	return base[:baseEnd] + "/" + path[pathStart:]
}
