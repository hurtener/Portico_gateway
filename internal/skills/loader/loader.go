// Package loader composes a JSON Schema validator + semantic checks
// over Skill Pack manifests. The loader does not own any state — it
// produces LoadResult values; the runtime decides what to register.
package loader

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	"github.com/hurtener/Portico_gateway/internal/skills/source"
)

// LoadResult is the outcome of a single Skill Pack load attempt.
type LoadResult struct {
	Source   source.Source
	Ref      source.Ref
	Manifest *manifest.Manifest
	Errors   []error
	Warnings []string
}

// Loader runs the schema + semantic checks for one or more Sources.
// It is safe for concurrent use; the schema is compiled once at
// construction.
type Loader struct {
	sources  []source.Source
	schema   *manifest.Schema
	registry *registry.Registry
	log      *slog.Logger
}

// New constructs a Loader. registry may be nil — semantic checks that
// rely on the registry (tool dependency resolution) become warnings
// instead of hard errors when the registry is absent.
func New(sources []source.Source, reg *registry.Registry, log *slog.Logger) (*Loader, error) {
	if log == nil {
		log = slog.Default()
	}
	schema, err := manifest.CompileSchema()
	if err != nil {
		return nil, fmt.Errorf("loader: compile schema: %w", err)
	}
	return &Loader{
		sources:  sources,
		schema:   schema,
		registry: reg,
		log:      log,
	}, nil
}

// LoadAll iterates every Source, lists every pack, parses + validates,
// and returns one LoadResult per pack. Order is sorted by ref.ID so
// callers get deterministic output.
func (l *Loader) LoadAll(ctx context.Context) []LoadResult {
	out := make([]LoadResult, 0)
	for _, src := range l.sources {
		refs, err := src.List(ctx)
		if err != nil {
			l.log.Warn("loader: source.List failed", "source", src.Name(), "err", err)
			continue
		}
		for _, ref := range refs {
			out = append(out, l.loadOne(ctx, src, ref))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.ID < out[j].Ref.ID })
	return out
}

// LoadOne re-loads a single pack on demand. Used by hot-reload.
func (l *Loader) LoadOne(ctx context.Context, src source.Source, ref source.Ref) LoadResult {
	return l.loadOne(ctx, src, ref)
}

// LoadFromBytes runs the schema + semantic checks on a manifest body
// supplied directly (no Source). Used by `portico validate-skills`.
//
// The semantic check that probes file existence is skipped — the CLI
// validates manifests the operator hands it, not necessarily attached
// to a live source.
func (l *Loader) LoadFromBytes(body []byte) LoadResult {
	res := LoadResult{}
	m, doc, err := manifest.Parse(body)
	if err != nil {
		res.Errors = append(res.Errors, err)
		return res
	}
	res.Manifest = m
	if err := l.schema.Validate(doc); err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("schema: %w", err))
	}
	errs, warns := ValidateSemantic(m, l.registry, false /*probeFiles*/, nil)
	res.Errors = append(res.Errors, errs...)
	res.Warnings = append(res.Warnings, warns...)
	return res
}

// AnnotateMissingTools reports the tools the manifest declares but
// the registry can't currently provide for the supplied tenant. The
// result is intended for the index generator + Console status pills.
//
// Phase 4 matches on server prefix (server.*) — Portico does not
// persist per-tool schemas in the registry yet (Phase 6 catalog
// snapshots will), so a tool whose server is registered is presumed
// available.
func (l *Loader) AnnotateMissingTools(ctx context.Context, tenantID string, m *manifest.Manifest) []string {
	if l.registry == nil || m == nil {
		return nil
	}
	servers := make(map[string]bool)
	if snaps, err := l.registry.List(ctx, tenantID); err == nil {
		for _, s := range snaps {
			servers[s.Spec.ID] = true
		}
	}
	missing := make([]string, 0)
	wanted := append([]string(nil), m.Binding.RequiredTools...)
	wanted = append(wanted, m.Binding.OptionalTools...)
	for _, t := range wanted {
		dot := -1
		for i := 0; i < len(t); i++ {
			if t[i] == '.' {
				dot = i
				break
			}
		}
		if dot <= 0 {
			missing = append(missing, t)
			continue
		}
		serverID := t[:dot]
		if !servers[serverID] {
			missing = append(missing, t)
		}
	}
	return missing
}

func (l *Loader) loadOne(ctx context.Context, src source.Source, ref source.Ref) LoadResult {
	res := LoadResult{Source: src, Ref: ref}
	body, err := readManifest(ctx, src, ref)
	if err != nil {
		res.Errors = append(res.Errors, err)
		return res
	}
	m, doc, perr := manifest.Parse(body)
	if perr != nil {
		res.Errors = append(res.Errors, perr)
		return res
	}
	res.Manifest = m

	if err := l.schema.Validate(doc); err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("schema: %w", err))
		// Schema failure short-circuits semantic checks — the manifest
		// is malformed enough that any further check would just amplify
		// the same root cause.
		return res
	}

	probe := func(rel string) error {
		rc, _, err := src.ReadFile(ctx, ref, rel)
		if err != nil {
			return err
		}
		_ = rc.Close()
		return nil
	}
	errs, warns := ValidateSemantic(m, l.registry, true, probe)
	res.Errors = append(res.Errors, errs...)
	res.Warnings = append(res.Warnings, warns...)

	return res
}

func readManifest(ctx context.Context, src source.Source, ref source.Ref) ([]byte, error) {
	rc, _, err := src.ReadFile(ctx, ref, "manifest.yaml")
	if err != nil {
		return nil, fmt.Errorf("read manifest.yaml: %w", err)
	}
	defer rc.Close()
	body := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, rerr := rc.Read(tmp)
		if n > 0 {
			body = append(body, tmp[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	return body, nil
}
