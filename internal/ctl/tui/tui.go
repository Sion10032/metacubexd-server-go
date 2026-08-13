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

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/profile"
	"metacubexd-server-go/internal/supervisor"
)

// Model is the top-level Bubble Tea model. It owns the kernel state, the log
// viewport and the current tab.
type Model struct {
	client      *ctl.Client
	state       *supervisor.KernelState
	err         error
	logs        LogsModel
	profiles    ProfilesModel
	config      ConfigModel
	profActive  string
	importing   bool
	form        importForm
	confirmDel  bool
	activeTab   int
	kSelected   int
	kConfirming bool
	operating   bool
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
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
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

// Init returns the initial commands: fetch kernel status, poll it every
// second, subscribe to the SSE log stream and load the profile list.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		fetchStatusCmd(m.client),
		statusTick(),
		subscribeCmd(m.client),
		fetchProfilesCmd(m.client),
	)
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
			if m.activeTab == 2 && !m.config.loaded {
				return m, fetchConfigCmd(m.client, m.config.mode)
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
		case "c":
			if m.activeTab == 2 {
				m.config.ToggleMode()
				return m, fetchConfigCmd(m.client, m.config.mode)
			}
		default:
			// Config tab: up/down/enter drive kernel selection; other keys
			// scroll the config viewport below.
			if m.activeTab == 2 {
				var cmd tea.Cmd
				m, cmd = m.updateKernelKeys(key)
				if cmd != nil {
					return m, cmd
				}
				if key == "up" || key == "down" {
					return m, nil // consumed by kernel selection
				}
				m.config.Update(msg)
				return m, nil
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

// updateKernelKeys handles selection and execution while the Config tab is
// active. The Recover confirmation state (kConfirming) is kept for when
// Recover is re-enabled.
func (m Model) updateKernelKeys(key string) (Model, tea.Cmd) {
	if m.kConfirming {
		m.kConfirming = false
		if key == "y" || key == "Y" {
			return m.startKernelOp(kernelOps[m.kSelected])
		}
		return m, nil
	}
	switch key {
	case "up", "k":
		m.kSelected = (m.kSelected + len(kernelOps) - 1) % len(kernelOps)
	case "down", "j":
		m.kSelected = (m.kSelected + 1) % len(kernelOps)
	case "enter", "space":
		op := kernelOps[m.kSelected]
		if op.label == "Recover" {
			m.kConfirming = true
			return m, nil
		}
		return m.startKernelOp(op)
	}
	return m, nil
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
		content = m.renderConfigTab(width, height)
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

// renderKernelTab renders the kernel control section of the Config tab:
// state summary, last error and the operation list with the selection
// highlighted. The config viewport (renderConfigTab) renders below this
// section. When confirming, a destructive-operation prompt is appended.
func renderKernelTab(state *supervisor.KernelState, selected int, err error, confirming bool) string {
	lines := []string{renderStatus(state, "")}
	if err != nil {
		lines = append(lines, errorStyle.Render("⚠ "+err.Error()))
	}
	if state != nil && state.LastError != "" {
		lines = append(lines, errorStyle.Render(state.LastError))
	}
	lines = append(lines, "", "kernel operations:")
	for i, op := range kernelOps {
		prefix, label := "  ", op.label
		if i == selected {
			prefix, label = "> ", selectedStyle.Render(op.label)
		}
		lines = append(lines, prefix+label)
	}
	if confirming {
		lines = append(lines, "", errorStyle.Render("⚠ Recover 将重置 active config,确认执行? (y 确认 / 其他取消)"))
	}
	return strings.Join(lines, "\n")
}

// renderConfigTab composes the Config tab body: the kernel operation list on
// top, then a mode header and the scrollable config viewport below.
func (m Model) renderConfigTab(width, height int) string {
	kernel := renderKernelTab(m.state, m.kSelected, m.err, m.kConfirming)
	kernelLines := strings.Count(kernel, "\n") + 1
	modeLine := "config (" + m.config.Mode() + "):"
	configHeight := height - kernelLines - 1
	if configHeight < 1 {
		configHeight = 1
	}
	m.config.SetSize(width, configHeight)
	content := strings.Join([]string{kernel, modeLine, m.config.View()}, "\n")
	return lipgloss.NewStyle().Height(height).MaxHeight(height).Render(content)
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
	{"1-3:tabs", "↑↓:select", "enter:run", "c:config", "q:quit"},
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
