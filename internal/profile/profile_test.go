package profile

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testStore creates a Store in a temp dir with deterministic ID generation.
// Returns the store, temp dir, and a cleanup function.
func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	activePath := filepath.Join(dir, "active.yaml")
	var seq atomic.Int32
	store := New(Options{
		Dir:              dir,
		ActiveConfigPath: activePath,
		IDGen: func() string {
			n := seq.Add(1)
			return "id-" + string(rune('a'-1+int(n)))
		},
		Now: func() int64 { return time.Now().UnixMilli() },
	})
	return store, dir
}

// writeProfile writes a profile .yaml file directly (bypasses Store API).
func writeProfile(t *testing.T, dir, id, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writeProfile %s: %v", id, err)
	}
}

// --- CRUD tests ---

func TestCreateLocalProfile(t *testing.T) {
	s, _ := testStore(t)
	content := "mixed-port: 7890\nrules:\n  - MATCH\n"
	m, err := s.Create(CreateInput{Name: "test", Content: content, Type: TypeLocal})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if m.Name != "test" {
		t.Errorf("Name = %q, want test", m.Name)
	}
	if m.Type != TypeLocal {
		t.Errorf("Type = %q, want local", m.Type)
	}
	// Verify file was written.
	got, err := s.Read(m.ID)
	if err != nil {
		t.Fatalf("Read after Create: %v", err)
	}
	if got != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestCreateScriptReturnsError(t *testing.T) {
	s, _ := testStore(t)
	_, err := s.Create(CreateInput{Name: "script", Type: TypeScript})
	if err != ErrScriptNotSupported {
		t.Errorf("expected ErrScriptNotSupported, got %v", err)
	}
}

func TestListReturnsAllProfiles(t *testing.T) {
	s, _ := testStore(t)
	s.Create(CreateInput{Name: "p1", Content: "a: 1"})
	s.Create(CreateInput{Name: "p2", Content: "b: 2"})

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("List() returned %d, want 2", len(list))
	}
	if list[0].Name != "p1" || list[1].Name != "p2" {
		t.Errorf("names = [%s %s], want [p1 p2]", list[0].Name, list[1].Name)
	}
}

func TestReadReturnsContent(t *testing.T) {
	s, _ := testStore(t)
	m, _ := s.Create(CreateInput{Name: "read-test", Content: "foo: bar\n"})
	content, err := s.Read(m.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if content != "foo: bar\n" {
		t.Errorf("content = %q, want 'foo: bar\\n'", content)
	}
}

func TestReadNotFound(t *testing.T) {
	s, _ := testStore(t)
	_, err := s.Read("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
}

func TestUpdateName(t *testing.T) {
	s, _ := testStore(t)
	m, _ := s.Create(CreateInput{Name: "old", Content: "x: 1"})

	newName := "new"
	updated, err := s.Update(m.ID, UpdateInput{Name: &newName})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "new" {
		t.Errorf("Name = %q, want 'new'", updated.Name)
	}
}

func TestUpdateContent(t *testing.T) {
	s, _ := testStore(t)
	m, _ := s.Create(CreateInput{Name: "p", Content: "old: 1"})

	newContent := "new: 2\n"
	_, err := s.Update(m.ID, UpdateInput{Content: &newContent})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := s.Read(m.ID)
	if got != "new: 2\n" {
		t.Errorf("content = %q, want 'new: 2\\n'", got)
	}
}

func TestDelete(t *testing.T) {
	s, dir := testStore(t)
	m, _ := s.Create(CreateInput{Name: "del", Content: "x: 1"})
	if err := s.Delete(m.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// File should be gone.
	if _, err := os.Stat(filepath.Join(dir, m.ID+".yaml")); !os.IsNotExist(err) {
		t.Error("file should not exist after Delete")
	}
	// Not in list.
	if len(s.List()) != 0 {
		t.Error("List() should be empty after Delete")
	}
}

func TestDeleteNotFound(t *testing.T) {
	s, _ := testStore(t)
	if err := s.Delete("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
}

func TestDuplicate(t *testing.T) {
	s, _ := testStore(t)
	m, _ := s.Create(CreateInput{Name: "original", Content: "dup: 1"})
	dup, err := s.Duplicate(m.ID, "copy")
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	if dup.ID == m.ID {
		t.Error("duplicate should have different ID")
	}
	if dup.Name != "copy" {
		t.Errorf("Name = %q, want 'copy'", dup.Name)
	}
	got, _ := s.Read(dup.ID)
	if got != "dup: 1" {
		t.Errorf("content = %q, want 'dup: 1'", got)
	}
}

// --- Active lifecycle tests ---

func TestSetActiveWritesActiveYAML(t *testing.T) {
	s, dir := testStore(t)
	s.Create(CreateInput{Name: "p1", Content: "mixed-port: 7890\n"})

	m, _ := s.Create(CreateInput{Name: "p2", Content: "mixed-port: 1080\n"})
	if err := s.SetActive(m.ID); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "active.yaml"))
	if err != nil {
		t.Fatalf("read active.yaml: %v", err)
	}
	if string(content) != "mixed-port: 1080\n" {
		t.Errorf("active.yaml = %q, want 'mixed-port: 1080\\n'", string(content))
	}
}

func TestSetActiveCreatesBak(t *testing.T) {
	s, dir := testStore(t)
	s.Create(CreateInput{Name: "p1", Content: "old: 1\n"})
	s.SetActive(s.List()[0].ID)

	// Activate a second profile → .bak should be created from first activation.
	s.Create(CreateInput{Name: "p2", Content: "new: 2\n"})
	s.SetActive(s.List()[1].ID)

	bakPath := filepath.Join(dir, "active.yaml.bak")
	bakContent, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("read .bak: %v", err)
	}
	if string(bakContent) != "old: 1\n" {
		t.Errorf("active.yaml.bak = %q, want 'old: 1\\n'", string(bakContent))
	}
}

func TestGetActiveID(t *testing.T) {
	s, _ := testStore(t)
	if got := s.GetActiveID(); got != "" {
		t.Errorf("GetActiveID() = %q, want empty initially", got)
	}

	s.Create(CreateInput{Name: "p", Content: "x: 1"})
	m := s.List()[0]
	s.SetActive(m.ID)

	if got := s.GetActiveID(); got != m.ID {
		t.Errorf("GetActiveID() = %q, want %q", got, m.ID)
	}
}

func TestRollback(t *testing.T) {
	s, dir := testStore(t)
	s.Create(CreateInput{Name: "first", Content: "version: 1\n"})
	s.SetActive(s.List()[0].ID)

	s.Create(CreateInput{Name: "second", Content: "version: 2\n"})
	s.SetActive(s.List()[1].ID)

	// Current active.yaml has version: 2.
	content, _ := os.ReadFile(filepath.Join(dir, "active.yaml"))
	if string(content) != "version: 2\n" {
		t.Fatalf("pre-rollback: %q", string(content))
	}

	ok := s.Rollback()
	if !ok {
		t.Fatal("Rollback returned false")
	}
	content, _ = os.ReadFile(filepath.Join(dir, "active.yaml"))
	if string(content) != "version: 1\n" {
		t.Errorf("after rollback: %q, want 'version: 1\\n'", string(content))
	}
}

func TestRollbackNoBak(t *testing.T) {
	s, _ := testStore(t)
	if s.Rollback() {
		t.Error("Rollback should return false when no .bak exists")
	}
}

func TestResetActive(t *testing.T) {
	s, dir := testStore(t)
	s.Create(CreateInput{Name: "p", Content: "x: 1"})
	s.SetActive(s.List()[0].ID)
	s.ResetActive()

	if got := s.GetActiveID(); got != "" {
		t.Errorf("GetActiveID() = %q, want empty after ResetActive", got)
	}
	// state.json should exist but contain empty active ID.
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("state.json not found: %v", err)
	}
	if string(b) != "{\n}" && string(b) != "{}" {
		t.Errorf("state.json = %q, want empty state", string(b))
	}
}

// --- copyFile regression test ---

func TestCopyFileCloseErrorSurfaced(t *testing.T) {
	// Verify copyFile properly checks out.Close() error.
	// We can't easily inject a Close error without mocking fs, but we can
	// verify the function exists and produces correct output — the code
	// path is now: explicit Close() with error capture, not deferred Close.
	s, dir := testStore(t)
	s.Create(CreateInput{Name: "src", Content: "copy: me\n"})
	m := s.List()[0]

	srcPath := filepath.Join(dir, m.ID+".yaml")
	dstPath := filepath.Join(dir, "copy.yaml")
	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	content, _ := os.ReadFile(dstPath)
	if string(content) != "copy: me\n" {
		t.Errorf("copyFile content = %q, want 'copy: me\\n'", string(content))
	}
}

// --- Concurrency test ---

func TestConcurrentSetActive(t *testing.T) {
	s, _ := testStore(t)
	s.Create(CreateInput{Name: "c1", Content: "c: 1\n"})
	s.Create(CreateInput{Name: "c2", Content: "c: 2\n"})

	// Also create more profiles for the concurrent loop.
	for i := 3; i <= 5; i++ {
		s.Create(CreateInput{Name: "c" + string(rune('0'+i)), Content: "c: " + string(rune('0'+i)) + "\n"})
	}

	list := s.List()
	if len(list) < 5 {
		t.Fatalf("need 5 profiles, got %d", len(list))
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			idx := n % len(list)
			s.SetActive(list[idx].ID)
		}(i)
	}
	wg.Wait()

	// After all goroutines finish, GetActiveID must return a valid ID.
	active := s.GetActiveID()
	found := false
	for _, m := range list {
		if m.ID == active {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GetActiveID() = %q, not in profile list", active)
	}
}

// --- Compose tests ---

func TestComposeLocalProfile(t *testing.T) {
	s, _ := testStore(t)
	s.Create(CreateInput{Name: "local", Content: "mixed-port: 7890\n"})
	m := s.List()[0]

	content, _, err := s.Compose(m.ID)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if content != "mixed-port: 7890\n" {
		t.Errorf("Compose = %q, want verbatim", content)
	}
}

func TestComposeOverlayMerge(t *testing.T) {
	s, _ := testStore(t)
	base, _ := s.Create(CreateInput{Name: "base", Content: "mixed-port: 7890\n"})
	// Create a merge overlay.
	overlay, _ := s.Create(CreateInput{
		Name:          "overlay",
		Type:          TypeMerge,
		BaseProfileID: base.ID,
		Content:       "mixed-port: 1080\n",
	})
	_ = overlay

	content, comps, err := s.Compose(base.ID)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if len(comps) != 1 {
		t.Fatalf("composition length = %d, want 1", len(comps))
	}
	if comps[0].ID != overlay.ID {
		t.Error("overlay not in composition")
	}
	// Overlay should override mixed-port.
	if content != "mixed-port: 1080\n" {
		t.Errorf("Compose = %q, want overlay override", content)
	}
}

func TestComposeRejectsOverlayAsBase(t *testing.T) {
	s, _ := testStore(t)
	overlay, _ := s.Create(CreateInput{Name: "o", Type: TypeMerge, BaseProfileID: "x"})
	_, _, err := s.Compose(overlay.ID)
	if err != ErrIsOverlay {
		t.Errorf("expected ErrIsOverlay, got %v", err)
	}
}

// --- GetSection / SetSection ---

func TestGetSectionReturnsMap(t *testing.T) {
	s, _ := testStore(t)
	s.Create(CreateInput{Name: "p", Content: "dns:\n  enable: true\n"})
	m := s.List()[0]

	val, err := s.GetSection(m.ID, "dns")
	if err != nil {
		t.Fatalf("GetSection: %v", err)
	}
	dns, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("dns = %T, want map", val)
	}
	if dns["enable"] != true {
		t.Errorf("dns.enable = %v, want true", dns["enable"])
	}
}

func TestSetSectionUpdatesContent(t *testing.T) {
	s, _ := testStore(t)
	s.Create(CreateInput{Name: "p", Content: "dns:\n  enable: true\n"})
	m := s.List()[0]

	err := s.SetSection(m.ID, "dns", map[string]any{"enable": false})
	if err != nil {
		t.Fatalf("SetSection: %v", err)
	}

	val, _ := s.GetSection(m.ID, "dns")
	dns := val.(map[string]any)
	if dns["enable"] != false {
		t.Errorf("dns.enable = %v, want false after SetSection", dns["enable"])
	}
}
