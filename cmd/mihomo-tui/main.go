// Command mihomo-tui is an interactive TUI client for metacubexd-server.
package main

import (
	"flag"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui"
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

	client := ctl.NewClient(*endpoint, *token, *insecure)
	if _, err := tea.NewProgram(tui.New(client)).Run(); err != nil {
		log.Fatalf("mihomo-tui: %v", err)
	}
}
