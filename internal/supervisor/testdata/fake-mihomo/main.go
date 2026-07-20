// fake-mihomo is a minimal HTTP server that mimics mihomo for supervisor
// integration tests. It reads the active config to find its listening port
// (injected by injectClashConfig) and serves /version.
//
// Modes:
//   - Normal:   fake-mihomo -d <dir> -f <config>   — listen on EC port, serve HTTP
//   - Validate: fake-mihomo -t -d <dir> -f <config> — check config, exit 0/1
//
// Convention: if the config contains a line "# invalid", validate exits 1.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]

	// Detect -t (validate mode) — consume it if present.
	validate := false
	for i, a := range args {
		if a == "-t" {
			validate = true
			args = append(args[:i], args[i+1:]...)
			break
		}
	}

	cfgPath := parseFlag(args, "-f")
	if cfgPath == "" {
		log.Fatal("fake-mihomo: -f <config> required")
	}

	if validate {
		b, err := os.ReadFile(cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fake-mihomo: read config: %v\n", err)
			os.Exit(1)
		}
		if strings.Contains(string(b), "# invalid") {
			fmt.Fprintln(os.Stderr, "fake-mihomo: config invalid")
			os.Exit(1)
		}
		// Config OK.
		fmt.Fprintln(os.Stderr, "fake-mihomo: config valid")
		os.Exit(0)
	}

	ec := parseExternalController(cfgPath)
	if ec == "" {
		log.Fatalf("fake-mihomo: no external-controller in %s", cfgPath)
	}

	addr := normalizeAddr(ec)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("fake-mihomo: listen %s: %v", addr, err)
	}
	fmt.Fprintf(os.Stderr, "fake-mihomo listening on %s\n", ln.Addr().String())

	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"version": "test-v1.0.0"})
	})
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/traffic", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/memory", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"inuse": 0, "inuseBytes": 0, "oslimit": 0})
	})
	mux.HandleFunc("/connections", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"downloadTotal": 0, "uploadTotal": 0, "connections": []any{}})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if err := http.Serve(ln, mux); err != nil {
		log.Fatalf("fake-mihomo: serve: %v", err)
	}
}

func parseFlag(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func parseExternalController(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "external-controller:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "external-controller:"))
			val = strings.Trim(val, "\"'")
			return val
		}
	}
	return ""
}

func normalizeAddr(ec string) string {
	addr := strings.TrimPrefix(ec, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	// Replace wildcard binds with loopback for test routability.
	if strings.HasPrefix(addr, "0.0.0.0:") || strings.HasPrefix(addr, "[::]:") || strings.HasPrefix(addr, ":") {
		port := addr[strings.IndexByte(addr, ':'):]
		addr = "127.0.0.1" + port
	} else if strings.HasPrefix(addr, "0.0.0.0") {
		addr = "127.0.0.1" + strings.TrimPrefix(addr, "0.0.0.0")
	}
	return addr
}
