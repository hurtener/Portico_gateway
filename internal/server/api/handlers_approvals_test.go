package api

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/policy/approval"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Phase 5 approvals handlers — list, get, resolve.

func TestListApprovals_HappyPath(t *testing.T) {
	stub := newStubApprovalStore()
	_ = stub.Insert(context.Background(), &ifaces.ApprovalRecord{
		ID: "a1", TenantID: "t1", Status: "pending", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	})
	d := Deps{Approvals: stub}
	r := newReq("GET", "/v1/approvals", nil)
	w := runHandler(listApprovalsHandler(d), r)
	statusOK(t, w, 200)
}

func TestListApprovals_UnsupportedStatusFilter(t *testing.T) {
	d := Deps{Approvals: newStubApprovalStore()}
	r := newReq("GET", "/v1/approvals?status=approved", nil)
	w := runHandler(listApprovalsHandler(d), r)
	statusOK(t, w, 400)
}

func TestGetApproval_HappyPath(t *testing.T) {
	stub := newStubApprovalStore()
	_ = stub.Insert(context.Background(), &ifaces.ApprovalRecord{
		ID: "a1", TenantID: "t1", Status: "pending", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	})
	d := Deps{Approvals: stub}
	r := newReq("GET", "/v1/approvals/a1", nil)
	r = withChiURLParam(r, "id", "a1")
	w := runHandler(getApprovalHandler(d), r)
	statusOK(t, w, 200)
}

func TestGetApproval_NotFound(t *testing.T) {
	d := Deps{Approvals: newStubApprovalStore()}
	r := newReq("GET", "/v1/approvals/missing", nil)
	r = withChiURLParam(r, "id", "missing")
	w := runHandler(getApprovalHandler(d), r)
	statusOK(t, w, 404)
}

func TestResolveApproval_HappyPath(t *testing.T) {
	resolve := func(_ context.Context, tenantID, id, status, _ string) (*approval.Approval, error) {
		return &approval.Approval{ID: id, TenantID: tenantID, Status: status, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	d := Deps{ApprovalFlow: NewApprovalFlowAdapter(resolve)}
	r := newReq("POST", "/v1/approvals/a1/approve", map[string]string{"note": "ok"})
	r = withChiURLParam(r, "id", "a1")
	w := runHandler(resolveApprovalHandler(d, "approved"), r)
	statusOK(t, w, 200)
}

func TestResolveApproval_NotFound(t *testing.T) {
	resolve := func(_ context.Context, _, _, _, _ string) (*approval.Approval, error) {
		return nil, ifaces.ErrNotFound
	}
	d := Deps{ApprovalFlow: NewApprovalFlowAdapter(resolve)}
	r := newReq("POST", "/v1/approvals/missing/approve", map[string]string{})
	r = withChiURLParam(r, "id", "missing")
	w := runHandler(resolveApprovalHandler(d, "approved"), r)
	statusOK(t, w, 404)
}

// Exercise the not-yet-set adapter path so router.go's ResolveManually
// guard reports back the configuration error.
func TestApprovalFlowAdapter_NilResolveManually(t *testing.T) {
	var f *approvalFlow
	if _, err := f.ResolveManually(context.Background(), "t", "a", "approved", "u"); err == nil {
		t.Errorf("expected error from nil flow")
	}
	emptyAdapter := NewApprovalFlowAdapter(nil)
	if _, err := emptyAdapter.ResolveManually(context.Background(), "t", "a", "approved", "u"); err == nil {
		t.Errorf("expected error from empty adapter")
	}
}

// Cover toApprovalDTO / recordToApproval via getApprovalHandler — done above.
// Add a direct test for the helpers to lift their coverage from 0% to ~100%.
func TestApprovalDTOConversion(t *testing.T) {
	now := time.Now()
	rec := &ifaces.ApprovalRecord{
		ID: "a1", TenantID: "t1", Tool: "x.y", RiskClass: "write",
		Status: "pending", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		MetadataJSON: `{"reason":"because"}`,
	}
	a := recordToApproval(rec)
	if a == nil || a.ID != "a1" || a.Tool != "x.y" {
		t.Errorf("recordToApproval lost fields: %+v", a)
	}
	dto := toApprovalDTO(a)
	if dto.ID != "a1" {
		t.Errorf("toApprovalDTO mismatch: %+v", dto)
	}
	if recordToApproval(nil) != nil {
		t.Errorf("nil input should return nil")
	}
}
