package profiles

import (
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/tui/client"
)

// FetchProfiles loads the profile list once.
func FetchProfiles(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		list, err := c.ProfilesList()
		return ProfilesLoadedMsg{List: list, Err: err}
	}
}

// profileOpCmd runs a profile operation; on success the caller refreshes the
// lists via ProfileOpMsg.
func profileOpCmd(c *client.Client, op func() error) tea.Cmd {
	return func() tea.Msg {
		if err := op(); err != nil {
			return ProfileOpMsg{Err: err}
		}
		return ProfileOpMsg{}
	}
}

// importCmd imports a subscription URL under an optional name into a new
// profile.
func importCmd(c *client.Client, url, name string) tea.Cmd {
	return func() tea.Msg {
		if _, err := c.ProfileImport(url, name); err != nil {
			return ProfileOpMsg{Err: err}
		}
		return ProfileOpMsg{}
	}
}
