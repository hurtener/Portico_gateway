// importer.go reads a tar.gz exported bundle and registers it under
// a synthetic `imported:<bundle_id>` session id so the inspector can
// render it identically to a live session. The importer is read-only;
// the runtime rejects writes against synthetic session ids.
//
// Phase 11.

package sessionbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/telemetry/spanstore"
)

// MaxBundleSize is the default per-tenant cap on bundle uploads.
// Operators can override per tenant later; for V1 the same cap applies
// to every tenant.
const MaxBundleSize = 100 * 1024 * 1024 // 100 MB

// ImportResult is the typed outcome of a successful import. The
// caller turns this into an HTTP 200 body or a CLI table.
type ImportResult struct {
	SyntheticSessionID string `json:"synthetic_session_id"`
	BundleID           string `json:"bundle_id"`
	Range              Range  `json:"range"`
	Counts             Counts `json:"counts"`
	OriginatedTenantID string `json:"originated_tenant_id"`
}

// ErrBundleCorrupt is returned when the manifest checksum doesn't
// match the streams. Surfaced as `bundle_corrupt` typed error in the
// REST handler so a tampered bundle is unambiguously identifiable.
var ErrBundleCorrupt = errors.New("sessionbundle: bundle_corrupt")

// ErrBundleSchema is returned when the manifest schema doesn't match
// what this build understands (e.g. a v2 bundle on a v1 binary).
var ErrBundleSchema = errors.New("sessionbundle: bundle_schema_mismatch")

// ErrBundleTooLarge surfaces when the on-the-wire size exceeds
// MaxBundleSize before we even get to the manifest.
var ErrBundleTooLarge = errors.New("sessionbundle: bundle_too_large")

// ImportedSink is the persistence seam the importer uses to register
// the synthetic session. It is small on purpose — Phase 11 only needs
// a write path; reads come back through the existing audit/span/etc
// stores via the imported_sessions virtual lookup.
type ImportedSink interface {
	// RegisterImported stores the bundle under (tenantID, syntheticSessionID).
	// The caller has already verified the checksum.
	RegisterImported(ctx context.Context, b *Bundle) error
}

// Importer ties the verified bundle reader to the persistence sink.
// Construct once at startup; Import is goroutine-safe iff the Sink is.
type Importer struct {
	Sink ImportedSink
}

// Import reads a bundle from r, verifies the manifest checksum,
// rewrites the synthetic session id, and registers it via the sink.
// The reader must NOT exceed MaxBundleSize bytes; the caller can
// enforce this with an http.MaxBytesReader wrapper.
func (im *Importer) Import(ctx context.Context, tenantID string, r io.Reader) (*ImportResult, error) {
	if im == nil || im.Sink == nil {
		return nil, errors.New("sessionbundle: importer missing sink")
	}
	if tenantID == "" {
		return nil, errors.New("sessionbundle: import requires tenant id")
	}

	// Read the whole bundle into memory: we need two passes (manifest
	// first, then the streams) and the size is bounded.
	limited := io.LimitReader(r, MaxBundleSize+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("sessionbundle: read bundle: %w", err)
	}
	if len(raw) > MaxBundleSize {
		return nil, ErrBundleTooLarge
	}

	files, err := readTarGz(raw)
	if err != nil {
		return nil, fmt.Errorf("sessionbundle: read tar.gz: %w", err)
	}

	manifestBytes, ok := files["manifest.json"]
	if !ok {
		return nil, fmt.Errorf("%w: manifest missing", ErrBundleCorrupt)
	}

	var manifest Manifest
	if err := json.Unmarshal(bytes.TrimRight(manifestBytes, "\n"), &manifest); err != nil {
		return nil, fmt.Errorf("%w: manifest json: %v", ErrBundleCorrupt, err)
	}
	if manifest.Schema != SchemaV1 {
		return nil, fmt.Errorf("%w: have %q want %q", ErrBundleSchema, manifest.Schema, SchemaV1)
	}
	if manifest.Encrypted {
		return nil, errors.New("sessionbundle: encrypted bundles are reserved for a follow-up")
	}

	// Re-compute checksum and verify before deserialising the rest.
	streamFiles := orderedStreamFiles(files)
	expected := computeChecksumFromMap(streamFiles)
	if expected != manifest.Checksum {
		return nil, fmt.Errorf("%w: checksum mismatch", ErrBundleCorrupt)
	}

	bundle, err := decodeBundle(files, manifest)
	if err != nil {
		return nil, err
	}

	// Capture the originating identity BEFORE we overwrite the
	// manifest fields so the persistence layer can store both the
	// rewritten and the source tenant/session ids.
	bundle.SourceTenantID = manifest.TenantID
	bundle.SourceSessionID = manifest.SessionID

	// Rewrite the session id to the synthetic prefix so the inspector
	// can render it without colliding with a live session of the same
	// id in this tenant.
	synthSessionID := "imported:" + manifest.BundleID
	bundle.Session.ID = synthSessionID
	bundle.Session.TenantID = tenantID
	bundle.Manifest.SessionID = synthSessionID
	bundle.Manifest.TenantID = tenantID
	for i := range bundle.Audit {
		bundle.Audit[i].SessionID = synthSessionID
		bundle.Audit[i].TenantID = tenantID
	}
	for i := range bundle.Drift {
		bundle.Drift[i].SessionID = synthSessionID
		bundle.Drift[i].TenantID = tenantID
	}
	for i := range bundle.Policy {
		bundle.Policy[i].SessionID = synthSessionID
		bundle.Policy[i].TenantID = tenantID
	}
	for i := range bundle.Spans {
		bundle.Spans[i].SessionID = synthSessionID
		bundle.Spans[i].TenantID = tenantID
	}
	for i := range bundle.Approvals {
		bundle.Approvals[i].SessionID = synthSessionID
		bundle.Approvals[i].TenantID = tenantID
	}

	if err := im.Sink.RegisterImported(ctx, bundle); err != nil {
		return nil, fmt.Errorf("sessionbundle: register: %w", err)
	}

	return &ImportResult{
		SyntheticSessionID: synthSessionID,
		BundleID:           manifest.BundleID,
		Range:              manifest.Range,
		Counts:             manifest.Counts,
		OriginatedTenantID: manifest.TenantID,
	}, nil
}

// LoadFromReader is the offline path: it returns a verified Bundle
// without persisting it. The CLI uses this for `--bundle` mode.
func LoadFromReader(_ context.Context, r io.Reader) (*Bundle, error) {
	limited := io.LimitReader(r, MaxBundleSize+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("sessionbundle: read bundle: %w", err)
	}
	if len(raw) > MaxBundleSize {
		return nil, ErrBundleTooLarge
	}
	files, err := readTarGz(raw)
	if err != nil {
		return nil, fmt.Errorf("sessionbundle: read tar.gz: %w", err)
	}
	manifestBytes, ok := files["manifest.json"]
	if !ok {
		return nil, fmt.Errorf("%w: manifest missing", ErrBundleCorrupt)
	}
	var manifest Manifest
	if err := json.Unmarshal(bytes.TrimRight(manifestBytes, "\n"), &manifest); err != nil {
		return nil, fmt.Errorf("%w: manifest json: %v", ErrBundleCorrupt, err)
	}
	if manifest.Schema != SchemaV1 {
		return nil, fmt.Errorf("%w: have %q want %q", ErrBundleSchema, manifest.Schema, SchemaV1)
	}

	streamFiles := orderedStreamFiles(files)
	expected := computeChecksumFromMap(streamFiles)
	if expected != manifest.Checksum {
		return nil, fmt.Errorf("%w: checksum mismatch", ErrBundleCorrupt)
	}

	return decodeBundle(files, manifest)
}

// readTarGz decodes raw into a name -> body map. We don't stream
// because the importer needs to verify the checksum across all
// streams before deserialising any.
func readTarGz(raw []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	out := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA { //nolint:staticcheck // TypeRegA is deprecated but appears in older bundles
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("tar body: %w", err)
		}
		out[hdr.Name] = body
	}
	return out, nil
}

// orderedStreamFiles returns a slice of (name, body) pairs in the
// SAME order the exporter wrote them. The checksum is order-sensitive
// (we hash the names alongside the bodies), so import has to mirror
// the writer's sequence.
func orderedStreamFiles(files map[string][]byte) []bundleFile {
	order := []string{
		"session.json",
		"snapshot.json",
		"spans.jsonl",
		"audit.jsonl",
		"drift.jsonl",
		"policy.jsonl",
		"approvals.jsonl",
	}
	out := make([]bundleFile, 0, len(order))
	for _, name := range order {
		if body, ok := files[name]; ok {
			out = append(out, bundleFile{name: name, body: body})
		}
	}
	return out
}

// computeChecksumFromMap mirrors the exporter's computeChecksum but
// works off the ordered slice we just rebuilt.
func computeChecksumFromMap(files []bundleFile) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.name))
		h.Write([]byte{0})
		h.Write(f.body)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// decodeBundle materialises the full Bundle from the verified file
// map. Snapshot is optional; everything else is expected (an empty
// stream is a zero-row stream, not a missing one).
func decodeBundle(files map[string][]byte, manifest Manifest) (*Bundle, error) {
	b := &Bundle{Manifest: manifest}

	if body, ok := files["session.json"]; ok {
		if err := json.Unmarshal(bytes.TrimRight(body, "\n"), &b.Session); err != nil {
			return nil, fmt.Errorf("%w: session: %v", ErrBundleCorrupt, err)
		}
	}
	if body, ok := files["snapshot.json"]; ok {
		b.Snapshot = json.RawMessage(bytes.TrimRight(body, "\n"))
	}
	if body, ok := files["spans.jsonl"]; ok {
		spans, err := decodeJSONLSpans(body)
		if err != nil {
			return nil, fmt.Errorf("%w: spans: %v", ErrBundleCorrupt, err)
		}
		b.Spans = spans
	}
	if body, ok := files["audit.jsonl"]; ok {
		ev, err := decodeJSONLEvents(body)
		if err != nil {
			return nil, fmt.Errorf("%w: audit: %v", ErrBundleCorrupt, err)
		}
		b.Audit = ev
	}
	if body, ok := files["drift.jsonl"]; ok {
		ev, err := decodeJSONLEvents(body)
		if err != nil {
			return nil, fmt.Errorf("%w: drift: %v", ErrBundleCorrupt, err)
		}
		b.Drift = ev
	}
	if body, ok := files["policy.jsonl"]; ok {
		ev, err := decodeJSONLEvents(body)
		if err != nil {
			return nil, fmt.Errorf("%w: policy: %v", ErrBundleCorrupt, err)
		}
		b.Policy = ev
	}
	if body, ok := files["approvals.jsonl"]; ok {
		rows, err := decodeJSONLApprovals(body)
		if err != nil {
			return nil, fmt.Errorf("%w: approvals: %v", ErrBundleCorrupt, err)
		}
		b.Approvals = rows
	}

	if got := len(b.Spans); got != manifest.Counts.Spans {
		return nil, fmt.Errorf("%w: spans count %d != manifest %d", ErrBundleCorrupt, got, manifest.Counts.Spans)
	}
	if got := len(b.Audit); got != manifest.Counts.Audit {
		return nil, fmt.Errorf("%w: audit count %d != manifest %d", ErrBundleCorrupt, got, manifest.Counts.Audit)
	}
	if got := len(b.Drift); got != manifest.Counts.Drift {
		return nil, fmt.Errorf("%w: drift count %d != manifest %d", ErrBundleCorrupt, got, manifest.Counts.Drift)
	}
	if got := len(b.Policy); got != manifest.Counts.Policy {
		return nil, fmt.Errorf("%w: policy count %d != manifest %d", ErrBundleCorrupt, got, manifest.Counts.Policy)
	}
	if got := len(b.Approvals); got != manifest.Counts.Approvals {
		return nil, fmt.Errorf("%w: approvals count %d != manifest %d", ErrBundleCorrupt, got, manifest.Counts.Approvals)
	}

	// Re-sort ascending by occurred_at — the on-disk order is canonical
	// for byte stability but the inspector wants chronological lanes.
	sort.SliceStable(b.Audit, func(i, j int) bool { return b.Audit[i].OccurredAt.Before(b.Audit[j].OccurredAt) })
	sort.SliceStable(b.Drift, func(i, j int) bool { return b.Drift[i].OccurredAt.Before(b.Drift[j].OccurredAt) })
	sort.SliceStable(b.Policy, func(i, j int) bool { return b.Policy[i].OccurredAt.Before(b.Policy[j].OccurredAt) })
	sort.SliceStable(b.Spans, func(i, j int) bool { return b.Spans[i].StartedAt.Before(b.Spans[j].StartedAt) })

	return b, nil
}

func decodeJSONLSpans(body []byte) ([]spanstore.Span, error) {
	var out []spanstore.Span
	for _, line := range jsonlLines(body) {
		var s spanstore.Span
		if err := json.Unmarshal(line, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func decodeJSONLEvents(body []byte) ([]audit.Event, error) {
	var out []audit.Event
	for _, line := range jsonlLines(body) {
		var e audit.Event
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func decodeJSONLApprovals(body []byte) ([]ApprovalRow, error) {
	var out []ApprovalRow
	for _, line := range jsonlLines(body) {
		var a ApprovalRow
		if err := json.Unmarshal(line, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// jsonlLines splits body on '\n' and drops empty lines.
func jsonlLines(body []byte) [][]byte {
	if len(body) == 0 {
		return nil
	}
	parts := bytes.Split(body, []byte{'\n'})
	out := make([][]byte, 0, len(parts))
	for _, p := range parts {
		if len(bytes.TrimSpace(p)) == 0 {
			continue
		}
		out = append(out, p)
	}
	return out
}

// SyntheticSessionPrefix is the prefix every imported session id
// carries. The runtime's write paths reject any session id starting
// with this prefix.
const SyntheticSessionPrefix = "imported:"

// IsSynthetic reports whether sid was produced by the importer.
func IsSynthetic(sid string) bool { return strings.HasPrefix(sid, SyntheticSessionPrefix) }

// MustParseTimeRFC3339 is a tiny helper for the tests; lives here so
// the test files don't have to redeclare it.
func mustParseTimeRFC3339(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// silence linter: unused helpers are reserved for follow-up CLI work.
var _ = mustParseTimeRFC3339
