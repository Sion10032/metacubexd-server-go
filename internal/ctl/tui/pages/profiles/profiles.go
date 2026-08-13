// Package profiles implements the Subscriptions tab: the profile list as a
// selectable table with activate/refresh/delete/import operations. The
// import form replaces the page body and the delete prompt drives the footer
// hint while active — neither is an overlay popup, matching the original
// rendering behavior.
package profiles

import (
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/components"
	"metacubexd-server-go/internal/ctl/tui/shared"
	"metacubexd-server-go/internal/profile"
)

// Model renders the profile list as a table. The active profile is marked
// with a dot; activeID is tracked from the operations this session has run
// (the server does not expose the active id via /profiles). The import form
// and the delete confirmation are inline states owned by this page.
type Model struct {
	table      table.Model
	profiles   []profile.Meta
	activeID   string
	importing  bool
	form       components.Form
	confirmDel bool
	client     *ctl.Client
	width      int
	height     int
}

// New returns the Subscriptions tab for the given control API client.
func New(client *ctl.Client) *Model {
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "Name", Width: 28},
			{Title: "Type", Width: 10},
			{Title: "Updated", Width: 16},
			{Title: "Active", Width: 8},
		}),
		table.WithFocused(true),
	)
	return &Model{table: t, client: client}
}

// Title implements shared.Tab.
func (m *Model) Title() string { return "Subscriptions" }

// Help implements shared.Tab, reflecting the current state with the same
// priority as the original footer: the import form hint while importing, the
// delete confirmation while confirming, otherwise the default operations.
func (m *Model) Help() string {
	if m.importing {
		return "import: tab:switch  enter:import  esc:cancel"
	}
	if m.confirmDel {
		return "⚠ 删除所选 profile? (y 确认 / 其他取消)"
	}
	return "1-3:tabs  a:activate  u:refresh  d:delete  i:import  q:quit"
}

// Busy implements shared.Tab; profile operations show no spinner.
func (m *Model) Busy() bool { return false }

// Overlay implements shared.Tab; the import form replaces the body and the
// delete prompt is a footer hint, so there is no popup overlay.
func (m *Model) Overlay() shared.Modal { return nil }

// SetSize stores the body size (used by the import form view) and resizes
// the table.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.table.SetWidth(width)
	m.table.SetHeight(height)
}

// SetProfiles rebuilds the table rows from a profile list.
func (m *Model) SetProfiles(list []profile.Meta, activeID string) {
	m.profiles = list
	m.activeID = activeID
	rows := make([]table.Row, 0, len(list))
	for _, meta := range list {
		active := ""
		if meta.ID == activeID {
			active = "●"
		}
		rows = append(rows, table.Row{
			meta.Name,
			meta.Type,
			time.UnixMilli(meta.UpdatedAt).Format("2006-01-02 15:04"),
			active,
		})
	}
	m.table.SetRows(rows)
}

// Update implements shared.Tab: profile loads refresh the table, operation
// results re-fetch the list and the kernel status, and key presses drive the
// current state (import form, delete confirm, operations or the table).
func (m *Model) Update(msg tea.Msg) (shared.Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	case ProfilesLoadedMsg:
		m.SetProfiles(msg.List, m.activeID)
		return m, nil
	case ProfileOpMsg:
		if msg.Err != nil {
			return m, nil
		}
		return m, tea.Batch(FetchProfiles(m.client), shared.FetchStatus(m.client))
	}
	return m, nil
}

// updateKey routes a key press by current state: the import form first, then
// the delete confirmation, then the operations (a/u/d/i) and the table
// selection keys.
func (m *Model) updateKey(msg tea.Msg) (shared.Tab, tea.Cmd) {
	key := msg.(tea.KeyPressMsg).String()
	if m.importing {
		return m.updateImport(msg)
	}
	if m.confirmDel {
		return m.updateConfirmDel(key)
	}
	switch key {
	case "a":
		if id := m.SelectedID(); id != "" {
			m.activeID = id
			return m, profileOpCmd(m.client, func() error {
				_, err := m.client.ProfileActivate(id)
				return err
			})
		}
	case "u":
		if id := m.SelectedID(); id != "" {
			return m, profileOpCmd(m.client, func() error {
				_, err := m.client.ProfileRefresh(id)
				return err
			})
		}
	case "d":
		if m.SelectedID() != "" {
			m.confirmDel = true
		}
	case "i":
		m.importing = true
		m.form = newImportForm()
		return m, m.form.Fields[0].Focus()
	default:
		// Selection keys drive the table.
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

// View implements shared.Tab: the import form replaces the table body while
// importing, otherwise the table is rendered.
func (m *Model) View() string {
	if m.importing {
		return m.importFormView(m.width, m.height)
	}
	return m.table.View()
}

// SelectedID returns the id of the currently selected row, or "" when the
// list is empty.
func (m *Model) SelectedID() string {
	if len(m.profiles) == 0 {
		return ""
	}
	return m.profiles[min(m.table.Cursor(), len(m.profiles)-1)].ID
}

// ActiveID returns the id of the profile marked active this session.
func (m *Model) ActiveID() string { return m.activeID }

// SetActiveID marks the given profile id as active.
func (m *Model) SetActiveID(id string) { m.activeID = id }

// ActiveSummary renders the status-bar line for the active profile, e.g.
// "active: sub (remote)".
func (m *Model) ActiveSummary() string {
	if m.activeID == "" {
		return "active: —"
	}
	for _, meta := range m.profiles {
		if meta.ID == m.activeID {
			return "active: " + meta.Name + " (" + meta.Type + ")"
		}
	}
	return "active: " + m.activeID
}

// Importing reports whether the import form is open.
func (m *Model) Importing() bool { return m.importing }

// ConfirmingDel reports whether the delete confirmation is active.
func (m *Model) ConfirmingDel() bool { return m.confirmDel }
