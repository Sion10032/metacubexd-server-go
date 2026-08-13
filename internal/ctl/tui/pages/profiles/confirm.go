package profiles

import (
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl/tui/shared"
)

// updateConfirmDel handles the delete-confirmation prompt: y confirms the
// deletion of the selected profile, any other key cancels.
func (m *Model) updateConfirmDel(key string) (shared.Tab, tea.Cmd, bool) {
	m.confirmDel = false
	if key == "y" || key == "Y" {
		id := m.SelectedID()
		if id != "" {
			return m, profileOpCmd(m.client, func() error {
				return m.client.ProfileDelete(id)
			}), true
		}
	}
	return m, nil, true
}
