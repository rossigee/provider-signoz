// Package clients/backoff.go: classifier that turns upstream errors into a
// recommended reconcile-delay. Helpers in this file are independent of the
// controller-runtime reconciler so they can be unit-tested in isolation.
//
// The shape of the helper intentionally matches what the ProviderConfig
// reconciler and managed-resource controllers need: a typed classification
// (for branching on cause) plus a duration (for the next reconcile).
package clients

import (
	stderrors "errors"
	"time"
)

// BackoffClass is the upstream error class that triggered the backoff.
type BackoffClass int

const (
	// ClassNone means the upstream call either succeeded or returned an
	// unclassified error; requeue immediately for normal drift detection.
	ClassNone BackoffClass = iota
	// ClassAuth means credentials were rejected (401/403) and the breaker
	// is likely open; do not requeue frequently since a recovery needs
	// operator action to fix credentials.
	ClassAuth
	// ClassTransient means an internally-failed upstream call (5xx) or a
	// network/timeout; requeue quickly so the controller recovers once
	// upstream heals.
	ClassTransient
	// ClassRateLimited means the upstream returned 429 with a Retry-After
	// header; the returned duration should honour Retry-After.
	ClassRateLimited
)

// BackoffPolicy decides which duration to recommend for a given error class.
// ProviderConfig reconciler uses AuthRequeueMin/Max; managed controllers use
// TransientInitial/Cap so a transient outage doesn't sit on a 5-minute
// requeue until the ProviderConfig secrets are also fixed.
type BackoffPolicy struct {
	AuthRequeueMin   time.Duration
	AuthRequeueMax   time.Duration
	TransientInitial time.Duration
	TransientCap     time.Duration
	RateLimitedCap   time.Duration
}

// DefaultBackoffPolicy matches the project defaults agreed for this pass:
//   Auth: 5m minimum, 15m cap
//   Transient: 1s initial, 60s cap (rapid recovery)
//   Rate-limited: 5m cap (retry-after wins when smaller)
var DefaultBackoffPolicy = BackoffPolicy{
	AuthRequeueMin:   5 * time.Minute,
	AuthRequeueMax:   15 * time.Minute,
	TransientInitial: 1 * time.Second,
	TransientCap:     60 * time.Second,
	RateLimitedCap:   5 * time.Minute,
}

// Classify returns the upstream-error class. Precedence: ErrRateLimited (it
// is a sentinel for a specific 429 variant) > ErrAuth > ErrTransient > any
// other (ClassNone). Callers should always treat any ErrAuth-class as
// long-requeue regardless of breaker state.
func Classify(err error) BackoffClass {
	switch {
	case err == nil:
		return ClassNone
	case stderrors.Is(err, ErrRateLimited):
		return ClassRateLimited
	case stderrors.Is(err, ErrAuth):
		return ClassAuth
	case stderrors.Is(err, ErrTransient):
		return ClassTransient
	default:
		return ClassNone
	}
}

// RetryAfter returns the Retry-After value if err is a *RateLimitedError,
// otherwise 0. Use this to honour upstream 429 hints when present.
func RetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}
	var rl *RateLimitedError
	if stderrors.As(err, &rl) {
		return rl.RetryAfter
	}
	return 0
}

// Backoff returns the recommended requeue-after duration for `err` under
// `policy`. failureCount is the number of consecutive failures observed for
// this caller (1-based: first failure uses Initial). For ClassNone the
// convention is 0 — meaning "no special backoff, honour the caller's normal
// poll interval".
func Backoff(err error, failureCount int, policy BackoffPolicy) (BackoffClass, time.Duration) {
	class := Classify(err)
	d := RetryAfter(err)
	switch class {
	case ClassNone:
		return class, 0
	case ClassRateLimited:
		if d <= 0 {
			d = policy.RateLimitedCap
		}
		return class, clamp(d, policy.RateLimitedCap)
	case ClassAuth:
		return class, expBound(failureCount, policy.AuthRequeueMin, policy.AuthRequeueMax)
	case ClassTransient:
		return class, expBound(failureCount, policy.TransientInitial, policy.TransientCap)
	}
	return class, 0
}

func expBound(failureCount int, initial, cap time.Duration) time.Duration {
	if failureCount < 1 {
		failureCount = 1
	}
	d := initial
	for i := 1; i < failureCount; i++ {
		if d >= cap {
			return cap
		}
		d *= 2
	}
	if d > cap {
		d = cap
	}
	return d
}

func clamp(d, cap time.Duration) time.Duration {
	if d > cap {
		return cap
	}
	return d
}
