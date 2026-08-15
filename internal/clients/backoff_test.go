/*
Copyright 2024 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package clients

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	pkgerrors "github.com/pkg/errors"
)

func TestClassify_NilOrUnknown(t *testing.T) {
	if c := Classify(nil); c != ClassNone {
		t.Errorf("nil: want ClassNone, got %v", c)
	}
	if c := Classify(errors.New("random downstream failure")); c != ClassNone {
		t.Errorf("unknown err: want ClassNone, got %v", c)
	}
}

func TestClassify_SentinelMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want BackoffClass
	}{
		{"ErrAuth", ErrAuth, ClassAuth},
		{"ErrTransient", ErrTransient, ClassTransient},
		{"ErrRateLimited-direct", ErrRateLimited, ClassRateLimited},
		{"ErrRateLimited-wrapped", pkgerrors.Wrap(ErrRateLimited, "x"), ClassRateLimited},
		{"ErrAuth-wrapped", pkgerrors.Wrap(ErrAuth, "x"), ClassAuth},
		{"ErrTransient-wrapped", pkgerrors.Wrap(ErrTransient, "x"), ClassTransient},
		// Plain string-shaped errors (no wrap) must NOT register as classified.
		{"unwrapped-plain", errors.New("forbidden"), ClassNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != tc.want {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestRetryAfter_Parsing(t *testing.T) {
	if ra := RetryAfter(nil); ra != 0 {
		t.Errorf("nil err: want 0, got %v", ra)
	}
	if ra := RetryAfter(errors.New("not a rate limit")); ra != 0 {
		t.Errorf("non-rl err: want 0, got %v", ra)
	}
	// Numeric form
	rl := &RateLimitedError{RetryAfter: 30 * time.Second, Body: "ok"}
	if ra := RetryAfter(rl); ra != 30*time.Second {
		t.Errorf("want 30s, got %v", ra)
	}
	// Zero Retry-After → caller-supplied RetryAfter is preserved verbatim.
	rl0 := &RateLimitedError{RetryAfter: 0, Body: ""}
	if ra := RetryAfter(rl0); ra != 0 {
		t.Errorf("want 0, got %v", ra)
	}
}

func TestBackoff_LinearUntilCap(t *testing.T) {
	p := BackoffPolicy{
		AuthRequeueMin:   5 * time.Minute,
		AuthRequeueMax:   15 * time.Minute,
		TransientInitial: 1 * time.Second,
		TransientCap:     60 * time.Second,
		RateLimitedCap:   5 * time.Minute,
	}
	// ClassNone always returns 0 (caller uses default poll interval).
	if _, d := Backoff(nil, 1, p); d != 0 {
		t.Errorf("ClassNone: want 0, got %v", d)
	}

	// ClassAuth: stays at min for first failure (count=1).
	if _, d := Backoff(ErrAuth, 1, p); d != 5*time.Minute {
		t.Errorf("auth#1: want 5m, got %v", d)
	}
	// ClassAuth doubles each time, capped at max.
	if _, d := Backoff(ErrAuth, 2, p); d != 10*time.Minute {
		t.Errorf("auth#2: want 10m, got %v", d)
	}
	if _, d := Backoff(ErrAuth, 3, p); d != 15*time.Minute {
		t.Errorf("auth#3: want 15m (already capped), got %v", d)
	}
	// Beyond the cap stays at cap.
	if _, d := Backoff(ErrAuth, 100, p); d != 15*time.Minute {
		t.Errorf("auth#100: want 15m (capped), got %v", d)
	}

	// ClassTransient: 1s, 2s, 4s, 8s, …, 60s (capped).
	for _, cnt := range []int{1, 2, 3, 4} {
		expected := time.Duration(1<<uint(cnt-1)) * time.Second
		if _, d := Backoff(ErrTransient, cnt, p); d != expected {
			t.Errorf("transient#%d: want %v, got %v", cnt, expected, d)
		}
	}
	if _, d := Backoff(ErrTransient, 10, p); d != 60*time.Second {
		t.Errorf("transient#10: want 60s capped, got %v", d)
	}

	// ClassRateLimited: respects Retry-After capped by RateLimitedCap.
	rl := &RateLimitedError{RetryAfter: 90 * time.Second, Body: "x"}
	if _, d := Backoff(rl, 1, p); d != 90*time.Second {
		t.Errorf("rl w/ 90s: want 90s, got %v", d)
	}
	// Retry-After > cap → cap.
	rlbig := &RateLimitedError{RetryAfter: 1 * time.Hour, Body: "x"}
	if _, d := Backoff(rlbig, 1, p); d != 5*time.Minute {
		t.Errorf("rl w/ 1h: want 5m capped, got %v", d)
	}
	// No Retry-After → cap (default).
	if _, d := Backoff(&RateLimitedError{RetryAfter: 0, Body: ""}, 1, p); d != 5*time.Minute {
		t.Errorf("rl w/o Retry-After: want 5m, got %v", d)
	}
}

func TestRateLimitedError_IsSentinel(t *testing.T) {
	rl := &RateLimitedError{RetryAfter: 5 * time.Second, Body: "x"}
	if !errors.Is(rl, ErrRateLimited) {
		t.Errorf("RateLimitedError must satisfy errors.Is(_, ErrRateLimited)")
	}
	if errors.Is(rl, ErrAuth) {
		t.Errorf("RateLimitedError must NOT satisfy errors.Is(_, ErrAuth)")
	}
	// Plain error must not match without wrap-as-sentinel.
	if errors.Is(errors.New("forbidden"), ErrAuth) {
		t.Errorf("a non-wrapped error must not match ErrAuth")
	}
}

func TestParseRetryAfter_HttpDate(t *testing.T) {
	// Build a date 30 seconds in the future and verify the parser accepts it.
	future := time.Now().UTC().Add(30 * time.Second)
	httpDate := future.Format(http.TimeFormat)
	if d := parseRetryAfter(httpDate); d < 29*time.Second || d > 31*time.Second {
		t.Errorf("HTTP-date 30s in future: want ~30s, got %v", d)
	}

	// Past date → 0 (no negative wait).
	past := time.Now().UTC().Add(-1 * time.Hour)
	if d := parseRetryAfter(past.Format(http.TimeFormat)); d != 0 {
		t.Errorf("past HTTP-date: want 0, got %v", d)
	}
}

func TestParseRetryAfter_SecondsForm(t *testing.T) {
	if d := parseRetryAfter(""); d != 0 {
		t.Errorf("empty: want 0, got %v", d)
	}
	if d := parseRetryAfter("3"); d != 3*time.Second {
		t.Errorf("3: want 3s, got %v", d)
	}
	if d := parseRetryAfter("-1"); d != 0 {
		t.Errorf("negative: want 0, got %v", d)
	}
	if d := parseRetryAfter("bogus-date"); d != 0 {
		t.Errorf("bad date: want 0, got %v", d)
	}
	if d := parseRetryAfter(strings.TrimSpace("  120  ")); d != 120*time.Second {
		t.Errorf("trimmed 120: want 120s, got %v", d)
	}
}
