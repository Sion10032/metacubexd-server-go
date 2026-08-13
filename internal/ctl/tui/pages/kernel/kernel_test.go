package kernel

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/components"
	"metacubexd-server-go/internal/ctl/tui/shared"
)

// TestParseNetworkSettings verifies the runtime YAML is parsed into the
// editable network fields, including the nested tun object.
func TestParseNetworkSettings(t *testing.T) {
	content := "mixed-port: 7890\nport: 7890\nsocks-port: 7891\ntun:\n  enable: true\n  device: tun0\n  stack: system\n"
	ns := parseNetworkSettings(content)
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
	ns := parseNetworkSettings("tun:\n  enable: false\n")
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
	if fields["tun-stack"] != "mixed" {
		t.Errorf("tun-stack = %q, want mixed", fields["tun-stack"])
	}
}

// TestViewMenu verifies the Config tab body lists every operation, the
// network fields and the raw YAML viewer, with the selected entry highlighted.
func TestViewMenu(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
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
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
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
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
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
