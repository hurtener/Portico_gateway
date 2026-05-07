// Package git implements the Git-based Skill source driver. Skill
// Packs come from a remote Git repository (HTTPS or SSH); the driver
// shallow-clones into the data dir and refreshes on a configurable
// interval. Submodules are disabled by default — enabling them is an
// explicit opt-in (see Config.AllowSubmodules).
//
// Pure-Go via go-git/go-git/v5 (CGo-free, MIT). Self-registers under
// driver name "git" in init().
package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	"github.com/hurtener/Portico_gateway/internal/skills/source"
)

// DriverName is the value the tenant_skill_sources.driver column carries.
const DriverName = "git"

// Default refresh interval when none is configured.
const defaultRefreshInterval = 5 * time.Minute

// Config is the per-source configuration persisted in
// tenant_skill_sources.config_json. Plan §Public types.
type Config struct {
	URL              string `json:"url"`
	Branch           string `json:"branch,omitempty"`
	SubdirGlob       string `json:"subdir_glob,omitempty"`
	RefreshInterval  string `json:"refresh_interval,omitempty"`
	CredentialRef    string `json:"credential_ref,omitempty"`
	AllowSubmodules  bool   `json:"allow_submodules,omitempty"`
	BasicUsername    string `json:"basic_username,omitempty"`
}

// Source is the Git Skill source. One instance per (tenant, source name).
type Source struct {
	cfg      Config
	tenantID string
	root     string // local clone tree
	log      *slog.Logger
	vault    source.VaultLookup
	name     string

	mu      sync.RWMutex
	cloned  bool
	listen  bool
	stop    chan struct{}
	stopped chan struct{}
}

func init() {
	source.Register(DriverName, factory)
}

func factory(_ context.Context, configJSON []byte, deps source.FactoryDeps) (source.Source, error) {
	var cfg Config
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, fmt.Errorf("git: decode config: %w", err)
	}
	if cfg.URL == "" {
		return nil, errors.New("git: url is required")
	}
	if deps.DataDir == "" {
		return nil, errors.New("git: DataDir is required")
	}
	hash := hashURL(cfg.URL, cfg.Branch)
	root := filepath.Join(deps.DataDir, "sources", "git", deps.TenantID, hash)
	if err := os.MkdirAll(filepath.Dir(root), 0o750); err != nil {
		return nil, fmt.Errorf("git: ensure cache root: %w", err)
	}
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	name := deps.SourceName
	if name == "" {
		name = DriverName
	}
	return &Source{
		cfg:      cfg,
		tenantID: deps.TenantID,
		root:     root,
		log:      log.With("driver", "git", "tenant_id", deps.TenantID, "source_name", name),
		vault:    deps.Vault,
		name:     name,
	}, nil
}

// Name returns the driver name.
func (s *Source) Name() string { return s.name }

// Root returns the local clone path; useful for diagnostics + tests.
func (s *Source) Root() string { return s.root }

// List clones (or refreshes) the repository and walks the tree for
// pack roots — directories that contain a manifest.yaml. Walks
// honour Config.SubdirGlob when non-empty.
func (s *Source) List(ctx context.Context) ([]source.Ref, error) {
	if err := s.ensureClone(ctx); err != nil {
		return nil, err
	}
	return s.scanPacks()
}

// Open parses the manifest at ref.Loc/manifest.yaml.
func (s *Source) Open(ctx context.Context, ref source.Ref) (manifest.Manifest, error) {
	if err := s.guardLoc(ref.Loc); err != nil {
		return manifest.Manifest{}, err
	}
	body, err := os.ReadFile(filepath.Join(ref.Loc, "manifest.yaml"))
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("git: read manifest: %w", err)
	}
	m, _, err := manifest.Parse(body)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("git: parse manifest: %w", err)
	}
	return *m, nil
}

// ReadFile returns the bytes of relpath inside ref.Loc. Path traversal
// rejected per CLAUDE.md §7.5.
func (s *Source) ReadFile(_ context.Context, ref source.Ref, relpath string) (io.ReadCloser, source.ContentInfo, error) {
	if err := s.guardLoc(ref.Loc); err != nil {
		return nil, source.ContentInfo{}, err
	}
	if filepath.IsAbs(relpath) {
		return nil, source.ContentInfo{}, errors.New("git: relpath must be relative")
	}
	abs := filepath.Clean(filepath.Join(ref.Loc, relpath))
	if !strings.HasPrefix(abs, ref.Loc+string(os.PathSeparator)) && abs != ref.Loc {
		return nil, source.ContentInfo{}, errors.New("git: relpath escapes pack root")
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, source.ContentInfo{}, fmt.Errorf("git: open %q: %w", relpath, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, source.ContentInfo{}, fmt.Errorf("git: stat %q: %w", relpath, err)
	}
	return f, source.ContentInfo{
		MIMEType: detectMIME(abs),
		Size:     info.Size(),
		ModTime:  info.ModTime().UTC(),
	}, nil
}

// Watch fetches every RefreshInterval and emits diff events.
func (s *Source) Watch(ctx context.Context) (<-chan source.Event, error) {
	out := make(chan source.Event, 16)
	s.mu.Lock()
	if s.listen {
		s.mu.Unlock()
		return nil, errors.New("git: already watching")
	}
	s.listen = true
	s.stop = make(chan struct{})
	s.stopped = make(chan struct{})
	s.mu.Unlock()

	go func() {
		defer close(out)
		defer close(s.stopped)

		interval := s.refreshInterval()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var prev map[string]string // skill_id -> commit hash for HEAD
		current, err := s.scanPacks()
		if err == nil {
			prev = packHashes(current)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-ticker.C:
				if err := s.refresh(ctx); err != nil {
					s.log.Warn("git refresh failed", "err", err)
					continue
				}
				next, err := s.scanPacks()
				if err != nil {
					s.log.Warn("git scan failed after refresh", "err", err)
					continue
				}
				nextHashes := packHashes(next)
				diffEmit(prev, nextHashes, next, out, s.log, s.name)
				prev = nextHashes
			}
		}
	}()
	return out, nil
}

// Stop joins the watcher goroutine. Idempotent.
func (s *Source) Stop() {
	s.mu.Lock()
	if !s.listen {
		s.mu.Unlock()
		return
	}
	close(s.stop)
	stopped := s.stopped
	s.listen = false
	s.mu.Unlock()
	<-stopped
}

// Refresh forces a fetch + scan and returns the latest list. Called
// by REST /api/skill-sources/{name}/refresh.
func (s *Source) Refresh(ctx context.Context) ([]source.Ref, error) {
	if err := s.refresh(ctx); err != nil {
		return nil, err
	}
	return s.scanPacks()
}

// --- internal -------------------------------------------------------

func (s *Source) ensureClone(ctx context.Context) error {
	s.mu.RLock()
	cloned := s.cloned
	s.mu.RUnlock()
	if cloned {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cloned {
		return nil
	}
	auth, err := s.resolveAuth(ctx)
	if err != nil {
		return err
	}
	opts := &gogit.CloneOptions{
		URL:               s.cfg.URL,
		Auth:              auth,
		Depth:             1,
		SingleBranch:      true,
		RecurseSubmodules: gogit.NoRecurseSubmodules,
		Progress:          io.Discard,
	}
	if s.cfg.Branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(s.cfg.Branch)
	}
	if s.cfg.AllowSubmodules {
		opts.RecurseSubmodules = gogit.DefaultSubmoduleRecursionDepth
	}
	if _, err := gogit.PlainCloneContext(ctx, s.root, false, opts); err != nil {
		// Clone errors might leave a partial directory; clear it so the
		// next attempt starts clean.
		_ = os.RemoveAll(s.root)
		return fmt.Errorf("git: clone %s: %w", s.cfg.URL, err)
	}
	s.cloned = true
	return nil
}

func (s *Source) refresh(ctx context.Context) error {
	if err := s.ensureClone(ctx); err != nil {
		return err
	}
	repo, err := gogit.PlainOpen(s.root)
	if err != nil {
		// Cache might be corrupt; force re-clone.
		s.mu.Lock()
		s.cloned = false
		s.mu.Unlock()
		_ = os.RemoveAll(s.root)
		return fmt.Errorf("git: open repo: %w", err)
	}
	auth, err := s.resolveAuth(ctx)
	if err != nil {
		return err
	}
	if err := repo.FetchContext(ctx, &gogit.FetchOptions{
		Auth:     auth,
		Progress: io.Discard,
		Prune:    true,
		Force:    true,
		RefSpecs: []gogitconfig.RefSpec{gogitconfig.RefSpec("+refs/heads/*:refs/remotes/origin/*")},
	}); err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return fmt.Errorf("git: fetch: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("git: worktree: %w", err)
	}
	candidates := []string{}
	if s.cfg.Branch != "" {
		candidates = append(candidates, s.cfg.Branch)
	}
	candidates = append(candidates, "main", "master", "trunk")
	var resolved string
	for _, b := range candidates {
		if _, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", b), true); err == nil {
			resolved = b
			break
		}
	}
	if resolved != "" {
		ref, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", resolved), true)
		if err == nil {
			if err := wt.Reset(&gogit.ResetOptions{Mode: gogit.HardReset, Commit: ref.Hash()}); err != nil {
				return fmt.Errorf("git: reset: %w", err)
			}
		}
	}
	return nil
}

func (s *Source) resolveAuth(ctx context.Context) (transport.AuthMethod, error) {
	cfg := s.cfg
	if cfg.CredentialRef == "" {
		return nil, nil
	}
	if s.vault == nil {
		return nil, errors.New("git: credential_ref set but vault not configured")
	}
	value, err := s.vault.Get(ctx, s.tenantID, cfg.CredentialRef)
	if err != nil {
		return nil, fmt.Errorf("git: vault lookup %q: %w", cfg.CredentialRef, err)
	}
	// Heuristic: SSH keys begin with -----BEGIN; else treat as a PAT.
	if strings.HasPrefix(strings.TrimSpace(value), "-----BEGIN") {
		return nil, errors.New("git: SSH key credentials not yet supported by the V1 driver; use HTTPS + PAT")
	}
	username := cfg.BasicUsername
	if username == "" {
		username = "x-access-token"
	}
	return &githttp.BasicAuth{Username: username, Password: value}, nil
}

func (s *Source) refreshInterval() time.Duration {
	if s.cfg.RefreshInterval == "" {
		return defaultRefreshInterval
	}
	d, err := time.ParseDuration(s.cfg.RefreshInterval)
	if err != nil || d < 30*time.Second {
		return defaultRefreshInterval
	}
	return d
}

func (s *Source) scanPacks() ([]source.Ref, error) {
	out := make([]source.Ref, 0)
	err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // tolerate walk errors per pack
		}
		if d.IsDir() {
			// Skip .git tree to avoid huge walks.
			if filepath.Base(path) == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "manifest.yaml" {
			return nil
		}
		packDir := filepath.Dir(path)
		// SubdirGlob filter, if configured.
		if s.cfg.SubdirGlob != "" {
			rel, _ := filepath.Rel(s.root, packDir)
			matched, _ := filepath.Match(s.cfg.SubdirGlob, rel)
			if !matched {
				return nil
			}
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		m, _, perr := manifest.Parse(body)
		id := filepath.Base(packDir)
		version := ""
		if perr == nil && m != nil {
			if m.ID != "" {
				id = m.ID
			}
			version = m.Version
		}
		out = append(out, source.Ref{
			ID: id, Version: version, Source: s.name, Loc: packDir,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("git: scan: %w", err)
	}
	return out, nil
}

func (s *Source) guardLoc(loc string) error {
	if loc == "" {
		return errors.New("git: empty loc")
	}
	abs, err := filepath.Abs(loc)
	if err != nil {
		return err
	}
	root, _ := filepath.Abs(s.root)
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
		return fmt.Errorf("git: loc %q outside cache %q", loc, s.root)
	}
	return nil
}

// --- diff helpers ---------------------------------------------------

func packHashes(refs []source.Ref) map[string]string {
	out := make(map[string]string, len(refs))
	for _, r := range refs {
		out[r.ID] = r.Version + "@" + r.Loc
	}
	return out
}

func diffEmit(prev, next map[string]string, refs []source.Ref, ch chan<- source.Event, log *slog.Logger, srcName string) {
	byID := make(map[string]source.Ref, len(refs))
	for _, r := range refs {
		byID[r.ID] = r
	}
	for id, h := range next {
		if old, ok := prev[id]; !ok {
			tryEmit(ch, source.Event{Kind: source.EventAdded, Ref: byID[id]}, log)
		} else if old != h {
			tryEmit(ch, source.Event{Kind: source.EventUpdated, Ref: byID[id]}, log)
		}
	}
	for id := range prev {
		if _, ok := next[id]; !ok {
			tryEmit(ch, source.Event{Kind: source.EventRemoved,
				Ref: source.Ref{ID: id, Source: srcName}}, log)
		}
	}
}

func tryEmit(ch chan<- source.Event, ev source.Event, log *slog.Logger) {
	select {
	case ch <- ev:
	default:
		log.Warn("git watch: dropping event (channel full)", "skill_id", ev.Ref.ID)
	}
}

func detectMIME(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md":
		return "text/markdown"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".json":
		return "application/json"
	case ".html", ".htm":
		return "text/html"
	case ".txt":
		return "text/plain"
	}
	if mt := http.DetectContentType(make([]byte, 0)); mt != "" {
		// Fallback to a generic MIME; tests don't depend on this branch.
		_ = mt
	}
	return "application/octet-stream"
}

func hashURL(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
