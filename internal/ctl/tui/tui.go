// Package tui implements the interactive terminal UI for mihomo-tui.
package tui

import (
	"context"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/components"
	"metacubexd-server-go/internal/ctl/tui/pages/connections"
	"metacubexd-server-go/internal/ctl/tui/pages/kernel"
	"metacubexd-server-go/internal/ctl/tui/pages/logs"
	"metacubexd-server-go/internal/ctl/tui/pages/profiles"
	"metacubexd-server-go/internal/ctl/tui/pages/proxies"
	"metacubexd-server-go/internal/ctl/tui/shared"
	"metacubexd-server-go/internal/api"
)

// Tab indices.
const (
	idxConnection = 0
	idxProxy      = 1
	idxProfiles   = 2
	idxLogs       = 3
	idxKernel     = 4
)

// Model is the top-level Bubble Tea model. It owns the kernel state, the
// active tab index and the tab pages; feature state lives inside each page.
type Model struct {
	client     *ctl.Client
	state      *api.KernelState
	err        error
	tabs       []shared.Tab
	activeTab  int
	spinner    spinner.Model
	width      int
	height     int
	logCh      <-chan ctl.Event
	logCancel  context.CancelFunc
	connCancel context.CancelFunc
	quitting   bool
}

// New returns a Model for the given control API client.
func New(client *ctl.Client) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = shared.SpinnerStyle
	return Model{
		client:  client,
		tabs:    []shared.Tab{connections.New(client), proxies.New(client), profiles.New(client), logs.New(client), kernel.New(client)},
		spinner: s,
	}
}

// Init returns the initial commands: fetch kernel status, poll it every
// second, subscribe to the SSE log stream and load the profile list.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		shared.FetchStatus(m.client),
		shared.StatusTick(),
		shared.Subscribe(m.client),
		profiles.FetchProfiles(m.client),
		connections.FetchConnections(m.client),
		shared.RequestBackgroundColor(),
	)
}

// Update routes messages: key presses go through updateKey, mouse/window/
// theme/quit messages are handled inline, and the remaining model messages
// are dispatched to the owning sub model's update handler.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	case tea.MouseMsg:
		// Wheel events scroll the active viewport; capturing them here keeps
		// the terminal from scrolling its own buffer (which would reveal
		// content from before the TUI started).
		if m.activeTab == idxKernel {
			m.tabs[idxKernel].Update(msg)
		} else {
			m.tabs[m.activeTab].Update(msg)
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if msg.Height > shared.FrameRows {
			m.tabs[idxLogs].SetSize(msg.Width-2, msg.Height-shared.FrameRows)
		}
		return m, nil
	case tea.BackgroundColorMsg:
		shared.SetTheme(msg.IsDark())
		shared.SetModalBackground(msg.Color)
		m.spinner.Style = shared.SpinnerStyle
		return m, nil
	case tea.QuitMsg:
		m.quitting = true
		m.closeLogStream()
		return m, nil
	case shared.SubscribedMsg, shared.LogLineMsg, shared.KernelStateMsg, shared.LogClosedMsg:
		return m.updateStream(msg)
	case shared.StatusLoadedMsg, shared.StatusErrorMsg, spinner.TickMsg, shared.TickMsg:
		return m.updateStatus(msg)
	case profiles.ProfilesLoadedMsg, profiles.ProfileOpMsg:
		return m.updateProfilesMsg(msg)
	case proxies.ProxiesLoadedMsg, proxies.ProxyOpMsg, proxies.ModeLoadedMsg, proxies.ModeOpMsg:
		return m.updateProxiesMsg(msg)
	case kernel.ConfigLoadedMsg, kernel.NetworkSettingsMsg, kernel.SectionEditMsg:
		return m.updateKernelMsg(msg)
	case connections.ConnectionsLoadedMsg, connections.ConnectionOpMsg:
		return m.updateConnectionsMsg(msg)
	case shared.ConnectionTickMsg:
		return m.updateConnectionTick(msg)
	}
	return m, nil
}

// updateKey routes key presses: the active page consumes first, then global
// keys (quit/tab-switch) and finally scroll fallback to the log viewport.
func (m Model) updateKey(msg tea.Msg) (Model, tea.Cmd) {
	tab, cmd, handled := m.tabs[m.activeTab].Update(msg)
	m.tabs[m.activeTab] = tab
	if handled {
		return m, cmd
	}
	key := msg.(tea.KeyPressMsg).String()
	switch key {
	case "q", "ctrl+c":
		m.quitting = true
		m.closeLogStream()
		return m, tea.Quit
	case "1", "2", "3", "4", "5":
		// Stop connection refresh when leaving the connection tab
		if m.activeTab == idxConnection && m.activeTab != int(key[0]-'1') {
			m.closeConnectionStream()
		}
		m.activeTab = int(key[0] - '1')
		if m.activeTab == idxKernel && !m.kernelPage().NetworkLoaded() {
			return m, kernel.FetchNetworkSettings(m.client)
		}
		if m.activeTab == idxProxy {
			// Lazy load proxies on first visit
			return m, tea.Batch(proxies.FetchProxies(m.client), proxies.FetchMode(m.client))
		}
		if m.activeTab == idxConnection {
			// Start connection refresh when entering the tab
			return m, shared.ConnectionTick()
		}
	default:
		// Scroll fallback: on any tab except Kernel, unhandled scroll keys
		// reach the log viewport. The whitelist keeps "/" and "f" from
		// leaking into the logs page on other tabs.
		if m.activeTab != idxKernel && shared.IsScrollKey(key) {
			m.tabs[idxLogs].Update(msg)
		}
	}
	return m, nil
}

// kernelPage returns the Kernel page stored in tabs[idxKernel].
func (m Model) kernelPage() *kernel.Model {
	return m.tabs[idxKernel].(*kernel.Model)
}

// profilesPage returns the Profiles page stored in tabs[idxProfiles].
func (m Model) profilesPage() *profiles.Model {
	return m.tabs[idxProfiles].(*profiles.Model)
}

// proxiesPage returns the Proxies page stored in tabs[idxProxy].
func (m Model) proxiesPage() *proxies.Model {
	return m.tabs[idxProxy].(*proxies.Model)
}

// View returns the rendered layout as a tea.View with mouse support enabled.
// Narrow screens (under shared.NarrowWidth columns) get the bare log stream instead
// of the frame — no frame, no tabs. Modal overlays are stacked on top of the
// frame based on the active page's Overlay.
func (m Model) View() tea.View {
	var content string
	if m.width > 0 && m.width < shared.NarrowWidth {
		m.tabs[idxLogs].SetSize(m.width, m.height)
		content = m.tabs[idxLogs].View()
	} else {
		content = m.frameView()
	}
	if modal := m.tabs[m.activeTab].Overlay(); modal != nil {
		content = components.OverlayModal(content, modal.View(m.width, m.height), m.width, m.height)
	}
	v := tea.NewView(content)
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
