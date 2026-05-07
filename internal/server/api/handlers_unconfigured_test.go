package api

import (
	"net/http"
	"testing"
)

// 503 / unconfigured paths for the older handlers. We don't try to mock
// their full dependencies; we simply assert that — when the gateway is
// missing the relevant subsystem — the handler answers with the
// documented "service unavailable" shape rather than crashing. This
// pulls the happy-path-uncovered surface above the package coverage gate
// without duplicating Phase 4-8 unit tests.

// runUnconfigured runs each handler under a Deps with no optional
// dependencies wired and asserts a 503.
func TestUnconfigured_Handlers_Return503(t *testing.T) {
	d := Deps{}
	cases := []struct {
		name string
		h    http.HandlerFunc
	}{
		{"listAppsHandler", listAppsHandler(d)},
		{"listSnapshotsHandler", listSnapshotsHandler(d)},
		{"getSnapshotHandler", getSnapshotHandler(d)},
		{"diffSnapshotsHandler", diffSnapshotsHandler(d)},
		{"resolveCatalogHandler", resolveCatalogHandler(d)},
		{"sessionSnapshotHandler", sessionSnapshotHandler(d)},
		{"listSkillSourcesHandler", listSkillSourcesHandler(d)},
		{"getSkillSourceHandler", getSkillSourceHandler(d)},
		{"upsertSkillSourceCreate", upsertSkillSourceHandler(d, false)},
		{"upsertSkillSourceUpdate", upsertSkillSourceHandler(d, true)},
		{"deleteSkillSourceHandler", deleteSkillSourceHandler(d)},
		{"refreshSkillSourceHandler", refreshSkillSourceHandler(d)},
		{"listSkillSourcePacksHandler", listSkillSourcePacksHandler(d)},
		{"listAuthoredHandler", listAuthoredHandler(d)},
		{"getAuthoredActiveHandler", getAuthoredActiveHandler(d)},
		{"historyAuthoredHandler", historyAuthoredHandler(d)},
		{"getAuthoredVersionHandler", getAuthoredVersionHandler(d)},
		{"createAuthoredHandler", createAuthoredHandler(d)},
		{"updateAuthoredHandler", updateAuthoredHandler(d)},
		{"publishAuthoredHandler", publishAuthoredHandler(d)},
		{"archiveAuthoredHandler", archiveAuthoredHandler(d)},
		{"deleteAuthoredDraftHandler", deleteAuthoredDraftHandler(d)},
		{"validateSkillHandler", validateSkillHandler(d)},
		{"listSkillsHandler", listSkillsHandler(d)},
		{"getSkillHandler", getSkillHandler(d)},
		{"getSkillManifestYAML", getSkillManifestYAML(d)},
		{"enableSkillHandler", enableSkillHandler(d, true)},
		{"listSessionSkillsHandler", listSessionSkillsHandler(d)},
		{"sessionSkillEnableHandler", sessionSkillEnableHandler(d, true)},
		{"listApprovalsHandler", listApprovalsHandler(d)},
		{"getApprovalHandler", getApprovalHandler(d)},
		{"resolveApprovalHandler", resolveApprovalHandler(d, "approved")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newReq("POST", "/x", nil)
			w := runHandler(tc.h, r)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: want 503, got %d body=%s", tc.name, w.Code, w.Body.String())
			}
		})
	}
}
