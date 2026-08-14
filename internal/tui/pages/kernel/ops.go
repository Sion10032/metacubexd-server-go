package kernel

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/spinner"

	"metacubexd-server-go/internal/tui/client"
	"metacubexd-server-go/internal/tui/shared"
	"metacubexd-server-go/internal/api"
)

// kernelOp is one operation available on the Config tab, in selection order.
// Rollback/Recover are commented out for now — destructive escape hatches,
// re-enabled later.
type kernelOp struct {
	label string
	op    func(*client.Client) (api.KernelState, error)
}

var kernelOps = []kernelOp{
	{"Start", (*client.Client).KernelStart},
	{"Stop", (*client.Client).KernelStop},
	{"Restart", (*client.Client).KernelRestart},
	// {"Rollback", (*client.Client).KernelRollback},
	// {"Recover", (*client.Client).KernelRecover},
}

// ConfigMenuLen is the number of selectable entries on the Config tab: kernel
// ops + network fields + the raw YAML viewer.
func ConfigMenuLen() int {
	return len(kernelOps) + len(networkFields) + 1
}

// OpCount returns the number of kernel operations in the menu.
func OpCount() int { return len(kernelOps) }

// kernelTick emits the first spinner frame tick so the root spinner animates
// while the operation runs; the root takes the animation over from there. The
// tick carries no spinner ID so any spinner accepts it.
var kernelTick tea.Cmd = func() tea.Msg {
	return spinner.TickMsg{Time: time.Now()}
}

// startKernelOp marks the operation as running, starts the spinner and issues
// the operation command.
func (m *Model) startKernelOp(op kernelOp) (shared.Tab, tea.Cmd, bool) {
	m.operating = true
	return m, tea.Batch(kernelOpCmd(m.client, op.op), kernelTick), true
}
