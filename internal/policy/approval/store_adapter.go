package approval

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// NewStorageAdapter wraps an ifaces.ApprovalStore so the approval Flow can
// drive it without importing the storage interface package directly.
func NewStorageAdapter(s ifaces.ApprovalStore) Store {
	return &storageAdapter{s: s}
}

type storageAdapter struct {
	s ifaces.ApprovalStore
}

func (a *storageAdapter) Insert(ctx context.Context, ap *Approval) error {
	if a == nil || a.s == nil {
		return errors.New("approval: storage adapter not configured")
	}
	rec := approvalToRecord(ap)
	return a.s.Insert(ctx, rec)
}

func (a *storageAdapter) UpdateStatus(ctx context.Context, tenantID, id, status string, decidedAt time.Time) error {
	if a == nil || a.s == nil {
		return errors.New("approval: storage adapter not configured")
	}
	return a.s.UpdateStatus(ctx, tenantID, id, status, decidedAt)
}

func (a *storageAdapter) Get(ctx context.Context, tenantID, id string) (*Approval, error) {
	rec, err := a.s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return recordToApproval(rec), nil
}

func (a *storageAdapter) ListPending(ctx context.Context, tenantID string) ([]*Approval, error) {
	recs, err := a.s.ListPending(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]*Approval, 0, len(recs))
	for _, r := range recs {
		out = append(out, recordToApproval(r))
	}
	return out, nil
}

func (a *storageAdapter) ExpireOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	return a.s.ExpireOlderThan(ctx, cutoff)
}

func approvalToRecord(a *Approval) *ifaces.ApprovalRecord {
	if a == nil {
		return nil
	}
	meta := ""
	if len(a.Metadata) > 0 {
		if b, err := json.Marshal(a.Metadata); err == nil {
			meta = string(b)
		}
	}
	return &ifaces.ApprovalRecord{
		ID:           a.ID,
		TenantID:     a.TenantID,
		SessionID:    a.SessionID,
		UserID:       a.UserID,
		Tool:         a.Tool,
		ArgsSummary:  a.ArgsSummary,
		RiskClass:    a.RiskClass,
		Status:       a.Status,
		CreatedAt:    a.CreatedAt,
		DecidedAt:    a.DecidedAt,
		ExpiresAt:    a.ExpiresAt,
		MetadataJSON: meta,
	}
}

func recordToApproval(r *ifaces.ApprovalRecord) *Approval {
	if r == nil {
		return nil
	}
	a := &Approval{
		ID:          r.ID,
		TenantID:    r.TenantID,
		SessionID:   r.SessionID,
		UserID:      r.UserID,
		Tool:        r.Tool,
		ArgsSummary: r.ArgsSummary,
		RiskClass:   r.RiskClass,
		Status:      r.Status,
		CreatedAt:   r.CreatedAt,
		DecidedAt:   r.DecidedAt,
		ExpiresAt:   r.ExpiresAt,
	}
	if r.MetadataJSON != "" {
		_ = json.Unmarshal([]byte(r.MetadataJSON), &a.Metadata)
	}
	return a
}
