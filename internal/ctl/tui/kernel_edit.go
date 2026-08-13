package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"gopkg.in/yaml.v3"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/components"
	"metacubexd-server-go/internal/ctl/tui/shared"
)

// startEditField opens the single-value editor for a network field, prefilled
// with its current value.
func (k KernelModel) startEditField(i int, m Model) (Model, tea.Cmd) {
	m.kernel = k
	m.kernel.editing = true
	m.kernel.editField = i
	in := textinput.New()
	in.Prompt = ""
	in.SetWidth(40)
	in.SetValue(m.kernel.network.valueOf(networkFields[i]))
	m.kernel.editInput = in
	return m, in.Focus()
}

// updateEdit drives the network field editor: enter saves via PutSection,
// esc cancels.
func (k KernelModel) updateEdit(msg tea.Msg, m Model) (Model, tea.Cmd) {
	m.kernel = k
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.kernel.editing = false
			return m, nil
		case "enter":
			raw := strings.TrimSpace(m.kernel.editInput.Value())
			m.kernel.editing = false
			if raw == "" {
				return m, nil
			}
			key, value := m.kernel.network.setField(networkFields[m.kernel.editField], raw)
			return m, putSectionCmd(m.client, key, value)
		default:
			var cmd tea.Cmd
			m.kernel.editInput, cmd = m.kernel.editInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// putSectionCmd replaces one top-level key with a parsed value and restarts
// the kernel.
func putSectionCmd(c *ctl.Client, key string, value any) tea.Cmd {
	return func() tea.Msg {
		if err := c.PutSection(key, value, true); err != nil {
			return sectionEditMsg{err: err}
		}
		return sectionEditMsg{}
	}
}

// updateConfig drives the config viewer modal: esc closes, e opens the
// section editor, c toggles active/runtime, other keys scroll the viewport.
func (k KernelModel) updateConfig(msg tea.Msg, m Model) (Model, tea.Cmd) {
	m.kernel = k
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.kernel.viewingConfig = false
			return m, nil
		case "e":
			m.kernel.editingSection = true
			m.kernel.sectionForm = newSectionForm()
			return m, m.kernel.sectionForm.Fields[0].Focus()
		case "c":
			m.config.ToggleMode()
			m.config.ResetScroll()
			return m, fetchConfigCmd(m.client, m.config.mode)
		default:
			return m, m.config.Update(msg)
		}
	}
	return m, nil
}

// newSectionForm builds a section editor popup with the key field focused.
func newSectionForm() components.Form {
	key := textinput.New()
	key.Prompt = "Key:   "
	key.Placeholder = "mixed-port"
	key.SetWidth(50)
	value := textinput.New()
	value.Prompt = "Value: "
	value.Placeholder = "7890"
	value.SetWidth(50)
	return components.NewForm(key, value)
}

// parseSectionValue parses the entered value as YAML so a plain scalar maps to
// its natural Go type (int, bool, ...); anything unparseable stays a string.
func parseSectionValue(s string) any {
	var v any
	if err := yaml.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return s
}

// updateSectionForm drives the section editor popup: tab switches fields,
// enter saves via PutSection, esc cancels.
func (k KernelModel) updateSectionForm(msg tea.Msg, m Model) (Model, tea.Cmd) {
	m.kernel = k
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.kernel.editingSection = false
			return m, nil
		case "enter":
			key := strings.TrimSpace(m.kernel.sectionForm.Fields[0].Value())
			value := strings.TrimSpace(m.kernel.sectionForm.Fields[1].Value())
			m.kernel.editingSection = false
			if key == "" {
				return m, nil
			}
			return m, sectionEditCmd(m.client, key, value)
		default:
			var cmd tea.Cmd
			m.kernel.sectionForm, cmd = m.kernel.sectionForm.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// sectionEditCmd replaces one top-level key with a YAML-parsed value and
// restarts the kernel.
func sectionEditCmd(c *ctl.Client, key, value string) tea.Cmd {
	return putSectionCmd(c, key, parseSectionValue(value))
}

// updateConfigMsg handles config fetches, network settings loads and section
// edit results; successful edits refresh the config, status and network
// fields.
func (m Model) updateConfigMsg(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case configLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		if msg.mode == m.config.mode {
			m.config.SetContent(msg.content)
		}
		return m, nil
	case networkSettingsMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.kernel.network = msg.settings
		return m, nil
	case sectionEditMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, tea.Batch(fetchConfigCmd(m.client, m.config.mode), shared.FetchStatus(m.client), fetchNetworkSettingsCmd(m.client))
	}
	return m, nil
}
