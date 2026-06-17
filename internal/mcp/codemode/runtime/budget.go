package runtime

import "time"

// Budget bounds a single sandbox execution along four independent dimensions.
// Every dimension is enforced and every dimension defaults conservative; an
// operator raises them deliberately, per route, via policy/config. The zero
// Budget is not usable — callers take DefaultBudget() and override fields.
//
// Memory is bounded by MaxAllocBytes via a heap-sampling watchdog (the step
// budget does NOT bound a single allocation op or a doubling loop — a red-team
// finding). See the threat model, class C3, for the watchdog's guarantees and
// its honest residual (a single Starlark op can transiently allocate up to the
// interpreter's built-in maxAlloc before the next sample fires; true
// per-execution memory isolation requires running the sandbox out-of-process).
type Budget struct {
	// MaxSteps caps Starlark abstract computation steps (thread step counter).
	MaxSteps uint64
	// WallClock caps total execution time; a watchdog cancels the thread at the
	// deadline.
	WallClock time.Duration
	// MaxOutputBytes caps buffered print() output; overflow is dropped.
	MaxOutputBytes int
	// MaxToolCalls caps tool calls issued from inside one execution.
	MaxToolCalls int
	// MaxAllocBytes caps the heap growth attributable to the execution. A
	// watchdog samples the process heap and cancels when growth exceeds this;
	// it catches gradual/looping allocation bombs (e.g. x = x + x) that the step
	// budget misses.
	MaxAllocBytes int64
}

// Default budget constants. These are the conservative out-of-the-box values
// from the Phase 13.5 plan (acceptance #6); operators tighten or (deliberately)
// loosen them per route.
const (
	DefaultMaxSteps       uint64        = 100_000
	DefaultWallClock      time.Duration = 30 * time.Second
	DefaultMaxOutputBytes int           = 1 << 20   // 1 MiB
	DefaultMaxToolCalls   int           = 20        //
	DefaultMaxAllocBytes  int64         = 256 << 20 // 256 MiB
)

// DefaultBudget returns the conservative default budget.
func DefaultBudget() Budget {
	return Budget{
		MaxSteps:       DefaultMaxSteps,
		WallClock:      DefaultWallClock,
		MaxOutputBytes: DefaultMaxOutputBytes,
		MaxToolCalls:   DefaultMaxToolCalls,
		MaxAllocBytes:  DefaultMaxAllocBytes,
	}
}

// normalized returns a copy with any non-positive field replaced by its
// default. This is fail-safe: a misconfigured zero budget can never mean
// "unlimited" — it means "the conservative default".
func (b Budget) normalized() Budget {
	out := b
	if out.MaxSteps == 0 {
		out.MaxSteps = DefaultMaxSteps
	}
	if out.WallClock <= 0 {
		out.WallClock = DefaultWallClock
	}
	if out.MaxOutputBytes <= 0 {
		out.MaxOutputBytes = DefaultMaxOutputBytes
	}
	if out.MaxToolCalls <= 0 {
		out.MaxToolCalls = DefaultMaxToolCalls
	}
	if out.MaxAllocBytes <= 0 {
		out.MaxAllocBytes = DefaultMaxAllocBytes
	}
	return out
}

// boundedBuffer accumulates print() output up to a hard cap. Past the cap,
// further writes are dropped (never grown), and Truncated reports that loss so
// the runtime can record it. It is not safe for concurrent use; the sandbox
// drives all writes from the single interpreter goroutine.
type boundedBuffer struct {
	buf       []byte
	max       int
	truncated bool
}

func newBoundedBuffer(max int) *boundedBuffer {
	if max <= 0 {
		max = DefaultMaxOutputBytes
	}
	return &boundedBuffer{max: max}
}

// writeLine appends s plus a newline, dropping any portion that would exceed
// the cap and marking the buffer truncated.
func (b *boundedBuffer) writeLine(s string) {
	b.write(s)
	b.write("\n")
}

func (b *boundedBuffer) write(s string) {
	if b.truncated {
		return
	}
	remaining := b.max - len(b.buf)
	if remaining <= 0 {
		b.truncated = true
		return
	}
	if len(s) > remaining {
		b.buf = append(b.buf, s[:remaining]...)
		b.truncated = true
		return
	}
	b.buf = append(b.buf, s...)
}

// String returns the accumulated output.
func (b *boundedBuffer) String() string { return string(b.buf) }

// Truncated reports whether output was dropped against the cap.
func (b *boundedBuffer) Truncated() bool { return b.truncated }
