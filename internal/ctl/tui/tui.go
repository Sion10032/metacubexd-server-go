// Package tui implements the interactive terminal UI for mihomo-tui.
package tui

import (
	"context"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/components"
	"metacubexd-server-go/internal/ctl/tui/pages/kernel"
	"metacubexd-server-go/internal/ctl/tui/pages/logs"
	"metacubexd-server-go/internal/ctl/tui/pages/profiles"
	"metacubexd-server-go/internal/ctl/tui/shared"
	"metacubexd-server-go/internal/supervisor"
)

// Model is the top-level Bubble Tea model. It owns the kernel state, the
// active tab index and the tab pages; feature state lives inside each page.
type Model struct {
	client    *ctl.Client
	state     *supervisor.KernelState
	err       error
	tabs      []shared.Tab
	activeTab int
	spinner   spinner.Model
	width     int
	height    int
	logCh     <-chan ctl.Event
	logCancel context.CancelFunc
	quitting  bool
}

// New returns a Model for the given control API client.
func New(client *ctl.Client) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = shared.SpinnerStyle
	return Model{
		client:  client,
		tabs:    []shared.Tab{logs.New(client), profiles.New(client), kernel.New(client)},
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
		if m.activeTab == 2 {
			m.tabs[2].Update(msg)
		} else {
			m.tabs[0].Update(msg)
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if msg.Height > shared.FrameRows {
			m.tabs[0].SetSize(msg.Width-2, msg.Height-shared.FrameRows)
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
	case kernel.ConfigLoadedMsg, kernel.NetworkSettingsMsg, kernel.SectionEditMsg:
		return m.updateKernelMsg(msg)
	}
	return m, nil
}

// updateKey routes key presses. Modal states (filter, import, kernel edit,
// delete confirm) take priority; the remaining keys drive the active tab.
func (m Model) updateKey(msg tea.Msg) (Model, tea.Cmd) {
	key := msg.(tea.KeyPressMsg).String()
	logsTab := m.logsPage()
	switch {
	case logsTab.Filtering():
		logsTab.UpdateFilterKey(key)
		return m, nil
	case m.activeTab == 1 && (m.profilesPage().Importing() || m.profilesPage().ConfirmingDel()):
		return m.updateTabKey(msg)
	}
	// The kernel page's popup (network-field editor, section editor, config
	// viewer) consumes all keys while open. The modal state is global: it is
	// only ever opened on the Config tab and swallows the tab-switch keys, so
	// in practice it is never open on another tab.
	if modal := m.tabs[2].Overlay(); modal != nil {
		_, cmd := modal.Update(msg)
		return m, cmd
	}
	switch key {
	case "q", "ctrl+c":
		m.quitting = true
		m.closeLogStream()
		return m, tea.Quit
	case "1", "2", "3":
		m.activeTab = int(key[0] - '1')
		if m.activeTab == 2 && !m.kernelPage().NetworkLoaded() {
			return m, kernel.FetchNetworkSettings(m.client)
		}
	case "/":
		if m.activeTab == 0 {
			logsTab.StartFilter()
		}
	case "f":
		if m.activeTab == 0 {
			logsTab.ToggleFollow()
		}
	case "a", "u", "d", "i":
		if m.activeTab == 1 {
			return m.updateTabKey(msg)
		}
	default:
		// Config tab: up/down/enter drive menu selection; the config viewer
		// modal handles its own scrolling when open.
		if m.activeTab == 2 {
			return m.updateTabKey(msg)
		}
		// Profiles tab: selection keys drive the table; scroll keys fall
		// through to the log viewport below.
		if m.activeTab == 1 {
			tab, _ := m.tabs[1].Update(msg)
			m.tabs[1] = tab
			if key == "up" || key == "down" || key == "enter" {
				return m, nil // consumed by the table
			}
		}
		// Scroll keys (PgUp/PgDn/arrows) reach the log viewport on any tab.
		m.tabs[0].Update(msg)
	}
	return m, nil
}

// logsPage returns the Logs page stored in tabs[0].
func (m Model) logsPage() *logs.Model {
	return m.tabs[0].(*logs.Model)
}

// profilesPage returns the Profiles page stored in tabs[1].
func (m Model) profilesPage() *profiles.Model {
	return m.tabs[1].(*profiles.Model)
}

// kernelPage returns the Kernel page stored in tabs[2].
func (m Model) kernelPage() *kernel.Model {
	return m.tabs[2].(*kernel.Model)
}

// updateTabKey routes a key press to the active tab page, storing the
// returned page back into tabs and forwarding its command.
func (m Model) updateTabKey(msg tea.Msg) (Model, tea.Cmd) {
	tab, cmd := m.tabs[m.activeTab].Update(msg)
	m.tabs[m.activeTab] = tab
	return m, cmd
}

// View returns the rendered layout as a tea.View with mouse support enabled.
// Narrow screens (under shared.NarrowWidth columns) get the bare log stream instead
// of the frame — no frame, no tabs. Modal overlays are stacked on top of the
// frame based on the active page's Overlay.
func (m Model) View() tea.View {
	var content string
	if m.width > 0 && m.width < shared.NarrowWidth {
		m.tabs[0].SetSize(m.width, m.height)
		content = m.tabs[0].View()
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
