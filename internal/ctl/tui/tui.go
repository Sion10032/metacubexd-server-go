// Package tui implements the interactive terminal UI for mihomo-tui.
package tui

import (
	"context"
	"fmt"
	"unicode/utf8"

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

// Update handles messages and key presses. Modal states (filter, import, edit,
// config viewer) take priority over tab routing; the KernelModel owns the
// Config tab's edit states and is queried for overlay decisions.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()
		switch {
		case m.filtering:
			m = m.updateFilter(key)
			return m, nil
		case m.importing:
			var cmd tea.Cmd
			m, cmd = m.updateImport(msg)
			return m, cmd
		case m.kernel.Editing():
			var cmd tea.Cmd
			m, cmd = m.kernel.updateEdit(msg, m)
			return m, cmd
		case m.kernel.EditingSection():
			var cmd tea.Cmd
			m, cmd = m.kernel.updateSectionForm(msg, m)
			return m, cmd
		case m.kernel.ViewingConfig():
			var cmd tea.Cmd
			m, cmd = m.kernel.updateConfig(msg, m)
			return m, cmd
		case m.confirmDel:
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
		default:
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
			case "a":
				if m.activeTab == 1 {
					if id := m.profiles.SelectedID(); id != "" {
						m.profActive = id
						return m, profileOpCmd(m.client, func() error {
							_, err := m.client.ProfileActivate(id)
							return err
						})
					}
				}
			case "u":
				if m.activeTab == 1 {
					if id := m.profiles.SelectedID(); id != "" {
						return m, profileOpCmd(m.client, func() error {
							_, err := m.client.ProfileRefresh(id)
							return err
						})
					}
				}
			case "d":
				if m.activeTab == 1 && m.profiles.SelectedID() != "" {
					m.confirmDel = true
				}
			case "i":
				if m.activeTab == 1 {
					m.importing = true
					m.form = newImportForm()
					return m, m.form.url.Focus()
				}
			default:
				// Config tab: up/down/enter drive menu selection; the config
				// viewer modal handles its own scrolling when open.
				if m.activeTab == 2 {
					var cmd tea.Cmd
					m, cmd = m.kernel.updateKeys(key, m)
					return m, cmd
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
		}
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
	case subscribedMsg:
		m.logCancel = msg.cancel
		m.logCh = msg.ch
		return m, forwardEventsCmd(msg.ch)
	case logMsg:
		m.logs.append(msg.line)
		return m, forwardEventsCmd(m.logCh)
	case stateMsg:
		m.state = &msg.state
		return m, forwardEventsCmd(m.logCh)
	case logClosedMsg:
		if !m.quitting {
			m.err = fmt.Errorf("log stream closed")
		}
		return m, nil
	case profilesLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.profiles.SetProfiles(msg.list, m.profActive)
		return m, nil
	case profileOpMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// Refresh the list (updated timestamps, imported/deleted entries) and
		// the kernel status (activate restarts the kernel).
		return m, tea.Batch(fetchProfilesCmd(m.client), fetchStatusCmd(m.client))
	case configLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		if msg.mode == m.config.mode {
			m.config.SetContent(msg.content)
		}
		return m, nil
	case networkSettingsMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.kernel.network = msg.settings
		return m, nil
	case sectionEditMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, tea.Batch(fetchConfigCmd(m.client, m.config.mode), fetchStatusCmd(m.client), fetchNetworkSettingsCmd(m.client))
	case tickMsg:
		return m, tea.Batch(fetchStatusCmd(m.client), statusTick())
	}
	return m, nil
}

// closeLogStream cancels the SSE subscription so its goroutine and HTTP
// connection are released on exit.
func (m *Model) closeLogStream() {
	if m.logCancel != nil {
		m.logCancel()
		m.logCancel = nil
	}
}

// updateFilter handles the filter input state: enter applies, esc cancels,
// backspace deletes, other single characters append.
func (m Model) updateFilter(key string) Model {
	switch key {
	case "enter":
		m.logs.SetFilter(m.filterInput)
		m.filterInput = ""
		m.filtering = false
	case "esc":
		m.filterInput = ""
		m.filtering = false
	case "backspace":
		if r := []rune(m.filterInput); len(r) > 0 {
			m.filterInput = string(r[:len(r)-1])
		}
	default:
		if utf8.RuneCountInString(key) == 1 {
			m.filterInput += key
		}
	}
	return m
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
