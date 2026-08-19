/*
Copyright 2024 The Crossplane Authors.
*/

package breaker

import (
	"testing"
	"time"
)

// fakeClock provides a deterministic monotonic clock for tests so behaviour
// around the sliding window and cooldown can be observed exactly.
type fakeClock struct{ now time.Time }

func (f *fakeClock) nowFn() time.Time        { return f.now }
func (f *fakeClock) advance(d time.Duration) { f.now = f.now.Add(d) }

func newBreakerWithClock(threshold int, window, cooldown time.Duration, fc *fakeClock) *Breaker {
	return &Breaker{
		threshold: threshold,
		window:    window,
		cooldown:  cooldown,
		keys:      make(map[string]*keyState),
		nowFn:     fc.nowFn,
	}
}

func TestBreaker_ClosedByDefault(t *testing.T) {
	fc := &fakeClock{now: time.Unix(0, 0)}
	br := newBreakerWithClock(3, 60*time.Second, 5*time.Minute, fc)

	if err := br.Allow("k1", false); err != nil {
		t.Fatal("new breaker must allow; got", err)
	}
	if s := br.State("k1"); s != Closed {
		t.Errorf("expected Closed, got %v", s)
	}
}

func TestBreaker_TripsAfterThreshold(t *testing.T) {
	fc := &fakeClock{now: time.Unix(0, 0)}
	br := newBreakerWithClock(3, 60*time.Second, 5*time.Minute, fc)

	for i := 0; i < 3; i++ {
		br.Record("k1", true)
		fc.advance(1 * time.Second)
	}

	if err := br.Allow("k1", false); err == nil {
		t.Fatal("breaker should be open after threshold failures")
	}
	if br.State("k1") != Open {
		t.Errorf("expected Open, got %v", br.State("k1"))
	}
	if rem := br.CooldownRemaining("k1"); rem <= 4*time.Minute {
		t.Errorf("expected cooldown near 5m, got %v", rem)
	}
}

func TestBreaker_ProbeBypassesOpen(t *testing.T) {
	fc := &fakeClock{now: time.Unix(0, 0)}
	br := newBreakerWithClock(2, 60*time.Second, 5*time.Minute, fc)

	br.Record("k1", true)
	br.Record("k1", true)

	if err := br.Allow("k1", false); err == nil {
		t.Fatal("breaker should be open after threshold")
	}
	if err := br.Allow("k1", true); err != nil {
		t.Fatal("probe must be permitted even when breaker is open")
	}
}

func TestBreaker_SuccessClearsFailures(t *testing.T) {
	fc := &fakeClock{now: time.Unix(0, 0)}
	br := newBreakerWithClock(3, 60*time.Second, 5*time.Minute, fc)

	br.Record("k1", true)
	br.Record("k1", true)
	// 2 failures, then success — counter reset.
	br.Record("k1", false)
	// 3 fresh failures now trip.
	for i := 0; i < 3; i++ {
		fc.advance(1 * time.Second)
		br.Record("k1", true)
	}
	if err := br.Allow("k1", false); err == nil {
		t.Fatal("breaker should trip after 3 fresh failures following a success reset")
	}
}

func TestBreaker_SlidingWindowDropsOld(t *testing.T) {
	fc := &fakeClock{now: time.Unix(0, 0)}
	br := newBreakerWithClock(3, 60*time.Second, 5*time.Minute, fc)

	// Spread 3 failures; the first rolls out of the 60s window.
	br.Record("k1", true)
	fc.advance(35 * time.Second)
	br.Record("k1", true)
	fc.advance(35 * time.Second)
	br.Record("k1", true)
	// Total elapsed 70s. First failure is now -35s relative and must be
	// dropped; window holds only the last 2 — breaker should remain closed.
	if err := br.Allow("k1", false); err != nil {
		t.Fatal("sliding-window drop must prevent trip; got", err)
	}
}

func TestBreaker_ClosesAfterCooldown(t *testing.T) {
	fc := &fakeClock{now: time.Unix(0, 0)}
	br := newBreakerWithClock(2, 60*time.Second, 5*time.Minute, fc)

	br.Record("k1", true)
	br.Record("k1", true)
	if err := br.Allow("k1", false); err == nil {
		t.Fatal("breaker should be open before cooldown elapses")
	}

	fc.advance(5 * time.Minute) // cooldown elapses
	if err := br.Allow("k1", false); err != nil {
		t.Fatal("breaker must close once cooldown elapses; got", err)
	}
	if br.State("k1") != Closed {
		t.Errorf("expected Closed after cooldown, got %v", br.State("k1"))
	}
	br.Record("k1", false)
	if br.State("k1") != Closed {
		t.Errorf("expected Closed after success, got %v", br.State("k1"))
	}
}

func TestBreaker_KeyIsolation(t *testing.T) {
	fc := &fakeClock{now: time.Unix(0, 0)}
	br := newBreakerWithClock(2, 60*time.Second, 5*time.Minute, fc)

	br.Record("k1", true)
	br.Record("k1", true)

	if err := br.Allow("k1", false); err == nil {
		t.Fatal("k1 must block")
	}
	if err := br.Allow("k2", false); err != nil {
		t.Fatal("k2 must be unaffected by k1")
	}
}

func TestBreaker_NilNowFnDefaultsToTimeNow(t *testing.T) {
	br := New(1, 60*time.Second, 5*time.Minute)
	if err := br.Allow("k", false); err != nil {
		t.Fatal("default breaker should allow; got", err)
	}
}

func TestBreaker_RecordBeforeStateSlotCreates(t *testing.T) {
	fc := &fakeClock{now: time.Unix(0, 0)}
	br := newBreakerWithClock(2, 60*time.Second, 5*time.Minute, fc)
	// First Record on an unseen key must create the slot and succeed.
	br.Record("first", false)
	if err := br.Allow("first", false); err != nil {
		t.Fatal("allow after first-success")
	}
}

func TestBreaker_BelowThresholdStaysClosed(t *testing.T) {
	fc := &fakeClock{now: time.Unix(0, 0)}
	br := newBreakerWithClock(5, 60*time.Second, 5*time.Minute, fc)
	for i := 0; i < 4; i++ {
		br.Record("k1", true)
	}
	if err := br.Allow("k1", false); err != nil {
		t.Fatal("4 failures under threshold=5 must not open breaker")
	}
}

func TestBreaker_RecordSameKeyMultipleTimesSafe(t *testing.T) {
	// Run concurrent Records on the same key to confirm safe under mutex.
	br := New(100, 60*time.Second, 5*time.Minute)
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() {
			br.Record("k", true)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 50; i++ {
		<-done
	}
	if br.State("k") != Closed {
		t.Errorf("50 failures under threshold=100, want Closed, got %v", br.State("k"))
	}
}
