// Command mihomo-tui is an interactive TUI client for metacubexd-server.
package main

import (
	"flag"
	"fmt"
	"os"
)

// defaultEndpoint mirrors the server's CONTROL_PORT default (8080) so a local
// server on the default port can be reached without any flags.
func defaultEndpoint() string {
	port := os.Getenv("CONTROL_PORT")
	if port == "" {
		port = "8080"
	}
	return "http://127.0.0.1:" + port
}

func main() {
	var (
		endpoint = flag.String("endpoint", defaultEndpoint(), "metacubexd-server endpoint URL")
		token    = flag.String("token", os.Getenv("CONTROL_TOKEN"), "control API Bearer token (CONTROL_TOKEN env)")
		insecure = flag.Bool("insecure", false, "skip TLS certificate verification")
	)
	flag.Parse()

	// TUI not implemented yet (Phase 1d): print resolved config and exit.
	fmt.Printf("mihomo-tui: tui not implemented yet (endpoint=%s insecure=%t)\n", *endpoint, *insecure)
	if *token != "" {
		fmt.Println("mihomo-tui: token set (redacted)")
	}
	os.Exit(1)
}
