package shared

import tea "charm.land/bubbletea/v2"

// Tab is the contract every page tab (Tab[0..2]) implements. The root App
// holds a []Tab and routes by the active tab.
type Tab interface {
	Title() string                     // tab bar label, e.g. "Logs" / "Subscriptions" / "Config"
	Help() string                      // bottom footer hint (may change with state, e.g. follow ON/OFF)
	SetSize(width, height int)         // respond to WindowSizeMsg
	Update(msg tea.Msg) (Tab, tea.Cmd) // handle messages routed to this page, return the new tab + side effects
	View() string                      // render this page's body (without the frame)
	Busy() bool                        // whether an operation is in flight (drives the root spinner)
	Overlay() Modal                    // the active popup, nil when none (root renders it as overlay)
}

// Modal is the contract for popup components. Import forms, section editors,
// network field editors and the config viewer implement it.
type Modal interface {
	Update(msg tea.Msg) (Modal, tea.Cmd) // handle keys inside the popup
	View(w, h int) string                // render the popup content (root centers it over the frame)
}
