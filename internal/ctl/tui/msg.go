package tui

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
