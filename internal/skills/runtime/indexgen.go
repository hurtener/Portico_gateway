package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// IndexGenerator renders the synthetic skill://_index resource.
//
// The index is the "skill catalog" the agent sees: id, version, what
// servers/tools it needs, missing-tools status, and per-(tenant,
// session) enablement flags. Phase 4 caches the rendered JSON for 60s
// per (tenant, session); cache invalidates on catalog changes,
// registry changes, and enablement mutations.
type IndexGenerator struct {
	catalog    *Catalog
	enablement *Enablement
	plans      func(tenantID string) (string, []string) // plan + glob entitlements
	annotate   func(ctx context.Context, tenantID, skillID string) ([]string, []string)

	mu    sync.Mutex
	cache map[indexKey]indexEntry
	ttl   time.Duration
}

type indexKey struct {
	tenantID  string
	sessionID string
}

type indexEntry struct {
	body      []byte
	expiresAt time.Time
}

// NewIndexGenerator constructs the generator. plans resolves the
// tenant's plan + entitlement globs. annotate is the loader's
// AnnotateMissingTools (returns the missing list + warnings).
func NewIndexGenerator(catalog *Catalog, enablement *Enablement, plans func(string) (string, []string), annotate func(context.Context, string, string) ([]string, []string)) *IndexGenerator {
	return &IndexGenerator{
		catalog:    catalog,
		enablement: enablement,
		plans:      plans,
		annotate:   annotate,
		cache:      make(map[indexKey]indexEntry),
		ttl:        60 * time.Second,
	}
}

// Invalidate drops the cached body for (tenant, session). Pass
// sessionID == "" to drop only the tenant-wide cached entry.
func (g *IndexGenerator) Invalidate(tenantID, sessionID string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.cache, indexKey{tenantID: tenantID, sessionID: sessionID})
}

// InvalidateAll clears every cached body. Called on catalog changes
// and (eventually) registry changes.
func (g *IndexGenerator) InvalidateAll() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cache = make(map[indexKey]indexEntry)
}

// Render returns the JSON body for the supplied tenant/session.
func (g *IndexGenerator) Render(ctx context.Context, tenantID, sessionID string) ([]byte, error) {
	if g == nil || g.catalog == nil {
		return nil, fmt.Errorf("index: not configured")
	}
	key := indexKey{tenantID: tenantID, sessionID: sessionID}
	g.mu.Lock()
	if e, ok := g.cache[key]; ok && time.Now().Before(e.expiresAt) {
		body := e.body
		g.mu.Unlock()
		return body, nil
	}
	g.mu.Unlock()

	plan, ents := "", []string{}
	if g.plans != nil {
		plan, ents = g.plans(tenantID)
	}

	type indexEntryItem struct {
		ID                string   `json:"id"`
		Version           string   `json:"version"`
		Title             string   `json:"title"`
		Description       string   `json:"description,omitempty"`
		Spec              string   `json:"spec"`
		RequiredServers   []string `json:"required_servers"`
		RequiredTools     []string `json:"required_tools"`
		OptionalTools     []string `json:"optional_tools,omitempty"`
		ManifestURI       string   `json:"manifest_uri"`
		InstructionsURI   string   `json:"instructions_uri"`
		UIResourceURI     string   `json:"ui_resource_uri,omitempty"`
		EnabledForTenant  bool     `json:"enabled_for_tenant"`
		EnabledForSession bool     `json:"enabled_for_session"`
		MissingTools      []string `json:"missing_tools,omitempty"`
		Warnings          []string `json:"warnings,omitempty"`
	}
	type indexDoc struct {
		Version     int              `json:"version"`
		TenantID    string           `json:"tenant_id"`
		SessionID   string           `json:"session_id,omitempty"`
		GeneratedAt string           `json:"generated_at"`
		Skills      []indexEntryItem `json:"skills"`
	}

	doc := indexDoc{
		Version:     1,
		TenantID:    tenantID,
		SessionID:   sessionID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Skills:      []indexEntryItem{},
	}
	for _, s := range g.catalog.ForTenant(ents, plan) {
		enabledTenant, _ := g.enablement.IsEnabled(ctx, tenantID, "", s.Manifest.ID)
		enabledSession := enabledTenant
		if sessionID != "" {
			enabledSession, _ = g.enablement.IsEnabled(ctx, tenantID, sessionID, s.Manifest.ID)
		}

		var missing, warnings []string
		if g.annotate != nil {
			missing, warnings = g.annotate(ctx, tenantID, s.Manifest.ID)
		}
		warnings = append(warnings, s.Warnings...)

		ns := s.Namespace()
		name := s.Name()
		base := "skill://" + ns + "/" + name + "/"
		ui := ""
		if s.Manifest.Binding.UI != nil {
			ui = s.Manifest.Binding.UI.ResourceURI
		}
		doc.Skills = append(doc.Skills, indexEntryItem{
			ID:                s.Manifest.ID,
			Version:           s.Manifest.Version,
			Title:             s.Manifest.Title,
			Description:       s.Manifest.Description,
			Spec:              s.Manifest.Spec,
			RequiredServers:   nilToEmpty(s.Manifest.Binding.ServerDependencies),
			RequiredTools:     nilToEmpty(s.Manifest.Binding.RequiredTools),
			OptionalTools:     s.Manifest.Binding.OptionalTools,
			ManifestURI:       base + "manifest.yaml",
			InstructionsURI:   base + s.Manifest.Instructions,
			UIResourceURI:     ui,
			EnabledForTenant:  enabledTenant,
			EnabledForSession: enabledSession,
			MissingTools:      missing,
			Warnings:          warnings,
		})
	}

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	g.cache[key] = indexEntry{body: body, expiresAt: time.Now().Add(g.ttl)}
	g.mu.Unlock()
	return body, nil
}

func nilToEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
