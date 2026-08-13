package proxies

import (
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
)

// FetchProxies returns a command that fetches all proxies.
func FetchProxies(c *ctl.Client) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.ListProxies()
		return ProxiesLoadedMsg{Resp: resp, Err: err}
	}
}

// FetchMode returns a command that fetches the current mode.
func FetchMode(c *ctl.Client) tea.Cmd {
	return func() tea.Msg {
		mode, err := c.GetMode()
		return ModeLoadedMsg{Mode: mode, Err: err}
	}
}

// selectCmd returns a command that switches a Selector group to a member.
func selectCmd(c *ctl.Client, group, member string) tea.Cmd {
	return func() tea.Msg {
		err := c.SelectProxy(group, member)
		return ProxyOpMsg{Err: err}
	}
}

// setModeCmd returns a command that sets the mode.
func setModeCmd(c *ctl.Client, mode string) tea.Cmd {
	return func() tea.Msg {
		err := c.SetMode(mode)
		return ModeOpMsg{Err: err}
	}
}