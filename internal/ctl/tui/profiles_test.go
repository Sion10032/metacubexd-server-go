package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/ctl/tui/pages/profiles"
	"metacubexd-server-go/internal/ctl/tui/shared"
	"metacubexd-server-go/internal/profile"
)

// TestProfilesTabLoaded verifies loading profiles renders the table and the
// status bar's second line shows the active profile summary.
func TestProfilesTabLoaded(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(profiles.ProfilesLoadedMsg{List: []profile.Meta{
		{ID: "a", Name: "base", Type: "local", UpdatedAt: 1723456789000},
		{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1723456789000},
	}})
	mdl := nm.(Model)
	mdl.tabs[2].(*profiles.Model).SetActiveID("b")
	nm = mdl

	if got := nm.View().Content; !strings.Contains(got, "active: sub (remote)") {
		t.Errorf("View missing active profile summary:\n%s", got)
	}

	nm, _ = nm.Update(keyPress("3"))
	got := shared.ANSIRe.ReplaceAllString(nm.View().Content, "")
	if !strings.Contains(got, "sub") || !strings.Contains(got, "●") {
		t.Errorf("profiles tab missing table content:\n%s", got)
	}
}

// TestProfileActivateKey verifies a activates the selected profile and marks
// it active.
func TestProfileActivateKey(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"starting"}`)
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(profiles.ProfilesLoadedMsg{List: []profile.Meta{{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1}}})
	nm, _ = nm.Update(keyPress("3")) // Profiles tab

	nm, cmd := nm.Update(keyPress("a"))
	if cmd == nil {
		t.Fatal("a returned no command")
	}
	if got := nm.(Model).tabs[2].(*profiles.Model).ActiveID(); got != "b" {
		t.Errorf("profActive = %q, want b", got)
	}
	if _, ok := cmd().(profiles.ProfileOpMsg); !ok {
		t.Fatalf("cmd returned %T, want profiles.ProfileOpMsg", cmd())
	}
	if want := "POST /api/control/profiles/b/activate"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
}

// TestProfileRefreshKey verifies u refreshes the selected profile.
func TestProfileRefreshKey(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"b","name":"sub","type":"remote","updatedAt":2}`)
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(profiles.ProfilesLoadedMsg{List: []profile.Meta{{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1}}})
	nm, _ = nm.Update(keyPress("3"))

	nm, cmd := nm.Update(keyPress("u"))
	if cmd == nil {
		t.Fatal("u returned no command")
	}
	cmd()
	if want := "POST /api/control/profiles/b/refresh"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
}

// TestProfileDeleteConfirm verifies d asks for confirmation and y deletes.
func TestProfileDeleteConfirm(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"b","name":"sub","type":"remote","updatedAt":1}]`)
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(profiles.ProfilesLoadedMsg{List: []profile.Meta{{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1}}})
	nm, _ = nm.Update(keyPress("3"))

	nm, cmd := nm.Update(keyPress("d"))
	if cmd != nil {
		t.Fatal("d should enter confirm state, not issue a command")
	}
	if !nm.(Model).tabs[2].(*profiles.Model).ConfirmingDel() {
		t.Fatal("confirmDel should be true after d")
	}
	if got := nm.View().Content; !strings.Contains(got, "删除所选 profile") {
		t.Errorf("View missing delete prompt:\n%s", got)
	}

	nm, cmd = nm.Update(keyPress("y"))
	if cmd == nil {
		t.Fatal("y returned no command")
	}
	cmd()
	if want := "DELETE /api/control/profiles/b"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}

	// Any other key cancels without issuing a request.
	gotURI = ""
	nm, _ = nm.Update(profiles.ProfilesLoadedMsg{List: []profile.Meta{{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1}}})
	nm, _ = nm.Update(keyPress("d"))
	nm, cmd = nm.Update(keyPress("n"))
	if cmd != nil {
		t.Fatal("cancelling should not issue a command")
	}
	if gotURI != "" {
		t.Errorf("unexpected request after cancel: %q", gotURI)
	}
	if nm.(Model).tabs[2].(*profiles.Model).ConfirmingDel() {
		t.Error("confirmDel should reset after cancel")
	}
}
