package tui

import (
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
)

// requestBackgroundColorCmd asks the terminal for its background color so the
// theme can adapt to dark/light.
func requestBackgroundColorCmd() tea.Cmd {
	return func() tea.Msg {
		return tea.RequestBackgroundColor()
	}
}

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

// fetchStatusCmd fetches the kernel status once.
func fetchStatusCmd(c *ctl.Client) tea.Cmd {
	return func() tea.Msg {
		st, err := c.KernelStatus()
		if err != nil {
			return statusErrorMsg{err: err}
		}
		return statusLoadedMsg{state: st}
	}
}

// statusTick schedules the next status refresh one second from now.
func statusTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

// updateStatus handles the kernel status messages: fresh states, errors and
// the periodic tick; the spinner keeps animating while an operation runs.
func (m Model) updateStatus(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusLoadedMsg:
		m.state = &msg.state
		m.kernel.operating = false
		m.kernel.kConfirming = false
		return m, nil
	case statusErrorMsg:
		m.err = msg.err
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
	case tickMsg:
		return m, tea.Batch(fetchStatusCmd(m.client), statusTick())
	}
	return m, nil
}
