// Package profile implements the on-disk ProfileStore: an index.json registry
// of profile metadata, one <id>.yaml file per profile, a state.json tracking
// the active profile, plus subscription import/refresh, compose (base + enabled
// merge overlays), and active-config lifecycle (setActive / rollback / reset).
//
// Direct port of packages/agent/src/profiles.ts with two simplifications
// per GO_SERVER_PLAN.md §2 砍项:
//   - script profiles: Create returns ErrScriptNotSupported; Compose silently
//     skips script overlays (the TS path that requires a scriptRunner is gone).
//   - visual-editor overlays: composed as ordinary merge overlays; the
//     x-metacubexd-visual-patch key is stripped by internal/merge.
package profile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"metacubexd-server-go/internal/api"
	"metacubexd-server-go/internal/merge"
)

// Profile types. Strings match the TS ProfileType union so the dashboard's
// type-narrowed UI works without translation.
const (
	TypeLocal   = "local"
	TypeRemote  = "remote"
	TypeMerge   = "merge"
	TypeScript  = "script"
)

// ManagedBy values. Only VisualEditor exists today.
const ManagedByVisualEditor = "visual-editor"

// Editor statuses, surfaced by refresh() when a compose conflict happens.
const (
	EditorClean      = "clean"
	EditorConflicted = "conflicted"
)

// DefaultUA is the user-agent used for subscription fetches (matches the TS
// hard-coded value; some subscription providers gate features on it).
const DefaultUA = "clash.meta"

// defaultSubscriptionTimeout caps subscription fetch/refresh network I/O.
const defaultSubscriptionTimeout = 30 * time.Second



// CreateInput is the body of POST /profiles.
type CreateInput struct {
	Name          string
	Content       string
	Type          string // empty defaults to "local"
	BaseProfileID string
	ManagedBy     string
	EditorStatus  string
}

// UpdateInput is the body of PUT /profiles/:id. Fields are *T to distinguish
// "omit" from "set to zero value" — mirrors the TS `p?.field != null` checks.
type UpdateInput struct {
	Name           *string
	Content        *string
	Enabled        *bool
	UpdateInterval *int // minutes; 0 is meaningful (disables auto-update)
	EditorStatus   *string
}

// Sentinels returned by Store ops; the control layer maps them to HTTP status.
var (
	ErrNotFound            = errors.New("profile not found")
	ErrScriptNotSupported  = errors.New("script profiles not supported in this build")
	ErrIsOverlay           = errors.New("profile is an overlay and cannot be the active base")
	ErrNotRemote           = errors.New("profile is not a remote subscription")
	ErrMultipleOverlays    = errors.New("multiple visual editor overlays found for profile")
)

// SubscriptionFetchError preserves the upstream HTTP status so the control
// layer can return it verbatim (e.g. 401 from a credentialed URL) without
// echoing the URL itself into logs/responses (#2138).
type SubscriptionFetchError struct {
	UpstreamStatus int
}

func (e *SubscriptionFetchError) Error() string {
	return fmt.Sprintf("subscription provider returned HTTP %d", e.UpstreamStatus)
}

// Fetcher abstracts subscription HTTP GETs (test double hook).
type Fetcher interface {
	Fetch(ctx context.Context, url, userAgent string) (body string, subInfo *api.SubscriptionInfo, err error)
}

// Store is the concurrent-safe ProfileStore. All public methods are serialized
// by mu; internal *Impl methods assume the caller already holds it.
type Store struct {
	dir              string
	activeConfigPath string
	fetcher          Fetcher
	now              func() int64
	idGen            func() string
	mu               sync.Mutex
}

// Options configures a Store. Dir + ActiveConfigPath are required.
type Options struct {
	Dir              string
	ActiveConfigPath string
	Fetcher          Fetcher // nil → defaultHTTPFetcher
	// Now returns Unix-millis; tests inject a fake clock. Defaults to time.Now.
	Now   func() int64
	IDGen func() string // nil → uuid.NewString
}

// New constructs a Store. Does not touch disk; first call lazy-creates the dir.
func New(opts Options) *Store {
	if opts.Fetcher == nil {
		opts.Fetcher = &httpFetcher{timeout: defaultSubscriptionTimeout}
	}
	if opts.Now == nil {
		opts.Now = func() int64 { return time.Now().UnixMilli() }
	}
	if opts.IDGen == nil {
		opts.IDGen = uuid.NewString
	}
	return &Store{
		dir:              opts.Dir,
		activeConfigPath: opts.ActiveConfigPath,
		fetcher:          opts.Fetcher,
		now:              opts.Now,
		idGen:            opts.IDGen,
	}
}

// List returns all profile metas in index order.
func (s *Store) List() []api.Meta {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, _ := s.readIndexLocked()
	// Return a copy so callers cannot mutate the on-disk representation via the
	// returned slice header. Element copies are value types (api.Meta has only
	// value-type fields except pointers, which we clone shallowly — profiles
	// don't mutate them post-read).
	out := make([]api.Meta, len(idx))
	copy(out, idx)
	return out
}

// Read returns the raw YAML content of a profile.
func (s *Store) Read(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.findMetaLocked(id); err != nil {
		return "", err
	}
	b, err := os.ReadFile(s.profilePath(id))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Create writes a new profile and updates the index.
//
// type == "script" returns ErrScriptNotSupported (砍项 — the UI's "new script"
// button surfaces this as a 501 toast).
func (s *Store) Create(in CreateInput) (api.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return api.Meta{}, err
	}
	current, _ := s.readIndexLocked()

	t := in.Type
	if t == "" {
		t = TypeLocal
	}
	// Script砍法 (create path): refuse outright so users can't accumulate
	// unservable script profiles.
	if t == TypeScript {
		return api.Meta{}, ErrScriptNotSupported
	}
	// Validate visual-editor overlays the same way as the TS server so a
	// migrated UI can't get into an inconsistent state.
	if in.ManagedBy == ManagedByVisualEditor {
		if t != TypeMerge || in.BaseProfileID == "" {
			return api.Meta{}, errors.New("visual editor overlays must be scoped merge profiles")
		}
		for _, m := range current {
			if m.ManagedBy == ManagedByVisualEditor && m.BaseProfileID == in.BaseProfileID {
				return api.Meta{}, fmt.Errorf("visual editor overlay already exists for %s", in.BaseProfileID)
			}
		}
	}

	id := s.idGen()
	m := api.Meta{
		ID:            id,
		Name:          in.Name,
		Type:          t,
		BaseProfileID: in.BaseProfileID,
		ManagedBy:     in.ManagedBy,
		EditorStatus:  in.EditorStatus,
		UpdatedAt:     s.now(),
	}
	if err := s.atomicWrite(s.profilePath(id), in.Content); err != nil {
		return api.Meta{}, err
	}
	// Append to the index AFTER the file is durable; if writeIndex fails we
	// clean up the orphaned file so a retry doesn't accumulate garbage.
	if err := s.writeIndexLocked(append(current, m)); err != nil {
		_ = os.Remove(s.profilePath(id))
		return api.Meta{}, err
	}
	return m, nil
}

// Update patches a profile's meta and optionally rewrites its content.
func (s *Store) Update(id string, in UpdateInput) (api.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, _ := s.readIndexLocked()
	pos := -1
	for i, m := range idx {
		if m.ID == id {
			pos = i
			break
		}
	}
	if pos == -1 {
		return api.Meta{}, ErrNotFound
	}
	m := idx[pos]

	if in.Content != nil {
		if err := s.atomicWrite(s.profilePath(id), *in.Content); err != nil {
			return api.Meta{}, err
		}
	}
	if in.Name != nil {
		m.Name = *in.Name
	}
	if in.Enabled != nil {
		m.Enabled = in.Enabled
	}
	// 0 is meaningful (disables auto-update) — distinguish from nil (omit).
	if in.UpdateInterval != nil {
		m.UpdateInterval = in.UpdateInterval
	}
	if in.EditorStatus != nil {
		if *in.EditorStatus == "" {
			// empty string == clear (mirrors TS `editorStatus === null`)
			m.EditorStatus = ""
		} else {
			m.EditorStatus = *in.EditorStatus
		}
	}
	// Editor status is derived metadata; only bump updatedAt when a real field
	// changes, otherwise refreshing the schedule would be reset spuriously.
	if in.Name != nil || in.Content != nil || in.Enabled != nil || in.UpdateInterval != nil {
		m.UpdatedAt = s.now()
	}
	idx[pos] = m
	if err := s.writeIndexLocked(idx); err != nil {
		return api.Meta{}, err
	}
	return m, nil
}

// Delete removes a profile and any overlays scoped to it. If the deleted
// profile was active, activeId is cleared.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, _ := s.readIndexLocked()
	pos := -1
	for i, m := range idx {
		if m.ID == id {
			pos = i
			break
		}
	}
	if pos == -1 {
		return ErrNotFound
	}
	removed := idx[pos]

	// Cascade: also remove overlays scoped to this base. Matches the TS
	// behavior — deleting a base profile shouldn't orphan its managed overlays.
	doomed := map[string]struct{}{id: {}}
	for _, m := range idx {
		if m.BaseProfileID == id {
			doomed[m.ID] = struct{}{}
		}
	}
	for d := range doomed {
		_ = os.Remove(s.profilePath(d))
	}
	retained := idx[:0]
	for _, m := range idx {
		if _, ok := doomed[m.ID]; !ok {
			retained = append(retained, m)
		}
	}
	// retained aliases idx's backing array; copy out before mutation.
	clean := make([]api.Meta, len(retained))
	copy(clean, retained)

	// If we just deleted a visual-editor overlay, mark its base as clean —
	// there's no longer a pending edit to resolve.
	if removed.ManagedBy == ManagedByVisualEditor && removed.BaseProfileID != "" {
		for i := range clean {
			if clean[i].ID == removed.BaseProfileID {
				clean[i].EditorStatus = EditorClean
				break
			}
		}
	}
	if err := s.writeIndexLocked(clean); err != nil {
		return err
	}

	state, _ := s.readStateLocked()
	if state.ActiveID == id {
		state.ActiveID = ""
		_ = s.writeStateLocked(state)
	}
	return nil
}

// Duplicate copies a profile's content to a new local profile. The copy is
// always type=local (matches TS) — duplicating a remote subscription would
// suggest it auto-refreshes, but it has no URL.
func (s *Store) Duplicate(id, name string) (api.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	src, err := s.findMetaLocked(id)
	if err != nil {
		return api.Meta{}, err
	}
	content, err := os.ReadFile(s.profilePath(id))
	if err != nil {
		return api.Meta{}, err
	}
	n := name
	if n == "" {
		n = src.Name + " copy"
	}
	newID := s.idGen()
	m := api.Meta{
		ID:        newID,
		Name:      n,
		Type:      TypeLocal,
		UpdatedAt: s.now(),
	}
	if err := s.atomicWrite(s.profilePath(newID), string(content)); err != nil {
		return api.Meta{}, err
	}
	idx, _ := s.readIndexLocked()
	if err := s.writeIndexLocked(append(idx, m)); err != nil {
		_ = os.Remove(s.profilePath(newID))
		return api.Meta{}, err
	}
	return m, nil
}

// ImportFromURL fetches a remote subscription (UA: clash.meta) and stores it
// as a new remote profile, parsing Subscription-Userinfo if present.
func (s *Store) ImportFromURL(urlStr, name string) (api.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return api.Meta{}, err
	}
	body, subInfo, err := s.fetcher.Fetch(context.Background(), urlStr, DefaultUA)
	if err != nil {
		return api.Meta{}, err
	}

	n := name
	if n == "" {
		n = urlStr
	}
	id := s.idGen()
	m := api.Meta{
		ID:               id,
		Name:             n,
		Type:             TypeRemote,
		URL:              urlStr,
		UserAgent:        DefaultUA,
		UpdatedAt:        s.now(),
		SubscriptionInfo: subInfo,
	}
	if err := s.atomicWrite(s.profilePath(id), body); err != nil {
		return api.Meta{}, err
	}
	idx, _ := s.readIndexLocked()
	if err := s.writeIndexLocked(append(idx, m)); err != nil {
		_ = os.Remove(s.profilePath(id))
		return api.Meta{}, err
	}
	return m, nil
}

// Refresh re-fetches a remote subscription in place (same id). A successful
// refresh also re-composes to detect visual-editor conflicts; the meta is
// marked clean/conflicted accordingly.
func (s *Store) Refresh(id string) (api.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, _ := s.readIndexLocked()
	pos := -1
	for i, m := range idx {
		if m.ID == id {
			pos = i
			break
		}
	}
	if pos == -1 {
		return api.Meta{}, ErrNotFound
	}
	m := idx[pos]
	if m.Type != TypeRemote || m.URL == "" {
		return api.Meta{}, ErrNotRemote
	}
	ua := m.UserAgent
	if ua == "" {
		ua = DefaultUA
	}
	body, subInfo, err := s.fetcher.Fetch(context.Background(), m.URL, ua)
	if err != nil {
		return api.Meta{}, err
	}
	if err := s.atomicWrite(s.profilePath(id), body); err != nil {
		return api.Meta{}, err
	}
	m.UpdatedAt = s.now()
	if subInfo != nil {
		m.SubscriptionInfo = subInfo
	}
	// Re-compose to surface editor conflicts. The conflict error type doesn't
	// occur in this build (we strip visual patches), so the status is always
	// clean — but we keep the path for parity with future editor support.
	if _, _, err := s.composeLocked(id); err != nil {
		// Soft-fail: keep the refreshed content on disk, just mark conflicted
		// so activation can refuse until the user resolves it.
		m.EditorStatus = EditorConflicted
	} else {
		m.EditorStatus = EditorClean
	}
	idx[pos] = m
	if err := s.writeIndexLocked(idx); err != nil {
		return api.Meta{}, err
	}
	return m, nil
}

// GetActiveID returns the active profile id, or "" if none.
func (s *Store) GetActiveID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _ := s.readStateLocked()
	return st.ActiveID
}

// SetActive composes the profile into active.yaml and remembers it as active.
// Before overwriting, the current active.yaml is snapshotted to .bak so
// /kernel/rollback can restore it.
func (s *Store) SetActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, _, err := s.composeLocked(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.activeConfigPath), 0o755); err != nil {
		return err
	}
	if exists(s.activeConfigPath) {
		// Snapshot the prior active config so Phase 3's rollback route can
		// restore it on a validate failure (#2109).
		_ = copyFile(s.activeConfigPath, s.activeConfigPath+".bak")
	}
	if err := s.atomicWrite(s.activeConfigPath, content); err != nil {
		return err
	}
	return s.writeStateLocked(stateFile{ActiveID: id})
}

// Compose materializes a profile + its enabled overlays into a single YAML
// document. It does NOT touch active.yaml; callers that want persistence
// should use SetActive.
func (s *Store) Compose(id string) (string, []api.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.composeLocked(id)
}

// Rollback restores active.yaml from the .bak snapshot. Returns false if no
// backup exists (#2109 escape hatch).
func (s *Store) Rollback() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	bak := s.activeConfigPath + ".bak"
	if !exists(bak) {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(s.activeConfigPath), 0o755); err != nil {
		return false
	}
	return copyFile(bak, s.activeConfigPath) == nil
}

// ResetActive clears active.yaml to a minimal file (the supervisor's
// injectClashConfig then writes just its managed header) and drops activeId,
// so a bricked config can't keep failing the kernel on boot.
func (s *Store) ResetActive() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.activeConfigPath), 0o755); err != nil {
		return err
	}
	if exists(s.activeConfigPath) {
		// Preserve the broken config aside for post-mortem inspection.
		_ = copyFile(s.activeConfigPath, s.activeConfigPath+".bak")
	}
	if err := s.atomicWrite(s.activeConfigPath, ""); err != nil {
		return err
	}
	return s.writeStateLocked(stateFile{})
}

// GetSection returns one top-level key from a profile's parsed YAML, or nil if
// absent / the profile isn't a mapping.
func (s *Store) GetSection(id, key string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.findMetaLocked(id); err != nil {
		return nil, err
	}
	content, err := os.ReadFile(s.profilePath(id))
	if err != nil {
		return nil, err
	}
	parsed, err := parseTopMap(string(content))
	if err != nil || parsed == nil {
		return nil, nil
	}
	v, ok := parsed[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

// SetSection replaces one top-level key in a profile's YAML and persists it,
// preserving every other key. value == nil deletes the key.
func (s *Store) SetSection(id, key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.findMetaLocked(id); err != nil {
		return err
	}
	content, err := os.ReadFile(s.profilePath(id))
	if err != nil {
		return err
	}
	parsed, _ := parseTopMap(string(content))
	if parsed == nil {
		parsed = map[string]any{}
	}
	if value == nil {
		delete(parsed, key)
	} else {
		parsed[key] = value
	}
	out, err := yaml.Marshal(parsed)
	if err != nil {
		return err
	}
	if err := s.atomicWrite(s.profilePath(id), string(out)); err != nil {
		return err
	}
	// Bump updatedAt via Update's content path (without re-acquiring the lock).
	idx, _ := s.readIndexLocked()
	for i, m := range idx {
		if m.ID == id {
			m.UpdatedAt = s.now()
			idx[i] = m
			break
		}
	}
	return s.writeIndexLocked(idx)
}

// --- internals (assume s.mu is held) ---

func (s *Store) profilePath(id string) string { return filepath.Join(s.dir, id+".yaml") }

func (s *Store) indexPath() string  { return filepath.Join(s.dir, "index.json") }
func (s *Store) statePath() string  { return filepath.Join(s.dir, "state.json") }

func (s *Store) findMetaLocked(id string) (api.Meta, error) {
	idx, _ := s.readIndexLocked()
	for _, m := range idx {
		if m.ID == id {
			return m, nil
		}
	}
	return api.Meta{}, ErrNotFound
}

// readIndexLocked reads index.json. Missing file → empty list (first run).
func (s *Store) readIndexLocked() ([]api.Meta, error) {
	b, err := os.ReadFile(s.indexPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var idx []api.Meta
	if err := jsonUnmarshal(b, &idx); err != nil {
		return nil, err
	}
	return idx, nil
}

func (s *Store) writeIndexLocked(idx []api.Meta) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := jsonMarshalIndent(idx)
	if err != nil {
		return err
	}
	return s.atomicWrite(s.indexPath(), string(b))
}

func (s *Store) readStateLocked() (stateFile, error) {
	b, err := os.ReadFile(s.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return stateFile{}, nil
	}
	if err != nil {
		return stateFile{}, err
	}
	var st stateFile
	if err := jsonUnmarshal(b, &st); err != nil {
		return stateFile{}, err
	}
	return st, nil
}

func (s *Store) writeStateLocked(st stateFile) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := jsonMarshalIndent(st)
	if err != nil {
		return err
	}
	return s.atomicWrite(s.statePath(), string(b))
}

// composeLocked implements the compose pipeline:
//   - base must be local/remote (not merge/script)
//   - iterate index in order, applying enabled merge overlays (scoped or
//     global); script overlays are silently skipped (砍项)
//   - no overlays → emit base verbatim (preserve formatting byte-for-byte)
//   - else → deep-merge
func (s *Store) composeLocked(id string) (string, []api.Meta, error) {
	base, err := s.findMetaLocked(id)
	if err != nil {
		return "", nil, err
	}
	if base.Type == TypeMerge || base.Type == TypeScript {
		return "", nil, ErrIsOverlay
	}
	baseBytes, err := os.ReadFile(s.profilePath(id))
	if err != nil {
		return "", nil, err
	}
	baseContent := string(baseBytes)

	idx, _ := s.readIndexLocked()

	// Visual-editor overlays scoped to this base. Multiple is an error (the UI
	// should never let this happen, but defend against index corruption).
	managedCount := 0
	for _, m := range idx {
		if m.ManagedBy == ManagedByVisualEditor && m.BaseProfileID == id {
			managedCount++
		}
	}
	if managedCount > 1 {
		return "", nil, ErrMultipleOverlays
	}

	var overlays []string
	var composition []api.Meta
	for _, m := range idx {
		// undefined enabled == on (overlay applies). Only explicit `false`
		// disables it.
		if m.Enabled != nil && !*m.Enabled {
			continue
		}
		if m.Type == TypeMerge {
			// Scoped overlay: only applies to its owning base. Legacy overlays
			// (no baseProfileId) stay global.
			if m.BaseProfileID != "" && m.BaseProfileID != id {
				continue
			}
			b, err := os.ReadFile(s.profilePath(m.ID))
			if err != nil {
				return "", nil, err
			}
			overlays = append(overlays, string(b))
			composition = append(composition, m)
		}
		// Script overlays are silently skipped (no scriptRunner in this build).
	}

	// No overlays → write the base verbatim. This preserves user formatting
	// (comments, key order) byte-for-byte when no overlays apply, matching the
	// TS behavior that avoids a stringify round-trip.
	if len(overlays) == 0 {
		return baseContent, composition, nil
	}

	merged, err := merge.MergeConfigs(baseContent, overlays)
	if err != nil {
		return "", nil, err
	}
	return merged, composition, nil
}

// atomicWrite writes content to path via a temp file + rename so a crash mid-
// write can't leave a half-written file (index.json corruption would lose the
// whole profile registry). 0644: world-readable, group/other can't write.
func (s *Store) atomicWrite(path, content string) error {
	tmp := path + "." + randomToken(8) + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	// Rename is atomic on POSIX when src and dst are on the same filesystem
	// (they always are here — both live in s.dir or its parent).
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// stateFile is the on-disk shape of state.json.
type stateFile struct {
	ActiveID string `json:"activeId,omitempty"`
}

// --- helpers ---

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	// Explicit Close after Copy: on network filesystems write failures surface
	// at close time, and a deferred Close would silently swallow them.
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// parseTopMap parses a YAML doc and returns the top-level mapping (or nil if
// empty / not a mapping). Errors are swallowed — callers treat both empty
// content and malformed YAML as "no top-level mapping".
func parseTopMap(content string) (map[string]any, error) {
	if strings.TrimSpace(content) == "" {
		return map[string]any{}, nil
	}
	var v any
	if err := yaml.Unmarshal([]byte(content), &v); err != nil {
		return nil, err
	}
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	return nil, nil
}

// --- HTTP fetcher ---

// httpFetcher is the default Fetcher: GET with UA + timeout, parse
// Subscription-Userinfo header.
type httpFetcher struct {
	timeout time.Duration
	client  *http.Client
}

func (f *httpFetcher) Fetch(ctx context.Context, urlStr, userAgent string) (string, *api.SubscriptionInfo, error) {
	if f.client == nil {
		f.client = &http.Client{Timeout: f.timeout}
	}
	reqCtx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", nil, err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		if errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			return "", nil, fmt.Errorf("subscription fetch timed out after %s for %s", f.timeout, urlStr)
		}
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, &SubscriptionFetchError{UpstreamStatus: resp.StatusCode}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	return string(body), parseSubscriptionUserinfo(resp.Header.Get("Subscription-Userinfo")), nil
}

// parseSubscriptionUserinfo parses the eponymous header, which is a
// semicolon-separated key=value list: `upload=...; download=...; total=...;
// expire=...`. Missing keys default to 0; a header with none of the known keys
// returns nil (so the meta omits the field).
func parseSubscriptionUserinfo(header string) *api.SubscriptionInfo {
	if header == "" {
		return nil
	}
	var info api.SubscriptionInfo
	seen := false
	for _, part := range strings.Split(header, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "upload":
			info.Upload = n
			seen = true
		case "download":
			info.Download = n
			seen = true
		case "total":
			info.Total = n
			seen = true
		case "expire":
			info.Expire = n
			seen = true
		}
	}
	if !seen {
		return nil
	}
	return &info
}
