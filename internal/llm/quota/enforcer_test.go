package quota

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestEnforcer() (*Enforcer, *fakeClock) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	return &Enforcer{windows: make(map[string]*window), now: c.now}, c
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func exceeded(t *testing.T, err error, wantLimit string) {
	t.Helper()
	var ex *ExceededError
	if !errors.As(err, &ex) {
		t.Fatalf("expected *ExceededError, got %v", err)
	}
	if ex.Limit != wantLimit {
		t.Fatalf("exceeded limit = %q, want %q", ex.Limit, wantLimit)
	}
}

func TestEnforcer_RequestsPerMinute(t *testing.T) {
	e, _ := newTestEnforcer()
	lim := Limits{RequestsPerMinute: 2}
	if err := e.Check("t1", lim); err != nil {
		t.Fatalf("req 1: %v", err)
	}
	if err := e.Check("t1", lim); err != nil {
		t.Fatalf("req 2: %v", err)
	}
	exceeded(t, e.Check("t1", lim), "requests_per_minute")
}

func TestEnforcer_WindowRollover(t *testing.T) {
	e, clk := newTestEnforcer()
	lim := Limits{RequestsPerMinute: 1}
	if err := e.Check("t1", lim); err != nil {
		t.Fatalf("req 1: %v", err)
	}
	exceeded(t, e.Check("t1", lim), "requests_per_minute")
	clk.advance(61 * time.Second) // window rolls over
	if err := e.Check("t1", lim); err != nil {
		t.Fatalf("after rollover: %v", err)
	}
}

func TestEnforcer_TokensPerMinute(t *testing.T) {
	e, _ := newTestEnforcer()
	lim := Limits{TokensPerMinute: 100}
	if err := e.Check("t1", lim); err != nil {
		t.Fatalf("first check: %v", err)
	}
	e.RecordUsage("t1", 100)
	exceeded(t, e.Check("t1", lim), "tokens_per_minute")
}

func TestEnforcer_TokensPerDay(t *testing.T) {
	e, clk := newTestEnforcer()
	lim := Limits{TokensPerDay: 1000}
	e.RecordUsage("t1", 1000)
	exceeded(t, e.Check("t1", lim), "tokens_per_day")
	// after a minute, per-minute window cleared but per-day still counts
	clk.advance(2 * time.Minute)
	exceeded(t, e.Check("t1", lim), "tokens_per_day")
	// after a day, the per-day window clears
	clk.advance(25 * time.Hour)
	if err := e.Check("t1", lim); err != nil {
		t.Fatalf("after a day: %v", err)
	}
}

func TestEnforcer_PerTenantIsolation(t *testing.T) {
	e, _ := newTestEnforcer()
	lim := Limits{RequestsPerMinute: 1}
	if err := e.Check("a", lim); err != nil {
		t.Fatal(err)
	}
	// tenant b has its own window
	if err := e.Check("b", lim); err != nil {
		t.Fatalf("tenant b should not be affected by a: %v", err)
	}
	exceeded(t, e.Check("a", lim), "requests_per_minute")
}

func TestEnforcer_ZeroMeansUnlimited(t *testing.T) {
	e, _ := newTestEnforcer()
	lim := Limits{} // all zero
	for i := 0; i < 1000; i++ {
		if err := e.Check("t1", lim); err != nil {
			t.Fatalf("unlimited check %d: %v", i, err)
		}
	}
}

func TestEnforcer_ConcurrentSafe(t *testing.T) {
	e := NewEnforcer()
	lim := Limits{RequestsPerMinute: 100000}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = e.Check("t1", lim)
			e.RecordUsage("t1", 10)
		}()
	}
	wg.Wait()
}
