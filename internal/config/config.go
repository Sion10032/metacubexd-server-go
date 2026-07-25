// Package config parses process environment variables into a typed ServerEnv,
// mirroring apps/server/lib/supervisor.ts in the upstream TS server.
//
// Every field has a documented default and lives entirely in env (no config
// files). The All-in-One server is single-binary, so the only configuration
// surface is the environment block passed to the container.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ServerEnv is the parsed process configuration.
//
// Field semantics are documented in GO_SERVER_PLAN.md §11 and match the
// upstream TS server one-for-one so the same docker-compose / k8s manifests
// work against either binary.
type ServerEnv struct {
	// ControlPort is the dashboard UI + control/clash API listen port
	// (default 8080). Browsers talk ONLY to this port — 9090 is internal.
	ControlPort int

	// ClashAPIPort is mihomo's Clash API port (default 9090). mihomo binds
	// 0.0.0.0:<port> so it is reachable inside the container netns; the Go
	// server rewrites the host to 127.0.0.1 for the readiness poll and the
	// same-origin proxy upstream.
	ClashAPIPort int

	// MixedPort is mihomo's mixed proxy port (default 7890). Still needs to
	// be published to the host for client traffic.
	MixedPort int

	// DataDir holds profiles/, active.yaml, geo caches, fake-ip cache.
	// Default "data" — resolved against the server's working directory, so a
	// bare `./metacubexd-server` invocation keeps its state local. Container
	// deployments override with DATA_DIR=/data (the Dockerfile VOLUME).
	DataDir string

	// MihomoBin is the absolute path to the mihomo binary. Overridable so
	// operators can mount their own build (matches MIHOMO_BIN in TS).
	MihomoBin string

	// ControlToken gates /api/control/** access via Bearer auth. Empty string
	// means unauthenticated (matches the in-process Electron case). For the
	// server this is always set in production; the static UI gets it injected
	// via /config.js so the browser authenticates transparently.
	ControlToken string

	// ClashSecret is mihomo's external-controller secret. Injected into
	// active.yaml by the supervisor; the same-origin proxy re-attaches it
	// as Authorization: Bearer <secret> on every upstream request.
	ClashSecret string

	// GitHubToken is optional; used for authenticated GitHub rate limits when
	// downloading mihomo/UI assets.
	GitHubToken string

	// UIDist overrides the embedded UI. Empty = use the go:embed FS.
	UIDist string

	// MihomoVersion is the build-time pinned mihomo release tag (default
	// v1.19.27). Reported by /api/control/info as kernel.version when the
	// supervisor has not yet observed a running kernel.
	MihomoVersion string
}

// FromEnv parses os.Environ into a ServerEnv. Missing or malformed values fall
// back to documented defaults; the function never returns an error.
func FromEnv() ServerEnv {
	return ServerEnv{
		ControlPort:   getIntEnv("CONTROL_PORT", 8080),
		ClashAPIPort:  getIntEnv("CLASH_API_PORT", 9090),
		MixedPort:     getIntEnv("MIXED_PORT", 7890),
		DataDir:       getEnv("DATA_DIR", "data"),
		MihomoBin:     getEnv("MIHOMO_BIN", "/usr/local/bin/mihomo"),
		ControlToken:  os.Getenv("CONTROL_TOKEN"),
		ClashSecret:   os.Getenv("CLASH_SECRET"),
		GitHubToken:   os.Getenv("GITHUB_TOKEN"),
		UIDist:        os.Getenv("UI_DIST"),
		MihomoVersion: getEnv("MIHOMO_VERSION", "v1.19.27"),
	}
}

// ProfilesDir returns <DataDir>/profiles — the on-disk profile store root.
func (e ServerEnv) ProfilesDir() string { return filepath.Join(e.DataDir, "profiles") }

// ActiveConfigPath returns <DataDir>/active.yaml — the file mihomo runs with -f.
func (e ServerEnv) ActiveConfigPath() string { return filepath.Join(e.DataDir, "active.yaml") }

// ExternalController returns the bind spec written into active.yaml's
// external-controller field. mihomo binds 0.0.0.0 so the container port is
// reachable; clients inside the netns (the proxy + readiness poll) rewrite
// to 127.0.0.1.
func (e ServerEnv) ExternalController() string {
	return "0.0.0.0:" + strconv.Itoa(e.ClashAPIPort)
}

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getIntEnv(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
