package process

import (
	"testing"
	"time"
)

func TestBackoff_DefaultProgression(t *testing.T) {
	b := DefaultBackoff()
	want := []time.Duration{
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
	}
	for i, expected := range want {
		got := b.Next(i + 1)
		// Jitter is ±20%; allow 25% slack.
		min := time.Duration(float64(expected) * 0.75)
		max := time.Duration(float64(expected) * 1.25)
		if got < min || got > max {
			t.Errorf("attempt %d: got %v, want roughly %v (±20%%)", i+1, got, expected)
		}
	}
}

func TestBackoff_CapsAtMax(t *testing.T) {
	b := Backoff{Initial: time.Second, Max: 5 * time.Second, Multiplier: 2.0, Jitter: 0}
	for i := 1; i <= 10; i++ {
		got := b.Next(i)
		if got > 5*time.Second+100*time.Millisecond { // small slack
			t.Errorf("attempt %d: got %v, exceeds Max=5s", i, got)
		}
	}
}

func TestBackoff_AttemptZero(t *testing.T) {
	b := DefaultBackoff()
	got := b.Next(0)
	if got <= 0 {
		t.Errorf("attempt=0 should return >=Initial, got %v", got)
	}
}
