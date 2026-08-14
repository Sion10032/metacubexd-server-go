package profiles

import "metacubexd-server-go/internal/api"

// ProfilesLoadedMsg carries the fetched profile list.
type ProfilesLoadedMsg struct {
	List []api.Meta
	Err  error
}

// ProfileOpMsg carries the result of a profile operation; a nil Err means the
// list and kernel status should be refreshed.
type ProfileOpMsg struct {
	Err error
}
