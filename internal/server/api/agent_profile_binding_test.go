package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func seedOneProfile(store *stubAgentProfileStore) {
	now := time.Now().UTC().Format(time.RFC3339)
	store.profiles["t1"] = map[string]*ifaces.AgentProfile{
		"ap_1": {TenantID: "t1", ID: "ap_1", Name: "P1", Enabled: true, CreatedAt: now, UpdatedAt: now},
	}
}

func bindReq(method, profileID, sub string, scopes ...string) *http.Request {
	r := newReq(method, "/api/agent-profiles/"+profileID+"/bindings/"+sub, nil, scopes...)
	r = withChiURLParam(r, "id", profileID)
	r = withChiURLParam(r, "sub", sub)
	return r
}

func TestPutBinding_204(t *testing.T) {
	store := newStubAgentProfileStore()
	seedOneProfile(store)
	d := Deps{AgentProfiles: store}
	w := runHandler(putAgentProfileBindingHandler(d), bindReq("PUT", "ap_1", "user-x"))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (%s)", w.Code, w.Body.String())
	}
}

func TestPutBinding_UnknownProfile_404(t *testing.T) {
	store := newStubAgentProfileStore()
	d := Deps{AgentProfiles: store}
	w := runHandler(putAgentProfileBindingHandler(d), bindReq("PUT", "nope", "user-x"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestDeleteBinding_204(t *testing.T) {
	store := newStubAgentProfileStore()
	seedOneProfile(store)
	d := Deps{AgentProfiles: store}
	w := runHandler(deleteAgentProfileBindingHandler(d), bindReq("DELETE", "ap_1", "user-x"))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

func TestBinding_NonAdmin_403(t *testing.T) {
	store := newStubAgentProfileStore()
	seedOneProfile(store)
	d := Deps{AgentProfiles: store}
	w := runHandler(putAgentProfileBindingHandler(d), bindReq("PUT", "ap_1", "user-x", "llm:invoke"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestBinding_NilStore_503(t *testing.T) {
	w := runHandler(putAgentProfileBindingHandler(Deps{}), bindReq("PUT", "ap_1", "user-x"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}
