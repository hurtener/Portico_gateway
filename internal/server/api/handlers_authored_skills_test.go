package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	"github.com/hurtener/Portico_gateway/internal/skills/source/authored"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Phase 8 authored skills handlers — covered here so the api package
// climbs to the Phase 9 coverage gate.

type stubAuthoredSkills struct {
	mu       sync.Mutex
	records  map[string]map[string]authored.Authored // tenant → skill@version → rec
	active   map[string]map[string]string            // tenant → skill → version
	failNext bool
}

func newStubAuthoredSkills() *stubAuthoredSkills {
	return &stubAuthoredSkills{
		records: map[string]map[string]authored.Authored{},
		active:  map[string]map[string]string{},
	}
}

func authKey(skill, version string) string { return skill + "@" + version }

func (s *stubAuthoredSkills) ListAuthored(_ context.Context, tenantID string) ([]authored.Authored, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []authored.Authored{}
	for _, r := range s.records[tenantID] {
		out = append(out, r)
	}
	return out, nil
}

func (s *stubAuthoredSkills) GetAuthored(_ context.Context, tenantID, skillID, version string) (*authored.Authored, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[tenantID][authKey(skillID, version)]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	cp := r
	return &cp, nil
}

func (s *stubAuthoredSkills) History(_ context.Context, tenantID, skillID string) ([]authored.Authored, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []authored.Authored{}
	for _, r := range s.records[tenantID] {
		if r.SkillID == skillID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *stubAuthoredSkills) GetActive(_ context.Context, tenantID, skillID string) (*authored.Authored, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.active[tenantID][skillID]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	r, ok := s.records[tenantID][authKey(skillID, v)]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	cp := r
	return &cp, nil
}

func (s *stubAuthoredSkills) CreateDraft(_ context.Context, tenantID, userID string, m manifest.Manifest, files []authored.File) (*authored.Authored, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext {
		s.failNext = false
		return nil, ifaces.ErrNotFound
	}
	rec := authored.Authored{
		SkillID:      m.ID,
		Version:      m.Version,
		Status:       "draft",
		Manifest:     m,
		Files:        files,
		ManifestRaw:  []byte(`{"id":"` + m.ID + `","version":"` + m.Version + `"}`),
		Checksum:     "deadbeef",
		AuthorUserID: userID,
		CreatedAt:    time.Now().UTC(),
	}
	if _, ok := s.records[tenantID]; !ok {
		s.records[tenantID] = map[string]authored.Authored{}
	}
	s.records[tenantID][authKey(m.ID, m.Version)] = rec
	return &rec, nil
}

func (s *stubAuthoredSkills) UpdateDraft(_ context.Context, tenantID, skillID, version, userID string, m manifest.Manifest, files []authored.File) (*authored.Authored, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[tenantID][authKey(skillID, version)]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	rec.Manifest = m
	rec.Files = files
	rec.AuthorUserID = userID
	s.records[tenantID][authKey(skillID, version)] = rec
	cp := rec
	return &cp, nil
}

func (s *stubAuthoredSkills) Publish(_ context.Context, tenantID, skillID, version string) (*authored.Authored, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[tenantID][authKey(skillID, version)]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	rec.Status = "published"
	now := time.Now().UTC()
	rec.PublishedAt = &now
	s.records[tenantID][authKey(skillID, version)] = rec
	if _, ok := s.active[tenantID]; !ok {
		s.active[tenantID] = map[string]string{}
	}
	s.active[tenantID][skillID] = version
	cp := rec
	return &cp, nil
}

func (s *stubAuthoredSkills) Archive(_ context.Context, tenantID, skillID, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[tenantID][authKey(skillID, version)]
	if !ok {
		return ifaces.ErrNotFound
	}
	rec.Status = "archived"
	s.records[tenantID][authKey(skillID, version)] = rec
	return nil
}

func (s *stubAuthoredSkills) DeleteDraft(_ context.Context, tenantID, skillID, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[tenantID][authKey(skillID, version)]; !ok {
		return ifaces.ErrNotFound
	}
	delete(s.records[tenantID], authKey(skillID, version))
	return nil
}

// validManifestYAML returns a minimally-valid Skill Pack manifest body.
func validManifestYAML(id, version string) string {
	return "id: " + id + "\n" +
		"title: Test\n" +
		"version: " + version + "\n" +
		"spec: skills/v1\n" +
		"instructions: |\n  hello\n" +
		"binding:\n" +
		"  server_dependencies: []\n"
}

func TestListAuthored_HappyPath(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	r := newReq("GET", "/api/skills/authored", nil)
	w := runHandler(listAuthoredHandler(d), r)
	statusOK(t, w, 200)
}

func TestCreateAuthored_HappyPath(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	body := authoredRequest{
		Manifest: validManifestYAML("foo.bar", "1.0.0"),
		Files:    []authoredFileDTO{{RelPath: "SKILL.md", Body: "# Hi"}},
	}
	r := newReq("POST", "/api/skills/authored", body)
	w := runHandler(createAuthoredHandler(d), r)
	statusOK(t, w, 201)
}

func TestCreateAuthored_BadManifest(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	body := authoredRequest{Manifest: ":::: not yaml ::::"}
	r := newReq("POST", "/api/skills/authored", body)
	w := runHandler(createAuthoredHandler(d), r)
	statusOK(t, w, 400)
}

func TestCreateAuthored_EmptyManifest(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	body := authoredRequest{Manifest: ""}
	r := newReq("POST", "/api/skills/authored", body)
	w := runHandler(createAuthoredHandler(d), r)
	statusOK(t, w, 400)
}

func TestCreateAuthored_BadJSON(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	r := newReq("POST", "/api/skills/authored", nil)
	r.Body = httpReadCloser("not-json")
	w := runHandler(createAuthoredHandler(d), r)
	statusOK(t, w, 400)
}

func TestCreateAuthored_NoBody(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	r := newReq("POST", "/api/skills/authored", nil)
	r.Body = nil
	w := runHandler(createAuthoredHandler(d), r)
	statusOK(t, w, 400)
}

func TestUpdateAuthored_HappyPath(t *testing.T) {
	stub := newStubAuthoredSkills()
	_, _ = stub.CreateDraft(context.Background(), "t1", "tester",
		manifest.Manifest{ID: "foo.bar", Version: "1.0.0"}, nil)
	d := Deps{AuthoredSkills: stub}
	body := authoredRequest{Manifest: validManifestYAML("foo.bar", "1.0.0")}
	r := newReq("PUT", "/api/skills/authored/foo.bar/versions/1.0.0", body)
	r = withChiURLParam(r, "id", "foo.bar")
	r = withChiURLParam(r, "v", "1.0.0")
	w := runHandler(updateAuthoredHandler(d), r)
	statusOK(t, w, 200)
}

func TestUpdateAuthored_NotFound(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	body := authoredRequest{Manifest: validManifestYAML("x.y", "1.0.0")}
	r := newReq("PUT", "/api/skills/authored/x.y/versions/1.0.0", body)
	r = withChiURLParam(r, "id", "x.y")
	r = withChiURLParam(r, "v", "1.0.0")
	w := runHandler(updateAuthoredHandler(d), r)
	statusOK(t, w, 404)
}

func TestUpdateAuthored_BadManifest(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	body := authoredRequest{Manifest: ":::: not yaml ::::"}
	r := newReq("PUT", "/api/skills/authored/x/versions/1.0.0", body)
	r = withChiURLParam(r, "id", "x")
	r = withChiURLParam(r, "v", "1.0.0")
	w := runHandler(updateAuthoredHandler(d), r)
	statusOK(t, w, 400)
}

func TestPublishAuthored_HappyPath(t *testing.T) {
	stub := newStubAuthoredSkills()
	_, _ = stub.CreateDraft(context.Background(), "t1", "tester",
		manifest.Manifest{ID: "foo.bar", Version: "1.0.0"}, nil)
	d := Deps{AuthoredSkills: stub}
	r := newReq("POST", "/api/skills/authored/foo.bar/versions/1.0.0/publish", nil)
	r = withChiURLParam(r, "id", "foo.bar")
	r = withChiURLParam(r, "v", "1.0.0")
	w := runHandler(publishAuthoredHandler(d), r)
	statusOK(t, w, 200)
}

func TestPublishAuthored_NotFound(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	r := newReq("POST", "/api/skills/authored/x/versions/1.0.0/publish", nil)
	r = withChiURLParam(r, "id", "x")
	r = withChiURLParam(r, "v", "1.0.0")
	w := runHandler(publishAuthoredHandler(d), r)
	statusOK(t, w, 404)
}

func TestArchiveAuthored_HappyPath(t *testing.T) {
	stub := newStubAuthoredSkills()
	_, _ = stub.CreateDraft(context.Background(), "t1", "tester",
		manifest.Manifest{ID: "foo.bar", Version: "1.0.0"}, nil)
	d := Deps{AuthoredSkills: stub}
	r := newReq("POST", "/api/skills/authored/foo.bar/versions/1.0.0/archive", nil)
	r = withChiURLParam(r, "id", "foo.bar")
	r = withChiURLParam(r, "v", "1.0.0")
	w := runHandler(archiveAuthoredHandler(d), r)
	statusOK(t, w, 204)
}

func TestArchiveAuthored_NotFound(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	r := newReq("POST", "/api/skills/authored/x/versions/1.0.0/archive", nil)
	r = withChiURLParam(r, "id", "x")
	r = withChiURLParam(r, "v", "1.0.0")
	w := runHandler(archiveAuthoredHandler(d), r)
	statusOK(t, w, 404)
}

func TestDeleteAuthored_HappyPath(t *testing.T) {
	stub := newStubAuthoredSkills()
	_, _ = stub.CreateDraft(context.Background(), "t1", "tester",
		manifest.Manifest{ID: "foo.bar", Version: "1.0.0"}, nil)
	d := Deps{AuthoredSkills: stub}
	r := newReq("DELETE", "/api/skills/authored/foo.bar/versions/1.0.0", nil)
	r = withChiURLParam(r, "id", "foo.bar")
	r = withChiURLParam(r, "v", "1.0.0")
	w := runHandler(deleteAuthoredDraftHandler(d), r)
	statusOK(t, w, 204)
}

func TestDeleteAuthored_NotFound(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	r := newReq("DELETE", "/api/skills/authored/x/versions/1.0.0", nil)
	r = withChiURLParam(r, "id", "x")
	r = withChiURLParam(r, "v", "1.0.0")
	w := runHandler(deleteAuthoredDraftHandler(d), r)
	statusOK(t, w, 404)
}

func TestGetAuthoredActive_HappyPath(t *testing.T) {
	stub := newStubAuthoredSkills()
	_, _ = stub.CreateDraft(context.Background(), "t1", "tester",
		manifest.Manifest{ID: "foo.bar", Version: "1.0.0"}, nil)
	_, _ = stub.Publish(context.Background(), "t1", "foo.bar", "1.0.0")
	d := Deps{AuthoredSkills: stub}
	r := newReq("GET", "/api/skills/authored/foo.bar", nil)
	r = withChiURLParam(r, "id", "foo.bar")
	w := runHandler(getAuthoredActiveHandler(d), r)
	statusOK(t, w, 200)
}

func TestGetAuthoredActive_NotFound(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	r := newReq("GET", "/api/skills/authored/x", nil)
	r = withChiURLParam(r, "id", "x")
	w := runHandler(getAuthoredActiveHandler(d), r)
	statusOK(t, w, 404)
}

func TestHistoryAuthored_HappyPath(t *testing.T) {
	stub := newStubAuthoredSkills()
	_, _ = stub.CreateDraft(context.Background(), "t1", "tester",
		manifest.Manifest{ID: "foo.bar", Version: "1.0.0"}, nil)
	d := Deps{AuthoredSkills: stub}
	r := newReq("GET", "/api/skills/authored/foo.bar/versions", nil)
	r = withChiURLParam(r, "id", "foo.bar")
	w := runHandler(historyAuthoredHandler(d), r)
	statusOK(t, w, 200)
}

func TestGetAuthoredVersion_HappyPath(t *testing.T) {
	stub := newStubAuthoredSkills()
	_, _ = stub.CreateDraft(context.Background(), "t1", "tester",
		manifest.Manifest{ID: "foo.bar", Version: "1.0.0"}, nil)
	d := Deps{AuthoredSkills: stub}
	r := newReq("GET", "/api/skills/authored/foo.bar/versions/1.0.0", nil)
	r = withChiURLParam(r, "id", "foo.bar")
	r = withChiURLParam(r, "v", "1.0.0")
	w := runHandler(getAuthoredVersionHandler(d), r)
	statusOK(t, w, 200)
}

func TestGetAuthoredVersion_NotFound(t *testing.T) {
	stub := newStubAuthoredSkills()
	d := Deps{AuthoredSkills: stub}
	r := newReq("GET", "/api/skills/authored/x/versions/1.0.0", nil)
	r = withChiURLParam(r, "id", "x")
	r = withChiURLParam(r, "v", "1.0.0")
	w := runHandler(getAuthoredVersionHandler(d), r)
	statusOK(t, w, 404)
}

func TestValidateSkill_HappyPath(t *testing.T) {
	d := Deps{SkillValidator: &stubSkillValidator{}}
	body := authoredRequest{Manifest: "good"}
	r := newReq("POST", "/api/skills/validate", body)
	w := runHandler(validateSkillHandler(d), r)
	statusOK(t, w, 200)
}

func TestValidateSkill_BadManifest(t *testing.T) {
	d := Deps{SkillValidator: &stubSkillValidator{}}
	body := authoredRequest{Manifest: "bad"}
	r := newReq("POST", "/api/skills/validate", body)
	w := runHandler(validateSkillHandler(d), r)
	statusOK(t, w, 200)
	if got := readErrorCode(t, w); got != "" {
		t.Errorf("expected no top-level error code, got %q", got)
	}
}

func TestValidateSkill_NoValidator(t *testing.T) {
	r := newReq("POST", "/api/skills/validate", authoredRequest{Manifest: "x"})
	w := runHandler(validateSkillHandler(Deps{}), r)
	statusOK(t, w, 503)
}

func TestValidateSkill_BadBody(t *testing.T) {
	d := Deps{SkillValidator: &stubSkillValidator{}}
	r := newReq("POST", "/api/skills/validate", nil)
	r.Body = httpReadCloser("not-json")
	w := runHandler(validateSkillHandler(d), r)
	statusOK(t, w, 400)
}

// authored helpers (DTO conversions).
func TestAuthoredDTOConversions(t *testing.T) {
	now := time.Now()
	a := authored.Authored{
		SkillID: "foo", Version: "1.0", Status: "draft",
		Manifest:    manifest.Manifest{ID: "foo", Title: "Foo", Description: "x"},
		ManifestRaw: []byte(`{"id":"foo"}`),
		Files:       []authored.File{{RelPath: "SKILL.md", Body: []byte("hi")}},
		Checksum:    "abc", AuthorUserID: "u", CreatedAt: now,
		PublishedAt: &now,
	}
	dto := authoredToDTO(a)
	if dto.SkillID != "foo" {
		t.Errorf("missing skill_id: %+v", dto)
	}
	det := authoredDetailDTO(a)
	if det.SkillID != "foo" || len(det.Files) != 1 {
		t.Errorf("detail mismatch: %+v", det)
	}
	list := authoredListDTO([]authored.Authored{a})
	if len(list) != 1 {
		t.Errorf("list mismatch: %d", len(list))
	}
	files := authoredFilesFromDTO([]authoredFileDTO{{RelPath: "x", Body: "y"}})
	if len(files) != 1 || string(files[0].Body) != "y" {
		t.Errorf("files mismatch: %+v", files)
	}
}
