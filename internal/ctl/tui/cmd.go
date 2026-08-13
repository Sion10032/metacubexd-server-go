package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/shared"
)

// fetchProfilesCmd loads the profile list once.
func fetchProfilesCmd(c *ctl.Client) tea.Cmd {
	return func() tea.Msg {
		list, err := c.ProfilesList()
		return profilesLoadedMsg{list: list, err: err}
	}
}

// fetchConfigCmd loads the active (mode 0) or runtime (mode 1) config once.
func fetchConfigCmd(c *ctl.Client, mode int) tea.Cmd {
	return func() tea.Msg {
		var (
			content string
			err     error
		)
		if mode == configRuntime {
			content, err = c.GetRuntimeConfig()
		} else {
			content, err = c.GetConfig()
		}
		return configLoadedMsg{mode: mode, content: content, err: err}
	}
}

// profileOpCmd runs a profile operation; on success the caller refreshes the
// lists via profileOpMsg.
func profileOpCmd(c *ctl.Client, op func() error) tea.Cmd {
	return func() tea.Msg {
		if err := op(); err != nil {
			return profileOpMsg{err: err}
		}
		return profileOpMsg{}
	}
}

// importCmd imports a subscription URL under an optional name into a new
// profile.
func importCmd(c *ctl.Client, url, name string) tea.Cmd {
	return func() tea.Msg {
		if _, err := c.ProfileImport(url, name); err != nil {
			return profileOpMsg{err: err}
		}
		return profileOpMsg{}
	}
}

// updateStatus handles the kernel status messages: fresh states, errors and
// the periodic tick; the spinner keeps animating while an operation runs.
func (m Model) updateStatus(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case shared.StatusLoadedMsg:
		m.state = &msg.State
		m.kernel.operating = false
		m.kernel.kConfirming = false
		return m, nil
	case shared.StatusErrorMsg:
		m.err = msg.Err
		m.kernel.operating = false
		m.kernel.kConfirming = false
		return m, nil
	case spinner.TickMsg:
		if m.kernel.operating {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case shared.TickMsg:
		return m, tea.Batch(shared.FetchStatus(m.client), shared.StatusTick())
	}
	return m, nil
}
