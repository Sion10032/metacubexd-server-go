package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/shared"
	"metacubexd-server-go/internal/supervisor"
)

// KernelModel owns the Config tab state: the kernel operation menu, the
// editable network fields and the config viewer/editor overlays.
type KernelModel struct {
	kSelected      int
	kConfirming    bool
	operating      bool
	network        networkSettings
	viewingConfig  bool
	editing        bool
	editField      int
	editInput      textinput.Model
	editingSection bool
	sectionForm    sectionForm
}

// NewKernelModel returns an empty Config tab model.
func NewKernelModel() KernelModel {
	return KernelModel{}
}

// Editing reports whether the network-field editor popup is open.
func (k KernelModel) Editing() bool { return k.editing }

// EditingSection reports whether the section editor popup is open.
func (k KernelModel) EditingSection() bool { return k.editingSection }

// ViewingConfig reports whether the config viewer modal is open.
func (k KernelModel) ViewingConfig() bool { return k.viewingConfig }

// kernelOpCmd runs a kernel operation via the client and pushes the fresh
// state, refreshing the status bar when done.
func kernelOpCmd(c *ctl.Client, op func(*ctl.Client) (supervisor.KernelState, error)) tea.Cmd {
	return func() tea.Msg {
		st, err := op(c)
		if err != nil {
			return shared.StatusErrorMsg{Err: err}
		}
		return shared.StatusLoadedMsg{State: st}
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

// updateKeys handles selection and execution while the Config tab is active.
// The Recover confirmation state (kConfirming) is kept for when Recover is
// re-enabled.
func (k KernelModel) updateKeys(key string, m Model) (Model, tea.Cmd) {
	m.kernel = k
	menuLen := configMenuLen()
	if m.kernel.kConfirming {
		m.kernel.kConfirming = false
		if key == "y" || key == "Y" {
			return m.kernel.startKernelOp(kernelOps[m.kernel.kSelected], m)
		}
		return m, nil
	}
	switch key {
	case "up", "k":
		m.kernel.kSelected = (m.kernel.kSelected + menuLen - 1) % menuLen
	case "down", "j":
		m.kernel.kSelected = (m.kernel.kSelected + 1) % menuLen
	case "enter", "space":
		switch {
		case m.kernel.kSelected < len(kernelOps):
			op := kernelOps[m.kernel.kSelected]
			if op.label == "Recover" {
				m.kernel.kConfirming = true
				return m, nil
			}
			return m.kernel.startKernelOp(op, m)
		case m.kernel.kSelected < len(kernelOps)+len(networkFields):
			return m.kernel.startEditField(m.kernel.kSelected-len(kernelOps), m)
		default:
			m.kernel.viewingConfig = true
			m.config.ResetScroll()
			return m, fetchConfigCmd(m.client, m.config.mode)
		}
	}
	return m, nil
}

// startKernelOp marks the operation as running, starts the spinner and issues
// the operation command.
func (k KernelModel) startKernelOp(op kernelOp, m Model) (Model, tea.Cmd) {
	m.kernel = k
	m.kernel.operating = true
	return m, tea.Batch(kernelOpCmd(m.client, op.op), m.spinner.Tick)
}

// renderKernelTab renders the Config tab body: the kernel operation list, the
// editable network fields (with current values), and the raw YAML viewer
// entry. The selected entry is highlighted.
func (m Model) renderKernelTab() string {
	lines := []string{shared.RenderStatus(m.state, "")}
	if m.err != nil {
		lines = append(lines, shared.ErrorStyle.Render("⚠ "+m.err.Error()))
	}
	if m.state != nil && m.state.LastError != "" {
		lines = append(lines, shared.ErrorStyle.Render(m.state.LastError))
	}

	lines = append(lines, "", "[kernel]")
	for i, op := range kernelOps {
		prefix, label := "  ", op.label
		if i == m.kernel.kSelected {
			prefix, label = "> ", shared.SelectedStyle.Render(op.label)
		}
		lines = append(lines, prefix+label)
	}

	lines = append(lines, "", "[network]")
	for i, f := range networkFields {
		sel := len(kernelOps) + i
		value := m.kernel.network.valueOf(f)
		if value == "" {
			value = "—"
		}
		prefix, label := "  ", fmt.Sprintf("%-12s %s", f.label, value)
		if sel == m.kernel.kSelected {
			prefix, label = "> ", shared.SelectedStyle.Render(label)
		}
		lines = append(lines, prefix+label)
	}

	{
		sel := len(kernelOps) + len(networkFields)
		prefix, label := "  ", "View YAML"
		if sel == m.kernel.kSelected {
			prefix, label = "> ", shared.SelectedStyle.Render(label)
		}
		lines = append(lines, prefix+label)
	}

	if m.kernel.kConfirming {
		lines = append(lines, "", shared.ErrorStyle.Render("⚠ Recover 将重置 active config,确认执行? (y 确认 / 其他取消)"))
	}
	return strings.Join(lines, "\n")
}
