package profiles

import "metacubexd-server-go/internal/profile"

// ProfilesLoadedMsg carries the fetched profile list.
type ProfilesLoadedMsg struct {
	List []profile.Meta
	Err  error
}

// ProfileOpMsg carries the result of a profile operation; a nil Err means the
// list and kernel status should be refreshed.
type ProfileOpMsg struct {
	Err error
}
