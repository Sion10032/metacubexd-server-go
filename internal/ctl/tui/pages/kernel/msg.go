package kernel

// ConfigLoadedMsg carries a fetched config body (active or runtime).
type ConfigLoadedMsg struct {
	Mode    int
	Content string
	Err     error
}

// SectionEditMsg carries the result of a config section edit.
type SectionEditMsg struct {
	Err error
}

// NetworkSettingsMsg carries the fetched network settings of the active
// config.
type NetworkSettingsMsg struct {
	Settings NetworkSettings
	Err      error
}
