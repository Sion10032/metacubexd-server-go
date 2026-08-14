// Command metacubexd-server is the All-in-One Go binary that replaces the
// upstream TS server (apps/server + packages/agent). It serves the dashboard,
// proxies the Clash API same-origin (HTTP + WebSocket), and supervises the
// mihomo kernel.
//
// Boot sequence mirrors apps/server/plugins/boot-kernel.ts:
//  1. Parse env → ServerEnv
//  2. Ensure DATA_DIR/profiles exists
//  3. Construct Supervisor with env-derived bind/secret/mixedPort
//  4. Auto-start the kernel (header-only active.yaml if no profile yet)
//  5. Mount routes: control > clash > static (catch-all)
//  6. Listen on CONTROL_PORT
//
// Phase 1 scope: HTTP server + supervisor + static + Clash proxy + temp info.
// Profiles, validate, auto-restart, scheduler, webdav, geo — Phases 2-5.
package main

import (
	"fmt"
	"log"
	"os"
	"runtime"

	"metacubexd-server-go/internal/server"
	"metacubexd-server-go/internal/server/config"
)

// Populated via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Handle --version / -v / version before any side effects. The full
	// server must not start when the caller just wants the version string
	// (e.g. metacubexd-ctl.sh update checks the installed binary this way).
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Printf("metacubexd-server-go %s (commit: %s, built: %s) %s/%s\n",
				version, commit, date, runtime.GOOS, runtime.GOARCH)
			return
		case "-h", "--help", "help":
			printUsage()
			return
		}
	}

	log.Printf("[server] metacubexd-server-go %s (commit: %s, built: %s)", version, commit, date)

	if err := server.Run(server.Options{
		Env:     config.FromEnv(),
		Version: version,
		Commit:  commit,
		Date:    date,
	}); err != nil {
		log.Fatalf("[server] %v", err)
	}
}

// printUsage writes the CLI help text to stdout.
func printUsage() {
	fmt.Printf(`metacubexd-server-go %s

Usage:
  metacubexd-server [flags]

Flags:
  -h, --help     show this help
  -v, --version  print version and exit

Configuration is via environment variables; see README.md § 配置 > 环境变量.
`, version)
}