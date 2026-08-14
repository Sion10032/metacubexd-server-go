package kernel

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"gopkg.in/yaml.v3"

	"metacubexd-server-go/internal/tui/components"
	"metacubexd-server-go/internal/tui/shared"
)

// editModal is the single-value network-field editor popup.
type editModal struct{ m *Model }

// Update implements shared.Modal, driving the editor (enter saves via
// PutSection, esc cancels, other keys edit the input).
func (md *editModal) Update(msg tea.Msg) (shared.Modal, tea.Cmd) {
	return md, md.m.updateEdit(msg)
}

// View implements shared.Modal.
func (md *editModal) View(w, h int) string {
	return md.m.editInputView()
}

// enumModal is the option-picker popup for an enumerated network field.
type enumModal struct{ m *Model }

// Update implements shared.Modal, driving the picker (←/→ switch, enter
// saves, esc cancels).
func (md *enumModal) Update(msg tea.Msg) (shared.Modal, tea.Cmd) {
	return md, md.m.updateEnumEdit(msg)
}

// View implements shared.Modal.
func (md *enumModal) View(w, h int) string {
	return md.m.enumEditView()
}

// sectionModal is the section editor popup. While open the config viewer
// stays visible beneath it (matching the original layout), so its View layers
// the section form over the viewer modal content.
type sectionModal struct{ m *Model }

// Update implements shared.Modal, driving the two-field form (tab switches
// fields, enter saves via SectionEdit, esc cancels).
func (md *sectionModal) Update(msg tea.Msg) (shared.Modal, tea.Cmd) {
	return md, md.m.updateSectionForm(msg)
}

// View implements shared.Modal.
func (md *sectionModal) View(w, h int) string {
	cfg := md.m.configModalView(w, h)
	sec := md.m.sectionFormView()
	unionW := max(lipgloss.Width(cfg), lipgloss.Width(sec))
	unionH := max(lipgloss.Height(cfg), lipgloss.Height(sec))
	return components.OverlayModal(cfg, sec, unionW, unionH)
}

// startEditField opens the editor for a network field. Enumerated fields
// (with non-empty options) open an option picker; the rest open the
// single-value text editor, prefilled with the current value.
func (m *Model) startEditField(i int) (shared.Tab, tea.Cmd, bool) {
	f := networkFields[i]
	if len(f.options) > 0 {
		m.editingEnum = true
		m.editField = i
		cur := m.network.valueOf(f)
		m.enumSel = 0
		for j, o := range f.options {
			if o == cur {
				m.enumSel = j
				break
			}
		}
		return m, nil, true
	}
	m.editing = true
	m.editField = i
	in := textinput.New()
	in.Prompt = ""
	in.SetWidth(40)
	in.SetValue(m.network.valueOf(networkFields[i]))
	cmd := in.Focus()
	m.editInput = in
	return m, cmd, true
}

// updateEdit drives the network field editor: enter saves via PutSection, esc
// cancels.
func (m *Model) updateEdit(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.editing = false
			return nil
		case "enter":
			raw := strings.TrimSpace(m.editInput.Value())
			m.editing = false
			if raw == "" {
				return nil
			}
			key, value := m.network.setField(networkFields[m.editField], raw)
			return PutSection(m.client, key, value)
		default:
			var cmd tea.Cmd
			m.editInput, cmd = m.editInput.Update(msg)
			return cmd
		}
	}
	return nil
}

// updateEnumEdit drives the option-picker popup for an enumerated network
// field: ←/→ (and ↑/↓, vim hjkl) cycle the selection, enter saves via
// PutSection, esc cancels.
func (m *Model) updateEnumEdit(msg tea.Msg) tea.Cmd {
	f := networkFields[m.editField]
	opts := f.options
	n := len(opts)
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.editingEnum = false
			return nil
		case "enter":
			v := opts[m.enumSel]
			m.editingEnum = false
			key, value := m.network.setField(f, v)
			return PutSection(m.client, key, value)
		case "up", "left", "k", "h":
			m.enumSel = (m.enumSel + n - 1) % n
		case "down", "right", "j", "l":
			m.enumSel = (m.enumSel + 1) % n
		}
	}
	return nil
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
// enter saves via SectionEdit, esc cancels.
func (m *Model) updateSectionForm(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.editingSection = false
			return nil
		case "enter":
			key := strings.TrimSpace(m.sectionForm.Fields[0].Value())
			value := strings.TrimSpace(m.sectionForm.Fields[1].Value())
			m.editingSection = false
			if key == "" {
				return nil
			}
			return SectionEdit(m.client, key, value)
		default:
			var cmd tea.Cmd
			m.sectionForm, cmd = m.sectionForm.Update(msg)
			return cmd
		}
	}
	return nil
}

// editInputView renders the network field editor popup: a single prefilled
// textinput with a header and a key-hint footer.
func (m *Model) editInputView() string {
	const cw = 40
	f := networkFields[m.editField]
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render("Edit " + f.label)
	sep := strings.Repeat("─", cw)
	footer := lipgloss.NewStyle().Width(cw).Align(lipgloss.Center).Render("enter:save  esc:cancel")
	inner := strings.Join([]string{header, sep, m.editInput.View(), sep, footer}, "\n")
	return components.BorderedModal(inner, cw)
}

// enumEditView renders the option-picker popup for an enumerated network
// field: a header, the options with the highlighted one marked, and a
// key-hint footer.
func (m *Model) enumEditView() string {
	f := networkFields[m.editField]
	const cw = 26
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render("Edit " + f.label)
	sep := strings.Repeat("─", cw)
	rows := make([]string, 0, len(f.options))
	for i, o := range f.options {
		marker := "  "
		style := lipgloss.NewStyle().Width(cw)
		if i == m.enumSel {
			marker = "▶ "
			style = shared.SelectedStyle.Width(cw)
		}
		rows = append(rows, style.Render(marker+o))
	}
	footer := lipgloss.NewStyle().Width(cw).Align(lipgloss.Center).Render("↑↓:switch  enter:save  esc:cancel")
	parts := append([]string{header, sep}, rows...)
	parts = append(parts, sep, footer)
	inner := strings.Join(parts, "\n")
	return components.BorderedModal(inner, cw)
}

// sectionFormView renders the section editor popup: key and value textinputs
// with a header and a key-hint footer.
func (m *Model) sectionFormView() string {
	const cw = 60
	title := "Edit Section"
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render(title)
	sep := strings.Repeat("─", cw)
	footer := lipgloss.NewStyle().Width(cw).Align(lipgloss.Center).Render("tab:switch  enter:save  esc:cancel")
	inner := strings.Join([]string{
		header,
		sep,
		m.sectionForm.Fields[0].View(),
		m.sectionForm.Fields[1].View(),
		sep,
		footer,
	}, "\n")
	return components.BorderedModal(inner, cw)
}
