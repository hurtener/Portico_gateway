package process

import (
	"bufio"
	"context"
	"io"
	"sync"
	"time"
)

// LogStream identifies which fd the line came from.
type LogStream string

// Log streams.
const (
	LogStreamStdout LogStream = "stdout"
	LogStreamStderr LogStream = "stderr"
)

// LogLine is one observed line from a supervised process. Mirrors
// registry.LogLine but lives here so the supervisor doesn't have to import
// the registry to publish entries.
type LogLine struct {
	At     time.Time `json:"at"`
	Stream string    `json:"stream"`
	Text   string    `json:"text"`
}

// DefaultLogRingBytes is the default per-process ring buffer size.
const DefaultLogRingBytes = 1 << 20 // 1 MiB

// LogRing is a per-process ring buffer of stdout/stderr lines plus a
// fan-out subscriber API. The buffer is bounded by total byte count
// (sum of Text lengths) and drops oldest entries to enforce the cap.
//
// Subscribe returns a buffered channel that immediately receives a copy
// of every retained historical line newer than `since`, then receives
// future lines until the supplied context is cancelled. Slow subscribers
// fall behind silently — the publisher never blocks.
type LogRing struct {
	mu       sync.Mutex
	maxBytes int
	bytes    int
	buf      []LogLine // bounded by total byte count

	// subscribers receive a copy of every published line. Slow
	// subscribers are dropped (drop-oldest semantics).
	subs   map[*subscriber]struct{}
	closed bool
}

type subscriber struct {
	ch    chan LogLine
	since time.Time
}

// NewLogRing constructs a LogRing with the given byte cap. A non-positive
// cap falls back to DefaultLogRingBytes.
func NewLogRing(maxBytes int) *LogRing {
	if maxBytes <= 0 {
		maxBytes = DefaultLogRingBytes
	}
	return &LogRing{
		maxBytes: maxBytes,
		buf:      make([]LogLine, 0, 64),
		subs:     make(map[*subscriber]struct{}),
	}
}

// PublishLog satisfies the stdio.LogSink contract.
func (r *LogRing) PublishLog(stream, text string) {
	if r == nil {
		return
	}
	if text == "" {
		return
	}
	if !endsWithNewline(text) {
		text += "\n"
	}
	r.Publish(LogLine{
		At:     time.Now().UTC(),
		Stream: stream,
		Text:   text,
	})
}

func endsWithNewline(s string) bool {
	return len(s) > 0 && s[len(s)-1] == '\n'
}

// Publish records a line in the ring and fans it out to live subscribers.
// Safe to call from multiple goroutines.
func (r *LogRing) Publish(line LogLine) {
	if r == nil {
		return
	}
	if line.At.IsZero() {
		line.At = time.Now().UTC()
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.appendLocked(line)
	subs := make([]*subscriber, 0, len(r.subs))
	for s := range r.subs {
		subs = append(subs, s)
	}
	r.mu.Unlock()
	// Fan-out outside the lock; non-blocking send.
	for _, s := range subs {
		if !line.At.Before(s.since) {
			select {
			case s.ch <- line:
			default:
				// Drop on backpressure — the consumer is slow.
			}
		}
	}
}

// appendLocked enforces the byte cap. Caller must hold r.mu.
func (r *LogRing) appendLocked(line LogLine) {
	r.buf = append(r.buf, line)
	r.bytes += len(line.Text)
	for r.bytes > r.maxBytes && len(r.buf) > 0 {
		first := r.buf[0]
		r.bytes -= len(first.Text)
		r.buf = r.buf[1:]
	}
}

// Snapshot returns a copy of the lines newer than since. Empty since
// returns the entire ring.
func (r *LogRing) Snapshot(since time.Time) []LogLine {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LogLine, 0, len(r.buf))
	for _, l := range r.buf {
		if !since.IsZero() && l.At.Before(since) {
			continue
		}
		out = append(out, l)
	}
	return out
}

// Subscribe returns a channel that will receive every line newer than
// `since`, plus all subsequent lines until ctx is cancelled. The channel
// is closed when the context is cancelled OR when the ring is Closed.
// Buffer is sized for short bursts; slow consumers see drops.
func (r *LogRing) Subscribe(ctx context.Context, since time.Time) <-chan LogLine {
	if r == nil {
		closed := make(chan LogLine)
		close(closed)
		return closed
	}
	sub := &subscriber{
		ch:    make(chan LogLine, 64),
		since: since,
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		close(sub.ch)
		return sub.ch
	}
	// Replay history first (best-effort; non-blocking).
	for _, l := range r.buf {
		if !since.IsZero() && l.At.Before(since) {
			continue
		}
		select {
		case sub.ch <- l:
		default:
			// Buffer full during replay — stop replay.
			break
		}
	}
	r.subs[sub] = struct{}{}
	r.mu.Unlock()

	// Cleanup goroutine: when ctx cancels, unsubscribe + close.
	go func() {
		<-ctx.Done()
		r.mu.Lock()
		if _, ok := r.subs[sub]; ok {
			delete(r.subs, sub)
			close(sub.ch)
		}
		r.mu.Unlock()
	}()

	return sub.ch
}

// Close stops the ring and closes every active subscriber.
func (r *LogRing) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	subs := r.subs
	r.subs = nil
	r.mu.Unlock()
	for s := range subs {
		close(s.ch)
	}
}

// Pump reads `r` line-by-line and publishes each line to ring tagged with
// the given stream label. Returns when the reader hits EOF or errors.
// Long lines are truncated at 8 KiB to keep the ring well-behaved.
func PumpReaderToRing(reader io.Reader, ring *LogRing, stream LogStream) {
	if reader == nil || ring == nil {
		return
	}
	const maxLine = 8 << 10
	br := bufio.NewReaderSize(reader, maxLine)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			text := line
			if len(text) > maxLine {
				text = text[:maxLine]
			}
			ring.Publish(LogLine{
				At:     time.Now().UTC(),
				Stream: string(stream),
				Text:   text,
			})
		}
		if err != nil {
			return
		}
	}
}

// LogRingRegistry indexes per-instance log rings keyed by (tenantID,
// serverID). Reads return the ring or nil if absent. The registry is
// optional — when the supervisor lacks one, /logs returns an empty
// stream.
type LogRingRegistry struct {
	mu    sync.RWMutex
	rings map[string]*LogRing
}

// NewLogRingRegistry constructs an empty registry.
func NewLogRingRegistry() *LogRingRegistry {
	return &LogRingRegistry{rings: make(map[string]*LogRing)}
}

// Key returns the canonical key for (tenantID, serverID).
func (g *LogRingRegistry) key(tenantID, serverID string) string {
	return tenantID + "|" + serverID
}

// Acquire returns the ring for (tenantID, serverID), creating it if absent.
func (g *LogRingRegistry) Acquire(tenantID, serverID string) *LogRing {
	if g == nil {
		return nil
	}
	k := g.key(tenantID, serverID)
	g.mu.RLock()
	r, ok := g.rings[k]
	g.mu.RUnlock()
	if ok {
		return r
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if r, ok := g.rings[k]; ok {
		return r
	}
	r = NewLogRing(0)
	g.rings[k] = r
	return r
}

// Get returns the ring or nil if no log lines have ever been published for
// the (tenantID, serverID) pair.
func (g *LogRingRegistry) Get(tenantID, serverID string) *LogRing {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.rings[g.key(tenantID, serverID)]
}

// Drop removes the ring for (tenantID, serverID) and closes it.
func (g *LogRingRegistry) Drop(tenantID, serverID string) {
	if g == nil {
		return
	}
	k := g.key(tenantID, serverID)
	g.mu.Lock()
	r, ok := g.rings[k]
	if ok {
		delete(g.rings, k)
	}
	g.mu.Unlock()
	if r != nil {
		r.Close()
	}
}

// LogsFor returns a channel of LogLine values for (tenantID, serverID).
// Closes the channel when the context is cancelled or the ring is
// dropped. If no ring exists yet (no log lines ever published), the
// channel is closed immediately so callers don't hang.
func (g *LogRingRegistry) LogsFor(ctx context.Context, tenantID, serverID string, since time.Time) <-chan LogLine {
	if g == nil {
		ch := make(chan LogLine)
		close(ch)
		return ch
	}
	r := g.Acquire(tenantID, serverID)
	return r.Subscribe(ctx, since)
}

// CloseAll closes every registered ring. Used on supervisor shutdown.
func (g *LogRingRegistry) CloseAll() {
	if g == nil {
		return
	}
	g.mu.Lock()
	rings := g.rings
	g.rings = make(map[string]*LogRing)
	g.mu.Unlock()
	for _, r := range rings {
		r.Close()
	}
}
