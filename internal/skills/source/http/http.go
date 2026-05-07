// Package http implements the HTTP-based Skill source driver. The
// driver pulls a JSON "feed of feeds" + per-pack content tarball
// indices. Designed to be the surface a future hosted Portico
// registry serves; the V1 surface accepts a feed URL + an optional
// vault-stored bearer/api-key.
//
// Self-registers under driver name "http" in init().
package http

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	"github.com/hurtener/Portico_gateway/internal/skills/source"
)

// DriverName is the value the tenant_skill_sources.driver column carries.
const DriverName = "http"

const defaultRefreshInterval = 5 * time.Minute

// Config is the per-source configuration persisted in
// tenant_skill_sources.config_json.
type Config struct {
	FeedURL         string `json:"feed_url"`
	RefreshInterval string `json:"refresh_interval,omitempty"`
	CredentialRef   string `json:"credential_ref,omitempty"`
	HeaderName      string `json:"header_name,omitempty"`
	HeaderPrefix    string `json:"header_prefix,omitempty"`
}

// FeedDocument is the JSON body the feed endpoint returns.
type FeedDocument struct {
	Schema  string          `json:"schema"`
	Updated time.Time       `json:"updated"`
	Packs   []FeedPackEntry `json:"packs"`
}

// FeedPackEntry describes one pack served by the feed.
type FeedPackEntry struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Checksum  string `json:"checksum"`   // sha256:<hex>
	BundleURL string `json:"bundle_url"` // tar+gz of the pack tree
}

// Source is the HTTP Skill source instance.
type Source struct {
	cfg      Config
	tenantID string
	cacheDir string
	log      *slog.Logger
	vault    source.VaultLookup
	client   *stdhttp.Client
	name     string

	mu      sync.RWMutex
	packs   map[string]FeedPackEntry // skill_id -> entry
	stop    chan struct{}
	stopped chan struct{}
	listen  bool
}

func init() {
	source.Register(DriverName, factory)
}

func factory(_ context.Context, configJSON []byte, deps source.FactoryDeps) (source.Source, error) {
	var cfg Config
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, fmt.Errorf("http: decode config: %w", err)
	}
	if cfg.FeedURL == "" {
		return nil, errors.New("http: feed_url is required")
	}
	if _, err := url.Parse(cfg.FeedURL); err != nil {
		return nil, fmt.Errorf("http: feed_url: %w", err)
	}
	if deps.DataDir == "" {
		return nil, errors.New("http: DataDir is required")
	}
	cacheDir := filepath.Join(deps.DataDir, "sources", "http", deps.TenantID, hashFeed(cfg.FeedURL))
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return nil, fmt.Errorf("http: ensure cache dir: %w", err)
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
		cacheDir: cacheDir,
		log:      log.With("driver", "http", "tenant_id", deps.TenantID, "source_name", name),
		vault:    deps.Vault,
		client:   &stdhttp.Client{Timeout: 30 * time.Second},
		name:     name,
		packs:    map[string]FeedPackEntry{},
	}, nil
}

// Name implements source.Source.
func (s *Source) Name() string { return s.name }

// List pulls the feed and returns one Ref per pack.
func (s *Source) List(ctx context.Context) ([]source.Ref, error) {
	feed, err := s.fetchFeed(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.packs = make(map[string]FeedPackEntry, len(feed.Packs))
	for _, p := range feed.Packs {
		s.packs[p.ID] = p
	}
	s.mu.Unlock()
	out := make([]source.Ref, 0, len(feed.Packs))
	for _, p := range feed.Packs {
		out = append(out, source.Ref{
			ID: p.ID, Version: p.Version, Source: s.name, Loc: p.ID + "@" + p.Version,
		})
	}
	return out, nil
}

// Open returns the parsed manifest for ref by lazy-fetching the
// bundle and decoding manifest.yaml.
func (s *Source) Open(ctx context.Context, ref source.Ref) (manifest.Manifest, error) {
	body, _, err := s.readBundleFile(ctx, ref, "manifest.yaml")
	if err != nil {
		return manifest.Manifest{}, err
	}
	m, _, err := manifest.Parse(body)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("http: parse manifest: %w", err)
	}
	return *m, nil
}

// ReadFile returns the bytes of relpath from the cached bundle.
func (s *Source) ReadFile(ctx context.Context, ref source.Ref, relpath string) (io.ReadCloser, source.ContentInfo, error) {
	body, mime, err := s.readBundleFile(ctx, ref, relpath)
	if err != nil {
		return nil, source.ContentInfo{}, err
	}
	return io.NopCloser(strings.NewReader(string(body))),
		source.ContentInfo{
			MIMEType: mime,
			Size:     int64(len(body)),
			ModTime:  time.Now().UTC(),
		}, nil
}

// Watch pulls the feed every RefreshInterval and emits diff events.
func (s *Source) Watch(ctx context.Context) (<-chan source.Event, error) {
	out := make(chan source.Event, 16)
	s.mu.Lock()
	if s.listen {
		s.mu.Unlock()
		return nil, errors.New("http: already watching")
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

		// Take an initial snapshot.
		var prev map[string]string
		init, err := s.List(ctx)
		if err == nil {
			prev = packHashes(init)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-ticker.C:
				next, err := s.List(ctx)
				if err != nil {
					s.log.Warn("http feed refresh failed", "err", err)
					continue
				}
				nextHashes := packHashes(next)
				for id, h := range nextHashes {
					prevHash, ok := prev[id]
					if !ok {
						emit(out, source.Event{Kind: source.EventAdded,
							Ref: refFromList(next, id)}, s.log)
					} else if prevHash != h {
						emit(out, source.Event{Kind: source.EventUpdated,
							Ref: refFromList(next, id)}, s.log)
					}
				}
				for id := range prev {
					if _, ok := nextHashes[id]; !ok {
						emit(out, source.Event{Kind: source.EventRemoved,
							Ref: source.Ref{ID: id, Source: s.name}}, s.log)
					}
				}
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

// --- internal -------------------------------------------------------

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

func (s *Source) fetchFeed(ctx context.Context) (*FeedDocument, error) {
	body, err := s.fetchURLWithRetry(ctx, s.cfg.FeedURL)
	if err != nil {
		return nil, fmt.Errorf("http: feed fetch: %w", err)
	}
	var doc FeedDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("http: feed decode: %w", err)
	}
	if doc.Schema == "" {
		return nil, errors.New("http: feed missing schema")
	}
	return &doc, nil
}

// readBundleFile returns the bytes of relpath inside the bundle for
// ref. The bundle is downloaded on first access and cached on disk.
func (s *Source) readBundleFile(ctx context.Context, ref source.Ref, relpath string) ([]byte, string, error) {
	s.mu.RLock()
	entry, ok := s.packs[ref.ID]
	s.mu.RUnlock()
	if !ok {
		// Try to refresh the feed.
		if _, err := s.fetchFeed(ctx); err != nil {
			return nil, "", fmt.Errorf("http: bundle ref %q: %w", ref.ID, err)
		}
		s.mu.RLock()
		entry, ok = s.packs[ref.ID]
		s.mu.RUnlock()
		if !ok {
			return nil, "", fmt.Errorf("http: pack %q not in feed", ref.ID)
		}
	}
	bundle, err := s.fetchBundle(ctx, entry)
	if err != nil {
		return nil, "", err
	}
	for _, f := range bundle {
		if f.RelPath == relpath {
			return f.Body, f.MIME, nil
		}
	}
	return nil, "", fmt.Errorf("http: file %q not in bundle %s", relpath, ref.ID)
}

type bundleFile struct {
	RelPath string
	MIME    string
	Body    []byte
}

// fetchBundle downloads + decodes a tar+gz pack, validating against
// entry.Checksum. Cached by checksum so a feed that pins different
// checksums for the same id triggers re-fetch.
func (s *Source) fetchBundle(ctx context.Context, entry FeedPackEntry) ([]bundleFile, error) {
	cachePath := filepath.Join(s.cacheDir, "bundles", entry.ID+"-"+entry.Version+"-"+strings.TrimPrefix(entry.Checksum, "sha256:")+".tgz")
	if data, err := os.ReadFile(cachePath); err == nil {
		if verifyChecksum(data, entry.Checksum) == nil {
			return decodeBundle(data)
		}
	}
	body, err := s.fetchURLWithRetry(ctx, entry.BundleURL)
	if err != nil {
		return nil, fmt.Errorf("http: bundle fetch: %w", err)
	}
	if err := verifyChecksum(body, entry.Checksum); err != nil {
		return nil, fmt.Errorf("http: bundle checksum: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o750); err != nil {
		return nil, fmt.Errorf("http: cache dir: %w", err)
	}
	if err := os.WriteFile(cachePath, body, 0o600); err != nil {
		s.log.Warn("http: failed to cache bundle", "err", err)
	}
	return decodeBundle(body)
}

func (s *Source) fetchURLWithRetry(ctx context.Context, urlStr string) ([]byte, error) {
	const maxAttempts = 3
	var lastErr error
	delay := 200 * time.Millisecond
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			delay *= 2
		}
		body, retry, err := s.fetchURL(ctx, urlStr)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("http: retries exhausted: %w", lastErr)
}

func (s *Source) fetchURL(ctx context.Context, urlStr string) ([]byte, bool, error) {
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, urlStr, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "*/*")
	if s.cfg.CredentialRef != "" {
		if s.vault == nil {
			return nil, false, errors.New("http: credential_ref set but vault not configured")
		}
		token, err := s.vault.Get(ctx, s.tenantID, s.cfg.CredentialRef)
		if err != nil {
			return nil, false, fmt.Errorf("http: vault lookup: %w", err)
		}
		header := s.cfg.HeaderName
		if header == "" {
			header = "Authorization"
		}
		prefix := s.cfg.HeaderPrefix
		if header == "Authorization" && prefix == "" {
			prefix = "Bearer "
		}
		req.Header.Set(header, prefix+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("http: do: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http: %s %d", urlStr, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, false, fmt.Errorf("http: %s %d", urlStr, resp.StatusCode)
	}
	return body, false, nil
}

// --- helpers --------------------------------------------------------

func decodeBundle(body []byte) ([]bundleFile, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("http: gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	out := make([]bundleFile, 0)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("http: tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Strip a leading common directory ("packs/foo/manifest.yaml" → keep nested form;
		// typical tarballs prefix with the pack name). We accept both: callers ask
		// for "manifest.yaml" or "prompts/x.md" — match longest suffix.
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "../") || strings.Contains(clean, string(os.PathSeparator)+"..") {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(tr, 5<<20))
		if err != nil {
			return nil, fmt.Errorf("http: read entry %q: %w", hdr.Name, err)
		}
		out = append(out, bundleFile{
			RelPath: trimBundlePrefix(clean),
			MIME:    detectMIME(hdr.Name),
			Body:    body,
		})
	}
	return out, nil
}

// trimBundlePrefix strips an optional leading "<packname>/" segment so
// callers can ask for "manifest.yaml" / "prompts/triage.md" uniformly.
func trimBundlePrefix(p string) string {
	if i := strings.IndexByte(p, '/'); i > 0 {
		first := p[:i]
		if first != ".." && first != "." {
			return p[i+1:]
		}
	}
	return p
}

func packHashes(refs []source.Ref) map[string]string {
	out := make(map[string]string, len(refs))
	for _, r := range refs {
		out[r.ID] = r.Version + "@" + r.Loc
	}
	return out
}

func refFromList(refs []source.Ref, id string) source.Ref {
	for _, r := range refs {
		if r.ID == id {
			return r
		}
	}
	return source.Ref{ID: id}
}

func emit(ch chan<- source.Event, ev source.Event, log *slog.Logger) {
	select {
	case ch <- ev:
	default:
		log.Warn("http watch: dropping event (channel full)", "skill_id", ev.Ref.ID)
	}
}

func verifyChecksum(body []byte, expected string) error {
	expected = strings.TrimPrefix(expected, "sha256:")
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return errors.New("http: feed entry missing checksum")
	}
	h := sha256.Sum256(body)
	got := hex.EncodeToString(h[:])
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch: got %s want %s", got, expected)
	}
	return nil
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
	return "application/octet-stream"
}

func hashFeed(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}
