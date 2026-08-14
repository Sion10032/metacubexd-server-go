package profiles

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"metacubexd-server-go/internal/tui/client"
	"metacubexd-server-go/internal/tui/shared"
	"metacubexd-server-go/internal/api"
)

// keyPress builds a tea.KeyPressMsg for a printable character, mirroring a
// user typing it. Text is set so msg.String() returns the character itself.
func keyPress(s string) tea.KeyPressMsg {
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

// TestProfilesSetRows verifies SetProfiles builds rows and marks the active
// profile, and SelectedID follows the cursor.
func TestProfilesSetRows(t *testing.T) {
	p := New(client.NewClient("http://127.0.0.1:1", "", false))
	p.SetSize(80, 10)
	list := []api.Meta{
		{ID: "a", Name: "base", Type: "local", UpdatedAt: 1723456789000},
		{ID: "b", Name: "sub", Type: "remote", UpdatedAt: 1723456789000},
	}
	p.SetProfiles(list, "b")
	got := shared.ANSIRe.ReplaceAllString(p.View(), "")
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

	p := New(client.NewClient(srv.URL, "", false))
	p.SetSize(60, 10)
	p.Update(keyPress("i"))
	if !p.Importing() {
		t.Fatal("importing should be true after i")
	}
	if p.form.Focus != 0 {
		t.Fatalf("initial focus = %d, want 0 (URL)", p.form.Focus)
	}
	if got := shared.ANSIRe.ReplaceAllString(p.View(), ""); !strings.Contains(got, "Import subscription") {
		t.Errorf("View missing import popup:\n%s", got)
	}
	formView := shared.ANSIRe.ReplaceAllString(p.importFormView(60, 10), "")
	for _, border := range []string{"┌", "├", "└"} {
		if !strings.Contains(formView, border) {
			t.Errorf("import popup missing %q border:\n%s", border, formView)
		}
	}

	for _, r := range "https://example.com/sub" {
		p.Update(keyPress(string(r)))
	}
	if got := p.form.Fields[0].Value(); got != "https://example.com/sub" {
		t.Errorf("url = %q, want the typed URL", got)
	}

	// tab moves focus to the name field.
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.form.Focus != 1 {
		t.Fatalf("focus after tab = %d, want 1 (Name)", p.form.Focus)
	}
	for _, r := range "my-sub" {
		p.Update(keyPress(string(r)))
	}
	if got := p.form.Fields[1].Value(); got != "my-sub" {
		t.Errorf("name = %q, want the typed name", got)
	}

	_, cmd, _ := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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

// TestProfileHelpPriority verifies Help reflects the current state with the
// same priority as the original footer: import form, then delete confirm,
// then the default operations.
func TestProfileHelpPriority(t *testing.T) {
	p := New(client.NewClient("http://127.0.0.1:1", "", false))
	if got := p.Help(); got != "1-3:tabs  a:activate  u:refresh  d:delete  i:import  q:quit" {
		t.Errorf("default Help = %q", got)
	}

	p.Update(keyPress("i"))
	if got := p.Help(); got != "import: tab:switch  enter:import  esc:cancel" {
		t.Errorf("import Help = %q", got)
	}

	p.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	p.SetProfiles([]api.Meta{{ID: "b", Name: "sub", Type: "remote"}}, "")
	p.Update(keyPress("d"))
	if got := p.Help(); got != "⚠ 删除所选 profile? (y 确认 / 其他取消)" {
		t.Errorf("confirm Help = %q", got)
	}
}
