package tui

import (
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Config modes select which config the viewport shows.
const (
	configActive  = 0
	configRuntime = 1
)

// ConfigModel renders the active/runtime config YAML in a scrollable
// viewport. The mode selects which of the two the view displays.
type ConfigModel struct {
	viewport viewport.Model
	mode     int // configActive or configRuntime
	loaded   bool
}

// NewConfigModel returns an empty config viewport.
func NewConfigModel() ConfigModel {
	return ConfigModel{viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}
}

// Mode returns the label of the config currently selected.
func (c ConfigModel) Mode() string {
	if c.mode == configRuntime {
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
