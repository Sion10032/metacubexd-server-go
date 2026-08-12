package ctl

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProfilesList decodes the profile list from a fake server.
func TestProfilesList(t *testing.T) {
	const body = `[{"id":"a","name":"base","type":"local","updatedAt":1723456789,"url":"https://example.com/sub"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.RequestURI != "/api/control/profiles" {
			t.Errorf("unexpected request: %s %s", r.Method, r.RequestURI)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", false)
	list, err := c.ProfilesList()
	if err != nil {
		t.Fatalf("ProfilesList: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	m := list[0]
	if m.ID != "a" || m.Name != "base" || m.Type != "local" || m.URL != "https://example.com/sub" {
		t.Errorf("meta = %+v, want decoded fields", m)
	}
}

// TestProfileOps covers the profile POST/DELETE operations: success decodes
// the response, failure surfaces the server {"error"} message.
func TestProfileOps(t *testing.T) {
	ops := []struct {
		name string
		call func(*Client) error
		meth string
		path string
		resp string
	}{
		{
			"activate",
			func(c *Client) error { _, err := c.ProfileActivate("p1"); return err },
			http.MethodPost, "/api/control/profiles/p1/activate",
			`{"status":"starting"}`,
		},
		{
			"refresh",
			func(c *Client) error { _, err := c.ProfileRefresh("p1"); return err },
			http.MethodPost, "/api/control/profiles/p1/refresh",
			`{"id":"p1","name":"n","type":"remote","updatedAt":1}`,
		},
		{
			"refresh-and-activate",
			func(c *Client) error { _, err := c.ProfileRefreshAndActivate("p1"); return err },
			http.MethodPost, "/api/control/profiles/p1/refresh-and-activate",
			`{"meta":{"id":"p1","name":"n","type":"remote","updatedAt":1}}`,
		},
		{
			"delete",
			func(c *Client) error { return c.ProfileDelete("p1") },
			http.MethodDelete, "/api/control/profiles/p1",
			"", // 204 No Content
		},
	}

	for _, op := range ops {
		t.Run(op.name+" ok", func(t *testing.T) {
			var gotURI string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotURI = r.Method + " " + r.RequestURI
				if op.resp == "" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, op.resp)
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "", false)
			if err := op.call(c); err != nil {
				t.Fatalf("%s: %v", op.name, err)
			}
			if want := op.meth + " " + op.path; gotURI != want {
				t.Errorf("request = %q, want %q", gotURI, want)
			}
		})

		t.Run(op.name+" error", func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"profile exploded"}`)
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "", false)
			if err := op.call(c); err == nil {
				t.Errorf("%s: want error, got nil", op.name)
			} else if !strings.Contains(err.Error(), "profile exploded") {
				t.Errorf("%s: error = %q, want server message", op.name, err)
			}
		})
	}
}

// TestProfileImport verifies the import request carries the URL/name body and
// decodes the created profile.
func TestProfileImport(t *testing.T) {
	var gotURI, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.Method + " " + r.RequestURI
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"new","name":"sub","type":"remote","updatedAt":1}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", false)
	m, err := c.ProfileImport("https://example.com/sub", "sub")
	if err != nil {
		t.Fatalf("ProfileImport: %v", err)
	}
	if m.ID != "new" || m.Name != "sub" {
		t.Errorf("meta = %+v, want imported profile", m)
	}
	if want := "POST /api/control/profiles/import"; gotURI != want {
		t.Errorf("request = %q, want %q", gotURI, want)
	}
	if !strings.Contains(gotBody, "https://example.com/sub") || !strings.Contains(gotBody, "\"name\":\"sub\"") {
		t.Errorf("body = %q, want url and name", gotBody)
	}
}
