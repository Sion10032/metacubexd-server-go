package components

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// Form is a generic two-field textinput form, shared by the import popup and
// the section editor. It owns the two fields plus the focus index; the caller
// implements the surrounding popup semantics (esc/enter and the header/footer
// rendering).
type Form struct {
	// Fields holds the two textinputs, index 0 being the first (URL/key) and
	// index 1 the second (name/value).
	Fields [2]textinput.Model
	// Focus is the index of the currently focused field (0 or 1).
	Focus int
}

// NewForm builds a two-field form focused on the first field.
func NewForm(first, second textinput.Model) Form {
	return Form{Fields: [2]textinput.Model{first, second}, Focus: 0}
}

// Update handles the keys shared by every two-field form: tab switches the
// focus between the two fields, any other key press is forwarded to the
// focused field. esc and enter stay with the caller so each popup keeps its
// own save/cancel semantics.
func (f Form) Update(msg tea.Msg) (Form, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return f, nil
	}
	if kp.String() == "tab" {
		f.Focus = 1 - f.Focus
		if f.Focus == 0 {
			f.Fields[1].Blur()
			return f, f.Fields[0].Focus()
		}
		f.Fields[0].Blur()
		return f, f.Fields[1].Focus()
	}
	var cmd tea.Cmd
	f.Fields[f.Focus], cmd = f.Fields[f.Focus].Update(kp)
	return f, cmd
}
