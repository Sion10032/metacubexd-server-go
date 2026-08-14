package kernel

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/tui/client"
	"metacubexd-server-go/internal/tui/components"
	"metacubexd-server-go/internal/tui/shared"
)

// TestNetworkSettingsFrom verifies mihomo's live /configs response is
// projected into the editable network fields, including the nested tun
// object.
func TestNetworkSettingsFrom(t *testing.T) {
	values := map[string]any{
		"mixed-port": float64(7890),
		"port":       float64(7890),
		"socks-port": float64(7891),
		"tun": map[string]any{
			"enable": true,
			"device": "tun0",
			"stack":  "system",
		},
	}
	ns := networkSettingsFrom(values)
	if !ns.Loaded {
		t.Fatal("loaded should be true")
	}
	for _, f := range networkFields {
		if got := ns.valueOf(f); got == "" {
			t.Errorf("%s value empty, want non-empty", f.label)
		}
	}
	if got := ns.valueOf(networkFields[3]); got != "true" { // tun-enable
		t.Errorf("tun-enable = %q, want true", got)
	}
	if got := ns.valueOf(networkFields[5]); got != "system" { // tun-stack
		t.Errorf("tun-stack = %q, want system", got)
	}
}

// TestTunDefaults verifies tun sub-fields absent from the config fall back to
// mihomo's defaults (device=Mihomo, stack=mixed).
func TestTunDefaults(t *testing.T) {
	ns := networkSettingsFrom(map[string]any{"tun": map[string]any{"enable": false}})
	fields := map[string]string{}
	for _, f := range networkFields {
		fields[f.label] = ns.valueOf(f)
	}
	if fields["tun-enable"] != "false" {
		t.Errorf("tun-enable = %q, want false", fields["tun-enable"])
	}
	if fields["tun-device"] != "Mihomo" {
		t.Errorf("tun-device = %q, want Mihomo", fields["tun-device"])
	}
	if fields["tun-stack"] != "gvisor" {
		t.Errorf("tun-stack = %q, want gvisor", fields["tun-stack"])
	}
}

// TestEditFieldAcceptsInput verifies the network-field editor receives typed
// characters. textinput.Model is a value type, so it must be stored AFTER
// Focus() flips its focus flag — otherwise Update() drops every key at the
// `if !m.focus { return }` guard.
func TestEditFieldAcceptsInput(t *testing.T) {
	cases := []struct {
		name   string
		idx    int // index into networkFields
		values map[string]any
	}{
		{"mixed-port", 0, map[string]any{"mixed-port": float64(7890)}},
		{"tun-device", 4, map[string]any{"tun": map[string]any{"enable": true, "device": "utun8"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(client.NewClient("http://127.0.0.1:1", "", false))
			m.network = networkSettingsFrom(tc.values)
			m.startEditField(tc.idx)
			if !m.editInput.Focused() {
				t.Fatal("editor not focused after startEditField")
			}
			m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
			if got := m.editInput.Value(); !strings.Contains(got, "x") {
				t.Errorf("input dropped: got %q, want it to contain 'x'", got)
			}
		})
	}
}

// TestEnumFieldSelect verifies tun-stack opens an option picker (not the text
// editor), navigation cycles the highlighted option with wraparound, and the
// picked value is written back through the tun sub-field without dropping
// sibling keys.
func TestEnumFieldSelect(t *testing.T) {
	m := New(client.NewClient("http://127.0.0.1:1", "", false))
	m.network = networkSettingsFrom(map[string]any{
		"tun": map[string]any{"enable": true, "stack": "system"},
	})

	// tun-stack = networkFields[5]; current value "system" is options[0].
	m.startEditField(5)
	if !m.editingEnum {
		t.Fatal("tun-stack should open the option picker, not the text editor")
	}
	if m.editing {
		t.Error("tun-stack should not open the free-form text editor")
	}
	if m.enumSel != 0 {
		t.Errorf("enumSel = %d, want 0 (current value 'system')", m.enumSel)
	}

	// right cycles to the next option without closing the picker.
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.enumSel != 1 {
		t.Errorf("after right: enumSel = %d, want 1", m.enumSel)
	}
	if !m.editingEnum {
		t.Error("picker closed on navigation key")
	}

	// wraparound: at n=3, two more rights wrap back to index 0.
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.enumSel != 0 {
		t.Errorf("after wraparound: enumSel = %d, want 0", m.enumSel)
	}

	// setField rebuilds the tun object with the picked stack, keeping siblings.
	key, value := m.network.setField(networkFields[5], "gvisor")
	if key != "tun" {
		t.Errorf("setField key = %q, want tun", key)
	}
	tun, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("setField value = %T, want map[string]any", value)
	}
	if tun["stack"] != "gvisor" {
		t.Errorf("tun.stack = %v, want gvisor", tun["stack"])
	}
	if tun["enable"] != true {
		t.Errorf("setField dropped existing tun.enable: %v", tun["enable"])
	}
}

// TestViewMenu verifies the Config tab body lists every operation, the
// network fields and the raw YAML viewer, with the selected entry highlighted.
func TestViewMenu(t *testing.T) {
	m := New(client.NewClient("http://127.0.0.1:1", "", false))
	got := m.View()
	plain := shared.ANSIRe.ReplaceAllString(got, "")
	for _, want := range []string{
		"[kernel]", "Start", "Stop", "Restart",
		"[network]", "mixed-port", "http-port", "socks-port", "tun-enable", "tun-device", "tun-stack",
		"View YAML",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("kernel tab = %q, missing %q", plain, want)
		}
	}
	if !strings.Contains(got, "> ") || !strings.Contains(got, "\x1b[") {
		t.Errorf("kernel tab missing selection highlight: %q", got)
	}
}

// TestSectionOverlayLayers verifies the section editor modal renders the
// config viewer beneath it: opening the viewer then the section editor keeps
// the viewer modal header visible in the combined overlay.
func TestSectionOverlayLayers(t *testing.T) {
	m := New(client.NewClient("http://127.0.0.1:1", "", false))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.SetViewingConfig(true)
	m.Update(ConfigLoadedMsg{Mode: ConfigActive, Content: "mixed-port: 7890\n"})

	// 'e' inside the viewer modal opens the section editor.
	viewer := m.Overlay()
	if viewer == nil {
		t.Fatal("config viewer should be the active overlay")
	}
	viewer.Update(tea.KeyPressMsg{Code: 'e'})
	modal := m.Overlay()
	if modal == nil {
		t.Fatal("section editor should be the active overlay")
	}
	got := shared.ANSIRe.ReplaceAllString(modal.View(80, 24), "")
	if !strings.Contains(got, "Edit Section") {
		t.Errorf("overlay missing section editor header:\n%s", got)
	}
	if !strings.Contains(got, "View Config (active)") {
		t.Errorf("overlay missing config viewer beneath section editor:\n%s", got)
	}
	if !strings.Contains(got, "mixed-port") {
		t.Errorf("overlay missing config content beneath section editor:\n%s", got)
	}
}

// TestSectionOverlayIdenticalToTwoPass verifies the section modal's combined
// View is byte-identical to the original two-pass overlay composition
// (config viewer first, section form layered on top).
func TestSectionOverlayIdenticalToTwoPass(t *testing.T) {
	m := New(client.NewClient("http://127.0.0.1:1", "", false))
	m.SetViewingConfig(true)
	m.Update(ConfigLoadedMsg{Mode: ConfigActive, Content: strings.Repeat("line\n", 50)})
	m.editingSection = true
	m.sectionForm = newSectionForm()

	const w, h = 80, 24

	// The original layout: config modal over the frame base, then the section
	// form over that.
	base := lipgloss.NewStyle().Width(w).Height(h).Render(strings.Repeat("\n", h))
	cfg := m.configModalView(w, h)
	sec := m.sectionFormView()
	step1 := components.OverlayModal(base, cfg, w, h)
	want := components.OverlayModal(step1, sec, w, h)

	// The page's single overlay: config modal + section form composed into one
	// canvas, then centered over the same base by the root.
	got := components.OverlayModal(base, m.Overlay().View(w, h), w, h)

	if got != want {
		t.Errorf("section overlay differs from the two-pass composition")
	}
}
