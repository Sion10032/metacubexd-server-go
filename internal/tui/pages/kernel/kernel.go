// Package kernel implements the Config tab: the kernel operation menu, the
// editable network fields and the config viewer/editor overlays. It depends
// only on shared, components and the standard/third-party libraries — never
// on the root tui package or the other pages.
package kernel

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/tui/client"
	"metacubexd-server-go/internal/tui/components"
	"metacubexd-server-go/internal/tui/shared"
	"metacubexd-server-go/internal/api"
)

// Model owns the Config tab state: the kernel operation menu, the editable
// network fields and the config viewer/editor overlays.
type Model struct {
	client *client.Client

	kSelected   int
	kConfirming bool
	operating   bool
	network     NetworkSettings

	viewingConfig  bool
	editing        bool
	editField      int
	editInput      textinput.Model
	editingEnum    bool // option-picker popup open for an enum network field
	enumSel        int  // highlighted index into networkFields[editField].options
	editingSection bool
	sectionForm    components.Form

	config ConfigModel

	// state/err mirror the root's globals so the tab body can render the
	// status line; the root refreshes them before every render.
	state *api.KernelState
	err   error

	width  int
	height int
}

// New returns the Config tab for the given control API client.
func New(client *client.Client) *Model {
	return &Model{client: client, config: NewConfigModel()}
}

// Title implements shared.Tab.
func (m *Model) Title() string { return "Config" }

// Help implements shared.Tab; the Config tab shows the static menu help.
func (m *Model) Help() string { return "1-3:tabs  ↑↓:select  enter:run  q:quit" }

// Busy implements shared.Tab: a kernel operation is in flight.
func (m *Model) Busy() bool { return m.operating }

// Overlay implements shared.Tab, returning the active popup with the highest
// priority first: the network-field editor, then the section editor, then the
// config viewer. The section editor renders the config viewer beneath it in
// its own View, matching the original layout.
func (m *Model) Overlay() shared.Modal {
	switch {
	case m.editing:
		return &editModal{m: m}
	case m.editingEnum:
		return &enumModal{m: m}
	case m.editingSection:
		return &sectionModal{m: m}
	case m.viewingConfig:
		return &viewerModal{m: m}
	}
	return nil
}

// SetSize stores the terminal body size; the config viewer sizes itself from
// the overlay view calls.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
}

// SetStatus refreshes the kernel state and status-bar error the tab body
// renders at its top. The root owns these globals and pushes them here before
// every render.
func (m *Model) SetStatus(state *api.KernelState, err error) {
	m.state = state
	m.err = err
}

// Update implements shared.Tab: menu/selection keys while the Config tab is
// active, mouse wheels forwarded to the config viewer, and the config fetch /
// network settings / section edit results.
func (m *Model) Update(msg tea.Msg) (shared.Tab, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.config.Update(msg)
		return m, cmd, false
	case ConfigLoadedMsg:
		if msg.Mode == m.config.mode {
			m.config.SetContent(msg.Content)
		}
		return m, nil, false
	case NetworkSettingsMsg:
		m.network = msg.Settings
		return m, nil, false
	case SectionEditMsg:
		return m, tea.Batch(FetchConfig(m.client, m.config.mode), shared.FetchStatus(m.client), FetchNetworkSettings(m.client)), false
	}
	return m, nil, false
}

// updateKey handles selection and execution while the Config tab is active.
// The Recover confirmation state (kConfirming) is kept for when Recover is
// re-enabled. Overlay keys (editing, section, viewer, kConfirming) are
// consumed here so the root never touches them.
func (m *Model) updateKey(msg tea.Msg) (shared.Tab, tea.Cmd, bool) {
	key := msg.(tea.KeyPressMsg).String()

	// Overlay states swallow all keys — match the old root Overlay() check.
	if m.editing {
		return m, m.updateEdit(msg), true
	}
	if m.editingEnum {
		return m, m.updateEnumEdit(msg), true
	}
	if m.editingSection {
		return m, m.updateSectionForm(msg), true
	}
	if m.viewingConfig {
		return m, m.updateConfig(msg), true
	}
	if m.kConfirming {
		m.kConfirming = false
		if key == "y" || key == "Y" {
			return m.startKernelOp(kernelOps[m.kSelected])
		}
		return m, nil, true // cancel — still consumed
	}
	menuLen := ConfigMenuLen()
	switch key {
	case "up", "k":
		m.kSelected = (m.kSelected + menuLen - 1) % menuLen
		return m, nil, true
	case "down", "j":
		m.kSelected = (m.kSelected + 1) % menuLen
		return m, nil, true
	case "enter", "space":
		switch {
		case m.kSelected < len(kernelOps):
			op := kernelOps[m.kSelected]
			if op.label == "Recover" {
				m.kConfirming = true
				return m, nil, true
			}
			return m.startKernelOp(op)
		case m.kSelected < len(kernelOps)+len(networkFields):
			return m.startEditField(m.kSelected - len(kernelOps))
		default:
			m.viewingConfig = true
			m.config.ResetScroll()
			return m, FetchConfig(m.client, m.config.mode), true
		}
	}
	return m, nil, false
}

// updateConfig drives the config viewer modal: esc closes, e opens the
// section editor, c toggles active/runtime, other keys scroll the viewport.
func (m *Model) updateConfig(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.viewingConfig = false
			return nil
		case "e":
			m.editingSection = true
			m.sectionForm = newSectionForm()
			return m.sectionForm.Fields[0].Focus()
		case "c":
			m.config.ToggleMode()
			m.config.ResetScroll()
			return FetchConfig(m.client, m.config.mode)
		default:
			return m.config.Update(msg)
		}
	}
	return nil
}

// View implements shared.Tab, rendering the Config tab body: the kernel
// status line, the operation list, the editable network fields (with current
// values), and the raw YAML viewer entry. The selected entry is highlighted.
func (m *Model) View() string {
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
		if i == m.kSelected {
			prefix, label = "> ", shared.SelectedStyle.Render(op.label)
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
			prefix, label = "> ", shared.SelectedStyle.Render(label)
		}
		lines = append(lines, prefix+label)
	}

	{
		sel := len(kernelOps) + len(networkFields)
		prefix, label := "  ", "View YAML"
		if sel == m.kSelected {
			prefix, label = "> ", shared.SelectedStyle.Render(label)
		}
		lines = append(lines, prefix+label)
	}

	if m.kConfirming {
		lines = append(lines, "", shared.ErrorStyle.Render("⚠ Recover 将重置 active config,确认执行? (y 确认 / 其他取消)"))
	}
	return strings.Join(lines, "\n")
}

// Editing reports whether the network-field editor popup is open.
func (m *Model) Editing() bool { return m.editing }

// EditingSection reports whether the section editor popup is open.
func (m *Model) EditingSection() bool { return m.editingSection }

// ViewingConfig reports whether the config viewer modal is open.
func (m *Model) ViewingConfig() bool { return m.viewingConfig }

// SetViewingConfig opens or closes the config viewer modal.
func (m *Model) SetViewingConfig(v bool) { m.viewingConfig = v }

// SelectedOp returns the index of the selected menu entry.
func (m *Model) SelectedOp() int { return m.kSelected }

// Operating reports whether a kernel operation is in flight.
func (m *Model) Operating() bool { return m.operating }

// Confirming reports whether the Recover confirmation prompt is active.
func (m *Model) Confirming() bool { return m.kConfirming }

// ResetOperation clears the operating flag and the Recover confirmation once
// a kernel operation finishes or fails; the root calls it when status lands.
func (m *Model) ResetOperation() {
	m.operating = false
	m.kConfirming = false
}

// NetworkLoaded reports whether the editable network settings were fetched.
func (m *Model) NetworkLoaded() bool { return m.network.Loaded }

// ConfigMode returns the label of the config currently selected.
func (m *Model) ConfigMode() string { return m.config.Mode() }

// ConfigYOffset reports the config viewer scroll offset.
func (m *Model) ConfigYOffset() int { return m.config.viewport.YOffset() }

// EditFieldValue returns the text currently in the network-field editor
// input.
func (m *Model) EditFieldValue() string { return m.editInput.Value() }

// OperationLabel returns the label of the selected operation, used by the
// root spinner while an operation runs.
func (m *Model) OperationLabel() string { return kernelOps[m.kSelected].label }
