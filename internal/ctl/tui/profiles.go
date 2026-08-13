package tui

import (
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl/tui/shared"
	"metacubexd-server-go/internal/profile"
)

// ProfilesModel renders the profile list as a table. The active profile is
// marked with a dot; activeID is tracked from the operations this session has
// run (the server does not expose the active id via /profiles).
type ProfilesModel struct {
	table    table.Model
	profiles []profile.Meta
	activeID string
}

// NewProfilesModel returns an empty profile table.
func NewProfilesModel() ProfilesModel {
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "Name", Width: 28},
			{Title: "Type", Width: 10},
			{Title: "Updated", Width: 16},
			{Title: "Active", Width: 8},
		}),
		table.WithFocused(true),
	)
	return ProfilesModel{table: t}
}

// SetProfiles rebuilds the table rows from a profile list.
func (p *ProfilesModel) SetProfiles(list []profile.Meta, activeID string) {
	p.profiles = list
	p.activeID = activeID
	rows := make([]table.Row, 0, len(list))
	for _, m := range list {
		active := ""
		if m.ID == activeID {
			active = "●"
		}
		rows = append(rows, table.Row{
			m.Name,
			m.Type,
			time.UnixMilli(m.UpdatedAt).Format("2006-01-02 15:04"),
			active,
		})
	}
	p.table.SetRows(rows)
}

// SetSize resizes the table.
func (p *ProfilesModel) SetSize(width, height int) {
	p.table.SetWidth(width)
	p.table.SetHeight(height)
}

// Update forwards messages to the table (selection keys etc).
func (p *ProfilesModel) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	p.table, cmd = p.table.Update(msg)
	return cmd
}

// View renders the table.
func (p ProfilesModel) View() string {
	return p.table.View()
}

// SelectedID returns the id of the currently selected row, or "" when the
// list is empty.
func (p ProfilesModel) SelectedID() string {
	if len(p.profiles) == 0 {
		return ""
	}
	return p.profiles[min(p.table.Cursor(), len(p.profiles)-1)].ID
}

// ActiveSummary renders the status-bar line for the active profile, e.g.
// "active: sub (remote)".
func (p ProfilesModel) ActiveSummary(activeID string) string {
	if activeID == "" {
		return "active: —"
	}
	for _, m := range p.profiles {
		if m.ID == activeID {
			return "active: " + m.Name + " (" + m.Type + ")"
		}
	}
	return "active: " + activeID
}

// updateProfilesMsg handles profile list loads and operation results; after a
// successful operation the list and kernel status are refreshed.
func (m Model) updateProfilesMsg(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		return m, tea.Batch(fetchProfilesCmd(m.client), shared.FetchStatus(m.client))
	}
	return m, nil
}
