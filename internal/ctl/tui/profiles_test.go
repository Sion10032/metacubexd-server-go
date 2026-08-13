package tui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"metacubexd-server-go/internal/ctl"
	"metacubexd-server-go/internal/profile"
)

// TestProfilesSetRows verifies SetProfiles builds rows and marks the active
// profile, and SelectedID follows the cursor.
func TestProfilesSetRows(t *testing.T) {
	p := NewProfilesModel()
	list := []profile.Meta{
		{ID: "a", Name: "base", Type: "local", UpdatedAt: 1723456789000},
		{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1723456789000},
	}
	p.SetProfiles(list, "b")
	got := ansiRe.ReplaceAllString(p.View(), "")
	if !strings.Contains(got, "base") || !strings.Contains(got, "sub") {
		t.Errorf("table view missing profile names:\n%s", got)
	}
	if !strings.Contains(got, "●") {
		t.Errorf("table view missing active marker:\n%s", got)
	}
	if id := p.SelectedID(); id != "a" {
		t.Errorf("SelectedID = %q, want a (first row)", id)
	}

	// Empty list renders empty and SelectedID is empty.
	p.SetProfiles(nil, "")
	if id := p.SelectedID(); id != "" {
		t.Errorf("SelectedID on empty list = %q, want empty", id)
	}
}

// TestProfilesTabLoaded verifies loading profiles renders the table and the
// status bar's second line shows the active profile summary.
func TestProfilesTabLoaded(t *testing.T) {
	m := New(ctl.NewClient("http://127.0.0.1:1", "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(profilesLoadedMsg{list: []profile.Meta{
		{ID: "a", Name: "base", Type: "local", UpdatedAt: 1723456789000},
		{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1723456789000},
	}})
	mdl := nm.(Model)
	mdl.profActive = "b"
	nm = mdl

	if got := nm.View(); !strings.Contains(got, "active: sub (remote)") {
		t.Errorf("View missing active profile summary:\n%s", got)
	}

	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	got := ansiRe.ReplaceAllString(nm.View(), "")
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
	nm, _ := m.Update(profilesLoadedMsg{list: []profile.Meta{{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1}}})
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")}) // Profiles tab

	nm, cmd := nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd == nil {
		t.Fatal("a returned no command")
	}
	if nm.(Model).profActive != "b" {
		t.Errorf("profActive = %q, want b", nm.(Model).profActive)
	}
	if _, ok := cmd().(profileOpMsg); !ok {
		t.Fatalf("cmd returned %T, want profileOpMsg", cmd())
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
	nm, _ := m.Update(profilesLoadedMsg{list: []profile.Meta{{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1}}})
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})

	nm, cmd := nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
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
	nm, _ := m.Update(profilesLoadedMsg{list: []profile.Meta{{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1}}})
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})

	nm, cmd := nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if cmd != nil {
		t.Fatal("d should enter confirm state, not issue a command")
	}
	if !nm.(Model).confirmDel {
		t.Fatal("confirmDel should be true after d")
	}
	if got := nm.View(); !strings.Contains(got, "删除所选 profile") {
		t.Errorf("View missing delete prompt:\n%s", got)
	}

	nm, cmd = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("y returned no command")
	}
	cmd()
	if want := "DELETE /api/control/profiles/b"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}

	// Any other key cancels without issuing a request.
	gotURI = ""
	nm, _ = nm.Update(profilesLoadedMsg{list: []profile.Meta{{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1}}})
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	nm, cmd = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd != nil {
		t.Fatal("cancelling should not issue a command")
	}
	if gotURI != "" {
		t.Errorf("unexpected request after cancel: %q", gotURI)
	}
	if nm.(Model).confirmDel {
		t.Error("confirmDel should reset after cancel")
	}
}

// TestProfileImportInput verifies i opens the URL+name popup, tab switches to
// the name field, and enter imports both.
func TestProfileImportInput(t *testing.T) {
	var gotURI, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"new","name":"my-sub","type":"remote","updatedAt":1}`)
	}))
	defer srv.Close()

	m := New(ctl.NewClient(srv.URL, "", false))
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")}) // Subscriptions tab
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if !nm.(Model).importing {
		t.Fatal("importing should be true after i")
	}
	if nm.(Model).form.focus != 0 {
		t.Fatalf("initial focus = %d, want 0 (URL)", nm.(Model).form.focus)
	}
	if got := ansiRe.ReplaceAllString(nm.View(), ""); !strings.Contains(got, "Import subscription") {
		t.Errorf("View missing import popup:\n%s", got)
	}
	formView := ansiRe.ReplaceAllString(nm.(Model).importFormView(60, 10), "")
	for _, border := range []string{"┌", "├", "└"} {
		if !strings.Contains(formView, border) {
			t.Errorf("import popup missing %q border:\n%s", border, formView)
		}
	}

	for _, r := range "https://example.com/sub" {
		nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(string(r))})
	}
	if got := nm.(Model).form.url.Value(); got != "https://example.com/sub" {
		t.Errorf("url = %q, want the typed URL", got)
	}

	// tab moves focus to the name field.
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyTab})
	if nm.(Model).form.focus != 1 {
		t.Fatalf("focus after tab = %d, want 1 (Name)", nm.(Model).form.focus)
	}
	for _, r := range "my-sub" {
		nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(string(r))})
	}
	if got := nm.(Model).form.name.Value(); got != "my-sub" {
		t.Errorf("name = %q, want the typed name", got)
	}

	nm, cmd := nm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	cmd()
	if want := "POST /api/control/profiles/import"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	if !strings.Contains(gotBody, "https://example.com/sub") {
		t.Errorf("body = %q, want the URL", gotBody)
	}
	if !strings.Contains(gotBody, "\"name\":\"my-sub\"") {
		t.Errorf("body = %q, want the name", gotBody)
	}
}
