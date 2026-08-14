package kernel

import (
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/tui/client"
	"metacubexd-server-go/internal/tui/shared"
	"metacubexd-server-go/internal/api"
)

// FetchConfig loads the active (ConfigActive) or runtime (ConfigRuntime)
// config once.
func FetchConfig(c *client.Client, mode int) tea.Cmd {
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

// FetchNetworkSettings loads the editable network fields from mihomo's live
// /configs endpoint (GET /api/clash/configs) — the values the kernel
// actually applies at runtime — so tun and other injected/merged/runtime
// values are reflected even when they differ from the on-disk active config.
func FetchNetworkSettings(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		values, err := c.GetConfigs()
		if err != nil {
			return NetworkSettingsMsg{Err: err}
		}
		return NetworkSettingsMsg{Settings: networkSettingsFrom(values)}
	}
}

// PutSection replaces one top-level key with the given value and restarts the
// kernel.
func PutSection(c *client.Client, key string, value any) tea.Cmd {
	return func() tea.Msg {
		if err := c.PutSection(key, value, true); err != nil {
			return SectionEditMsg{Err: err}
		}
		return SectionEditMsg{}
	}
}

// SectionEdit replaces one top-level key with a YAML-parsed value and
// restarts the kernel.
func SectionEdit(c *client.Client, key, value string) tea.Cmd {
	return PutSection(c, key, parseSectionValue(value))
}

// kernelOpCmd runs a kernel operation via the client and pushes the fresh
// state, refreshing the status bar when done.
func kernelOpCmd(c *client.Client, op func(*client.Client) (api.KernelState, error)) tea.Cmd {
	return func() tea.Msg {
		st, err := op(c)
		if err != nil {
			return shared.StatusErrorMsg{Err: err}
		}
		return shared.StatusLoadedMsg{State: st}
	}
}
