package sessionbundle_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/sessionbundle"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/telemetry/spanstore"
	spanstoresqlite "github.com/hurtener/Portico_gateway/internal/telemetry/spanstore/sqlite"
)

// fakeSessionReader is the trivial in-memory implementation tests
// hand to a Loader so we don't need to bring up the full SQLite
// stack just to load a bundle.
type fakeSessionReader struct {
	rows map[string]map[string]*sessionbundle.SessionRow // tenant → sid → row
}

func (f *fakeSessionReader) GetSession(_ context.Context, tenantID, sessionID string) (*sessionbundle.SessionRow, error) {
	if t, ok := f.rows[tenantID]; ok {
		if r, ok := t[sessionID]; ok {
			return r, nil
		}
	}
	return nil, nil
}

// fakeApprovalStore stubs the approval store. We feed it pre-built
// rows so Loader.Load can stitch them in.
type fakeApprovalStore struct {
	byID map[string]*ifaces.ApprovalRecord
}

func (f *fakeApprovalStore) Insert(_ context.Context, _ *ifaces.ApprovalRecord) error {
	return errors.New("not used in tests")
}
func (f *fakeApprovalStore) UpdateStatus(_ context.Context, _, _, _ string, _ time.Time) error {
	return errors.New("not used in tests")
}
func (f *fakeApprovalStore) Get(_ context.Context, tenantID, id string) (*ifaces.ApprovalRecord, error) {
	r := f.byID[id]
	if r == nil || r.TenantID != tenantID {
		return nil, ifaces.ErrNotFound
	}
	return r, nil
}
func (f *fakeApprovalStore) ListPending(_ context.Context, _ string) ([]*ifaces.ApprovalRecord, error) {
	return nil, nil
}
func (f *fakeApprovalStore) ExpireOlderThan(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

// snapshotStub is a minimal ifaces.SnapshotStore — only Get is read
// by the bundle loader.
type snapshotStub struct {
	rows map[string]*ifaces.SnapshotRecord
}

func (s *snapshotStub) Insert(context.Context, *ifaces.SnapshotRecord) error { return nil }
func (s *snapshotStub) Get(_ context.Context, id string) (*ifaces.SnapshotRecord, error) {
	if r, ok := s.rows[id]; ok {
		return r, nil
	}
	return nil, ifaces.ErrNotFound
}
func (s *snapshotStub) List(context.Context, string, ifaces.SnapshotListQuery) ([]*ifaces.SnapshotRecord, string, error) {
	return nil, "", nil
}
func (s *snapshotStub) StampSession(context.Context, string, string) error { return nil }
func (s *snapshotStub) UpsertFingerprint(context.Context, *ifaces.FingerprintRecord) error {
	return nil
}
func (s *snapshotStub) LatestFingerprint(context.Context, string, string) (*ifaces.FingerprintRecord, error) {
	return nil, ifaces.ErrNotFound
}
func (s *snapshotStub) ActiveSessions(context.Context, time.Time) ([]ifaces.ActiveSessionRecord, error) {
	return nil, nil
}
func (s *snapshotStub) CloseSession(context.Context, string) error { return nil }

// newAuditDB and newSpanStore stand up the persistence backends each
// test needs. They live in this file (not a shared helper) because
// the bundle tests are the only consumer.
func newAuditDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS audit_events (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    type         TEXT NOT NULL,
    session_id   TEXT,
    user_id      TEXT,
    occurred_at  TEXT NOT NULL,
    trace_id     TEXT,
    span_id      TEXT,
    payload_json TEXT,
    summary      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_time ON audit_events(tenant_id, occurred_at DESC);`); err != nil {
		t.Fatal(err)
	}
	return db
}

func newSpanDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS spans (
    tenant_id    TEXT NOT NULL,
    session_id   TEXT,
    trace_id     TEXT NOT NULL,
    span_id      TEXT NOT NULL,
    parent_id    TEXT,
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    started_at   TEXT NOT NULL,
    ended_at     TEXT NOT NULL,
    status       TEXT NOT NULL,
    status_msg   TEXT NOT NULL DEFAULT '',
    attrs_json   TEXT NOT NULL DEFAULT '{}',
    events_json  TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (tenant_id, trace_id, span_id)
);
CREATE INDEX IF NOT EXISTS idx_spans_session ON spans(tenant_id, session_id, started_at);`); err != nil {
		t.Fatal(err)
	}
	return db
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// buildLoaderWithFixture seeds a session, snapshot, audit events, and
// spans for "acme/sess-1". Returns the loader + the fixture so the
// caller can also spot-check the raw data.
func buildLoaderWithFixture(t *testing.T) (*sessionbundle.Loader, fixture) {
	t.Helper()
	auditDB := newAuditDB(t)
	spanDB := newSpanDB(t)
	auditStore := audit.NewStore(auditDB, discardLogger())
	span := spanstoresqlite.New(spanDB)

	ctx := context.Background()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	// Audit fixture: tool start, schema drift, policy allow, tool complete.
	for i, e := range []audit.Event{
		{
			Type:       audit.EventToolCallStart,
			TenantID:   "acme",
			SessionID:  "sess-1",
			OccurredAt: now,
			TraceID:    "trace-A",
			Payload:    map[string]any{"tool": "github_search_repos"},
		},
		{
			Type:       "schema.drift",
			TenantID:   "acme",
			SessionID:  "sess-1",
			OccurredAt: now.Add(time.Second),
			Payload:    map[string]any{"server": "github"},
		},
		{
			Type:       audit.EventPolicyAllowed,
			TenantID:   "acme",
			SessionID:  "sess-1",
			OccurredAt: now.Add(2 * time.Second),
			Payload:    map[string]any{"rule": "github.read.allow"},
		},
		{
			Type:       audit.EventToolCallComplete,
			TenantID:   "acme",
			SessionID:  "sess-1",
			OccurredAt: now.Add(3 * time.Second),
			Payload:    map[string]any{"approval_id": "appr-1"},
		},
	} {
		if err := auditStore.EmitSync(ctx, e); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}

	// Spans fixture: 2 spans for sess-1, 1 unrelated for noise.
	if err := span.Put(ctx, []spanstore.Span{
		{
			TenantID: "acme", SessionID: "sess-1",
			TraceID: "trace-A", SpanID: "span-1",
			Name: "github.search", Kind: spanstore.KindClient,
			StartedAt: now, EndedAt: now.Add(500 * time.Millisecond),
			Status: spanstore.StatusOK,
		},
		{
			TenantID: "acme", SessionID: "sess-1",
			TraceID: "trace-A", SpanID: "span-2", ParentID: "span-1",
			Name: "github.api.list_repos", Kind: spanstore.KindClient,
			StartedAt: now.Add(50 * time.Millisecond), EndedAt: now.Add(450 * time.Millisecond),
			Status: spanstore.StatusOK,
		},
		{
			TenantID: "acme", SessionID: "sess-noise",
			TraceID: "trace-Z", SpanID: "span-Z1",
			Name: "noise", Kind: spanstore.KindInternal,
			StartedAt: now, EndedAt: now,
			Status: spanstore.StatusOK,
		},
	}); err != nil {
		t.Fatalf("span put: %v", err)
	}

	approvals := &fakeApprovalStore{byID: map[string]*ifaces.ApprovalRecord{
		"appr-1": {
			ID: "appr-1", TenantID: "acme", SessionID: "sess-1",
			Tool: "github_search_repos", Status: "decided",
			CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute),
		},
	}}

	snapshots := &snapshotStub{rows: map[string]*ifaces.SnapshotRecord{
		"snap-1": {
			ID: "snap-1", TenantID: "acme", SessionID: "sess-1",
			OverallHash: "h", PayloadJSON: `{"servers":[{"id":"github"}]}`,
			CreatedAt: now,
		},
	}}

	sessions := &fakeSessionReader{rows: map[string]map[string]*sessionbundle.SessionRow{
		"acme": {
			"sess-1": {
				ID:         "sess-1",
				TenantID:   "acme",
				SnapshotID: "snap-1",
				StartedAt:  now,
				EndedAt:    now.Add(5 * time.Second),
			},
		},
		"beta": {
			"sess-beta": {
				ID:        "sess-beta",
				TenantID:  "beta",
				StartedAt: now,
			},
		},
	}}

	loader := &sessionbundle.Loader{
		Sessions:  sessions,
		Snapshots: snapshots,
		Audit:     auditStore,
		Approvals: approvals,
		Spans:     span,
	}

	return loader, fixture{
		now:        now,
		auditStore: auditStore,
	}
}

type fixture struct {
	now        time.Time
	auditStore *audit.Store
}

func TestLoad_HappyPath(t *testing.T) {
	loader, _ := buildLoaderWithFixture(t)
	ctx := context.Background()

	b, err := loader.Load(ctx, "acme", "sess-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if b.Session.ID != "sess-1" {
		t.Errorf("session id = %q", b.Session.ID)
	}
	if b.Manifest.SessionID != "sess-1" || b.Manifest.TenantID != "acme" {
		t.Errorf("manifest: %+v", b.Manifest)
	}
	if b.Manifest.Schema != sessionbundle.SchemaV1 {
		t.Errorf("schema mismatch: %q", b.Manifest.Schema)
	}
	if len(b.Spans) != 2 {
		t.Errorf("expected 2 spans for sess-1; got %d", len(b.Spans))
	}
	if len(b.Audit) != 2 {
		// tool_call.start + tool_call.complete (drift + policy split out)
		t.Errorf("expected 2 audit lane events; got %d", len(b.Audit))
	}
	if len(b.Drift) != 1 {
		t.Errorf("expected 1 drift event; got %d", len(b.Drift))
	}
	if len(b.Policy) != 1 {
		t.Errorf("expected 1 policy event; got %d", len(b.Policy))
	}
	if len(b.Approvals) != 1 {
		t.Errorf("expected 1 approval row; got %d", len(b.Approvals))
	}
	if len(b.Snapshot) == 0 {
		t.Error("expected snapshot to be populated")
	}
	if b.Manifest.Counts.Spans != 2 || b.Manifest.Counts.Audit != 2 {
		t.Errorf("counts mismatch: %+v", b.Manifest.Counts)
	}
}

func TestLoad_NotFound(t *testing.T) {
	loader, _ := buildLoaderWithFixture(t)
	_, err := loader.Load(context.Background(), "acme", "missing")
	if !errors.Is(err, sessionbundle.ErrSessionNotFound) {
		t.Errorf("want ErrSessionNotFound; got %v", err)
	}
}

func TestLoad_TenantIsolation(t *testing.T) {
	loader, _ := buildLoaderWithFixture(t)
	// Trying to load acme's session as beta should fail —
	// the fakeSessionReader filters by tenant.
	_, err := loader.Load(context.Background(), "beta", "sess-1")
	if !errors.Is(err, sessionbundle.ErrSessionNotFound) {
		t.Errorf("cross-tenant load should fail; got %v", err)
	}
}

func TestExport_DeterministicBytes(t *testing.T) {
	loader, _ := buildLoaderWithFixture(t)
	ctx := context.Background()

	b1, err := loader.Load(ctx, "acme", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := loader.Load(ctx, "acme", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	// BundleID + GeneratedAt are non-deterministic by design.
	// Pin them so we're testing the real determinism axes (sort
	// stability, key ordering, gzip header ModTime).
	pinManifest(b1, b2)

	var buf1, buf2 bytes.Buffer
	if err := sessionbundle.Export(ctx, b1, &buf1, sessionbundle.ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := sessionbundle.Export(ctx, b2, &buf2, sessionbundle.ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		t.Errorf("two exports of the same bundle should be byte-identical: lens %d / %d", buf1.Len(), buf2.Len())
	}
	if b1.Manifest.Checksum == "" || !strings.HasPrefix(b1.Manifest.Checksum, "sha256:") {
		t.Errorf("checksum not populated: %q", b1.Manifest.Checksum)
	}
}

func TestExport_RedactsPayloads(t *testing.T) {
	loader, _ := buildLoaderWithFixture(t)
	ctx := context.Background()

	// Seed a credential-shaped payload and ensure the audit
	// redactor scrubs it before it lands in the bundle.
	if err := loader.Audit.EmitSync(ctx, audit.Event{
		Type:       "tool_call.start",
		TenantID:   "acme",
		SessionID:  "sess-1",
		OccurredAt: time.Now().UTC(),
		Payload:    map[string]any{"token": "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	}); err != nil {
		t.Fatal(err)
	}

	b, err := loader.Load(ctx, "acme", "sess-1")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := sessionbundle.Export(ctx, b, &buf, sessionbundle.ExportOptions{}); err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(buf.Bytes(), []byte("ghp_AAAA")) {
		t.Errorf("raw token leaked into bundle bytes")
	}
}

func TestExport_OmitPayloads(t *testing.T) {
	loader, _ := buildLoaderWithFixture(t)
	ctx := context.Background()
	b, err := loader.Load(ctx, "acme", "sess-1")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := sessionbundle.Export(ctx, b, &buf, sessionbundle.ExportOptions{OmitPayloads: true}); err != nil {
		t.Fatal(err)
	}
	// Payload values should not appear; payload key may still appear
	// at the manifest counts level. We check for one specific value.
	if bytes.Contains(buf.Bytes(), []byte("github_search_repos")) {
		t.Errorf("payload value leaked despite OmitPayloads")
	}
}

func TestImport_HappyPath(t *testing.T) {
	loader, _ := buildLoaderWithFixture(t)
	ctx := context.Background()
	b, err := loader.Load(ctx, "acme", "sess-1")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := sessionbundle.Export(ctx, b, &buf, sessionbundle.ExportOptions{}); err != nil {
		t.Fatal(err)
	}

	sink := &capturingSink{}
	im := &sessionbundle.Importer{Sink: sink}

	res, err := im.Import(ctx, "beta", &buf)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !sessionbundle.IsSynthetic(res.SyntheticSessionID) {
		t.Errorf("synthetic prefix missing: %q", res.SyntheticSessionID)
	}
	if sink.bundle == nil {
		t.Fatal("sink did not receive bundle")
	}
	if sink.bundle.Session.TenantID != "beta" {
		t.Errorf("imported tenant rewrite failed: %q", sink.bundle.Session.TenantID)
	}
	if sink.bundle.Session.ID != res.SyntheticSessionID {
		t.Errorf("session id rewrite failed: %q vs %q", sink.bundle.Session.ID, res.SyntheticSessionID)
	}
	if res.OriginatedTenantID != "acme" {
		t.Errorf("originated tenant lost: %q", res.OriginatedTenantID)
	}
}

func TestImport_PreservesSourceIdentity(t *testing.T) {
	loader, _ := buildLoaderWithFixture(t)
	ctx := context.Background()
	b, err := loader.Load(ctx, "acme", "sess-1")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := sessionbundle.Export(ctx, b, &buf, sessionbundle.ExportOptions{}); err != nil {
		t.Fatal(err)
	}

	sink := &capturingSink{}
	im := &sessionbundle.Importer{Sink: sink}

	res, err := im.Import(ctx, "beta", &buf)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.OriginatedTenantID != "acme" {
		t.Errorf("originated tenant lost: %q", res.OriginatedTenantID)
	}
	if sink.bundle.SourceTenantID != "acme" {
		t.Errorf("source tenant id missing on bundle: %q", sink.bundle.SourceTenantID)
	}
	if sink.bundle.SourceSessionID != "sess-1" {
		t.Errorf("source session id missing on bundle: %q", sink.bundle.SourceSessionID)
	}
	// And the rewritten fields point at the importing tenant.
	if sink.bundle.Manifest.TenantID != "beta" {
		t.Errorf("rewrite failed: %q", sink.bundle.Manifest.TenantID)
	}
}

func TestImport_TamperedChecksum_Refused(t *testing.T) {
	loader, _ := buildLoaderWithFixture(t)
	ctx := context.Background()
	b, err := loader.Load(ctx, "acme", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := sessionbundle.Export(ctx, b, &buf, sessionbundle.ExportOptions{}); err != nil {
		t.Fatal(err)
	}

	// Flip a single byte deep in the gz body — anywhere past the
	// gzip header. The audit/spans/etc streams are roughly halfway in.
	tampered := make([]byte, buf.Len())
	copy(tampered, buf.Bytes())
	flipAt := len(tampered) / 2
	tampered[flipAt] ^= 0x01

	im := &sessionbundle.Importer{Sink: &capturingSink{}}
	_, err = im.Import(ctx, "beta", bytes.NewReader(tampered))
	if err == nil {
		t.Fatal("expected tampered bundle to be refused")
	}
	// Either the gzip header rejects it (still a refusal) or the
	// checksum mismatches; both count as a positive result.
	if !errors.Is(err, sessionbundle.ErrBundleCorrupt) && !strings.Contains(err.Error(), "gzip") && !strings.Contains(err.Error(), "tar") {
		t.Errorf("unexpected error type: %v", err)
	}
}

func TestImport_RejectsTooLarge(t *testing.T) {
	im := &sessionbundle.Importer{Sink: &capturingSink{}}
	// Build a fake reader bigger than MaxBundleSize.
	huge := bytes.NewReader(make([]byte, sessionbundle.MaxBundleSize+1))
	_, err := im.Import(context.Background(), "acme", huge)
	if !errors.Is(err, sessionbundle.ErrBundleTooLarge) {
		t.Errorf("want ErrBundleTooLarge; got %v", err)
	}
}

func TestLoadFromReader_RoundTrip(t *testing.T) {
	loader, _ := buildLoaderWithFixture(t)
	ctx := context.Background()
	b, err := loader.Load(ctx, "acme", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := sessionbundle.Export(ctx, b, &buf, sessionbundle.ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := sessionbundle.LoadFromReader(ctx, &buf)
	if err != nil {
		t.Fatalf("load from reader: %v", err)
	}
	if got.Session.ID != "sess-1" || got.Session.TenantID != "acme" {
		t.Errorf("session round-trip lost: %+v", got.Session)
	}
	if got.Manifest.Counts.Spans != b.Manifest.Counts.Spans {
		t.Errorf("counts lost: %+v vs %+v", got.Manifest.Counts, b.Manifest.Counts)
	}
}

// pinManifest forces b2's manifest to use b1's bundle id + generated
// timestamp so the test isolates the *content* determinism from the
// id-generation indeterminism. The real export still uses fresh ids
// in production.
func pinManifest(b1, b2 *sessionbundle.Bundle) {
	b2.Manifest.BundleID = b1.Manifest.BundleID
	b2.Manifest.GeneratedAt = b1.Manifest.GeneratedAt
}

// capturingSink is a one-shot ImportedSink for tests.
type capturingSink struct {
	bundle *sessionbundle.Bundle
}

func (c *capturingSink) RegisterImported(_ context.Context, b *sessionbundle.Bundle) error {
	c.bundle = b
	return nil
}

// (linter satisfaction — io is used by jsonlLines indirectly but the
// test file references it via the readers it constructs.)
var _ = io.EOF
