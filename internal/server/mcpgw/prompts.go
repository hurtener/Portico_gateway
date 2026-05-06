package mcpgw

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/hurtener/Portico_gateway/internal/catalog/namespace"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/registry"
)

// PromptAggregator implements `prompts/list` and `prompts/get` over the
// downstream fleet. Names are namespaced as `{server}.{name}`; the
// aggregator splits and routes on `prompts/get`. The Phase 4 skill
// surface contributes additional prompts via the SkillProvider seam
// configured on the shared ResourceAggregator.
type PromptAggregator struct {
	log     *slog.Logger
	manager clientFleet
	timeout time.Duration

	// shares the resource aggregator's cache plumbing for symmetry; the
	// list-changed mux invalidates by sessionID. cache.skills is also
	// the SkillProvider used by Phase 4.
	cache *ResourceAggregator
}

// NewPromptAggregator constructs an aggregator. Pass the same
// ResourceAggregator instance so cache invalidation flows through one
// surface.
func NewPromptAggregator(m clientFleet, cache *ResourceAggregator, log *slog.Logger) *PromptAggregator {
	if log == nil {
		log = slog.Default()
	}
	return &PromptAggregator{
		log:     log,
		manager: m,
		timeout: 5 * time.Second,
		cache:   cache,
	}
}

// ListAll fans out prompts/list and returns a single namespaced list.
func (a *PromptAggregator) ListAll(ctx context.Context, sess *Session, cursor string) (*protocol.ListPromptsResult, error) {
	if a.cache != nil {
		if cached, ok := a.cache.lookupCache(sess.ID, "prompts", cursor); ok {
			var res protocol.ListPromptsResult
			if err := json.Unmarshal(cached, &res); err == nil {
				return &res, nil
			}
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
		serverID string
		prompts  []protocol.Prompt
		next     string
		err      error
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
			items, next, err := c.ListPrompts(listCtx, per[s.Spec.ID])
			if err != nil && protocol.IsMethodNotFound(err) {
				resCh <- result{serverID: s.Spec.ID}
				return
			}
			resCh <- result{serverID: s.Spec.ID, prompts: items, next: next, err: err}
		}()
	}

	combined := make([]protocol.Prompt, 0)
	nextPer := make(map[string]string)
	for i := 0; i < len(servers); i++ {
		r := <-resCh
		if r.err != nil {
			a.log.Warn("prompts/list partial failure",
				"server_id", r.serverID, "err", r.err)
			continue
		}
		for _, p := range r.prompts {
			p.Name = namespace.RewritePromptName(r.serverID, p.Name)
			combined = append(combined, p)
		}
		if r.next != "" {
			nextPer[r.serverID] = r.next
		}
	}
	sort.Slice(combined, func(i, j int) bool { return combined[i].Name < combined[j].Name })

	// Phase 4: append skill prompts. Skill ids are dotted, so the
	// names appear like `github.code-review.review_pr` and naturally
	// sort below `<server>.<tool>` style names if those happened to
	// share a leading namespace.
	if a.cache != nil && a.cache.skills != nil {
		plan, ents := a.cache.resolveTenant(sess.TenantID)
		skillPrompts, err := a.cache.skills.ListPrompts(listCtx, sess.TenantID, sess.ID, plan, ents)
		if err != nil {
			a.log.Warn("skills/prompts partial failure", "err", err)
		} else {
			combined = append(combined, skillPrompts...)
			sort.Slice(combined, func(i, j int) bool { return combined[i].Name < combined[j].Name })
		}
	}

	out := &protocol.ListPromptsResult{Prompts: combined}
	if encoded := encodeAggregatorCursor(nextPer); encoded != "" {
		out.NextCursor = encoded
	}
	if a.cache != nil {
		body, _ := json.Marshal(out)
		a.cache.storeCache(sess.ID, "prompts", cursor, body)
	}
	return out, nil
}

// Get strips the namespace prefix and routes to the origin server, or
// to the skills runtime when the name is owned by a skill (Phase 4).
func (a *PromptAggregator) Get(ctx context.Context, sess *Session, name string, args map[string]string) (*protocol.GetPromptResult, error) {
	// Phase 4: skill prompts are namespaced `{skillID}.{prompt}` and
	// the skill id itself contains a dot. RestorePromptName splits on
	// the first dot, so `github.code-review.review_pr` would route to
	// the `github` server. Probe the catalog first to detect the
	// skill case.
	if a.cache != nil && a.cache.skills != nil {
		if res, err := a.cache.skills.GetPrompt(ctx, sess.TenantID, sess.ID, name, args); err == nil {
			return res, nil
		}
	}
	serverID, original, ok := namespace.RestorePromptName(name)
	if !ok {
		return nil, protocol.NewError(protocol.ErrInvalidParams, "prompt name must be qualified as <server>.<name>", map[string]string{"name": name})
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
	res, err := client.GetPrompt(ctx, original, args)
	if err != nil {
		var pe *protocol.Error
		if errors.As(err, &pe) {
			return nil, pe
		}
		return nil, protocol.NewError(protocol.ErrUpstreamUnavailable, err.Error(), map[string]string{"server_id": serverID})
	}
	return res, nil
}

func (a *PromptAggregator) serversFor(ctx context.Context, sess *Session) ([]*registry.Snapshot, error) {
	if a.manager == nil {
		return nil, nil
	}
	if sess.TenantID == "" {
		return []*registry.Snapshot{}, nil
	}
	return a.manager.Servers(ctx, sess.TenantID)
}

func (a *PromptAggregator) acquireFor(ctx context.Context, sess *Session, snap *registry.Snapshot) (southbound.Client, error) {
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
