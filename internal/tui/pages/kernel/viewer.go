package kernel

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/tui/components"
	"metacubexd-server-go/internal/tui/shared"
)

// Config modes select which config the viewport shows.
const (
	ConfigActive  = 0
	ConfigRuntime = 1
)

// ConfigModel renders the active/runtime config YAML in a scrollable
// viewport. The mode selects which of the two the view displays. It is the
// Config tab's viewer component, owned by the page model and exposed to the
// root through the viewer modal.
type ConfigModel struct {
	viewport viewport.Model
	mode     int // ConfigActive or ConfigRuntime
	loaded   bool
}

// NewConfigModel returns an empty config viewport.
func NewConfigModel() ConfigModel {
	return ConfigModel{viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}
}

// Mode returns the label of the config currently selected.
func (c ConfigModel) Mode() string {
	if c.mode == ConfigRuntime {
		return "runtime"
	}
	return "active"
}

// ToggleMode flips between active and runtime config and clears the loaded
// flag so the view shows a placeholder until the next fetch lands.
func (c *ConfigModel) ToggleMode() {
	c.mode = 1 - c.mode
	c.loaded = false
}

// ResetScroll scrolls the viewport back to the top.
func (c *ConfigModel) ResetScroll() {
	c.viewport.GotoTop()
}

// SetSize resizes the viewport.
func (c *ConfigModel) SetSize(width, height int) {
	c.viewport.SetWidth(width)
	c.viewport.SetHeight(height)
}

// SetContent replaces the viewport content and marks it loaded.
func (c *ConfigModel) SetContent(content string) {
	c.loaded = true
	c.viewport.SetContent(content)
}

// Update forwards messages to the viewport.
func (c *ConfigModel) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	c.viewport, cmd = c.viewport.Update(msg)
	return cmd
}

// View renders the viewport, or a centered hint while no config is loaded.
func (c ConfigModel) View() string {
	if !c.loaded {
		return lipgloss.NewStyle().
			Width(c.viewport.Width()).
			Height(c.viewport.Height()).
			Align(lipgloss.Center, lipgloss.Center).
			Faint(true).
			Render("— no config loaded —")
	}
	return c.viewport.View()
}

// viewerModal is the config viewer popup: the bordered YAML viewport plus the
// key-hint footer. It also hosts the section editor, which renders on top of
// the viewer when editingSection is active, matching the original layout.
type viewerModal struct{ m *Model }

// Update implements shared.Modal, driving the viewer state (esc closes, e
// opens the section editor, c toggles active/runtime, other keys scroll).
func (md *viewerModal) Update(msg tea.Msg) (shared.Modal, tea.Cmd) {
	return md, md.m.updateConfig(msg)
}

// View implements shared.Modal.
func (md *viewerModal) View(w, h int) string {
	return md.m.configModalView(w, h)
}

// configModalView renders the bordered config viewer modal for the given
// terminal size: a bold header, the scrollable config viewport, and a
// key-hint footer.
func (m *Model) configModalView(w, h int) string {
	cw := w - 8
	if cw < 24 {
		cw = 24
	}
	if cw > 80 {
		cw = 80
	}
	title := "View Config (" + m.config.Mode() + ")"
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render(title)
	sep := strings.Repeat("─", cw)
	footer := lipgloss.NewStyle().Width(cw).Align(lipgloss.Center).Render("c:active/runtime  e:edit  ↑↓:scroll  esc:close")
	viewHeight := h - 10
	if viewHeight < 1 {
		viewHeight = 1
	}
	m.config.SetSize(cw, viewHeight)
	inner := strings.Join([]string{header, sep, m.config.View(), sep, footer}, "\n")
	return components.BorderedModal(inner, cw)
}
