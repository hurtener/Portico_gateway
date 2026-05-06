package mcpgw

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/catalog/namespace"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/registry"
)

// ResourceLimits controls how Portico clamps oversized resource reads.
type ResourceLimits struct {
	// MaxBytesPerRead is the maximum size (Text length or decoded Blob
	// size) Portico will pass through. Zero disables the limit. Default
	// 10 MB per resources/read response per content chunk.
	MaxBytesPerRead int64
}

// DefaultResourceLimits returns the project default.
func DefaultResourceLimits() ResourceLimits {
	return ResourceLimits{MaxBytesPerRead: 10 * 1024 * 1024}
}

// clientFleet is the seam the aggregators talk to. The southbound
// manager satisfies it implicitly; tests substitute an in-memory fake
// without spinning up a real registry + supervisor.
type clientFleet interface {
	Servers(ctx context.Context, tenantID string) ([]*registry.Snapshot, error)
	Acquire(ctx context.Context, req southboundmgr.AcquireRequest) (southbound.Client, error)
}

// ResourceAggregator implements `resources/list`, `resources/read`, and
// `resources/templates/list` over the configured downstream fleet. Tied
// to a Manager (for client acquisition), the registry (to discover
// servers per tenant), and an apps.Registry (to index ui:// resources).
type ResourceAggregator struct {
	log     *slog.Logger
	manager clientFleet
	apps    *apps.Registry
	limits  ResourceLimits
	timeout time.Duration

	cacheMu sync.Mutex
	// cache is keyed by (sessionID, kind, cursor) and stores the rendered
	// list payload. List-changed in stable mode invalidates by sessionID.
	cache map[cacheKey]cacheEntry
}

type cacheKey struct {
	sessionID string
	kind      string // "resources" | "templates" | "prompts"
	cursor    string
}

type cacheEntry struct {
	body      json.RawMessage
	expiresAt time.Time
}

const aggregatorCacheTTL = 60 * time.Second

// NewResourceAggregator constructs an aggregator with the supplied
// dependencies. Pass nil apps registry to disable ui:// indexing.
func NewResourceAggregator(m clientFleet, appReg *apps.Registry, limits ResourceLimits, log *slog.Logger) *ResourceAggregator {
	if log == nil {
		log = slog.Default()
	}
	if limits.MaxBytesPerRead == 0 {
		limits = DefaultResourceLimits()
	}
	return &ResourceAggregator{
		log:     log,
		manager: m,
		apps:    appReg,
		limits:  limits,
		timeout: 5 * time.Second,
		cache:   make(map[cacheKey]cacheEntry),
	}
}

// InvalidateSession removes every cached entry for sessionID. Called by
// the list-changed mux when a downstream emits list_changed in stable
// mode.
func (a *ResourceAggregator) InvalidateSession(sessionID string) {
	if sessionID == "" {
		return
	}
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	for k := range a.cache {
		if k.sessionID == sessionID {
			delete(a.cache, k)
		}
	}
}

// ListAll fans out resources/list across the session's servers and
// returns a single aggregated, sorted list.
func (a *ResourceAggregator) ListAll(ctx context.Context, sess *Session, cursor string) (*protocol.ListResourcesResult, error) {
	if cached, ok := a.lookupCache(sess.ID, "resources", cursor); ok {
		var res protocol.ListResourcesResult
		if err := json.Unmarshal(cached, &res); err == nil {
			return &res, nil
		}
	}

	listCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	servers, err := a.serversFor(listCtx, sess)
	if err != nil {
		return nil, err
	}

	per, err := decodeAggregatorCursor(cursor)
	if err != nil {
		return nil, err
	}

	type result struct {
		serverID  string
		resources []protocol.Resource
		next      string
		err       error
	}
	resCh := make(chan result, len(servers))
	for _, s := range servers {
		s := s
		go func() {
			c, err := a.acquireFor(listCtx, sess, s)
			if err != nil {
				resCh <- result{serverID: s.Spec.ID, err: err}
				return
			}
			items, next, err := c.ListResources(listCtx, per[s.Spec.ID])
			if err != nil && protocol.IsMethodNotFound(err) {
				// Server doesn't expose resources — silently skip.
				resCh <- result{serverID: s.Spec.ID}
				return
			}
			resCh <- result{serverID: s.Spec.ID, resources: items, next: next, err: err}
		}()
	}

	combined := make([]protocol.Resource, 0)
	nextPer := make(map[string]string)
	for i := 0; i < len(servers); i++ {
		r := <-resCh
		if r.err != nil {
			a.log.Warn("resources/list partial failure",
				"server_id", r.serverID, "err", r.err,
				"event_type", "resource_list_partial_failure")
			continue
		}
		for _, item := range r.resources {
			combined = append(combined, a.namespaceResource(r.serverID, item))
		}
		if r.next != "" {
			nextPer[r.serverID] = r.next
		}
	}
	sort.Slice(combined, func(i, j int) bool { return combined[i].URI < combined[j].URI })

	out := &protocol.ListResourcesResult{
		Resources: combined,
	}
	if encoded := encodeAggregatorCursor(nextPer); encoded != "" {
		out.NextCursor = encoded
	}
	body, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	a.storeCache(sess.ID, "resources", cursor, body)
	return out, nil
}

// ListTemplates aggregates resources/templates/list across servers.
func (a *ResourceAggregator) ListTemplates(ctx context.Context, sess *Session, cursor string) (*protocol.ListResourceTemplatesResult, error) {
	if cached, ok := a.lookupCache(sess.ID, "templates", cursor); ok {
		var res protocol.ListResourceTemplatesResult
		if err := json.Unmarshal(cached, &res); err == nil {
			return &res, nil
		}
	}

	listCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	servers, err := a.serversFor(listCtx, sess)
	if err != nil {
		return nil, err
	}
	per, err := decodeAggregatorCursor(cursor)
	if err != nil {
		return nil, err
	}

	type result struct {
		serverID  string
		templates []protocol.ResourceTemplate
		next      string
		err       error
	}
	resCh := make(chan result, len(servers))
	for _, s := range servers {
		s := s
		go func() {
			c, err := a.acquireFor(listCtx, sess, s)
			if err != nil {
				resCh <- result{serverID: s.Spec.ID, err: err}
				return
			}
			items, next, err := c.ListResourceTemplates(listCtx, per[s.Spec.ID])
			if err != nil && protocol.IsMethodNotFound(err) {
				resCh <- result{serverID: s.Spec.ID}
				return
			}
			resCh <- result{serverID: s.Spec.ID, templates: items, next: next, err: err}
		}()
	}

	combined := make([]protocol.ResourceTemplate, 0)
	nextPer := make(map[string]string)
	for i := 0; i < len(servers); i++ {
		r := <-resCh
		if r.err != nil {
			a.log.Warn("resources/templates/list partial failure",
				"server_id", r.serverID, "err", r.err)
			continue
		}
		for _, item := range r.templates {
			rewritten, _ := namespace.RewriteResourceURI(r.serverID, item.URITemplate)
			item.URITemplate = rewritten
			combined = append(combined, item)
		}
		if r.next != "" {
			nextPer[r.serverID] = r.next
		}
	}
	sort.Slice(combined, func(i, j int) bool { return combined[i].URITemplate < combined[j].URITemplate })

	out := &protocol.ListResourceTemplatesResult{ResourceTemplates: combined}
	if encoded := encodeAggregatorCursor(nextPer); encoded != "" {
		out.NextCursor = encoded
	}
	body, _ := json.Marshal(out)
	a.storeCache(sess.ID, "templates", cursor, body)
	return out, nil
}

// Read routes a namespaced URI back to its origin server, fetches the
// content, and applies size limits + CSP wrapping (for ui:// HTML).
func (a *ResourceAggregator) Read(ctx context.Context, sess *Session, uri string) (*protocol.ReadResourceResult, error) {
	serverID, original, isUI, ok := namespace.RestoreResourceURI(uri)
	if !ok {
		return nil, protocol.NewError(protocol.ErrInvalidParams, "uri is not in the gateway namespace", map[string]string{"uri": uri})
	}
	client, err := a.manager.Acquire(ctx, southboundmgr.AcquireRequest{
		TenantID:  sess.TenantID,
		UserID:    sess.UserID,
		SessionID: sess.ID,
		ServerID:  serverID,
	})
	if err != nil {
		return nil, protocol.NewError(protocol.ErrUpstreamUnavailable, err.Error(), map[string]string{"server_id": serverID})
	}
	res, err := client.ReadResource(ctx, original)
	if err != nil {
		var pe *protocol.Error
		if errors.As(err, &pe) {
			return nil, pe
		}
		return nil, protocol.NewError(protocol.ErrUpstreamUnavailable, err.Error(), map[string]string{"server_id": serverID})
	}
	out := &protocol.ReadResourceResult{Contents: make([]protocol.ResourceContent, 0, len(res.Contents))}
	for _, content := range res.Contents {
		// Re-namespace the URI on the way back out so clients don't see
		// the raw upstream URI.
		rewritten, _ := namespace.RewriteResourceURI(serverID, content.URI)
		content.URI = rewritten
		// Infer MIME type from extension when downstream omits or sets octet-stream.
		content.MimeType = inferMimeType(content.MimeType, original)
		// Truncation.
		content = a.applyLimit(content, original, serverID)
		// CSP wrap for ui:// + text/html.
		if isUI && isHTMLMime(content.MimeType) && content.Text != "" {
			body, meta := a.cspFor(serverID).Compose([]byte(content.Text))
			content.Text = string(body)
			content.Meta = mergeMetaPortico(content.Meta, meta)
		}
		out.Contents = append(out.Contents, content)
	}
	return out, nil
}

func (a *ResourceAggregator) cspFor(_ string) apps.CSPConfig {
	if a.apps != nil {
		return a.apps.CSP()
	}
	return apps.DefaultCSP()
}

func (a *ResourceAggregator) namespaceResource(serverID string, in protocol.Resource) protocol.Resource {
	original := in.URI
	rewritten, isUI := namespace.RewriteResourceURI(serverID, original)
	in.URI = rewritten
	in.Meta = mergeMetaPortico(in.Meta, map[string]string{
		"upstreamURI": original,
		"serverID":    serverID,
	})
	if isUI && a.apps != nil {
		a.apps.Register(&apps.App{
			URI:         rewritten,
			UpstreamURI: original,
			ServerID:    serverID,
			Name:        in.Name,
			Description: in.Description,
			MimeType:    in.MimeType,
			Annotations: rawAnnotations(in.Annotations),
		})
	}
	return in
}

func rawAnnotations(a *protocol.Annotations) json.RawMessage {
	if a == nil {
		return nil
	}
	b, _ := json.Marshal(a)
	return b
}

func (a *ResourceAggregator) applyLimit(c protocol.ResourceContent, originalURI, serverID string) protocol.ResourceContent {
	if a.limits.MaxBytesPerRead <= 0 {
		return c
	}
	size := int64(len(c.Text))
	if c.Blob != "" {
		// Estimate decoded size: base64 ratio is ~3/4.
		size = int64(len(c.Blob)) * 3 / 4
	}
	if size <= a.limits.MaxBytesPerRead {
		return c
	}
	// Truncate text or blob and emit metadata.
	artifactID := newArtifactID(serverID, originalURI)
	a.log.Warn("resource truncated by gateway limit",
		"event_type", "resource_truncated",
		"server_id", serverID,
		"original_uri", originalURI,
		"limit_bytes", a.limits.MaxBytesPerRead,
		"artifact_uri", artifactID,
		"size_bytes", size)
	if c.Text != "" {
		if int64(len(c.Text)) > a.limits.MaxBytesPerRead {
			c.Text = c.Text[:a.limits.MaxBytesPerRead]
		}
	}
	if c.Blob != "" {
		// Each base64 quartet encodes 3 bytes; trim to a quartet boundary.
		quartetCap := (a.limits.MaxBytesPerRead + 2) / 3 * 4
		if int64(len(c.Blob)) > quartetCap {
			c.Blob = c.Blob[:quartetCap]
		}
	}
	c.Meta = mergeMetaPortico(c.Meta, map[string]string{
		"truncated":    "true",
		"artifact_uri": "artifact://" + artifactID,
	})
	return c
}

func newArtifactID(serverID, original string) string {
	// Phase 3 just needs a stable, opaque id; Phase 5 will replace with a
	// hash that maps to the artifact store. base64url of a millisecond
	// timestamp + server id is plenty for now.
	now := time.Now().UnixNano()
	raw := fmt.Sprintf("%d:%s:%s", now, serverID, original)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func inferMimeType(declared, original string) string {
	if declared != "" && declared != "application/octet-stream" {
		return declared
	}
	lower := strings.ToLower(original)
	switch {
	case strings.HasSuffix(lower, ".md"):
		return "text/markdown"
	case strings.HasSuffix(lower, ".json"):
		return "application/json"
	case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		return "application/yaml"
	case strings.HasSuffix(lower, ".html"), strings.HasSuffix(lower, ".htm"):
		return "text/html"
	case strings.HasSuffix(lower, ".css"):
		return "text/css"
	case strings.HasSuffix(lower, ".js"):
		return "application/javascript"
	case strings.HasSuffix(lower, ".txt"):
		return "text/plain"
	}
	return declared
}

func isHTMLMime(m string) bool {
	if m == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(m), "text/html")
}

// mergeMetaPortico appends a top-level "portico" object containing the
// supplied attributes to a JSON `_meta` blob, leaving existing keys
// untouched.
func mergeMetaPortico(existing json.RawMessage, attrs map[string]string) json.RawMessage {
	root := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &root)
	}
	portico, _ := root["portico"].(map[string]any)
	if portico == nil {
		portico = map[string]any{}
	}
	for k, v := range attrs {
		portico[k] = v
	}
	root["portico"] = portico
	out, _ := json.Marshal(root)
	return out
}

// serversFor reuses the dispatcher's filter: every enabled server visible
// to the session's tenant.
func (a *ResourceAggregator) serversFor(ctx context.Context, sess *Session) ([]*registry.Snapshot, error) {
	if a.manager == nil {
		return nil, nil
	}
	if sess.TenantID == "" {
		return []*registry.Snapshot{}, nil
	}
	return a.manager.Servers(ctx, sess.TenantID)
}

func (a *ResourceAggregator) acquireFor(ctx context.Context, sess *Session, snap *registry.Snapshot) (southbound.Client, error) {
	if !snap.Record.Enabled {
		return nil, errors.New("server disabled")
	}
	return a.manager.Acquire(ctx, southboundmgr.AcquireRequest{
		TenantID:  sess.TenantID,
		UserID:    sess.UserID,
		SessionID: sess.ID,
		ServerID:  snap.Spec.ID,
	})
}

func (a *ResourceAggregator) lookupCache(sessionID, kind, cursor string) (json.RawMessage, bool) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	e, ok := a.cache[cacheKey{sessionID: sessionID, kind: kind, cursor: cursor}]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.body, true
}

func (a *ResourceAggregator) storeCache(sessionID, kind, cursor string, body json.RawMessage) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.cache[cacheKey{sessionID: sessionID, kind: kind, cursor: cursor}] = cacheEntry{
		body:      body,
		expiresAt: time.Now().Add(aggregatorCacheTTL),
	}
}

// Aggregator cursors are an opaque base64 encoding of a per-server
// cursor map. A real client never inspects them; the aggregator round-
// trips them on the next list call.
func decodeAggregatorCursor(c string) (map[string]string, error) {
	if c == "" {
		return map[string]string{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInvalidParams, "malformed cursor", nil)
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, protocol.NewError(protocol.ErrInvalidParams, "malformed cursor", nil)
	}
	return out, nil
}

func encodeAggregatorCursor(per map[string]string) string {
	if len(per) == 0 {
		return ""
	}
	body, _ := json.Marshal(per)
	return base64.RawURLEncoding.EncodeToString(body)
}
