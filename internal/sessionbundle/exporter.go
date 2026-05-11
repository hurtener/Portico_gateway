// exporter.go writes a Bundle to an `io.Writer` as a deterministic
// tar.gz. The on-disk layout:
//
//   manifest.json    — written first; carries checksum
//   session.json     — single SessionRow
//   snapshot.json    — raw snapshot payload (omitted when empty)
//   spans.jsonl      — one Span per line, sorted by (started_at, span_id)
//   audit.jsonl      — non-drift, non-policy audit events, sorted by (occurred_at, type)
//   drift.jsonl      — schema.drift events
//   policy.jsonl     — policy.* events
//   approvals.jsonl  — approvals
//
// All JSONL streams use canonical JSON so the tar bytes are stable
// across platforms (modulo gzip header timestamps, which we zero out).
//
// Phase 11.

package sessionbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/telemetry/spanstore"
)

// ExportOptions tunes the exporter. The defaults are the right choice
// for "send this to support" — payloads kept, no encryption.
type ExportOptions struct {
	// Encrypt — reserved. Phase 11 ships the manifest flag and the
	// option struct; the actual age recipient flow lands in a follow-up.
	Encrypt      bool
	RecipientKey string

	// OmitPayloads strips audit payload maps before serialisation. Use
	// when sharing a bundle with a third party who shouldn't see even
	// redacted payloads (regulated industries, cross-org sharing).
	OmitPayloads bool
}

// Export writes b as a tar.gz to w. The function buffers each stream
// in memory so the manifest checksum can be computed before the first
// byte hits w; for typical bundles (<10 MB) that's the right trade.
//
// On success, b.Manifest.Checksum is set on the in-memory bundle so
// callers can persist it alongside the bytes.
func Export(ctx context.Context, b *Bundle, w io.Writer, opt ExportOptions) error {
	if b == nil {
		return errors.New("sessionbundle: nil bundle")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if opt.Encrypt {
		// Reserved; clear, typed error so operators see the gap rather
		// than getting a silently-unencrypted bundle.
		return errors.New("sessionbundle: age encryption is reserved for a follow-up")
	}

	streams, err := buildStreams(b, opt)
	if err != nil {
		return err
	}

	checksum := computeChecksum(streams)
	b.Manifest.Checksum = checksum
	b.Manifest.Encrypted = opt.Encrypt
	if b.Manifest.Schema == "" {
		b.Manifest.Schema = SchemaV1
	}
	if b.Manifest.GeneratedAt.IsZero() {
		b.Manifest.GeneratedAt = time.Now().UTC()
	}

	manifestBytes, err := canonicalMarshal(b.Manifest)
	if err != nil {
		return fmt.Errorf("sessionbundle: marshal manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')

	gz := gzip.NewWriter(w)
	// Zero the modtime in the gzip header so the same bundle hashes
	// identically across runs. (The tar payload's checksum is the
	// real integrity gate.)
	gz.ModTime = time.Time{}
	gz.Name = ""
	tw := tar.NewWriter(gz)

	files := []bundleFile{
		{name: "manifest.json", body: manifestBytes},
	}
	files = append(files, streams...)

	for _, f := range files {
		if err := writeTarFile(tw, f.name, f.body); err != nil {
			return fmt.Errorf("sessionbundle: write %s: %w", f.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("sessionbundle: tar close: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("sessionbundle: gzip close: %w", err)
	}
	return nil
}

type bundleFile struct {
	name string
	body []byte
}

// buildStreams produces the canonical JSONL streams. Order of records
// inside each stream is deterministic so two exports of the same
// bundle yield byte-identical streams (the input to the checksum).
func buildStreams(b *Bundle, opt ExportOptions) ([]bundleFile, error) {
	files := make([]bundleFile, 0, 7)

	// session.json — single canonical doc.
	sessionBytes, err := canonicalMarshal(b.Session)
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	sessionBytes = append(sessionBytes, '\n')
	files = append(files, bundleFile{name: "session.json", body: sessionBytes})

	// snapshot.json — only if present.
	if len(b.Snapshot) > 0 {
		// Re-canonicalise so the embedded snapshot bytes are stable
		// even if the writer that originally produced them did not
		// sort keys.
		var generic any
		if err := jsonUnmarshalRaw(b.Snapshot, &generic); err == nil {
			canon, err := canonicalMarshal(generic)
			if err != nil {
				return nil, fmt.Errorf("snapshot: %w", err)
			}
			canon = append(canon, '\n')
			files = append(files, bundleFile{name: "snapshot.json", body: canon})
		}
	}

	spans := append([]spanstore.Span(nil), b.Spans...)
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].StartedAt.Equal(spans[j].StartedAt) {
			return spans[i].SpanID < spans[j].SpanID
		}
		return spans[i].StartedAt.Before(spans[j].StartedAt)
	})
	spansBody, err := jsonlSpans(spans)
	if err != nil {
		return nil, fmt.Errorf("spans: %w", err)
	}
	files = append(files, bundleFile{name: "spans.jsonl", body: spansBody})

	auditCopy := cloneEvents(b.Audit, opt.OmitPayloads)
	sort.Slice(auditCopy, func(i, j int) bool { return eventLess(auditCopy[i], auditCopy[j]) })
	auditBody, err := jsonlEvents(auditCopy)
	if err != nil {
		return nil, fmt.Errorf("audit: %w", err)
	}
	files = append(files, bundleFile{name: "audit.jsonl", body: auditBody})

	drift := cloneEvents(b.Drift, opt.OmitPayloads)
	sort.Slice(drift, func(i, j int) bool { return eventLess(drift[i], drift[j]) })
	driftBody, err := jsonlEvents(drift)
	if err != nil {
		return nil, fmt.Errorf("drift: %w", err)
	}
	files = append(files, bundleFile{name: "drift.jsonl", body: driftBody})

	pol := cloneEvents(b.Policy, opt.OmitPayloads)
	sort.Slice(pol, func(i, j int) bool { return eventLess(pol[i], pol[j]) })
	polBody, err := jsonlEvents(pol)
	if err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	files = append(files, bundleFile{name: "policy.jsonl", body: polBody})

	apprs := append([]ApprovalRow(nil), b.Approvals...)
	sort.Slice(apprs, func(i, j int) bool {
		if apprs[i].CreatedAt.Equal(apprs[j].CreatedAt) {
			return apprs[i].ID < apprs[j].ID
		}
		return apprs[i].CreatedAt.Before(apprs[j].CreatedAt)
	})
	apprsBody, err := jsonlApprovals(apprs)
	if err != nil {
		return nil, fmt.Errorf("approvals: %w", err)
	}
	files = append(files, bundleFile{name: "approvals.jsonl", body: apprsBody})

	return files, nil
}

// cloneEvents copies and optionally strips payloads. We never mutate
// the input slice — that's owned by the loader and may be reused for
// the API JSON response on the same request.
func cloneEvents(in []audit.Event, omitPayloads bool) []audit.Event {
	out := make([]audit.Event, len(in))
	for i, e := range in {
		ev := e
		if omitPayloads {
			ev.Payload = nil
		}
		out[i] = ev
	}
	return out
}

// eventLess sorts events by (occurred_at, type, span_id) so the order
// is fully deterministic even when several events share a timestamp.
func eventLess(a, b audit.Event) bool {
	if !a.OccurredAt.Equal(b.OccurredAt) {
		return a.OccurredAt.Before(b.OccurredAt)
	}
	if a.Type != b.Type {
		return a.Type < b.Type
	}
	return a.SpanID < b.SpanID
}

// jsonlSpans / jsonlEvents / jsonlApprovals serialise a slice of
// records as canonical JSON with one record per line.
func jsonlSpans(spans []spanstore.Span) ([]byte, error) {
	var buf bytes.Buffer
	for _, s := range spans {
		line, err := canonicalMarshalLine(s)
		if err != nil {
			return nil, err
		}
		buf.Write(line)
	}
	return buf.Bytes(), nil
}

func jsonlEvents(events []audit.Event) ([]byte, error) {
	var buf bytes.Buffer
	for _, e := range events {
		line, err := canonicalMarshalLine(e)
		if err != nil {
			return nil, err
		}
		buf.Write(line)
	}
	return buf.Bytes(), nil
}

func jsonlApprovals(rows []ApprovalRow) ([]byte, error) {
	var buf bytes.Buffer
	for _, r := range rows {
		line, err := canonicalMarshalLine(r)
		if err != nil {
			return nil, err
		}
		buf.Write(line)
	}
	return buf.Bytes(), nil
}

// jsonUnmarshalRaw decodes a json.RawMessage into a generic any. Tiny
// helper so the snapshot canonicalisation path doesn't pull encoding/json
// directly into the export hot path.
func jsonUnmarshalRaw(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}

// computeChecksum hashes the concatenated stream bodies (manifest is
// excluded — it's the carrier of the checksum, not part of it).
func computeChecksum(files []bundleFile) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.name))
		h.Write([]byte{0})
		h.Write(f.body)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// writeTarFile writes a USTAR header that's stable across platforms:
// modtime zeroed, ownership zeroed, mode 0644.
func writeTarFile(tw *tar.Writer, name string, body []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(body)),
		ModTime: time.Time{},
		Format:  tar.FormatUSTAR,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(body); err != nil {
		return err
	}
	return nil
}

// newBundleID returns a 16-byte random identifier hex-encoded. We use
// it for both the bundle's own id and (in the importer) the synthetic
// session id under which the bundle is registered.
func newBundleID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Crypto-strength id is desirable but not load-bearing — the
		// bundle is identified by its checksum, the bundle_id is
		// purely a human-readable handle. Fall back to a time-derived
		// id so dev mode never panics.
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (i * 8))
		}
	}
	return "bdl_" + hex.EncodeToString(b[:])
}
