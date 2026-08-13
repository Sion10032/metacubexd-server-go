package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// importForm bundles the two textinputs of the import popup.
type importForm struct {
	url   textinput.Model
	name  textinput.Model
	focus int // 0 = URL, 1 = Name
}

// newImportForm builds an import popup with the URL field focused.
func newImportForm() importForm {
	url := textinput.New()
	url.Prompt = "URL:  "
	url.Placeholder = "https://example.com/sub.yaml"
	url.SetWidth(50)
	name := textinput.New()
	name.Prompt = "Name: "
	name.Placeholder = "(optional)"
	name.SetWidth(50)
	return importForm{url: url, name: name}
}

// updateImport drives the import popup: tab switches the focused field, enter
// imports the entered URL+name, esc cancels; other keys feed the focused
// textinput.
func (m Model) updateImport(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.importing = false
			m.form.url.Reset()
			m.form.name.Reset()
			return m, nil
		case "tab":
			m.form.focus = 1 - m.form.focus
			if m.form.focus == 0 {
				m.form.name.Blur()
				return m, m.form.url.Focus()
			}
			m.form.url.Blur()
			return m, m.form.name.Focus()
		case "enter":
			url := strings.TrimSpace(m.form.url.Value())
			name := strings.TrimSpace(m.form.name.Value())
			m.importing = false
			m.form.url.Reset()
			m.form.name.Reset()
			if url == "" {
				return m, nil
			}
			return m, importCmd(m.client, url, name)
		default:
			if m.form.focus == 0 {
				var cmd tea.Cmd
				m.form.url, cmd = m.form.url.Update(msg)
				return m, cmd
			}
			var cmd tea.Cmd
			m.form.name, cmd = m.form.name.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// importFormView renders the import popup as a bordered modal: a bold header,
// the two textinputs, then a separated key-hint footer, centered in the body
// area.
func (m Model) importFormView(width, height int) string {
	const cw = 60 // modal content width
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render("Import subscription")
	content := strings.Join([]string{
		frameTop(cw, "", ""),
		frameRow(header, cw),
		frameSep(cw),
		frameRow(m.form.url.View(), cw),
		frameRow(m.form.name.View(), cw),
		frameSep(cw),
		frameRow("tab:switch  enter:import  esc:cancel", cw),
		frameBottom(cw),
	}, "\n")
	return lipgloss.NewStyle().
		Width(width).Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)
}
