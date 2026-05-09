// Package sessionbundle assembles, exports, and imports portable
// session telemetry archives.
//
// A bundle gathers everything Portico knows about one session — the
// session row, the bound catalog snapshot, every span, every audit
// event (which already includes drift + policy markers), and every
// approval — into a single deterministic artifact an operator can
// attach to a ticket, share with another instance, or load offline
// in the inspector.
//
// Phase 11 ships the JSON-shape Bundle plus tar.gz export/import.
// Optional age encryption is reserved for a follow-up; the on-the-wire
// shape carries a `manifest.encrypted` flag for forward compatibility.

package sessionbundle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/telemetry/spanstore"
)

// schemaDriftEventType matches the raw string the catalog drift
// detector emits. No exported constant exists in the audit package
// because drift originates outside it.
const schemaDriftEventType = "schema.drift"

// SchemaV1 is the manifest.schema string for the on-disk format
// shipped in Phase 11. Importers reject other values.
const SchemaV1 = "portico-bundle/v1"

// SessionRow is the persisted projection of a session. Mirrors the
// `sessions` table; we re-declare it here so this package doesn't
// pull the storage SQL driver into its surface area.
type SessionRow struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	UserID     string    `json:"user_id,omitempty"`
	SnapshotID string    `json:"snapshot_id,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

// Range is the time window the bundle covers. Inclusive on From,
// exclusive on To when populated; both zero on import-only bundles
// that didn't carry a session row.
type Range struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Counts is the materialised count of each lane in the bundle. The
// importer cross-checks these against the stream lengths so a bundle
// that was truncated mid-write can be detected even before the
// checksum verifies.
type Counts struct {
	Spans     int `json:"spans"`
	Audit     int `json:"audit"`
	Drift     int `json:"drift"`
	Policy    int `json:"policy"`
	Approvals int `json:"approvals"`
}

// ImportedRow is the listed projection of an imported_sessions row.
// The Store interface returns it; the API surfaces it directly to
// the inspector list endpoint.
type ImportedRow struct {
	BundleID         string    `json:"bundle_id"`
	TenantID         string    `json:"tenant_id"`
	SourceTenantID   string    `json:"source_tenant_id"`
	SyntheticSession string    `json:"synthetic_session_id"`
	SourceSessionID  string    `json:"source_session_id"`
	ImportedAt       time.Time `json:"imported_at"`
	Range            Range     `json:"range"`
	Counts           Counts    `json:"counts"`
	Checksum         string    `json:"checksum"`
}

// ImportedStore is the read surface the API needs over imported
// bundles. Concrete drivers live under sessionbundle/<driver>/ and
// the cmd_serve wiring picks one.
type ImportedStore interface {
	ImportedSink
	List(ctx context.Context, tenantID string) ([]ImportedRow, error)
	LoadImported(ctx context.Context, tenantID, sessionID string) (*Bundle, error)
	LoadImportedBytes(ctx context.Context, tenantID, sessionID string) ([]byte, error)
}

// Manifest is the small, eager-loaded header of every bundle. The
// exporter writes it FIRST in the tar so the importer can verify the
// schema + checksum before paying for the rest of the streams.
type Manifest struct {
	Schema      string    `json:"schema"`
	BundleID    string    `json:"bundle_id"`
	SessionID   string    `json:"session_id"`
	TenantID    string    `json:"tenant_id"`
	GeneratedAt time.Time `json:"generated_at"`
	Range       Range     `json:"range"`
	Counts      Counts    `json:"counts"`
	Checksum    string    `json:"checksum"`
	Encrypted   bool      `json:"encrypted"`
}

// Bundle is the in-memory shape Load returns. Exporter / importer use
// it directly; the inspector frontend consumes a JSON projection of it.
//
// SourceTenantID + SourceSessionID are non-canonical fields populated
// only on the importer side. They preserve "where this bundle came
// from" after the importer rewrites Manifest/Session/Audit/Span tenant
// + session ids to point at the importing tenant + synthetic session
// id. Excluded from canonical serialisation so two bundles imported
// from the same export still hash identically.
type Bundle struct {
	Manifest  Manifest         `json:"manifest"`
	Session   SessionRow       `json:"session"`
	Snapshot  json.RawMessage  `json:"snapshot,omitempty"`
	Spans     []spanstore.Span `json:"spans"`
	Audit     []audit.Event    `json:"audit"`
	Drift     []audit.Event    `json:"drift"`
	Policy    []audit.Event    `json:"policy"`
	Approvals []ApprovalRow    `json:"approvals"`

	// SourceTenantID is the tenant the bundle was originally exported
	// from. Set by the Importer; "" on a freshly-loaded live bundle.
	SourceTenantID string `json:"-"`
	// SourceSessionID is the live session id from the source instance,
	// preserved so operators can correlate an imported bundle back to
	// its origin even after the synthetic rewrite. "" on live bundles.
	SourceSessionID string `json:"-"`
}

// ApprovalRow is the bundle projection of an approval. Mirrors
// ifaces.ApprovalRecord but with strict JSON tags so on-disk format is
// stable across driver-shape changes.
type ApprovalRow struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	SessionID    string    `json:"session_id"`
	UserID       string    `json:"user_id,omitempty"`
	Tool         string    `json:"tool"`
	ArgsSummary  string    `json:"args_summary,omitempty"`
	RiskClass    string    `json:"risk_class,omitempty"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	DecidedAt    time.Time `json:"decided_at,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	MetadataJSON string    `json:"metadata_json,omitempty"`
}

// SessionReader is the persistence seam for "give me one session row".
// Lives here (not in ifaces) because the bundle is currently the only
// caller and storage.Backend doesn't expose it elsewhere.
type SessionReader interface {
	GetSession(ctx context.Context, tenantID, sessionID string) (*SessionRow, error)
}

// Loader stitches the per-store reads into a single Bundle. Construct
// once at server startup; every call to Load reuses the same store
// handles.
type Loader struct {
	Sessions  SessionReader
	Snapshots ifaces.SnapshotStore
	Audit     *audit.Store
	Approvals ifaces.ApprovalStore
	Spans     spanstore.Store
}

// ErrSessionNotFound surfaces when the session row is missing.
// Imported sessions that arrived via the importer raise this if the
// caller tried to load them as a live session.
var ErrSessionNotFound = errors.New("sessionbundle: session not found")

// Load assembles a Bundle for (tenant, session). Tenant scoping is
// enforced at every read — the loader never falls back to "all
// tenants" even when one of the underlying stores would let it.
func (l *Loader) Load(ctx context.Context, tenantID, sessionID string) (*Bundle, error) {
	if tenantID == "" || sessionID == "" {
		return nil, errors.New("sessionbundle: tenant + session required")
	}
	if l == nil || l.Sessions == nil {
		return nil, errors.New("sessionbundle: loader missing session reader")
	}

	row, err := l.Sessions.GetSession(ctx, tenantID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("sessionbundle: load session: %w", err)
	}
	if row == nil {
		return nil, ErrSessionNotFound
	}
	if row.TenantID != tenantID {
		// Belt + braces: SessionReader is supposed to filter, but the
		// bundle is too sensitive to trust that on its own.
		return nil, ErrSessionNotFound
	}

	b := &Bundle{Session: *row}

	if row.SnapshotID != "" && l.Snapshots != nil {
		snap, err := l.Snapshots.Get(ctx, row.SnapshotID)
		if err == nil && snap != nil && snap.TenantID == tenantID {
			b.Snapshot = json.RawMessage(snap.PayloadJSON)
		}
	}

	if l.Spans != nil {
		spans, err := l.Spans.QueryBySession(ctx, tenantID, sessionID)
		if err != nil {
			return nil, fmt.Errorf("sessionbundle: query spans: %w", err)
		}
		b.Spans = spans
	}

	if l.Audit != nil {
		all, err := l.loadAuditEvents(ctx, tenantID, sessionID)
		if err != nil {
			return nil, fmt.Errorf("sessionbundle: query audit: %w", err)
		}
		// Split into lanes by type so the inspector doesn't have to
		// re-walk the slice on every render.
		for _, e := range all {
			switch {
			case e.Type == schemaDriftEventType:
				b.Drift = append(b.Drift, e)
			case isPolicyEvent(e.Type):
				b.Policy = append(b.Policy, e)
			default:
				b.Audit = append(b.Audit, e)
			}
		}
	}

	if l.Approvals != nil {
		// ApprovalStore.ListPending only returns pending; we want every
		// approval the session ever raised. Audit events carry the same
		// info but the typed approval row is the source of truth for
		// status transitions, so we fetch it directly via Get on each
		// approval.id we saw in the audit lane.
		seen := map[string]struct{}{}
		for _, ev := range b.Audit {
			if id, ok := ev.Payload["approval_id"].(string); ok && id != "" {
				if _, dup := seen[id]; dup {
					continue
				}
				seen[id] = struct{}{}
				if ar, err := l.Approvals.Get(ctx, tenantID, id); err == nil && ar != nil {
					b.Approvals = append(b.Approvals, approvalRowFromRecord(ar))
				}
			}
		}
	}

	b.Manifest = Manifest{
		Schema:      SchemaV1,
		BundleID:    newBundleID(),
		SessionID:   sessionID,
		TenantID:    tenantID,
		GeneratedAt: time.Now().UTC(),
		Range: Range{
			From: row.StartedAt,
			To:   coalesceTime(row.EndedAt, time.Now().UTC()),
		},
		Counts: Counts{
			Spans:     len(b.Spans),
			Audit:     len(b.Audit),
			Drift:     len(b.Drift),
			Policy:    len(b.Policy),
			Approvals: len(b.Approvals),
		},
	}
	return b, nil
}

// loadAuditEvents pulls every event for (tenant, session) using the
// existing pagination cursor, then sorts by occurred_at ASC so the
// inspector lane reads chronologically. Cap at 10k events per session
// (sane default; same as the inspector's bundle ceiling) — anything
// past that is the audit log itself failing to rotate.
func (l *Loader) loadAuditEvents(ctx context.Context, tenantID, sessionID string) ([]audit.Event, error) {
	const pageSize = 500
	const maxEvents = 10000
	out := make([]audit.Event, 0, pageSize)
	cursor := ""
	for len(out) < maxEvents {
		page, next, err := l.Audit.Query(ctx, audit.Query{
			TenantID: tenantID,
			Limit:    pageSize,
			Cursor:   cursor,
		})
		if err != nil {
			return nil, err
		}
		for _, e := range page {
			if e.SessionID == sessionID {
				out = append(out, e)
			}
		}
		if next == "" || len(page) < pageSize {
			break
		}
		cursor = next
	}
	// Audit query returns DESC (newest first); the inspector wants ASC.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func isPolicyEvent(typ string) bool {
	// Match every policy.* event regardless of decision flavour. The
	// audit emitter currently exposes allowed/denied/dry_run/etc; new
	// ones added downstream automatically land in the policy lane.
	return strings.HasPrefix(typ, "policy.")
}

func approvalRowFromRecord(r *ifaces.ApprovalRecord) ApprovalRow {
	row := ApprovalRow{
		ID:           r.ID,
		TenantID:     r.TenantID,
		SessionID:    r.SessionID,
		UserID:       r.UserID,
		Tool:         r.Tool,
		ArgsSummary:  r.ArgsSummary,
		RiskClass:    r.RiskClass,
		Status:       r.Status,
		CreatedAt:    r.CreatedAt,
		ExpiresAt:    r.ExpiresAt,
		MetadataJSON: r.MetadataJSON,
	}
	if r.DecidedAt != nil {
		row.DecidedAt = *r.DecidedAt
	}
	return row
}

func coalesceTime(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	return a
}
