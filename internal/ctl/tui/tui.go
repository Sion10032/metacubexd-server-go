// Package tui implements the interactive terminal UI for mihomo-tui.
package tui

import (
	"context"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/supervisor"
)

// Model is the top-level Bubble Tea model. It owns the kernel state, the log
// viewport, the current tab and the per-tab sub models.
type Model struct {
	client      *ctl.Client
	state       *supervisor.KernelState
	err         error
	logs        LogsModel
	profiles    ProfilesModel
	config      ConfigModel
	kernel      KernelModel
	profActive  string
	importing   bool
	form        importForm
	confirmDel  bool
	activeTab   int
	spinner     spinner.Model
	filtering   bool
	filterInput string
	width       int
	height      int
	logCh       <-chan ctl.Event
	logCancel   context.CancelFunc
	quitting    bool
}

// New returns a Model for the given control API client.
func New(client *ctl.Client) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle
	return Model{
		client:   client,
		logs:     NewLogsModel(),
		profiles: NewProfilesModel(),
		config:   NewConfigModel(),
		kernel:   NewKernelModel(),
		spinner:  s,
	}
}

// Init returns the initial commands: fetch kernel status, poll it every
// second, subscribe to the SSE log stream and load the profile list.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		fetchStatusCmd(m.client),
		statusTick(),
		subscribeCmd(m.client),
		fetchProfilesCmd(m.client),
		requestBackgroundColorCmd(),
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
			m.config.Update(msg)
		} else {
			m.logs.Update(msg)
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if msg.Height > frameRows {
			m.logs.SetSize(msg.Width-2, msg.Height-frameRows)
		}
		return m, nil
	case tea.BackgroundColorMsg:
		setTheme(msg.IsDark())
		setModalBackground(msg.Color)
		m.spinner.Style = spinnerStyle
		return m, nil
	case tea.QuitMsg:
		m.quitting = true
		m.closeLogStream()
		return m, nil
	case subscribedMsg, logMsg, stateMsg, logClosedMsg:
		return m.updateStream(msg)
	case statusLoadedMsg, statusErrorMsg, spinner.TickMsg, tickMsg:
		return m.updateStatus(msg)
	case profilesLoadedMsg, profileOpMsg:
		return m.updateProfilesMsg(msg)
	case configLoadedMsg, networkSettingsMsg, sectionEditMsg:
		return m.updateConfigMsg(msg)
	}
	return m, nil
}

// updateKey routes key presses. Modal states (filter, import, kernel edit,
// delete confirm) take priority; the remaining keys drive the active tab.
func (m Model) updateKey(msg tea.Msg) (Model, tea.Cmd) {
	key := msg.(tea.KeyPressMsg).String()
	switch {
	case m.filtering:
		return m.updateFilter(key), nil
	case m.importing:
		return m.updateImport(msg)
	case m.kernel.Editing():
		return m.kernel.updateEdit(msg, m)
	case m.kernel.EditingSection():
		return m.kernel.updateSectionForm(msg, m)
	case m.kernel.ViewingConfig():
		return m.kernel.updateConfig(msg, m)
	case m.confirmDel:
		return m.updateConfirmDel(key)
	}
	switch key {
	case "q", "ctrl+c":
		m.quitting = true
		m.closeLogStream()
		return m, tea.Quit
	case "1", "2", "3":
		m.activeTab = int(key[0] - '1')
		if m.activeTab == 2 && !m.kernel.network.loaded {
			return m, fetchNetworkSettingsCmd(m.client)
		}
	case "/":
		if m.activeTab == 0 {
			m.filtering = true
			// Prefill with the current filter so it is editable and can be
			// cleared by deleting to empty and pressing enter.
			m.filterInput = m.logs.filter
		}
	case "f":
		if m.activeTab == 0 {
			m.logs.follow = !m.logs.follow
		}
	case "a", "u", "d", "i":
		if m.activeTab == 1 {
			return m.updateProfilesKeys(key, msg)
		}
	default:
		// Config tab: up/down/enter drive menu selection; the config viewer
		// modal handles its own scrolling when open.
		if m.activeTab == 2 {
			return m.kernel.updateKeys(key, m)
		}
		// Profiles tab: selection keys drive the table; scroll keys fall
		// through to the log viewport below.
		if m.activeTab == 1 {
			m.profiles.Update(msg)
			if key == "up" || key == "down" || key == "enter" {
				return m, nil // consumed by the table
			}
		}
		// Scroll keys (PgUp/PgDn/arrows) reach the log viewport on any tab.
		m.logs.Update(msg)
	}
	return m, nil
}

// View returns the rendered layout as a tea.View with mouse support enabled.
// Narrow screens (under narrowWidth columns) get the bare log stream instead
// of the frame — no frame, no tabs. Modal overlays are stacked on top of the
// frame based on the kernel model's edit states.
func (m Model) View() tea.View {
	var content string
	if m.width > 0 && m.width < narrowWidth {
		m.logs.SetSize(m.width, m.height)
		content = m.logs.View()
	} else {
		content = m.frameView()
	}
	if m.kernel.Editing() {
		content = overlayModal(content, m.editInputView(), m.width, m.height)
	}
	if m.kernel.ViewingConfig() {
		content = overlayModal(content, m.configModal(m.width, m.height), m.width, m.height)
		if m.kernel.EditingSection() {
			content = overlayModal(content, m.sectionFormView(), m.width, m.height)
		}
	}
	v := tea.NewView(content)
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
