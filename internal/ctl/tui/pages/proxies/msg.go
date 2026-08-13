package proxies

import "metacubexd-server-go/internal/ctl"

// ProxiesLoadedMsg is sent when proxies are fetched.
type ProxiesLoadedMsg struct {
	Resp ctl.ProxiesResponse
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