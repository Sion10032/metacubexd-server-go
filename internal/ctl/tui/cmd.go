package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl/tui/shared"
)

// updateStatus handles the kernel status messages: fresh states, errors and
// the periodic tick; the spinner keeps animating while an operation runs.
func (m Model) updateStatus(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case shared.StatusLoadedMsg:
		m.state = &msg.State
		m.kernelPage().ResetOperation()
		return m, nil
	case shared.StatusErrorMsg:
		m.err = msg.Err
		m.kernelPage().ResetOperation()
		return m, nil
	case spinner.TickMsg:
		if m.tabs[2].Busy() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case shared.TickMsg:
		return m, tea.Batch(shared.FetchStatus(m.client), shared.StatusTick())
	}
	return m, nil
}
