package runtime

import (
	"testing"
	"time"
)

func TestBudget_NormalizeReplacesNonPositiveWithDefaults(t *testing.T) {
	got := Budget{}.normalized()
	if got.MaxSteps != DefaultMaxSteps {
		t.Errorf("MaxSteps = %d, want default %d", got.MaxSteps, DefaultMaxSteps)
	}
	if got.WallClock != DefaultWallClock {
		t.Errorf("WallClock = %v, want default %v", got.WallClock, DefaultWallClock)
	}
	if got.MaxOutputBytes != DefaultMaxOutputBytes {
		t.Errorf("MaxOutputBytes = %d, want default %d", got.MaxOutputBytes, DefaultMaxOutputBytes)
	}
	if got.MaxToolCalls != DefaultMaxToolCalls {
		t.Errorf("MaxToolCalls = %d, want default %d", got.MaxToolCalls, DefaultMaxToolCalls)
	}
	if got.MaxAllocBytes != DefaultMaxAllocBytes {
		t.Errorf("MaxAllocBytes = %d, want default %d", got.MaxAllocBytes, DefaultMaxAllocBytes)
	}
}

func TestBudget_NormalizeKeepsExplicitValues(t *testing.T) {
	in := Budget{MaxSteps: 7, WallClock: time.Second, MaxOutputBytes: 11, MaxToolCalls: 13, MaxAllocBytes: 4096}
	got := in.normalized()
	if got != in {
		t.Errorf("normalized mutated explicit budget: %+v", got)
	}
}

func TestBoundedBuffer_TruncatesAtCap(t *testing.T) {
	b := newBoundedBuffer(10)
	b.write("12345")
	b.write("67890")
	b.write("OVERFLOW")
	if b.Truncated() != true {
		t.Errorf("expected truncated")
	}
	if len(b.String()) != 10 {
		t.Errorf("len = %d, want 10", len(b.String()))
	}
	if b.String() != "1234567890" {
		t.Errorf("content = %q", b.String())
	}
}

func TestBoundedBuffer_WriteLineAddsNewline(t *testing.T) {
	b := newBoundedBuffer(100)
	b.writeLine("hello")
	if b.String() != "hello\n" {
		t.Errorf("content = %q, want hello\\n", b.String())
	}
}

func TestBoundedBuffer_PartialFinalWrite(t *testing.T) {
	b := newBoundedBuffer(8)
	b.write("123456")
	b.write("ABCDEF") // only "AB" fits
	if b.String() != "123456AB" {
		t.Errorf("content = %q, want 123456AB", b.String())
	}
	if !b.Truncated() {
		t.Errorf("expected truncated")
	}
}
