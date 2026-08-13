package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"gopkg.in/yaml.v3"

	"metacubexd-server-go/internal/ctl"
)

// networkSettings holds the editable network fields of the active config.
type networkSettings struct {
	values map[string]any // top-level keys: mixed-port, port, socks-port, tun
	loaded bool
}

// networkField describes one editable network entry.
type networkField struct {
	label string
	key   string // top-level config key
	sub   string // tun sub-key ("" for top-level scalars)
}

// networkFields lists the editable network entries in display order.
var networkFields = []networkField{
	{"mixed-port", "mixed-port", ""},
	{"http-port", "port", ""},
	{"socks-port", "socks-port", ""},
	{"tun-enable", "tun", "enable"},
	{"tun-device", "tun", "device"},
	{"tun-stack", "tun", "stack"},
}

// valueOf returns the current value of a network field as a string. Absent tun
// sub-fields fall back to mihomo's defaults (stack=mixed, device=Mihomo).
func (ns networkSettings) valueOf(f networkField) string {
	if f.sub == "" {
		return fmtValue(ns.values[f.key])
	}
	if m, ok := ns.values["tun"].(map[string]any); ok {
		if v, ok := m[f.sub]; ok && v != nil {
			return fmtValue(v)
		}
	}
	switch f.sub {
	case "stack":
		return "mixed"
	case "device":
		return "Mihomo"
	}
	return ""
}

// setField returns the (key, value) pair for PutSection when editing f with
// the raw string raw. Tun sub-fields rebuild the whole tun object.
func (ns networkSettings) setField(f networkField, raw string) (string, any) {
	v := parseSectionValue(raw)
	if f.sub == "" {
		return f.key, v
	}
	tun := map[string]any{}
	if m, ok := ns.values["tun"].(map[string]any); ok {
		for k, vv := range m {
			tun[k] = vv
		}
	}
	tun[f.sub] = v
	return "tun", tun
}

// fmtValue renders a config value for display.
func fmtValue(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// fetchNetworkSettingsCmd loads the editable network fields from the runtime
// config — the file mihomo actually runs — so injected and merged values (like
// tun from a merge overlay) are included.
func fetchNetworkSettingsCmd(c *ctl.Client) tea.Cmd {
	return func() tea.Msg {
		content, err := c.GetRuntimeConfig()
		if err != nil {
			return networkSettingsMsg{err: err}
		}
		return networkSettingsMsg{settings: parseNetworkSettings(content)}
	}
}

// parseNetworkSettings extracts the editable network fields from a YAML config
// body.
func parseNetworkSettings(content string) networkSettings {
	ns := networkSettings{values: map[string]any{}}
	var v any
	if err := yaml.Unmarshal([]byte(content), &v); err != nil {
		return ns
	}
	top, ok := v.(map[string]any)
	if !ok {
		return ns
	}
	for _, key := range []string{"mixed-port", "port", "socks-port", "tun"} {
		if val, ok := top[key]; ok && val != nil {
			ns.values[key] = val
		}
	}
	ns.loaded = true
	return ns
}
