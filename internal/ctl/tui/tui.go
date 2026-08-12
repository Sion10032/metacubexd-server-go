// Package tui implements the interactive terminal UI for mihomo-tui.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/supervisor"
)

// Model is the top-level Bubble Tea model. It owns the kernel state, the log
// viewport and the current tab.
type Model struct {
	client     *ctl.Client
	state      *supervisor.KernelState
	err        error
	logs       LogsModel
	activeTab  int
	kSelected  int
	kConfirming bool
	operating  bool
	spinner    spinner.Model
	width      int
	height     int
	logCh      <-chan ctl.Event
	logCancel  context.CancelFunc
	quitting   bool
}

// New returns a Model for the given control API client.
func New(client *ctl.Client) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	return Model{client: client, logs: NewLogsModel(), spinner: s}
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

// Init returns the initial commands: fetch kernel status, poll it every
// second and subscribe to the SSE log stream.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		fetchStatusCmd(m.client),
		statusTick(),
		subscribeCmd(m.client),
	)
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
	case tea.KeyMsg:
		key := msg.String()
		switch key {
		case "q", "ctrl+c":
			m.quitting = true
			m.closeLogStream()
			return m, tea.Quit
		case "1", "2", "3":
			m.activeTab = int(key[0] - '1')
		default:
			if m.activeTab == 2 {
				var cmd tea.Cmd
				m, cmd = m.updateKernelKeys(key)
				if cmd != nil {
					return m, cmd
				}
			}
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
// order.
type kernelOp struct {
	label string
	op    func(*ctl.Client) (supervisor.KernelState, error)
}

var kernelOps = []kernelOp{
	{"Start", (*ctl.Client).KernelStart},
	{"Stop", (*ctl.Client).KernelStop},
	{"Restart", (*ctl.Client).KernelRestart},
	{"Rollback", (*ctl.Client).KernelRollback},
	{"Recover", (*ctl.Client).KernelRecover},
}

// updateKernelKeys handles selection, execution and the Recover confirmation
// while the Config tab is active. Recover is destructive, so Enter on it
// enters a confirm state where only "y" runs it.
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
	case "enter", " ":
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

// View renders the framed layout: bordered box with a title, status bar, tab
// bar, active tab body and the key binding help line. The frame fills the
// whole terminal window.
func (m Model) View() string {
	w, h := m.width, m.height
	if w < minWidth {
		w = minWidth
	}
	if h < minHeight {
		h = minHeight
	}
	inner := w - 2

	// The log viewport (or placeholder) fills everything between the fixed
	// frame rows: top + status + tab + sep + body + sep + help + bottom.
	// (Status and tab share the header area — no separator between them.)
	const frameRows = 7
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

	title := " mihomo-tui · " + m.client.Endpoint() + " "
	// TEMP diagnostic: show the resolved window size until the short-window
	// report is confirmed — remove once verified.
	size := fmt.Sprintf(" %dx%d ", w, h)
	return strings.Join([]string{
		frameTop(inner, title, size),
		frameRow(statusLine, inner),
		frameRow(renderTabs(m.activeTab), inner),
		frameSep(inner),
		m.body(inner, h-frameRows),
		frameSep(inner),
		frameRow(helpLine, inner),
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

// body renders the active tab's content at the given size, wrapping every
// line with the frame borders. Logs shows the viewport; the other tabs are
// centered placeholders until their phases land.
func (m Model) body(width, height int) string {
	var content string
	switch m.activeTab {
	case 0:
		content = m.logs.View()
	case 2:
		content = lipgloss.NewStyle().Height(height).MaxHeight(height).Render(renderKernelTab(m.state, m.kSelected, m.err, m.kConfirming))
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

// Minimum frame dimensions.
const (
	minWidth  = 40
	minHeight = 10
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
// highlighted. Config editing (Phase 3) lands below this section. When
// confirming, a destructive-operation prompt is appended.
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
	lines = append(lines, "", lipgloss.NewStyle().Faint(true).Render("config editor — Phase 3"))
	return strings.Join(lines, "\n")
}

// tabTitles are the tab names, one per index.
var tabTitles = []string{"Logs", "Profiles", "Config"}

// helpLine lists the key bindings.
const helpLine = "1-3:tabs  ↑↓:select  enter:run  /:filter  f:follow  q:quit"

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
