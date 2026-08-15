/*
Copyright 2024 The Crossplane Authors.
*/

package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pkg/errors"
)

type fakeAuthBreaker struct {
	allowError    error
	allowProbeErr error
	allowed       []string
	probeAllowed  []string
	recordedAuth  []bool
	cooldown      time.Duration
}

func (f *fakeAuthBreaker) Allow(key string, probe bool) error {
	if probe {
		f.probeAllowed = append(f.probeAllowed, key)
		if f.allowProbeErr == nil {
			return nil
		}
		return f.allowProbeErr
	}
	f.allowed = append(f.allowed, key)
	return f.allowError
}
func (f *fakeAuthBreaker) Record(key string, isAuthFailure bool) {
	f.recordedAuth = append(f.recordedAuth, isAuthFailure)
}
func (f *fakeAuthBreaker) CooldownRemaining(string) time.Duration { return f.cooldown }

func installBreaker(b AuthBreaker) func() {
	prev := authBreaker
	authBreaker = b
	return func() { authBreaker = prev }
}

func TestClient_EmptyAPIKeyRejectsBeforeHTTP(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := NewClient(Config{BaseURL: server.URL, APIKey: ""})
	_, err := c.doRequest(context.Background(), http.MethodGet, "/api/v1/channels", nil)
	if err == nil {
		t.Fatal("expected error for empty APIKey")
	}
	if called {
		t.Fatal("server should not be reached when APIKey is empty (no flood)")
	}
	if err.Error() != errNoCredentials {
		t.Errorf("want %q, got %q", errNoCredentials, err.Error())
	}
}

func TestClient_Classifies404AsNotFoundFromUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`not here`))
	}))
	defer server.Close()

	c := NewClient(Config{BaseURL: server.URL, APIKey: "k"})
	_, err := c.doRequest(context.Background(), http.MethodGet, "/x", nil)
	if err == nil {
		t.Fatal("want error from 404")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound want true for body=%q", err)
	}
}

func TestClient_Classifies401AsErrAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":"error","error":{"code":"unauthenticated"}}`))
	}))
	defer server.Close()

	rec := &fakeAuthBreaker{}
	restore := installBreaker(rec)
	defer restore()

	c := NewClient(Config{BaseURL: server.URL, APIKey: "k"})
	_, err := c.doRequest(context.Background(), http.MethodGet, "/api/v1/channels", nil)
	if err == nil {
		t.Fatal("want error from 401")
	}
	if !errors.Is(err, ErrAuth) {
		t.Errorf("expected ErrAuth-classified error, got %v", err)
	}
	if len(rec.allowed) != 1 {
		t.Fatalf("Expected exactly one allowed call, got %v", rec.allowed)
	}
	if len(rec.allowed[0]) != 64 { // sha256 hex
		t.Errorf("Expected hashed breaker key (64 hex chars), got %q", rec.allowed[0])
	}
	if len(rec.probeAllowed) != 0 {
		t.Errorf("probe calls should be 0 outside ProviderConfig reconcile, got %v", rec.probeAllowed)
	}
	if len(rec.recordedAuth) != 1 || !rec.recordedAuth[0] {
		t.Errorf("expected exactly one Record(_, true), got %v", rec.recordedAuth)
	}
}

func TestClient_Classifies403AsErrAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":"forbidden"}`))
	}))
	defer server.Close()

	rec := &fakeAuthBreaker{}
	restore := installBreaker(rec)
	defer restore()

	c := NewClient(Config{BaseURL: server.URL, APIKey: "k"})
	_, err := c.doRequest(context.Background(), http.MethodGet, "/api/v1/channels", nil)
	if err == nil {
		t.Fatal("want error from 403")
	}
	if !errors.Is(err, ErrAuth) {
		t.Errorf("expected ErrAuth-classified error, got %v", err)
	}
	if len(rec.recordedAuth) != 1 || !rec.recordedAuth[0] {
		t.Errorf("expected Record(_, true), got %v", rec.recordedAuth)
	}
}

func TestClient_Classifies429WithRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`slow down`))
	}))
	defer server.Close()

	c := NewClient(Config{BaseURL: server.URL, APIKey: "k"})
	_, err := c.doRequest(context.Background(), http.MethodGet, "/api/v1/channels", nil)
	if err == nil {
		t.Fatal("want error from 429")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatal("expected RateLimitedError concrete type")
	}
	if rl.RetryAfter != 42*time.Second {
		t.Errorf("want 42s, got %v", rl.RetryAfter)
	}
}

func TestClient_Classifies5xxAsErrTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`nope`))
	}))
	defer server.Close()

	c := NewClient(Config{BaseURL: server.URL, APIKey: "k"})
	_, err := c.doRequest(context.Background(), http.MethodGet, "/api/v1/channels", nil)
	if err == nil {
		t.Fatal("want error from 500")
	}
	if !errors.Is(err, ErrTransient) {
		t.Errorf("expected ErrTransient, got %v", err)
	}
}

func TestClient_BreakerBlocksAfterConsecutiveAuthFailures(t *testing.T) {
	// 500 server, breaker rejecting non-probe calls after threshold met.
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	rec := &fakeAuthBreaker{allowError: errors.New("breaker is open: upstream authentication is failing")}
	restore := installBreaker(rec)
	defer restore()

	c := NewClient(Config{BaseURL: server.URL, APIKey: "k"})
	_, err := c.doRequest(context.Background(), http.MethodGet, "/api/v1/channels", nil)
	if err == nil {
		t.Fatal("want error from breaker")
	}
	if called {
		t.Fatal("breaker must reject before any upstream call")
	}
	// The allow error is opaque (strings.Contains match in callers does the
	// classification), so we only assert the test sentinel here and that
	// no upstream HTTP occurred.
	if err.Error() != rec.allowError.Error() {
		t.Errorf("want breaker error verbatim, got %v", err)
	}
}

func TestMinAPIKeyLengthSetter(t *testing.T) {
	prev := MinAPIKeyLength
	defer func() { MinAPIKeyLength = prev }()

	MinAPIKeyLength = 8
	SetMinAPIKeyLength(20)
	if MinAPIKeyLength != 20 {
		t.Errorf("want 20, got %d", MinAPIKeyLength)
	}
	SetMinAPIKeyLength(-5) // negative ignored
	if MinAPIKeyLength != 20 {
		t.Errorf("negative SetMinAPIKeyLength ignored; want 20, got %d", MinAPIKeyLength)
	}
}
