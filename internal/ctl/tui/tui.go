// Package tui implements the interactive terminal UI for mihomo-tui.
package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"gopkg.in/yaml.v3"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/supervisor"
)

// Model is the top-level Bubble Tea model. It owns the kernel state, the log
// viewport and the current tab.
type Model struct {
	client         *ctl.Client
	state          *supervisor.KernelState
	err            error
	logs           LogsModel
	profiles       ProfilesModel
	config         ConfigModel
	profActive     string
	importing      bool
	form           importForm
	confirmDel     bool
	viewingConfig  bool
	network        networkSettings
	editing        bool
	editField      int
	editInput      textinput.Model
	editingSection bool
	sectionForm    sectionForm
	activeTab      int
	kSelected      int
	kConfirming    bool
	operating      bool
	spinner        spinner.Model
	filtering      bool
	filterInput    string
	width          int
	height         int
	logCh          <-chan ctl.Event
	logCancel      context.CancelFunc
	quitting       bool
}

// New returns a Model for the given control API client.
func New(client *ctl.Client) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle
	return Model{client: client, logs: NewLogsModel(), profiles: NewProfilesModel(), config: NewConfigModel(), spinner: s}
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

// networkSettings holds the editable network fields of the active config.
type networkSettings struct {
	values map[string]any // top-level keys: mixed-port, port, socks-port, tun
	loaded bool
}

// networkField describes one editable network entry.
type networkField struct {
	label string
	key   string // top-level config key
	sub   string // tun sub-key ("" for top-level scalars)
}

// networkFields lists the editable network entries in display order.
var networkFields = []networkField{
	{"mixed-port", "mixed-port", ""},
	{"http-port", "port", ""},
	{"socks-port", "socks-port", ""},
	{"tun-enable", "tun", "enable"},
	{"tun-device", "tun", "device"},
	{"tun-stack", "tun", "stack"},
}

// valueOf returns the current value of a network field as a string. Absent tun
// sub-fields fall back to mihomo's defaults (stack=mixed, device=Mihomo).
func (ns networkSettings) valueOf(f networkField) string {
	if f.sub == "" {
		return fmtValue(ns.values[f.key])
	}
	if m, ok := ns.values["tun"].(map[string]any); ok {
		if v, ok := m[f.sub]; ok && v != nil {
			return fmtValue(v)
		}
	}
	switch f.sub {
	case "stack":
		return "mixed"
	case "device":
		return "Mihomo"
	}
	return ""
}

// setField returns the (key, value) pair for PutSection when editing f with
// the raw string raw. Tun sub-fields rebuild the whole tun object.
func (ns networkSettings) setField(f networkField, raw string) (string, any) {
	v := parseSectionValue(raw)
	if f.sub == "" {
		return f.key, v
	}
	tun := map[string]any{}
	if m, ok := ns.values["tun"].(map[string]any); ok {
		for k, vv := range m {
			tun[k] = vv
		}
	}
	tun[f.sub] = v
	return "tun", tun
}

// fmtValue renders a config value for display.
func fmtValue(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// fetchNetworkSettingsCmd loads the editable network fields from the runtime
// config — the file mihomo actually runs — so injected and merged values (like
// tun from a merge overlay) are included.
func fetchNetworkSettingsCmd(c *ctl.Client) tea.Cmd {
	return func() tea.Msg {
		content, err := c.GetRuntimeConfig()
		if err != nil {
			return networkSettingsMsg{err: err}
		}
		return networkSettingsMsg{settings: parseNetworkSettings(content)}
	}
}

// parseNetworkSettings extracts the editable network fields from a YAML config
// body.
func parseNetworkSettings(content string) networkSettings {
	ns := networkSettings{values: map[string]any{}}
	var v any
	if err := yaml.Unmarshal([]byte(content), &v); err != nil {
		return ns
	}
	top, ok := v.(map[string]any)
	if !ok {
		return ns
	}
	for _, key := range []string{"mixed-port", "port", "socks-port", "tun"} {
		if val, ok := top[key]; ok && val != nil {
			ns.values[key] = val
		}
	}
	ns.loaded = true
	return ns
}


// Update handles messages and key presses.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()
		if m.filtering {
			m = m.updateFilter(key)
			return m, nil
		}
		if m.importing {
			var cmd tea.Cmd
			m, cmd = m.updateImport(msg)
			return m, cmd
		}
		if m.editing {
			var cmd tea.Cmd
			m, cmd = m.updateEditInput(msg)
			return m, cmd
		}
		if m.editingSection {
			var cmd tea.Cmd
			m, cmd = m.updateSectionForm(msg)
			return m, cmd
		}
		if m.viewingConfig {
			var cmd tea.Cmd
			m, cmd = m.updateConfigViewer(msg)
			return m, cmd
		}
		if m.confirmDel {
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
		}
		switch key {
		case "q", "ctrl+c":
			m.quitting = true
			m.closeLogStream()
			return m, tea.Quit
		case "1", "2", "3":
			m.activeTab = int(key[0] - '1')
			if m.activeTab == 2 && !m.network.loaded {
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
				m, cmd = m.updateKernelKeys(key)
				if cmd != nil {
					return m, cmd
				}
				return m, nil // consumed by menu selection
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
		m.operating = false
		m.kConfirming = false
		return m, nil
	case statusErrorMsg:
		m.err = msg.err
		m.operating = false
		m.kConfirming = false
		return m, nil
	case spinner.TickMsg:
		if m.operating {
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
		m.network = msg.settings
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

// kernelOpCmd runs a kernel operation via the client and pushes the fresh
// state, refreshing the status bar when done.
func kernelOpCmd(c *ctl.Client, op func(*ctl.Client) (supervisor.KernelState, error)) tea.Cmd {
	return func() tea.Msg {
		st, err := op(c)
		if err != nil {
			return statusErrorMsg{err: err}
		}
		return statusLoadedMsg{state: st}
	}
}

// kernelOps are the operations available on the Config tab, in selection
// order. Rollback/Recover are commented out for now — destructive escape
// hatches, re-enabled later.
type kernelOp struct {
	label string
	op    func(*ctl.Client) (supervisor.KernelState, error)
}

var kernelOps = []kernelOp{
	{"Start", (*ctl.Client).KernelStart},
	{"Stop", (*ctl.Client).KernelStop},
	{"Restart", (*ctl.Client).KernelRestart},
	// {"Rollback", (*ctl.Client).KernelRollback},
	// {"Recover", (*ctl.Client).KernelRecover},
}

// configMenuLen is the number of selectable entries on the Config tab: kernel
// ops + network fields + the raw YAML viewer.
func configMenuLen() int {
	return len(kernelOps) + len(networkFields) + 1
}

// updateKernelKeys handles selection and execution while the Config tab is
// active. The Recover confirmation state (kConfirming) is kept for when
// Recover is re-enabled.
func (m Model) updateKernelKeys(key string) (Model, tea.Cmd) {
	menuLen := configMenuLen()
	if m.kConfirming {
		m.kConfirming = false
		if key == "y" || key == "Y" {
			return m.startKernelOp(kernelOps[m.kSelected])
		}
		return m, nil
	}
	switch key {
	case "up", "k":
		m.kSelected = (m.kSelected + menuLen - 1) % menuLen
	case "down", "j":
		m.kSelected = (m.kSelected + 1) % menuLen
	case "enter", "space":
		switch {
		case m.kSelected < len(kernelOps):
			op := kernelOps[m.kSelected]
			if op.label == "Recover" {
				m.kConfirming = true
				return m, nil
			}
			return m.startKernelOp(op)
		case m.kSelected < len(kernelOps)+len(networkFields):
			return m.startEditField(m.kSelected - len(kernelOps))
		default:
			m.viewingConfig = true
			m.config.ResetScroll()
			return m, fetchConfigCmd(m.client, m.config.mode)
		}
	}
	return m, nil
}

// startEditField opens the single-value editor for a network field, prefilled
// with its current value.
func (m Model) startEditField(i int) (Model, tea.Cmd) {
	m.editing = true
	m.editField = i
	in := textinput.New()
	in.Prompt = ""
	in.SetWidth(40)
	in.SetValue(m.network.valueOf(networkFields[i]))
	m.editInput = in
	return m, in.Focus()
}

// updateEditInput drives the network field editor: enter saves via PutSection,
// esc cancels.
func (m Model) updateEditInput(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.editing = false
			return m, nil
		case "enter":
			raw := strings.TrimSpace(m.editInput.Value())
			m.editing = false
			if raw == "" {
				return m, nil
			}
			key, value := m.network.setField(networkFields[m.editField], raw)
			return m, putSectionCmd(m.client, key, value)
		default:
			var cmd tea.Cmd
			m.editInput, cmd = m.editInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// putSectionCmd replaces one top-level key with a parsed value and restarts
// the kernel.
func putSectionCmd(c *ctl.Client, key string, value any) tea.Cmd {
	return func() tea.Msg {
		if err := c.PutSection(key, value, true); err != nil {
			return sectionEditMsg{err: err}
		}
		return sectionEditMsg{}
	}
}

// updateConfigViewer drives the config viewer modal: esc closes, c toggles
// active/runtime, other keys scroll the viewport.
func (m Model) updateConfigViewer(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.viewingConfig = false
			return m, nil
		case "e":
			m.editingSection = true
			m.sectionForm = newSectionForm()
			return m, m.sectionForm.key.Focus()
		case "c":
			m.config.ToggleMode()
			m.config.ResetScroll()
			return m, fetchConfigCmd(m.client, m.config.mode)
		default:
			return m, m.config.Update(msg)
		}
	}
	return m, nil
}

// sectionForm bundles the two textinputs of the section editor popup.
type sectionForm struct {
	key   textinput.Model
	value textinput.Model
	focus int // 0 = key, 1 = value
}

// newSectionForm builds a section editor popup with the key field focused.
func newSectionForm() sectionForm {
	key := textinput.New()
	key.Prompt = "Key:   "
	key.Placeholder = "mixed-port"
	key.SetWidth(50)
	value := textinput.New()
	value.Prompt = "Value: "
	value.Placeholder = "7890"
	value.SetWidth(50)
	return sectionForm{key: key, value: value}
}

// parseSectionValue parses the entered value as YAML so a plain scalar maps to
// its natural Go type (int, bool, ...); anything unparseable stays a string.
func parseSectionValue(s string) any {
	var v any
	if err := yaml.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return s
}

// updateSectionForm drives the section editor popup: tab switches fields,
// enter saves via PutSection, esc cancels.
func (m Model) updateSectionForm(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.editingSection = false
			return m, nil
		case "tab":
			m.sectionForm.focus = 1 - m.sectionForm.focus
			if m.sectionForm.focus == 0 {
				m.sectionForm.value.Blur()
				return m, m.sectionForm.key.Focus()
			}
			m.sectionForm.key.Blur()
			return m, m.sectionForm.value.Focus()
		case "enter":
			key := strings.TrimSpace(m.sectionForm.key.Value())
			value := strings.TrimSpace(m.sectionForm.value.Value())
			m.editingSection = false
			if key == "" {
				return m, nil
			}
			return m, sectionEditCmd(m.client, key, value)
		default:
			if m.sectionForm.focus == 0 {
				var cmd tea.Cmd
				m.sectionForm.key, cmd = m.sectionForm.key.Update(msg)
				return m, cmd
			}
			var cmd tea.Cmd
			m.sectionForm.value, cmd = m.sectionForm.value.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// sectionEditCmd replaces one top-level key with a YAML-parsed value and
// restarts the kernel.
func sectionEditCmd(c *ctl.Client, key, value string) tea.Cmd {
	return putSectionCmd(c, key, parseSectionValue(value))
}

// startKernelOp marks the operation as running, starts the spinner and issues
// the operation command.
func (m Model) startKernelOp(op kernelOp) (Model, tea.Cmd) {
	m.operating = true
	return m, tea.Batch(kernelOpCmd(m.client, op.op), m.spinner.Tick)
}

// View returns the rendered layout as a tea.View with mouse support enabled.
// Narrow screens (under narrowWidth columns) get the bare log stream instead
// of the frame — no frame, no tabs.
func (m Model) View() tea.View {
	var content string
	if m.width > 0 && m.width < narrowWidth {
		m.logs.SetSize(m.width, m.height)
		content = m.logs.View()
	} else {
		content = m.frameView()
	}
	if m.editing {
		content = overlayModal(content, m.editInputView(), m.width, m.height)
	}
	if m.viewingConfig {
		content = overlayModal(content, m.configModal(m.width, m.height), m.width, m.height)
		if m.editingSection {
			content = overlayModal(content, m.sectionFormView(), m.width, m.height)
		}
	}
	v := tea.NewView(content)
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// renderKernelTab renders the Config tab body: the kernel operation list, the
// editable network fields (with current values), and the raw YAML viewer
// entry. The selected entry is highlighted.
func (m Model) renderKernelTab() string {
	lines := []string{renderStatus(m.state, "")}
	if m.err != nil {
		lines = append(lines, errorStyle.Render("⚠ "+m.err.Error()))
	}
	if m.state != nil && m.state.LastError != "" {
		lines = append(lines, errorStyle.Render(m.state.LastError))
	}

	lines = append(lines, "", "[kernel]")
	for i, op := range kernelOps {
		prefix, label := "  ", op.label
		if i == m.kSelected {
			prefix, label = "> ", selectedStyle.Render(op.label)
		}
		lines = append(lines, prefix+label)
	}

	lines = append(lines, "", "[network]")
	for i, f := range networkFields {
		sel := len(kernelOps) + i
		value := m.network.valueOf(f)
		if value == "" {
			value = "—"
		}
		prefix, label := "  ", fmt.Sprintf("%-12s %s", f.label, value)
		if sel == m.kSelected {
			prefix, label = "> ", selectedStyle.Render(label)
		}
		lines = append(lines, prefix+label)
	}

	{
		sel := len(kernelOps) + len(networkFields)
		prefix, label := "  ", "View YAML"
		if sel == m.kSelected {
			prefix, label = "> ", selectedStyle.Render(label)
		}
		lines = append(lines, prefix+label)
	}

	if m.kConfirming {
		lines = append(lines, "", errorStyle.Render("⚠ Recover 将重置 active config,确认执行? (y 确认 / 其他取消)"))
	}
	return strings.Join(lines, "\n")
}

