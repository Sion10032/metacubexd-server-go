package proxies

import "metacubexd-server-go/internal/tui/client"

// ProxiesLoadedMsg is sent when proxies are fetched.
type ProxiesLoadedMsg struct {
	Resp client.ProxiesResponse
	Err  error
}

// ProxyOpMsg is sent after a proxy switch operation.
type ProxyOpMsg struct {
	Err error
}

// ModeLoadedMsg is sent when the mode is fetched.
type ModeLoadedMsg struct {
	Mode string
	Err  error
}

// ModeOpMsg is sent after a mode switch operation.
type ModeOpMsg struct {
	Err error
}

// GroupDelayMsg is sent when a group delay test completes.
type GroupDelayMsg struct {
	Group  string
	Delays map[string]int
	Err    error
}