package kernel

import (
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/shared"
	"metacubexd-server-go/internal/supervisor"
)

// FetchConfig loads the active (ConfigActive) or runtime (ConfigRuntime)
// config once.
func FetchConfig(c *ctl.Client, mode int) tea.Cmd {
	return func() tea.Msg {
		var (
			content string
			err     error
		)
		if mode == ConfigRuntime {
			content, err = c.GetRuntimeConfig()
		} else {
			content, err = c.GetConfig()
		}
		return ConfigLoadedMsg{Mode: mode, Content: content, Err: err}
	}
}

// FetchNetworkSettings loads the editable network fields from the runtime
// config — the file mihomo actually runs — so injected and merged values (like
// tun from a merge overlay) are included.
func FetchNetworkSettings(c *ctl.Client) tea.Cmd {
	return func() tea.Msg {
		content, err := c.GetRuntimeConfig()
		if err != nil {
			return NetworkSettingsMsg{Err: err}
		}
		return NetworkSettingsMsg{Settings: parseNetworkSettings(content)}
	}
}

// PutSection replaces one top-level key with the given value and restarts the
// kernel.
func PutSection(c *ctl.Client, key string, value any) tea.Cmd {
	return func() tea.Msg {
		if err := c.PutSection(key, value, true); err != nil {
			return SectionEditMsg{Err: err}
		}
		return SectionEditMsg{}
	}
}

// SectionEdit replaces one top-level key with a YAML-parsed value and
// restarts the kernel.
func SectionEdit(c *ctl.Client, key, value string) tea.Cmd {
	return PutSection(c, key, parseSectionValue(value))
}

// kernelOpCmd runs a kernel operation via the client and pushes the fresh
// state, refreshing the status bar when done.
func kernelOpCmd(c *ctl.Client, op func(*ctl.Client) (supervisor.KernelState, error)) tea.Cmd {
	return func() tea.Msg {
		st, err := op(c)
		if err != nil {
			return shared.StatusErrorMsg{Err: err}
		}
		return shared.StatusLoadedMsg{State: st}
	}
}
