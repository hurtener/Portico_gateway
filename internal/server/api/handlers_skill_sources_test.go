package api

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Phase 8 skill sources handlers — covered here so the api package
// climbs to the Phase 9 coverage gate.

func TestListSkillSources_HappyPath(t *testing.T) {
	stub := newStubSkillSources()
	_ = stub.Upsert(context.Background(), &ifaces.SkillSourceRecord{
		TenantID: "t1", Name: "git-1", Driver: "git", ConfigJSON: []byte(`{}`),
	})
	d := Deps{SkillSources: stub}
	r := newReq("GET", "/api/skill-sources", nil)
	w := runHandler(listSkillSourcesHandler(d), r)
	statusOK(t, w, 200)
}

func TestGetSkillSource_HappyPath(t *testing.T) {
	stub := newStubSkillSources()
	_ = stub.Upsert(context.Background(), &ifaces.SkillSourceRecord{
		TenantID: "t1", Name: "git-1", Driver: "git", ConfigJSON: []byte(`{}`),
	})
	d := Deps{SkillSources: stub}
	r := newReq("GET", "/api/skill-sources/git-1", nil)
	r = withChiURLParam(r, "name", "git-1")
	w := runHandler(getSkillSourceHandler(d), r)
	statusOK(t, w, 200)
}

func TestGetSkillSource_NotFound(t *testing.T) {
	d := Deps{SkillSources: newStubSkillSources()}
	r := newReq("GET", "/api/skill-sources/missing", nil)
	r = withChiURLParam(r, "name", "missing")
	w := runHandler(getSkillSourceHandler(d), r)
	statusOK(t, w, 404)
}

func TestUpsertSkillSource_Create(t *testing.T) {
	d := Deps{SkillSources: newStubSkillSources()}
	body := skillSourceDTO{Name: "git-1", Driver: "git", Config: map[string]any{"url": "x"}}
	r := newReq("POST", "/api/skill-sources", body)
	w := runHandler(upsertSkillSourceHandler(d, false), r)
	statusOK(t, w, 201)
}

func TestUpsertSkillSource_Update(t *testing.T) {
	stub := newStubSkillSources()
	_ = stub.Upsert(context.Background(), &ifaces.SkillSourceRecord{
		TenantID: "t1", Name: "git-1", Driver: "git", ConfigJSON: []byte(`{}`),
	})
	d := Deps{SkillSources: stub}
	body := skillSourceDTO{Driver: "git", Config: map[string]any{"url": "y"}}
	r := newReq("PUT", "/api/skill-sources/git-1", body)
	r = withChiURLParam(r, "name", "git-1")
	w := runHandler(upsertSkillSourceHandler(d, true), r)
	statusOK(t, w, 200)
}

func TestUpsertSkillSource_BadJSON(t *testing.T) {
	d := Deps{SkillSources: newStubSkillSources()}
	r := newReq("POST", "/api/skill-sources", nil)
	r.Body = httpReadCloser("not-json")
	w := runHandler(upsertSkillSourceHandler(d, false), r)
	statusOK(t, w, 400)
}

func TestUpsertSkillSource_MissingFields(t *testing.T) {
	d := Deps{SkillSources: newStubSkillSources()}
	body := skillSourceDTO{Name: "", Driver: ""}
	r := newReq("POST", "/api/skill-sources", body)
	w := runHandler(upsertSkillSourceHandler(d, false), r)
	statusOK(t, w, 400)
}

func TestDeleteSkillSource_HappyPath(t *testing.T) {
	stub := newStubSkillSources()
	_ = stub.Upsert(context.Background(), &ifaces.SkillSourceRecord{
		TenantID: "t1", Name: "git-1", Driver: "git", ConfigJSON: []byte(`{}`),
	})
	d := Deps{SkillSources: stub}
	r := newReq("DELETE", "/api/skill-sources/git-1", nil)
	r = withChiURLParam(r, "name", "git-1")
	w := runHandler(deleteSkillSourceHandler(d), r)
	statusOK(t, w, 204)
}

func TestDeleteSkillSource_NotFound(t *testing.T) {
	d := Deps{SkillSources: newStubSkillSources()}
	r := newReq("DELETE", "/api/skill-sources/missing", nil)
	r = withChiURLParam(r, "name", "missing")
	w := runHandler(deleteSkillSourceHandler(d), r)
	statusOK(t, w, 404)
}

func TestRefreshSkillSource_HappyPath(t *testing.T) {
	stub := newStubSkillSources()
	_ = stub.Upsert(context.Background(), &ifaces.SkillSourceRecord{
		TenantID: "t1", Name: "git-1", Driver: "git", ConfigJSON: []byte(`{}`),
	})
	d := Deps{SkillSources: stub}
	r := newReq("POST", "/api/skill-sources/git-1/refresh", nil)
	r = withChiURLParam(r, "name", "git-1")
	w := runHandler(refreshSkillSourceHandler(d), r)
	statusOK(t, w, 200)
}

func TestRefreshSkillSource_StoreError(t *testing.T) {
	d := Deps{SkillSources: newStubSkillSources()}
	r := newReq("POST", "/api/skill-sources/missing/refresh", nil)
	r = withChiURLParam(r, "name", "missing")
	w := runHandler(refreshSkillSourceHandler(d), r)
	statusOK(t, w, 500)
}

func TestListSkillSourcePacks_HappyPath(t *testing.T) {
	stub := newStubSkillSources()
	_ = stub.Upsert(context.Background(), &ifaces.SkillSourceRecord{
		TenantID: "t1", Name: "git-1", Driver: "git", ConfigJSON: []byte(`{}`),
	})
	d := Deps{SkillSources: stub}
	r := newReq("GET", "/api/skill-sources/git-1/packs", nil)
	r = withChiURLParam(r, "name", "git-1")
	w := runHandler(listSkillSourcePacksHandler(d), r)
	statusOK(t, w, 200)
}

func TestListSkillSourcePacks_StoreError(t *testing.T) {
	d := Deps{SkillSources: newStubSkillSources()}
	r := newReq("GET", "/api/skill-sources/missing/packs", nil)
	r = withChiURLParam(r, "name", "missing")
	w := runHandler(listSkillSourcePacksHandler(d), r)
	statusOK(t, w, 500)
}

// skillSourceToDTO conversion: spot-check a fully-populated record.
func TestSkillSourceToDTO(t *testing.T) {
	now := time.Now()
	rec := &ifaces.SkillSourceRecord{
		TenantID: "t1", Name: "git-1", Driver: "git",
		ConfigJSON: []byte(`{"url":"x"}`), Enabled: true,
		Priority: 100, RefreshSeconds: 300,
		CreatedAt: now, UpdatedAt: now, LastRefreshAt: &now,
	}
	dto := skillSourceToDTO(rec)
	if dto.Name != "git-1" || dto.Driver != "git" {
		t.Errorf("unexpected dto: %+v", dto)
	}
}
