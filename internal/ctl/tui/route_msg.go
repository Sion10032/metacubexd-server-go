package tui

import (
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl/tui/pages/kernel"
	"metacubexd-server-go/internal/ctl/tui/pages/profiles"
	"metacubexd-server-go/internal/ctl/tui/pages/proxies"
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
		tab, cmd, _ := m.tabs[idxProfiles].Update(msg)
		m.tabs[idxProfiles] = tab
		return m, cmd
	case profiles.ProfileOpMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		tab, cmd, _ := m.tabs[idxProfiles].Update(msg)
		m.tabs[idxProfiles] = tab
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
	tab, cmd, _ := m.tabs[idxKernel].Update(msg)
	m.tabs[idxKernel] = tab
	return m, cmd
}

// updateProxiesMsg routes proxy load results and operation outcomes to the Proxies page.
func (m Model) updateProxiesMsg(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case proxies.ProxiesLoadedMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		tab, cmd, _ := m.tabs[idxProxy].Update(msg)
		m.tabs[idxProxy] = tab
		return m, cmd
	case proxies.ProxyOpMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		tab, cmd, _ := m.tabs[idxProxy].Update(msg)
		m.tabs[idxProxy] = tab
		return m, cmd
	case proxies.ModeLoadedMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		tab, cmd, _ := m.tabs[idxProxy].Update(msg)
		m.tabs[idxProxy] = tab
		return m, cmd
	case proxies.ModeOpMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		tab, cmd, _ := m.tabs[idxProxy].Update(msg)
		m.tabs[idxProxy] = tab
		return m, cmd
	}
	return m, nil
}
