package snapshots

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/telemetry"
)

// ErrNotFound is the canonical "no such snapshot" sentinel.
var ErrNotFound = errors.New("snapshots: not found")

// CatalogProbe is the seam the service uses to read the live catalog
// during Create. Implemented by mcpgw.PolicyPipeline-adjacent helpers in
// production; tests pass a fake.
type CatalogProbe interface {
	// ListTools returns the per-server tools, namespaced.
	ListTools(ctx context.Context, tenantID, sessionID string) ([]NamespacedTool, error)
	// ListResources returns namespaced resources.
	ListResources(ctx context.Context, tenantID, sessionID string) ([]NamespacedResource, error)
	// ListPrompts returns namespaced prompts.
	ListPrompts(ctx context.Context, tenantID, sessionID string) ([]NamespacedPrompt, error)
	// ServerInfos returns the per-server transport/health summary.
	ServerInfos(ctx context.Context, tenantID string) ([]ServerInfo, error)
	// SkillInfos returns the per-session skill enablement summary.
	SkillInfos(ctx context.Context, tenantID, sessionID string) ([]SkillInfo, error)
	// CredentialInfos returns the per-server strategy + secret-ref names.
	CredentialInfos(ctx context.Context, tenantID string) ([]CredentialInfo, error)
	// PoliciesInfo returns the resolved per-tenant policy summary.
	PoliciesInfo(ctx context.Context, tenantID string) PoliciesInfo
	// ResolveToolPolicy returns (riskClass, requiresApproval, skillID)
	// for a namespaced tool — the policy engine's lookup without running
	// the full dispatcher chain.
	ResolveToolPolicy(ctx context.Context, tenantID, sessionID, qualifiedName string) (riskClass string, requiresApproval bool, skillID string)
}

// NamespacedTool is the probe's per-tool answer. Hash is computed by the
// service after probe response.
type NamespacedTool struct {
	NamespacedName string
	ServerID       string
	Tool           protocol.Tool
}

// NamespacedResource carries the namespaced URI alongside the upstream URI.
type NamespacedResource struct {
	URI         string
	UpstreamURI string
	ServerID    string
	MIMEType    string
}

// NamespacedPrompt is the probe's per-prompt answer.
type NamespacedPrompt struct {
	NamespacedName string
	ServerID       string
	Arguments      []protocol.PromptArgument
}

// Store is the persistence seam. The SQLite-backed implementation lives
// in internal/storage/sqlite/snapshot_store.go.
type Store interface {
	Insert(ctx context.Context, s *Snapshot) error
	Get(ctx context.Context, id string) (*Snapshot, error)
	List(ctx context.Context, tenantID string, q ListQuery) ([]*Snapshot, string, error)
	StampSession(ctx context.Context, sessionID, snapshotID string) error
	UpsertFingerprint(ctx context.Context, tenantID, serverID, hash string, toolsCount int) error
	LatestFingerprint(ctx context.Context, tenantID, serverID string) (string, error)
	// ActiveSessions returns the (sessionID, tenantID, snapshotID) tuples
	// the drift detector consults. Filters to sessions with no ended_at.
	ActiveSessions(ctx context.Context, since time.Time) ([]ActiveSession, error)
}

// ListQuery filters Service.List.
type ListQuery struct {
	Since  time.Time
	Until  time.Time
	Limit  int
	Cursor string
}

// ActiveSession is the projection the drift detector reads.
type ActiveSession struct {
	SessionID  string
	TenantID   string
	SnapshotID string
	StartedAt  time.Time
}

// Service builds, stores, and diffs snapshots. The drift detector and
// dispatcher both consume it.
type Service struct {
	store   Store
	probe   CatalogProbe
	emitter audit.Emitter
	log     *slog.Logger
}

// NewService wires the snapshot service.
func NewService(store Store, probe CatalogProbe, emitter audit.Emitter, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	if emitter == nil {
		emitter = audit.NopEmitter{}
	}
	return &Service{store: store, probe: probe, emitter: emitter, log: log}
}

// Create freezes the current catalog into a snapshot and persists it.
// Stamp the result onto the session row so audit events can reference it.
func (s *Service) Create(ctx context.Context, tenantID, sessionID string) (*Snapshot, error) {
	if s == nil || s.store == nil || s.probe == nil {
		return nil, errors.New("snapshots: service not configured")
	}
	ctx, span := telemetry.StartSpan(ctx, telemetry.SpanSnapshotCreate,
		telemetry.String(telemetry.AttrTenantID, tenantID),
		telemetry.String(telemetry.AttrSessionID, sessionID),
	)
	defer span.End()
	now := time.Now().UTC()
	snap := &Snapshot{
		ID:        newSnapshotID(now),
		TenantID:  tenantID,
		SessionID: sessionID,
		CreatedAt: now,
	}

	servers, err := s.probe.ServerInfos(ctx, tenantID)
	if err != nil {
		snap.Warnings = append(snap.Warnings, "server_infos: "+err.Error())
	}

	tools, err := s.probe.ListTools(ctx, tenantID, sessionID)
	if err != nil {
		snap.Warnings = append(snap.Warnings, "list_tools: "+err.Error())
	}
	resources, err := s.probe.ListResources(ctx, tenantID, sessionID)
	if err != nil {
		snap.Warnings = append(snap.Warnings, "list_resources: "+err.Error())
	}
	prompts, err := s.probe.ListPrompts(ctx, tenantID, sessionID)
	if err != nil {
		snap.Warnings = append(snap.Warnings, "list_prompts: "+err.Error())
	}
	skills, err := s.probe.SkillInfos(ctx, tenantID, sessionID)
	if err != nil {
		snap.Warnings = append(snap.Warnings, "skill_infos: "+err.Error())
	}
	creds, err := s.probe.CredentialInfos(ctx, tenantID)
	if err != nil {
		snap.Warnings = append(snap.Warnings, "credential_infos: "+err.Error())
	}

	snap.Servers = servers
	snap.Resources = resourcesToInfo(resources)
	snap.Prompts = promptsToInfo(prompts)
	snap.Skills = skills
	snap.Credentials = creds
	snap.Policies = s.probe.PoliciesInfo(ctx, tenantID)
	snap.Tools = make([]ToolInfo, 0, len(tools))
	for _, t := range tools {
		risk, requires, skillID := s.probe.ResolveToolPolicy(ctx, tenantID, sessionID, t.NamespacedName)
		ti := ToolInfo{
			NamespacedName:   t.NamespacedName,
			ServerID:         t.ServerID,
			Description:      t.Tool.Description,
			InputSchema:      t.Tool.InputSchema,
			Annotations:      t.Tool.Annotations,
			RiskClass:        risk,
			RequiresApproval: requires,
			SkillID:          skillID,
		}
		ti.Hash = ToolFingerprint(ti)
		snap.Tools = append(snap.Tools, ti)
	}

	// Per-server tools-list fingerprint (independent of the policy
	// resolution; drift detector compares only the upstream-visible
	// shape).
	for i := range snap.Servers {
		snap.Servers[i].SchemaHash = ServerToolsFingerprint(serverTools(tools, snap.Servers[i].ID))
	}

	snap.OverallHash = OverallFingerprint(snap)

	if err := s.store.Insert(ctx, snap); err != nil {
		return nil, err
	}
	if sessionID != "" {
		if err := s.store.StampSession(ctx, sessionID, snap.ID); err != nil {
			s.log.Warn("snapshots: stamp session failed", "err", err)
		}
	}
	for _, sv := range snap.Servers {
		_ = s.store.UpsertFingerprint(ctx, tenantID, sv.ID, sv.SchemaHash, countServerTools(tools, sv.ID))
	}
	s.emitter.Emit(ctx, audit.Event{
		Type:      "snapshot.created",
		TenantID:  tenantID,
		SessionID: sessionID,
		Payload: map[string]any{
			"snapshot_id":  snap.ID,
			"overall_hash": snap.OverallHash,
			"tools_count":  len(snap.Tools),
			"servers":      len(snap.Servers),
		},
	})
	span.SetAttributes(
		telemetry.String(telemetry.AttrSnapshotID, snap.ID),
		telemetry.Int(telemetry.AttrSnapshotServers, len(snap.Servers)),
		telemetry.Int(telemetry.AttrSnapshotToolsCount, len(snap.Tools)),
	)
	return snap, nil
}

// Get fetches a snapshot by id.
func (s *Service) Get(ctx context.Context, id string) (*Snapshot, error) {
	return s.store.Get(ctx, id)
}

// List returns recent snapshots for a tenant.
func (s *Service) List(ctx context.Context, tenantID string, q ListQuery) ([]*Snapshot, string, error) {
	return s.store.List(ctx, tenantID, q)
}

// Diff returns the structured difference between two snapshots.
func (s *Service) Diff(ctx context.Context, idA, idB string) (*Diff, error) {
	a, err := s.store.Get(ctx, idA)
	if err != nil {
		return nil, err
	}
	b, err := s.store.Get(ctx, idB)
	if err != nil {
		return nil, err
	}
	return DiffSnapshots(a, b), nil
}

// EmitterFor exposes the audit emitter so the drift detector can share
// its fan-out without re-importing.
func (s *Service) EmitterFor() audit.Emitter { return s.emitter }

// StoreFor exposes the underlying store for the drift detector. Internal
// to the package; not intended for general use.
func (s *Service) StoreFor() Store { return s.store }

// LogFor exposes the slog logger.
func (s *Service) LogFor() *slog.Logger { return s.log }

func resourcesToInfo(in []NamespacedResource) []ResourceInfo {
	out := make([]ResourceInfo, 0, len(in))
	for _, r := range in {
		out = append(out, ResourceInfo{
			URI:         r.URI,
			UpstreamURI: r.UpstreamURI,
			ServerID:    r.ServerID,
			MIMEType:    r.MIMEType,
		})
	}
	return out
}

func promptsToInfo(in []NamespacedPrompt) []PromptInfo {
	out := make([]PromptInfo, 0, len(in))
	for _, p := range in {
		out = append(out, PromptInfo{
			NamespacedName: p.NamespacedName,
			ServerID:       p.ServerID,
			Arguments:      p.Arguments,
		})
	}
	return out
}

func serverTools(all []NamespacedTool, serverID string) []protocol.Tool {
	out := make([]protocol.Tool, 0)
	for _, t := range all {
		if t.ServerID == serverID {
			out = append(out, t.Tool)
		}
	}
	return out
}

func countServerTools(all []NamespacedTool, serverID string) int {
	n := 0
	for _, t := range all {
		if t.ServerID == serverID {
			n++
		}
	}
	return n
}

// ifaces re-export so the cmd/portico wiring file can hand the SQLite
// driver's snapshot-store implementation directly to NewService without
// re-importing storage/ifaces.
type StorageBackend = ifaces.Backend
