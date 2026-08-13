package kernel

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// NetworkSettings holds the editable network fields of the active config.
type NetworkSettings struct {
	Values map[string]any // top-level keys: mixed-port, port, socks-port, tun
	Loaded bool
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
func (ns NetworkSettings) valueOf(f networkField) string {
	if f.sub == "" {
		return fmtValue(ns.Values[f.key])
	}
	if m, ok := ns.Values["tun"].(map[string]any); ok {
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
func (ns NetworkSettings) setField(f networkField, raw string) (string, any) {
	v := parseSectionValue(raw)
	if f.sub == "" {
		return f.key, v
	}
	tun := map[string]any{}
	if m, ok := ns.Values["tun"].(map[string]any); ok {
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

// parseNetworkSettings extracts the editable network fields from a YAML config
// body.
func parseNetworkSettings(content string) NetworkSettings {
	ns := NetworkSettings{Values: map[string]any{}}
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
			ns.Values[key] = val
		}
	}
	ns.Loaded = true
	return ns
}
