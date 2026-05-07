package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// AuditEmitter is the slim interface api/handlers depend on. The cmd/portico
// wiring sets Deps.AuditEmitter to a *audit.FanoutEmitter; tests pass a
// *audit.SliceEmitter.
type AuditEmitter interface {
	Emit(ctx context.Context, e audit.Event)
}

// emitWithActor emits an audit event populated with tenant + actor context
// extracted from the request. Phase 9 handlers call this from their write
// paths so every CRUD touch lands in the audit table + entity_activity
// projection.
func emitWithActor(d Deps, r *http.Request, eventType, targetTenantID string, payload map[string]any) {
	if d.AuditEmitter == nil {
		return
	}
	id, _ := tenant.From(r.Context())
	tenantID := id.TenantID
	if targetTenantID != "" {
		tenantID = targetTenantID
	}
	actor := id.UserID
	if payload == nil {
		payload = map[string]any{}
	}
	if id.TenantID != "" && id.TenantID != tenantID {
		payload["acting_tenant_id"] = id.TenantID
		payload["target_tenant_id"] = tenantID
	}
	d.AuditEmitter.Emit(r.Context(), audit.Event{
		Type:       eventType,
		TenantID:   tenantID,
		UserID:     actor,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	})
}

// emitEntityEvent emits an audit event AND writes an entity_activity row
// so the per-entity Activity tab can render it. Does nothing when the
// entity activity store is not configured.
func emitEntityEvent(d Deps, r *http.Request, eventType, kind, id, summary string, payload map[string]any) {
	emitWithActor(d, r, eventType, "", payload)
	if d.EntityActivity == nil {
		return
	}
	tID, _ := tenant.From(r.Context())
	rec := &ifaces.EntityActivityRecord{
		TenantID:    tID.TenantID,
		EntityKind:  kind,
		EntityID:    id,
		EventID:     newEventID(),
		OccurredAt:  time.Now().UTC(),
		ActorUserID: tID.UserID,
		Summary:     summary,
	}
	if payload != nil {
		// Best-effort marshal; failure here shouldn't block the request.
		// The handler already logged the canonical event.
		if b := mustJSON(payload); b != nil {
			rec.DiffJSON = b
		}
	}
	_ = d.EntityActivity.Append(r.Context(), rec)
}

// emitTenantEvent is a convenience wrapper for tenant CRUD events.
func emitTenantEvent(d Deps, r *http.Request, tenantID string, isCreate bool) {
	t := audit.EventTenantUpdated
	summary := "tenant.updated"
	if isCreate {
		t = audit.EventTenantCreated
		summary = "tenant.created"
	}
	emitEntityEvent(d, r, t, "tenant", tenantID, summary,
		map[string]any{"tenant_id": tenantID})
}

func newEventID() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

func mustJSON(v any) []byte {
	if v == nil {
		return nil
	}
	b, err := jsonEncode(v)
	if err != nil {
		return nil
	}
	return b
}
