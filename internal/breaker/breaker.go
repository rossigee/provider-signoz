// Package breaker implements a per-key in-memory circuit breaker for upstream
// authentication failures.
//
// The breaker is intentionally minimal: it tracks consecutive auth errors per
// key within a sliding window; once a threshold is reached it opens for a
// cooldown period during which regular calls are rejected. Probe calls are
// still permitted while open so the ProviderConfig reconciler can verify
// recovery without re-opening on every reconcile.
//
// All operations are safe for concurrent use.
package breaker

import (
	"errors"
	"sync"
	"time"
)

// ErrBreakerOpen is returned by Allow when the breaker is open and the caller
// is not operating in probe mode.
var ErrBreakerOpen = errors.New("breaker is open: upstream authentication is failing")

// State describes the current disposition of a key, returned from Record and
// State as int for callers that don't want to import the breaker package.
type State int

const (
	// Closed means consecutive failure count is below the threshold and
	// calls are permitted.
	Closed State = iota
	// Open means the threshold was reached; calls (non-probe) are rejected
	// until the cooldown expires.
	Open
)

// Breaker counts auth failures per key and trips open when the count within
// the sliding window meets or exceeds the threshold. Once open it stays open
// until the cooldown elapses, after which the next call closes the breaker
// and records its outcome.
type Breaker struct {
	threshold int
	window    time.Duration
	cooldown  time.Duration

	mu   sync.Mutex
	keys map[string]*keyState

	nowFn func() time.Time
}

type keyState struct {
	failures  []time.Time
	openUntil time.Time
}

// New constructs a Breaker. The defaults match what the provider wires in via
// CLI flags; tests can override by constructing Breaker directly.
//
// threshold  - consecutive auth failures permitted within `window` before opening
// window     - sliding window for failure counting
// cooldown   - duration the breaker stays open after tripping
func New(threshold int, window, cooldown time.Duration) *Breaker {
	if threshold < 1 {
		threshold = 1
	}
	return &Breaker{
		threshold: threshold,
		window:    window,
		cooldown:  cooldown,
		keys:      make(map[string]*keyState),
		nowFn:     time.Now,
	}
}

// Allow returns nil if a call against `key` is permitted.
//
// probe=true allows the call to proceed even when the breaker is open. Probe
// callers (e.g. ProviderConfig credentials-check) MUST avoid mutating upstream
// state.
func (b *Breaker) Allow(key string, probe bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	ks, ok := b.keys[key]
	if !ok {
		return nil
	}
	now := b.nowFn()
	if !ks.openUntil.IsZero() && now.Before(ks.openUntil) {
		if probe {
			return nil
		}
		return ErrBreakerOpen
	}
	// Cooldown elapsed; reset state so a probe becomes the next trial.
	if !ks.openUntil.IsZero() && !now.Before(ks.openUntil) {
		ks.failures = nil
		ks.openUntil = time.Time{}
	}
	return nil
}

// Record reports an upstream outcome for `key`. isAuthFailure=true increments
// the failure count and may trip the breaker. isAuthFailure=false records a
// success which clears the failure window.
func (b *Breaker) Record(key string, isAuthFailure bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ks, ok := b.keys[key]
	if !ok {
		ks = &keyState{}
		b.keys[key] = ks
	}
	now := b.nowFn()
	if isAuthFailure {
		// Drop failures older than the sliding window.
		cutoff := now.Add(-b.window)
		fresh := ks.failures[:0]
		for _, t := range ks.failures {
			if t.After(cutoff) {
				fresh = append(fresh, t)
			}
		}
		ks.failures = append(fresh, now)
		if len(ks.failures) >= b.threshold {
			ks.openUntil = now.Add(b.cooldown)
		}
		return
	}
	// Success clears the window.
	ks.failures = nil
	ks.openUntil = time.Time{}
}

// State returns the current disposition of `key` for logging/metrics.
func (b *Breaker) State(key string) State {
	b.mu.Lock()
	defer b.mu.Unlock()
	ks, ok := b.keys[key]
	if !ok {
		return Closed
	}
	now := b.nowFn()
	if !ks.openUntil.IsZero() && now.Before(ks.openUntil) {
		return Open
	}
	return Closed
}

// CooldownRemaining returns the time until the breaker will close on `key`,
// or 0 if the breaker is closed.
func (b *Breaker) CooldownRemaining(key string) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	ks, ok := b.keys[key]
	if !ok || ks.openUntil.IsZero() {
		return 0
	}
	d := time.Until(ks.openUntil)
	if d < 0 {
		return 0
	}
	return d
}
