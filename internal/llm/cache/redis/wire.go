package redis

import (
	"time"

	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

// wireEntry is the JSON shape stored in Redis. []byte marshals as base64, so the
// payload survives a round trip through redis-cli / Valkey. TenantID is stored
// alongside the value and re-checked on lookup as defence in depth: the redis
// key is tenant-first, but a re-keyed value must never be deliverable.
type wireEntry struct {
	TenantID   string    `json:"t"`
	Payload    []byte    `json:"p"`
	Mode       string    `json:"m"`
	Similarity float32   `json:"s"`
	CreatedAt  time.Time `json:"c"`
	ExpiresAt  time.Time `json:"e"`
	Tokens     int       `json:"tok"`
	CostUSD    float64   `json:"cost"`
}

// toWire maps an ifaces.Entry + tenant into the stored shape.
func toWire(tenantID string, e ifaces.Entry) wireEntry {
	return wireEntry{
		TenantID:   tenantID,
		Payload:    e.Payload,
		Mode:       string(e.Mode),
		Similarity: e.Similarity,
		CreatedAt:  e.CreatedAt,
		ExpiresAt:  e.ExpiresAt,
		Tokens:     e.Tokens,
		CostUSD:    e.CostUSD,
	}
}

// fromWire maps the stored shape back into an ifaces.Entry. Mode is an
// ifaces.Mode(string); an unknown value decodes to the zero Mode ("").
func (w wireEntry) fromWire() ifaces.Entry {
	return ifaces.Entry{
		Payload:    w.Payload,
		Mode:       ifaces.Mode(w.Mode),
		Similarity: w.Similarity,
		CreatedAt:  w.CreatedAt,
		ExpiresAt:  w.ExpiresAt,
		Tokens:     w.Tokens,
		CostUSD:    w.CostUSD,
	}
}
