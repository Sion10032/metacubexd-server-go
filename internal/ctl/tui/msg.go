package tui

import (
	"metacubexd-server-go/internal/profile"
)

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

// sectionEditMsg carries the result of a config section edit.
type sectionEditMsg struct {
	err error
}

// networkSettingsMsg carries the fetched network settings of the active
// config.
type networkSettingsMsg struct {
	settings networkSettings
	err      error
}
