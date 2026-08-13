// Package tui implements the interactive terminal UI for mihomo-tui.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"gopkg.in/yaml.v3"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/profile"
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

// statusLoadedMsg carries a fresh kernel state from the control API.
type statusLoadedMsg struct {
	state supervisor.KernelState
}

// statusErrorMsg carries a control API failure (connection, auth, ...).
type statusErrorMsg struct {
	err error
}

// tickMsg fires once per second to refresh the kernel status.
type tickMsg struct{}

// subscribedMsg carries the live SSE log stream and its cancellation func.
type subscribedMsg struct {
	ch     <-chan ctl.Event
	cancel context.CancelFunc
}

// logMsg carries one formatted kernel log line.
type logMsg struct {
	line string
}

// stateMsg carries a kernel state pushed over SSE.
type stateMsg struct {
	state supervisor.KernelState
}

// logClosedMsg fires when the SSE log stream ends.
type logClosedMsg struct{}

// profilesLoadedMsg carries the fetched profile list.
type profilesLoadedMsg struct {
	list []profile.Meta
	err  error
}

// profileOpMsg carries the result of a profile operation; a nil err means the
// lists and kernel status should be refreshed.
type profileOpMsg struct {
	err error
}

// configLoadedMsg carries a fetched config body (active or runtime).
type configLoadedMsg struct {
	mode    int
	content string
	err     error
}

// sectionEditMsg carries the result of a config section edit.
type sectionEditMsg struct {
	err error
}

// networkSettingsMsg carries the fetched network settings of the active
// config.
type networkSettingsMsg struct {
	settings networkSettings
	err      error
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

// importForm bundles the two textinputs of the import popup.
type importForm struct {
	url   textinput.Model
	name  textinput.Model
	focus int // 0 = URL, 1 = Name
}

// newImportForm builds an import popup with the URL field focused.
func newImportForm() importForm {
	url := textinput.New()
	url.Prompt = "URL:  "
	url.Placeholder = "https://example.com/sub.yaml"
	url.SetWidth(50)
	name := textinput.New()
	name.Prompt = "Name: "
	name.Placeholder = "(optional)"
	name.SetWidth(50)
	return importForm{url: url, name: name}
}

// updateImport drives the import popup: tab switches the focused field, enter
// imports the entered URL+name, esc cancels; other keys feed the focused
// textinput.
func (m Model) updateImport(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.importing = false
			m.form.url.Reset()
			m.form.name.Reset()
			return m, nil
		case "tab":
			m.form.focus = 1 - m.form.focus
			if m.form.focus == 0 {
				m.form.name.Blur()
				return m, m.form.url.Focus()
			}
			m.form.url.Blur()
			return m, m.form.name.Focus()
		case "enter":
			url := strings.TrimSpace(m.form.url.Value())
			name := strings.TrimSpace(m.form.name.Value())
			m.importing = false
			m.form.url.Reset()
			m.form.name.Reset()
			if url == "" {
				return m, nil
			}
			return m, importCmd(m.client, url, name)
		default:
			if m.form.focus == 0 {
				var cmd tea.Cmd
				m.form.url, cmd = m.form.url.Update(msg)
				return m, cmd
			}
			var cmd tea.Cmd
			m.form.name, cmd = m.form.name.Update(msg)
			return m, cmd
		}
	}
	return m, nil
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

// subscribeCmd opens the SSE log subscription and hands the stream to the
// model via subscribedMsg.
func subscribeCmd(c *ctl.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := c.SubscribeLogs(ctx)
		if err != nil {
			cancel()
			return statusErrorMsg{err: err}
		}
		return subscribedMsg{ch: ch, cancel: cancel}
	}
}

// forwardEventsCmd pumps one event from the stream into the message loop,
// re-arming itself so the stream keeps flowing.
func forwardEventsCmd(ch <-chan ctl.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return logClosedMsg{}
		}
		if msg := parseLogEvent(ev); msg != nil {
			return msg
		}
		return forwardEventsCmd(ch)()
	}
}

// parseLogEvent decodes an SSE payload into a logMsg or stateMsg, or nil for
// unknown event types.
func parseLogEvent(ev ctl.Event) tea.Msg {
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(ev.Data), &header); err != nil {
		return nil
	}
	switch header.Type {
	case "log":
		var l supervisor.KernelLogLine
		if err := json.Unmarshal([]byte(ev.Data), &l); err != nil {
			return nil
		}
		return logMsg{line: formatLogLine(l)}
	case "state":
		var st supervisor.KernelState
		if err := json.Unmarshal([]byte(ev.Data), &st); err != nil {
			return nil
		}
		return stateMsg{state: st}
	}
	return nil
}

// formatLogLine renders a kernel log line as "2006-01-02 15:04:05 LEVEL  line".
func formatLogLine(l supervisor.KernelLogLine) string {
	level := "INFO "
	if l.Stream == "stderr" {
		level = errorStyle.Render("ERROR")
	}
	ts := time.UnixMilli(l.TS).Format("2006-01-02 15:04:05")
	return fmt.Sprintf("%s %s  %s", ts, level, l.Line)
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

// frameView renders the framed layout: bordered box with a title, status bar,
// tab bar, active tab body and the key binding help line. The frame fills the
// whole terminal window.
func (m Model) frameView() string {
	w, h := m.width, m.height
	if w < minWidth {
		w = minWidth
	}
	if h < minHeight {
		h = minHeight
	}
	inner := w - 2

	// The log viewport (or placeholder) fills everything between the fixed
	// frame rows. SetSize here is a belt-and-suspenders re-scroll; the size is
	// applied on WindowSizeMsg so the model's viewport is always current.
	if h > frameRows {
		m.logs.SetSize(inner, h-frameRows)
	}

	statusLine := renderStatus(m.state, m.client.Endpoint())
	if m.err != nil {
		statusLine += "  " + errorStyle.Render("⚠ "+m.errText())
	}
	if m.operating {
		statusLine += "  " + m.spinner.View() + " " + kernelOps[m.kSelected].label + "…"
	}

	// Second status line: the active profile summary.
	activeLine := m.profiles.ActiveSummary(m.profActive)

	title := " mihomo-tui · " + m.client.Endpoint() + " "
	// TEMP diagnostic: show the resolved window size until the short-window
	// report is confirmed — remove once verified.
	size := fmt.Sprintf(" %dx%d ", w, h)
	help := tabHelp(m.activeTab)
	switch {
	case m.filtering:
		help = "filter: " + m.filterInput + "▌  (enter:apply  esc:cancel)"
	case m.importing:
		help = "import: tab:switch  enter:import  esc:cancel"
	case m.confirmDel:
		help = "⚠ 删除所选 profile? (y 确认 / 其他取消)"
	default:
		// Surface the follow-at-bottom state on the Logs tab only.
		if m.activeTab == 0 {
			flag := "ON"
			if !m.logs.follow {
				flag = "OFF"
			}
			help = strings.Replace(help, "f:follow", "f:follow("+flag+")", 1)
		}
	}
	return strings.Join([]string{
		frameTop(inner, title, size),
		frameRow(statusLine, inner),
		frameRow(activeLine, inner),
		frameRow(renderTabs(m.activeTab), inner),
		frameSep(inner),
		m.body(inner, h-frameRows),
		frameSep(inner),
		frameRow(help, inner),
		frameBottom(inner),
	}, "\n")
}

// errText maps low-level errors to friendly status-bar text.
func (m Model) errText() string {
	if m.err == nil {
		return ""
	}
	if errors.Is(m.err, ctl.ErrUnauthorized) {
		return "认证失败：token 无效或 server 开启了认证"
	}
	return m.err.Error()
}

// importFormView renders the import popup as a bordered modal: a bold header,
// the two textinputs, then a separated key-hint footer, centered in the body
// area.
func (m Model) importFormView(width, height int) string {
	const cw = 60 // modal content width
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render("Import subscription")
	content := strings.Join([]string{
		frameTop(cw, "", ""),
		frameRow(header, cw),
		frameSep(cw),
		frameRow(m.form.url.View(), cw),
		frameRow(m.form.name.View(), cw),
		frameSep(cw),
		frameRow("tab:switch  enter:import  esc:cancel", cw),
		frameBottom(cw),
	}, "\n")
	return lipgloss.NewStyle().
		Width(width).Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)
}

// overlayModal centers modal over base using lipgloss's compositor, so the
// styled modal draws on top of the styled frame without mangling ANSI codes.
func overlayModal(base, modal string, w, h int) string {
	mw, mh := lipgloss.Width(modal), lipgloss.Height(modal)
	x := (w - mw) / 2
	if x < 0 {
		x = 0
	}
	y := (h - mh) / 2
	if y < 0 {
		y = 0
	}
	comp := lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(modal).X(x).Y(y).Z(1),
	)
	return comp.Render()
}

// configModal renders the bordered config viewer modal for the given terminal
// size: a bold header, the scrollable config viewport, and a key-hint footer.
func (m Model) configModal(w, h int) string {
	cw := w - 8
	if cw < 24 {
		cw = 24
	}
	if cw > 80 {
		cw = 80
	}
	title := "View Config (" + m.config.Mode() + ")"
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render(title)
	sep := strings.Repeat("─", cw)
	footer := lipgloss.NewStyle().Width(cw).Align(lipgloss.Center).Render("c:active/runtime  e:edit  ↑↓:scroll  esc:close")
	viewHeight := h - 10
	if viewHeight < 1 {
		viewHeight = 1
	}
	m.config.SetSize(cw, viewHeight)
	inner := strings.Join([]string{header, sep, m.config.View(), sep, footer}, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Background(modalBackground).
		Width(cw + 2).
		Render(inner)
}

// sectionFormView renders the section editor popup: key and value textinputs
// with a header and a key-hint footer.
func (m Model) sectionFormView() string {
	const cw = 60
	title := "Edit Section"
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render(title)
	sep := strings.Repeat("─", cw)
	footer := lipgloss.NewStyle().Width(cw).Align(lipgloss.Center).Render("tab:switch  enter:save  esc:cancel")
	inner := strings.Join([]string{
		header,
		sep,
		m.sectionForm.key.View(),
		m.sectionForm.value.View(),
		sep,
		footer,
	}, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Background(modalBackground).
		Width(cw + 2).
		Render(inner)
}

// editInputView renders the network field editor popup: a single prefilled
// textinput with a header and a key-hint footer.
func (m Model) editInputView() string {
	const cw = 40
	f := networkFields[m.editField]
	header := lipgloss.NewStyle().Bold(true).Width(cw).Align(lipgloss.Center).Render("Edit " + f.label)
	sep := strings.Repeat("─", cw)
	footer := lipgloss.NewStyle().Width(cw).Align(lipgloss.Center).Render("enter:save  esc:cancel")
	inner := strings.Join([]string{header, sep, m.editInput.View(), sep, footer}, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Background(modalBackground).
		Width(cw + 2).
		Render(inner)
}

// body renders the active tab's content at the given size, wrapping every
// line with the frame borders. Logs shows the viewport; the other tabs are
// centered placeholders until their phases land.
func (m Model) body(width, height int) string {
	var content string
	switch m.activeTab {
	case 0:
		content = m.logs.View()
	case 1:
		if m.importing {
			content = m.importFormView(width, height)
		} else {
			m.profiles.SetSize(width, height)
			content = m.profiles.View()
		}
	case 2:
		content = lipgloss.NewStyle().Height(height).MaxHeight(height).Render(m.renderKernelTab())
	default:
		content = lipgloss.NewStyle().
			Width(width).Height(height).
			Align(lipgloss.Center, lipgloss.Center).
			Faint(true).
			Render("[" + tabTitles[m.activeTab] + " tab — not implemented yet]")
	}
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		// MaxWidth guards against wide/emoji content overflowing the right
		// border when the computed width and the terminal's rendering differ.
		clipped := lipgloss.NewStyle().MaxWidth(width).Render(l)
		lines[i] = frameRow(clipped, width)
	}
	return strings.Join(lines, "\n")
}

// Minimum frame dimensions, the narrow-screen threshold below which the
// frame is dropped in favor of a bare log stream, and the number of fixed
// rows (top + 2 status + tab + sep + body + sep + help + bottom) the log
// viewport must leave room for.
const (
	minWidth    = 40
	minHeight   = 10
	narrowWidth = 60
	frameRows   = 8
)

// frameTop renders the top border with the title embedded on the left and an
// optional right-side label (e.g. window size).
func frameTop(inner int, title, right string) string {
	mid := strings.Repeat("─", max(0, inner-2-lipgloss.Width(title)-lipgloss.Width(right)))
	return "┌─" + title + mid + right + "─┐"
}

// frameSep renders an internal separator line.
func frameSep(inner int) string {
	return "├" + strings.Repeat("─", inner) + "┤"
}

// frameBottom renders the bottom border.
func frameBottom(inner int) string {
	return "└" + strings.Repeat("─", inner) + "┘"
}

// frameRow wraps a content line with the vertical borders, padded to the
// inner width.
func frameRow(content string, inner int) string {
	return "│" + lipgloss.PlaceHorizontal(inner, lipgloss.Left, content) + "│"
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

// tabTitles are the tab names, one per index.
var tabTitles = []string{"Logs", "Subscriptions", "Config"}

// tabHelp lists the key bindings for a tab; the footer switches with the tab
// so only the relevant operations are shown.
var helpByTab = [][]string{
	// Logs
	{"1-3:tabs", "/:filter", "f:follow", "q:quit"},
	// Profiles
	{"1-3:tabs", "a:activate", "u:refresh", "d:delete", "i:import", "q:quit"},
	// Config
	{"1-3:tabs", "↑↓:select", "enter:run", "q:quit"},
}

func tabHelp(active int) string {
	return strings.Join(helpByTab[active], "  ")
}

// renderTabs renders the tab bar, highlighting the active tab.
func renderTabs(active int) string {
	var b strings.Builder
	for i, title := range tabTitles {
		label := fmt.Sprintf("[%d] %s", i+1, title)
		if i == active {
			label = tabActiveStyle.Render(label)
		}
		b.WriteString(label)
		b.WriteString("  ")
	}
	return b.String()
}
