package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"metacubexd-server-go/internal/ctl/tui/components"
	"metacubexd-server-go/internal/ctl/tui/shared"
)

// newImportForm builds an import popup with the URL field focused.
func newImportForm() components.Form {
	url := textinput.New()
	url.Prompt = "URL:  "
	url.Placeholder = "https://example.com/sub.yaml"
	url.SetWidth(50)
	name := textinput.New()
	name.Prompt = "Name: "
	name.Placeholder = "(optional)"
	name.SetWidth(50)
	return components.NewForm(url, name)
}

// updateConfirmDel handles the delete-confirmation prompt: y confirms the
// deletion of the selected profile, any other key cancels.
func (m Model) updateConfirmDel(key string) (Model, tea.Cmd) {
	m.confirmDel = false
	if key == "y" || key == "Y" {
		id := m.profiles.SelectedID()
		if id != "" {
			return m, profileOpCmd(m.client, func() error {
				return m.client.ProfileDelete(id)
			})
		}
	}
	return m, nil
}

// updateProfilesKeys handles the Profiles tab operations: a activates the
// selected profile, u refreshes it, d asks for delete confirmation, i opens
// the import form.
func (m Model) updateProfilesKeys(key string, msg tea.Msg) (Model, tea.Cmd) {
	switch key {
	case "a":
		if id := m.profiles.SelectedID(); id != "" {
			m.profActive = id
			return m, profileOpCmd(m.client, func() error {
				_, err := m.client.ProfileActivate(id)
				return err
			})
		}
	case "u":
		if id := m.profiles.SelectedID(); id != "" {
			return m, profileOpCmd(m.client, func() error {
				_, err := m.client.ProfileRefresh(id)
				return err
			})
		}
	case "d":
		if m.profiles.SelectedID() != "" {
			m.confirmDel = true
		}
	case "i":
		m.importing = true
		m.form = newImportForm()
		return m, m.form.Fields[0].Focus()
	default:
		// Selection keys drive the table; scroll keys fall through to the log
		// viewport.
		m.profiles.Update(msg)
		if key == "up" || key == "down" || key == "enter" {
			return m, nil // consumed by the table
		}
	}
	return m, nil
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
			m.form.Fields[0].Reset()
			m.form.Fields[1].Reset()
			return m, nil
		case "enter":
			url := strings.TrimSpace(m.form.Fields[0].Value())
			name := strings.TrimSpace(m.form.Fields[1].Value())
			m.importing = false
			m.form.Fields[0].Reset()
			m.form.Fields[1].Reset()
			if url == "" {
				return m, nil
			}
			return m, importCmd(m.client, url, name)
		default:
			var cmd tea.Cmd
			m.form, cmd = m.form.Update(msg)
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
		shared.FrameTop(cw, "", ""),
		shared.FrameRow(header, cw),
		shared.FrameSep(cw),
		shared.FrameRow(m.form.Fields[0].View(), cw),
		shared.FrameRow(m.form.Fields[1].View(), cw),
		shared.FrameSep(cw),
		shared.FrameRow("tab:switch  enter:import  esc:cancel", cw),
		shared.FrameBottom(cw),
	}, "\n")
	return lipgloss.NewStyle().
		Width(width).Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)
}
