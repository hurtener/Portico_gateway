// Package authored is the in-Portico Skill Pack source: operators
// compose Skill Packs from the Console (manifest, SKILL.md, prompts,
// optional UI) and the published bytes land in SQLite. The package
// exports both:
//
//   - a source.Source implementation (List/Open/ReadFile/Watch) that the
//     loader consumes alongside Git/HTTP/LocalDir,
//   - a Repo CRUD surface used by the REST handlers,
//
// behind a single Store value. Self-registers under driver name
// "authored" via init() so the per-tenant Registry can construct it.
package authored

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	"github.com/hurtener/Portico_gateway/internal/skills/source"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// DriverName is the value the tenant_skill_sources.driver column carries
// for the authored driver.
const DriverName = "authored"

// SourceName is the operator-visible name used for the in-Portico
// authored source. Authored skills always come from this single source
// per tenant; operators don't (and shouldn't) instantiate multiple
// authored sources.
const SourceName = "authored"

// File is the Console-facing shape — symmetric with ifaces.AuthoredFileRecord
// but uses public field names so the REST surface composes cleanly.
type File struct {
	RelPath  string
	MIMEType string
	Body     []byte
}

// Authored is the public DTO returned by the REST handlers.
type Authored struct {
	SkillID      string
	Version      string
	Status       string
	Manifest     manifest.Manifest
	ManifestRaw  []byte
	Files        []File
	Checksum     string
	AuthorUserID string
	CreatedAt    time.Time
	PublishedAt  *time.Time
}

// Store is the SQLite-backed CRUD surface + source.Source instance.
// A single Store is shared across REST handlers and the per-tenant
// authored Source — Watch fanout uses an in-process notifier so
// publish/archive events are visible to the loader without fsnotify.
type Store struct {
	repo ifaces.AuthoredSkillStore
	log  *slog.Logger

	mu          sync.RWMutex
	subscribers map[string]map[chan source.Event]struct{} // tenantID -> set of subscribers
}

// NewStore constructs a Store wrapping the SQLite repo.
func NewStore(repo ifaces.AuthoredSkillStore, log *slog.Logger) *Store {
	if log == nil {
		log = slog.Default()
	}
	return &Store{
		repo:        repo,
		log:         log.With("component", "skills.authored"),
		subscribers: make(map[string]map[chan source.Event]struct{}),
	}
}

func init() {
	source.Register(DriverName, factory)
}

// factory is the source.Factory entry point. The authored driver
// requires a Store to be supplied via FactoryDeps.AuthoredRepo (the
// Registry threads it through). configJSON carries no driver-specific
// fields in V1.
func factory(ctx context.Context, configJSON []byte, deps source.FactoryDeps) (source.Source, error) {
	if deps.AuthoredRepo == nil {
		return nil, errors.New("authored: AuthoredRepo dependency is nil; pass via FactoryDeps")
	}
	repoStore, ok := deps.AuthoredRepo.(*Store)
	if !ok {
		return nil, errors.New("authored: AuthoredRepo is not an *authored.Store")
	}
	return &tenantSource{store: repoStore, tenantID: deps.TenantID}, nil
}

// tenantSource is the per-tenant view onto the authored Store.
type tenantSource struct {
	store    *Store
	tenantID string
}

// Name implements source.Source.
func (s *tenantSource) Name() string { return SourceName }

// List enumerates the published authored skills for the tenant.
func (s *tenantSource) List(ctx context.Context) ([]source.Ref, error) {
	rows, err := s.store.repo.ListPublished(ctx, s.tenantID)
	if err != nil {
		return nil, fmt.Errorf("authored: list: %w", err)
	}
	out := make([]source.Ref, 0, len(rows))
	for _, r := range rows {
		out = append(out, source.Ref{
			ID:      r.SkillID,
			Version: r.Version,
			Source:  SourceName,
			Loc:     r.SkillID + "@" + r.Version,
		})
	}
	return out, nil
}

// Open returns the parsed manifest. The manifest is stored canonical
// JSON in SQLite — Parse handles both YAML and JSON.
func (s *tenantSource) Open(ctx context.Context, ref source.Ref) (manifest.Manifest, error) {
	rec, _, err := s.store.repo.Get(ctx, s.tenantID, ref.ID, ref.Version)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("authored: get %s@%s: %w", ref.ID, ref.Version, err)
	}
	m, _, err := manifest.Parse(rec.ManifestJSON)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("authored: parse manifest: %w", err)
	}
	return *m, nil
}

// ReadFile returns the bytes of relpath. The special path
// "manifest.yaml" is materialized from the stored canonical manifest
// JSON (the loader asks for manifest.yaml; we hand back YAML-friendly
// JSON which the parser accepts).
func (s *tenantSource) ReadFile(ctx context.Context, ref source.Ref, relpath string) (io.ReadCloser, source.ContentInfo, error) {
	relpath = strings.TrimPrefix(relpath, "./")
	rec, files, err := s.store.repo.Get(ctx, s.tenantID, ref.ID, ref.Version)
	if err != nil {
		return nil, source.ContentInfo{}, fmt.Errorf("authored: get: %w", err)
	}
	if relpath == "manifest.yaml" || relpath == "manifest.json" {
		return io.NopCloser(bytes.NewReader(rec.ManifestJSON)),
			source.ContentInfo{
				MIMEType: "application/yaml",
				Size:     int64(len(rec.ManifestJSON)),
				ModTime:  rec.CreatedAt,
			}, nil
	}
	for _, f := range files {
		if f.RelPath == relpath {
			ct := f.MIMEType
			if ct == "" {
				ct = "application/octet-stream"
			}
			return io.NopCloser(bytes.NewReader(f.Contents)),
				source.ContentInfo{MIMEType: ct, Size: int64(len(f.Contents)), ModTime: rec.CreatedAt}, nil
		}
	}
	return nil, source.ContentInfo{}, fmt.Errorf("authored: file %q not found in %s@%s", relpath, ref.ID, ref.Version)
}

// Watch fans out the per-tenant publish/archive events.
func (s *tenantSource) Watch(ctx context.Context) (<-chan source.Event, error) {
	return s.store.subscribe(ctx, s.tenantID)
}

// --- Subscription fanout --------------------------------------------

func (s *Store) subscribe(ctx context.Context, tenantID string) (<-chan source.Event, error) {
	ch := make(chan source.Event, 16)
	s.mu.Lock()
	if _, ok := s.subscribers[tenantID]; !ok {
		s.subscribers[tenantID] = make(map[chan source.Event]struct{})
	}
	s.subscribers[tenantID][ch] = struct{}{}
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.subscribers[tenantID], ch)
		if len(s.subscribers[tenantID]) == 0 {
			delete(s.subscribers, tenantID)
		}
		s.mu.Unlock()
		close(ch)
	}()
	return ch, nil
}

func (s *Store) emit(tenantID string, ev source.Event) {
	s.mu.RLock()
	subs := make([]chan source.Event, 0, len(s.subscribers[tenantID]))
	for ch := range s.subscribers[tenantID] {
		subs = append(subs, ch)
	}
	s.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// drop-oldest: drain one then push.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- ev:
			default:
			}
		}
	}
}

// --- Subscriber surface for source.AuthoredRepo ---------------------

// ListPublished implements source.AuthoredRepo.
func (s *Store) ListPublished(ctx context.Context, tenantID string) ([]source.AuthoredHandle, error) {
	rows, err := s.repo.ListPublished(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]source.AuthoredHandle, 0, len(rows))
	for _, r := range rows {
		out = append(out, source.AuthoredHandle{SkillID: r.SkillID, Version: r.Version})
	}
	return out, nil
}

// LoadManifest implements source.AuthoredRepo.
func (s *Store) LoadManifest(ctx context.Context, tenantID, skillID, version string) ([]byte, error) {
	rec, _, err := s.repo.Get(ctx, tenantID, skillID, version)
	if err != nil {
		return nil, err
	}
	return rec.ManifestJSON, nil
}

// LoadFile implements source.AuthoredRepo.
func (s *Store) LoadFile(ctx context.Context, tenantID, skillID, version, relpath string) ([]byte, string, error) {
	_, files, err := s.repo.Get(ctx, tenantID, skillID, version)
	if err != nil {
		return nil, "", err
	}
	for _, f := range files {
		if f.RelPath == relpath {
			return f.Contents, f.MIMEType, nil
		}
	}
	return nil, "", fmt.Errorf("authored: file %q not found", relpath)
}

// SubscribePublishes implements source.AuthoredRepo.
func (s *Store) SubscribePublishes(ctx context.Context, tenantID string) (<-chan source.Event, error) {
	return s.subscribe(ctx, tenantID)
}

// --- CRUD operations ------------------------------------------------

// ListAuthored returns every authored revision for a tenant.
func (s *Store) ListAuthored(ctx context.Context, tenantID string) ([]Authored, error) {
	rows, err := s.repo.ListAuthored(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]Authored, 0, len(rows))
	for _, r := range rows {
		out = append(out, recordToDTO(r, nil))
	}
	return out, nil
}

// History returns every revision (draft + published + archived) for
// a single skill_id.
func (s *Store) History(ctx context.Context, tenantID, skillID string) ([]Authored, error) {
	rows, err := s.repo.History(ctx, tenantID, skillID)
	if err != nil {
		return nil, err
	}
	out := make([]Authored, 0, len(rows))
	for _, r := range rows {
		out = append(out, recordToDTO(r, nil))
	}
	return out, nil
}

// GetAuthored fetches one revision (manifest + files).
func (s *Store) GetAuthored(ctx context.Context, tenantID, skillID, version string) (*Authored, error) {
	rec, files, err := s.repo.Get(ctx, tenantID, skillID, version)
	if err != nil {
		return nil, err
	}
	out := recordToDTO(rec, files)
	return &out, nil
}

// GetActive returns the currently active (published) version for a
// skill, or nil when no version is published.
func (s *Store) GetActive(ctx context.Context, tenantID, skillID string) (*Authored, error) {
	v, err := s.repo.ActiveVersion(ctx, tenantID, skillID)
	if err != nil {
		return nil, err
	}
	return s.GetAuthored(ctx, tenantID, skillID, v)
}

// CreateDraft persists a new draft revision. The manifest is encoded
// canonically; the checksum covers the manifest + every file.
func (s *Store) CreateDraft(ctx context.Context, tenantID, userID string, m manifest.Manifest, files []File) (*Authored, error) {
	if tenantID == "" {
		return nil, errors.New("authored: tenant_id required")
	}
	if m.ID == "" || m.Version == "" {
		return nil, errors.New("authored: manifest.id and manifest.version required")
	}
	canonicalManifest, err := canonicalManifest(m)
	if err != nil {
		return nil, err
	}
	checksum := computeChecksum(canonicalManifest, files)
	rec := &ifaces.AuthoredSkillRecord{
		TenantID:     tenantID,
		SkillID:      m.ID,
		Version:      m.Version,
		Status:       "draft",
		ManifestJSON: canonicalManifest,
		Checksum:     checksum,
		AuthorUserID: userID,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.repo.CreateDraft(ctx, rec, filesToRecords(files)); err != nil {
		return nil, err
	}
	out := recordToDTO(rec, filesToRecords(files))
	return &out, nil
}

// UpdateDraft replaces the contents of an existing draft revision.
func (s *Store) UpdateDraft(ctx context.Context, tenantID, skillID, version, userID string, m manifest.Manifest, files []File) (*Authored, error) {
	if tenantID == "" || skillID == "" || version == "" {
		return nil, errors.New("authored: tenant_id, skill_id, version required")
	}
	if m.ID != skillID || m.Version != version {
		return nil, fmt.Errorf("authored: manifest id/version (%s@%s) must match path (%s@%s)", m.ID, m.Version, skillID, version)
	}
	canonicalManifest, err := canonicalManifest(m)
	if err != nil {
		return nil, err
	}
	checksum := computeChecksum(canonicalManifest, files)
	rec := &ifaces.AuthoredSkillRecord{
		TenantID:     tenantID,
		SkillID:      skillID,
		Version:      version,
		Status:       "draft",
		ManifestJSON: canonicalManifest,
		Checksum:     checksum,
		AuthorUserID: userID,
	}
	if err := s.repo.UpdateDraft(ctx, rec, filesToRecords(files)); err != nil {
		return nil, err
	}
	out := recordToDTO(rec, filesToRecords(files))
	return &out, nil
}

// Publish flips a draft to "published" and emits an EventAdded.
func (s *Store) Publish(ctx context.Context, tenantID, skillID, version string) (*Authored, error) {
	rec, err := s.repo.Publish(ctx, tenantID, skillID, version, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	dtoRec, fileRecs, _ := s.repo.Get(ctx, tenantID, skillID, version)
	if dtoRec == nil {
		dtoRec = rec
	}
	out := recordToDTO(dtoRec, fileRecs)
	s.emit(tenantID, source.Event{Kind: source.EventAdded, Ref: source.Ref{
		ID: skillID, Version: version, Source: SourceName, Loc: skillID + "@" + version,
	}})
	return &out, nil
}

// Archive flips a published revision to "archived" and emits an
// EventRemoved.
func (s *Store) Archive(ctx context.Context, tenantID, skillID, version string) error {
	if err := s.repo.Archive(ctx, tenantID, skillID, version, time.Now().UTC()); err != nil {
		return err
	}
	s.emit(tenantID, source.Event{Kind: source.EventRemoved, Ref: source.Ref{
		ID: skillID, Version: version, Source: SourceName, Loc: skillID + "@" + version,
	}})
	return nil
}

// DeleteDraft removes a draft revision; refuses to delete published.
func (s *Store) DeleteDraft(ctx context.Context, tenantID, skillID, version string) error {
	return s.repo.DeleteDraft(ctx, tenantID, skillID, version)
}

// --- Helpers --------------------------------------------------------

func filesToRecords(files []File) []ifaces.AuthoredFileRecord {
	out := make([]ifaces.AuthoredFileRecord, 0, len(files))
	for _, f := range files {
		out = append(out, ifaces.AuthoredFileRecord{
			RelPath: f.RelPath, MIMEType: f.MIMEType, Contents: f.Body,
		})
	}
	return out
}

func recordToDTO(rec *ifaces.AuthoredSkillRecord, files []ifaces.AuthoredFileRecord) Authored {
	out := Authored{
		SkillID:      rec.SkillID,
		Version:      rec.Version,
		Status:       rec.Status,
		ManifestRaw:  rec.ManifestJSON,
		Checksum:     rec.Checksum,
		AuthorUserID: rec.AuthorUserID,
		CreatedAt:    rec.CreatedAt,
		PublishedAt:  rec.PublishedAt,
	}
	if m, _, err := manifest.Parse(rec.ManifestJSON); err == nil && m != nil {
		out.Manifest = *m
	}
	for _, f := range files {
		out.Files = append(out.Files, File{RelPath: f.RelPath, MIMEType: f.MIMEType, Body: f.Contents})
	}
	return out
}

// canonicalManifest encodes a manifest deterministically. Used both
// for the on-disk JSON column and for the checksum input. Reuses the
// snapshot package's canonical encoder so file format conventions
// (sorted keys, dropped nulls) match across Phase 6 and Phase 8.
func canonicalManifest(m manifest.Manifest) ([]byte, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("authored: marshal manifest: %w", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("authored: re-decode manifest: %w", err)
	}
	out, err := snapshots.CanonicalEncode(doc)
	if err != nil {
		return nil, fmt.Errorf("authored: canonical encode: %w", err)
	}
	return out, nil
}

// computeChecksum hashes (canonical manifest, sorted file list).
func computeChecksum(manifestJSON []byte, files []File) string {
	h := sha256.New()
	h.Write([]byte("manifest:"))
	h.Write(manifestJSON)
	h.Write([]byte{'\n'})
	// stable order over RelPath
	sorted := make([]File, len(files))
	copy(sorted, files)
	sortFiles(sorted)
	for _, f := range sorted {
		h.Write([]byte("file:"))
		h.Write([]byte(f.RelPath))
		h.Write([]byte{':'})
		h.Write(f.Body)
		h.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func sortFiles(files []File) {
	for i := 1; i < len(files); i++ {
		for j := i; j > 0 && files[j-1].RelPath > files[j].RelPath; j-- {
			files[j-1], files[j] = files[j], files[j-1]
		}
	}
}
