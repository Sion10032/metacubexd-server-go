package tui

import (
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl/tui/pages/kernel"
	"metacubexd-server-go/internal/ctl/tui/pages/profiles"
)

// updateProfilesMsg routes profile load results and operation outcomes.
// Errors surface on the root status bar; successful results are forwarded to
// the Profiles page, which refreshes its table and re-issues the refetch.
func (m Model) updateProfilesMsg(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case profiles.ProfilesLoadedMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		tab, cmd := m.tabs[1].Update(msg)
		m.tabs[1] = tab
		return m, cmd
	case profiles.ProfileOpMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		tab, cmd := m.tabs[1].Update(msg)
		m.tabs[1] = tab
		return m, cmd
	}
	return m, nil
}

// updateKernelMsg routes config fetches, network settings loads and section
// edit results to the Kernel page. Errors surface on the root status bar;
// successful results are forwarded to the page.
func (m Model) updateKernelMsg(msg tea.Msg) (Model, tea.Cmd) {
	var err error
	switch msg := msg.(type) {
	case kernel.ConfigLoadedMsg:
		err = msg.Err
	case kernel.NetworkSettingsMsg:
		err = msg.Err
	case kernel.SectionEditMsg:
		err = msg.Err
	}
	if err != nil {
		m.err = err
		return m, nil
	}
	tab, cmd := m.tabs[2].Update(msg)
	m.tabs[2] = tab
	return m, cmd
}
