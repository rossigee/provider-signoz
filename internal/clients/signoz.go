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
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/pkg/errors"
	"github.com/rossigee/provider-signoz/apis/v1beta1"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// GenerateExternalName generates a deterministic UUID-v7 from namespace and name
func GenerateExternalName(namespace, name string) string {
	seed := fmt.Sprintf("provider-signoz/v1/%s/%s", namespace, name)
	h := sha256.New()
	h.Write([]byte(seed))
	hash := h.Sum(nil)

	b := [16]byte{}
	copy(b[:6], hash[:6])
	b[6] = (hash[6] & 0x0f) | 0x70 // version 7
	b[7] = hash[7]
	b[8] = (hash[8] & 0x3f) | 0x80 // variant
	copy(b[9:], hash[9:16])

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

const (
	errNoProviderConfig     = "no providerConfig specified"
	errGetProviderConfig    = "cannot get providerConfig"
	errTrackUsage           = "cannot track ProviderConfig usage"
	errExtractCredentials   = "cannot extract credentials"
	errUnmarshalCredentials = "cannot unmarshal signoz credentials as JSON"
	errEmptyAPIKey          = "signoz credentials are missing an apiKey (misconfigured ProviderConfig or empty secret)"
	errShortAPIKey          = "signoz credentials apiKey is shorter than the configured minimum"
	errNoCredentials        = "refusing to call Signoz API: apiKey is empty"
)

// Sentinel errors returned by API calls. Callers (controllers) branch on these
// to choose the correct reconcile backoff (see controller backoff helpers).
var (
	ErrAuth        = errors.New("signoz API: authentication failed")
	ErrTransient   = errors.New("signoz API: transient error")
	ErrRateLimited = errors.New("signoz API: rate limited")
)

// RateLimitedError is returned when the upstream Signoz API returns HTTP 429.
// It carries the Retry-After duration when the upstream provides one so callers
// can honour it instead of using a generic backoff.
type RateLimitedError struct {
	RetryAfter time.Duration
	Body       string
}

func (e *RateLimitedError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("signoz API: rate limited (retry after %s) - %s", e.RetryAfter, e.Body)
	}
	return fmt.Sprintf("signoz API: rate limited - %s", e.Body)
}

// Is reports the sentinel ErrRateLimited so callers can use errors.Is for
// type-switching while still accessing Retry-After via errors.As.
func (e *RateLimitedError) Is(target error) bool {
	return target == ErrRateLimited
}

// MinAPIKeyLength is the minimum acceptable length of an extracted Signoz
// apiKey. Keys shorter than this are almost certainly a misconfiguration
// (empty secret, placeholder, wrong key) and are rejected before any
// upstream call is attempted, preventing repeated 401 hammering of the
// Signoz API.
//
// The default of 8 is intentionally generous; legitimate Signoz API keys are
// much longer. Override at provider startup with SetMinAPIKeyLength.
var MinAPIKeyLength = 8

// SetMinAPIKeyLength overrides the package-level minimum. Negative values are
// ignored. Intended to be called once at provider boot from flag parsing.
func SetMinAPIKeyLength(n int) {
	if n < 0 {
		return
	}
	MinAPIKeyLength = n
}

// AuthBreaker is the package-level circuit breaker for upstream auth
// failures. It is shared across all Client instances in this process and is
// keyed by the destination BaseURL plus an apiKey fingerprint, so a bad
// ProviderConfig trips its own breaker without affecting healthy ones.
//
// SetAuthBreaker lets main() install a configured breaker; tests can disable
// the breaker by passing nil (the resulting "no-op" behaves as always closed).
func SetAuthBreaker(b AuthBreaker) {
	authBreaker = b
}

// AuthBreaker is the minimal interface the Client needs from a breaker.
// internal/breaker.Breaker satisfies it; tests can substitute fakes without
// importing the breaker package.
type AuthBreaker interface {
	Allow(key string, probe bool) error
	Record(key string, isAuthFailure bool)
	CooldownRemaining(key string) time.Duration
}

var authBreaker AuthBreaker = noopBreaker{}

type noopBreaker struct{}

func (noopBreaker) Allow(string, bool) error              { return nil }
func (noopBreaker) Record(string, bool)                   {}
func (noopBreaker) CooldownRemaining(string) time.Duration { return 0 }

// breakerKey returns the lookup key for the auth breaker: a sha256-derived
// fingerprint of BaseURL + ":" + apiKey so a misconfigured ProviderConfig is
// isolated from healthy ones.
func breakerKey(baseURL, apiKey string) string {
	h := sha256.New()
	h.Write([]byte(baseURL))
	h.Write([]byte{0})
	h.Write([]byte(apiKey))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Config holds SigNoz client configuration
type Config struct {
	BaseURL               string
	APIKey                string
	InsecureSkipTLSVerify bool
}

// Credentials holds SigNoz authentication credentials
type Credentials struct {
	APIKey string `json:"apiKey"`
}

// Client is a SigNoz API client
type Client struct {
	config     Config
	httpClient *http.Client
}

// NewClient creates a new SigNoz API client
func NewClient(cfg Config) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipTLSVerify}, //nolint:gosec // opt-in via ProviderConfig
	}
	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

// GetConfig extracts SigNoz configuration from a ProviderConfig
func GetConfig(ctx context.Context, c resource.ClientApplicator, mg resource.Managed) (*Config, error) {
	// Get provider config reference from the managed resource's ResourceSpec
	var pcRef *xpv1.ProviderConfigReference

	// Type assert to extract the ProviderConfigReference from the managed resource.
	// The forked crossplane-runtime defines TypedProviderConfigReferencer (new style)
	// which returns *ProviderConfigReference instead of *Reference.
	switch mr := mg.(type) {
	case resource.TypedProviderConfigReferencer:
		pcRef = mr.GetProviderConfigReference()
	case interface {
		GetProviderConfigReference() *xpv1.ProviderConfigReference
	}:
		pcRef = mr.GetProviderConfigReference()
	case interface{ GetProviderConfigReference() *xpv1.Reference }:
		// Legacy fallback for older CRDs returning *Reference
		r := mr.GetProviderConfigReference()
		if r != nil {
			pcRef = &xpv1.ProviderConfigReference{Name: r.Name}
		}
	default:
		return nil, errors.New(errGetProviderConfig)
	}

	if pcRef == nil {
		return nil, errors.New(errGetProviderConfig)
	}

	pc := &v1beta1.ProviderConfig{}
	// Try cluster-scoped first (newer Crossplane), then namespace-scoped
	logger := log.FromContext(ctx)
	logger.V(1).Info("DEBUG GetConfig: attempting cluster-scoped ProviderConfig lookup", "name", pcRef.Name, "namespace", "")
	if err := c.Get(ctx, types.NamespacedName{Name: pcRef.Name}, pc); err != nil {
		logger.V(1).Info("DEBUG GetConfig: cluster-scoped lookup failed, trying namespace-scoped", "error", err, "namespace", mg.GetNamespace())
		// If not found cluster-scoped, try with the managed resource's namespace
		if err := c.Get(ctx, types.NamespacedName{Namespace: mg.GetNamespace(), Name: pcRef.Name}, pc); err != nil {
			logger.V(1).Info("DEBUG GetConfig: namespace-scoped lookup also failed", "error", err)
			return nil, errors.Wrap(err, errGetProviderConfig)
		}
		logger.V(1).Info("DEBUG GetConfig: namespace-scoped lookup succeeded")
	}

	// Use no-op tracker for xpv1.0.0 compatibility
	t := resource.ModernTrackerFn(func(ctx context.Context, mg resource.ModernManaged) error { return nil })
	if err := t.Track(ctx, mg.(resource.ModernManaged)); err != nil {
		return nil, errors.Wrap(err, errTrackUsage)
	}

	return GetConfigFromProviderConfig(ctx, c.Client, pc)
}

// GetConfigFromProviderConfig extracts Signoz Config from an already-loaded
// ProviderConfig object. Used by the ProviderConfig reconciler which doesn't
// have a managed resource to reference — and is the single source of truth
// for credential-validation logic shared with managed-resource controllers.
func GetConfigFromProviderConfig(ctx context.Context, c client.Client, pc *v1beta1.ProviderConfig) (*Config, error) {
	data, err := resource.CommonCredentialExtractor(ctx, pc.Spec.Credentials.Source, c, pc.Spec.Credentials.CommonCredentialSelectors)
	if err != nil {
		return nil, errors.Wrap(err, errExtractCredentials)
	}

	creds := &Credentials{}
	if err := json.Unmarshal(data, creds); err != nil {
		return nil, errors.Wrap(err, errUnmarshalCredentials)
	}

	if creds.APIKey == "" {
		return nil, errors.New(errEmptyAPIKey)
	}
	if MinAPIKeyLength > 0 && len(creds.APIKey) < MinAPIKeyLength {
		return nil, errors.Errorf("%s (got %d chars, need >= %d)", errShortAPIKey, len(creds.APIKey), MinAPIKeyLength)
	}

	endpoint := "https://api.signoz.cloud"
	if pc.Spec.Endpoint != nil && *pc.Spec.Endpoint != "" {
		endpoint = *pc.Spec.Endpoint
	}

	skipTLS := false
	if pc.Spec.InsecureSkipTLSVerify != nil {
		skipTLS = *pc.Spec.InsecureSkipTLSVerify
	}

	return &Config{
		BaseURL:               strings.TrimSuffix(endpoint, "/"),
		APIKey:                creds.APIKey,
		InsecureSkipTLSVerify: skipTLS,
	}, nil
}

// doRequest performs an HTTP request with authentication.
//
// If the client is misconfigured with an empty APIKey, the call is rejected
// before any TCP connection is opened. This prevents a single bad ProviderConfig
// from hammering the upstream Signoz API with unauthenticated traffic.
//
// For probe calls (used by ProviderConfig credentials check), use probeRequest
// instead — it bypasses the breaker so the credentials check can still verify
// recovery after the breaker has tripped.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	logger := log.FromContext(ctx)

	if c.config.APIKey == "" {
		return nil, errors.New(errNoCredentials)
	}

	key := breakerKey(c.config.BaseURL, c.config.APIKey)
	if err := authBreaker.Allow(key, false); err != nil {
		logger.V(1).Info("AuthBreaker: rejecting call", "key", key, "cooldown_remaining", authBreaker.CooldownRemaining(key).String())
		return nil, err
	}

	url := fmt.Sprintf("%s%s", c.config.BaseURL, path)

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal request body")
		}
		bodyReader = bytes.NewReader(jsonBody)
		logger.V(1).Info("Request body", "body", string(jsonBody))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("SIGNOZ-API-KEY", c.config.APIKey)

	logger.V(1).Info("Making request", "method", method, "url", url)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute request")
	}

	// Record outcome on the auth breaker before any classification so the
	// breaker observes every upstream result.
	authFailure := resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden
	authBreaker.Record(key, authFailure)

	if resp.StatusCode >= 400 {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				// Ignore close error in error path
				_ = err
			}
		}()
		bodyBytes, _ := io.ReadAll(resp.Body)
		body := string(bodyBytes)
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, errors.Wrapf(ErrAuth, "%s - %s", resp.Status, body)
		case resp.StatusCode == http.StatusTooManyRequests:
			ra := parseRetryAfter(resp.Header.Get("Retry-After"))
			return nil, &RateLimitedError{RetryAfter: ra, Body: fmt.Sprintf("%s - %s", resp.Status, body)}
		case resp.StatusCode >= 500:
			return nil, errors.Wrapf(ErrTransient, "%s - %s", resp.Status, body)
		default:
			return nil, fmt.Errorf("API error: %s - %s", resp.Status, body)
		}
	}

	return resp, nil
}

// probeRequest performs an HTTP request for ProviderConfig credentials check.
// It bypasses the auth breaker (key=probe, true) so a recovery probe can
// verify credentials even while the breaker is open.
//
// probeRequest intentionally performs only safe GET operations; callers must
// not use it for state-mutating verbs.
func (c *Client) probeRequest(ctx context.Context, method, path string) (*http.Response, error) {
	logger := log.FromContext(ctx)

	if c.config.APIKey == "" {
		return nil, errors.New(errNoCredentials)
	}

	url := fmt.Sprintf("%s%s", c.config.BaseURL, path)
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create probe request")
	}
	req.Header.Set("SIGNOZ-API-KEY", c.config.APIKey)

	logger.V(1).Info("Probe request", "method", method, "url", url)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute probe request")
	}

	// Probe outcomes update the breaker so a successful probe closes the
	// breaker; a failing probe re-arms it.
	key := breakerKey(c.config.BaseURL, c.config.APIKey)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		authBreaker.Record(key, true)
	} else if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		authBreaker.Record(key, false)
	}
	return resp, nil
}

// ProbeCredentials verifies that the configured credentials are accepted by
// the upstream Signoz API. It performs a cheap, side-effect-free GET against
// the channels endpoint and returns nil on a 2xx response, a typed error
// otherwise. The result is fed back to the breaker.
func (c *Client) ProbeCredentials(ctx context.Context) error {
	resp, err := c.probeRequest(ctx, http.MethodGet, "/api/v1/channels?limit=1")
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return errors.Wrapf(ErrAuth, "%s - %s", resp.Status, body)
	case resp.StatusCode >= 500:
		return errors.Wrapf(ErrTransient, "%s - %s", resp.Status, body)
	default:
		return fmt.Errorf("probe failed: %s - %s", resp.Status, body)
	}
}

// parseResponse parses the response body into the given interface
func parseResponse(resp *http.Response, v interface{}) error {
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// Ignore close error
			_ = err
		}
	}()

	if v == nil {
		return nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "failed to read response body")
	}

	if len(bodyBytes) == 0 {
		return nil
	}

	if err := json.Unmarshal(bodyBytes, v); err != nil {
		return errors.Wrapf(err, "failed to unmarshal response: %s", string(bodyBytes))
	}

	return nil
}

// parseRetryAfter parses a Retry-After header value, supporting both
// the delay-seconds form (e.g. "120") and the HTTP-date form.
// Returns 0 if the value is empty or unparseable.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

// Dashboard API methods

// DashboardData represents a dashboard in SigNoz (V1 format - deprecated)
type DashboardData struct {
	ID          string                 `json:"id,omitempty"`
	UUID        string                 `json:"uuid,omitempty"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	Layout      []interface{}          `json:"layout,omitempty"`
	Widgets     []interface{}          `json:"widgets"`
	Variables   map[string]interface{} `json:"variables,omitempty"`
	CreatedAt   string                 `json:"created_at,omitempty"`
	UpdatedAt   string                 `json:"updated_at,omitempty"`
}

// DashboardTag represents a tag in V2 format
type DashboardTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// DashboardV2Spec represents the spec section of a V2 dashboard
type DashboardV2Spec struct {
	Display   *DashboardV2Display    `json:"display,omitempty"`
	Layouts   []interface{}          `json:"layouts"`
	Panels    map[string]interface{} `json:"panels,omitempty"`
	Variables []interface{}          `json:"variables"`
}

// DashboardV2Display represents the display section of a V2 dashboard
type DashboardV2Display struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// DashboardV2Data represents the full V2 dashboard response including ID
type DashboardV2Data struct {
	Name          string          `json:"name,omitempty"`
	ID            string          `json:"id,omitempty"`
	UUID          string          `json:"uuid,omitempty"`
	SchemaVersion string          `json:"schemaVersion,omitempty"`
	Tags          []DashboardTag  `json:"tags,omitempty"`
	Spec          DashboardV2Spec `json:"spec"`
	CreatedAt     string          `json:"created_at,omitempty"`
	UpdatedAt     string          `json:"updated_at,omitempty"`
}

// DashboardV2Response wraps V2 dashboard API responses
type DashboardV2Response struct {
	Status string           `json:"status"`
	Data   *DashboardV2Data `json:"data"`
}

// ListDashboardsV2Response wraps list dashboards V2 response
type ListDashboardsV2Response struct {
	Status string             `json:"status"`
	Data   []*DashboardV2Data `json:"data"`
}

// DashboardResponse wraps dashboard API responses
type DashboardResponse struct {
	Status string         `json:"status"`
	Data   *DashboardData `json:"data"`
}

// ListDashboardsResponse wraps list dashboards response
type ListDashboardsResponse struct {
	Status string           `json:"status"`
	Data   []*DashboardData `json:"data"`
}

// CreateDashboard creates a new dashboard
func (c *Client) CreateDashboard(ctx context.Context, dashboard *DashboardData) (*DashboardData, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v2/dashboards", dashboard)
	if err != nil {
		return nil, err
	}

	var result DashboardResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// GetDashboard retrieves a dashboard by ID
func (c *Client) GetDashboard(ctx context.Context, id string) (*DashboardData, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v2/dashboards/%s", id), nil)
	if err != nil {
		return nil, err
	}

	var result DashboardResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// UpdateDashboard updates an existing dashboard
func (c *Client) UpdateDashboard(ctx context.Context, id string, dashboard *DashboardData) (*DashboardData, error) {
	resp, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/v2/dashboards/%s", id), dashboard)
	if err != nil {
		return nil, err
	}

	var result DashboardResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// DeleteDashboard deletes a dashboard
func (c *Client) DeleteDashboard(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v2/dashboards/%s", id), nil)
	return err
}

// ListDashboards lists all dashboards
func (c *Client) ListDashboards(ctx context.Context) ([]*DashboardData, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v2/dashboards", nil)
	if err != nil {
		return nil, err
	}

	var result ListDashboardsResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// CreateDashboardV2 creates a new dashboard using V2 API
func (c *Client) CreateDashboardV2(ctx context.Context, dashboard *DashboardV2Data) (*DashboardV2Data, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v2/dashboards", dashboard)
	if err != nil {
		return nil, err
	}

	var result DashboardV2Response
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// GetDashboardV2 retrieves a dashboard by ID using V2 API
func (c *Client) GetDashboardV2(ctx context.Context, id string) (*DashboardV2Data, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v2/dashboards/%s", id), nil)
	if err != nil {
		return nil, err
	}

	var result DashboardV2Response
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// UpdateDashboardV2 updates an existing dashboard using V2 API
func (c *Client) UpdateDashboardV2(ctx context.Context, id string, dashboard *DashboardV2Data) (*DashboardV2Data, error) {
	resp, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/v2/dashboards/%s", id), dashboard)
	if err != nil {
		return nil, err
	}

	var result DashboardV2Response
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// ListDashboardsV2 lists all dashboards using V2 API
func (c *Client) ListDashboardsV2(ctx context.Context) ([]*DashboardV2Data, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v2/dashboards", nil)
	if err != nil {
		return nil, err
	}

	var result ListDashboardsV2Response
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// Alert/Rule API methods

// RuleData represents an alert rule in SigNoz
type RuleData struct {
	ID                string                 `json:"id,omitempty"`
	AlertName         string                 `json:"alert"`
	AlertType         string                 `json:"alertType"`
	RuleType          string                 `json:"ruleType,omitempty"`
	EvalWindow        string                 `json:"evalWindow"`
	Frequency         string                 `json:"frequency"`
	Condition         map[string]interface{} `json:"condition"`
	Labels            map[string]string      `json:"labels,omitempty"`
	Annotations       map[string]string      `json:"annotations,omitempty"`
	PreferredChannels []string               `json:"preferredChannels,omitempty"`
	Disabled          bool                   `json:"disabled"`
	Severity          string                 `json:"severity,omitempty"`
	Version           string                 `json:"version,omitempty"`
	CreatedAt         string                 `json:"created_at,omitempty"`
	UpdatedAt         string                 `json:"updated_at,omitempty"`
	State             string                 `json:"state,omitempty"`
}

// RuleResponse wraps rule API responses
type RuleResponse struct {
	Status string    `json:"status"`
	Data   *RuleData `json:"data"`
}

// ListRulesResponse wraps list rules response
type ListRulesResponse struct {
	Status string      `json:"status"`
	Data   []*RuleData `json:"data"`
}

// CreateRule creates a new alert rule
func (c *Client) CreateRule(ctx context.Context, rule *RuleData) (*RuleData, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/rules", rule)
	if err != nil {
		return nil, err
	}

	var result RuleResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// GetRule retrieves a rule by ID
func (c *Client) GetRule(ctx context.Context, id string) (*RuleData, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/rules/%s", id), nil)
	if err != nil {
		return nil, err
	}

	var result RuleResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// UpdateRule updates an existing rule
func (c *Client) UpdateRule(ctx context.Context, id string, rule *RuleData) (*RuleData, error) {
	resp, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/v1/rules/%s", id), rule)
	if err != nil {
		return nil, err
	}

	var result RuleResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// DeleteRule deletes a rule
func (c *Client) DeleteRule(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/rules/%s", id), nil)
	return err
}

// ListRules lists all rules
func (c *Client) ListRules(ctx context.Context) ([]*RuleData, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/rules", nil)
	if err != nil {
		return nil, err
	}

	var result ListRulesResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// NotificationChannel API methods

// ChannelData represents a notification channel in SigNoz
type ChannelData struct {
	ID               string        `json:"id,omitempty"`
	CreatedAt        string        `json:"createdAt,omitempty"`
	UpdatedAt        string        `json:"updatedAt,omitempty"`
	Name             string        `json:"name"`
	Type             string        `json:"type"`
	Data             string        `json:"data,omitempty"`
	WebhookConfigs   []interface{} `json:"webhook_configs,omitempty"`
	SlackConfigs     []interface{} `json:"slack_configs,omitempty"`
	EmailConfigs     []interface{} `json:"email_configs,omitempty"`
	OpsGenieConfigs  []interface{} `json:"opsgenie_configs,omitempty"`
	MSTeamsConfigs   []interface{} `json:"msteams_configs,omitempty"`
	SNSConfigs       []interface{} `json:"sns_configs,omitempty"`
	PagerDutyConfigs []interface{} `json:"pagerduty_configs,omitempty"`
}

// ChannelResponse wraps channel API responses
type ChannelResponse struct {
	Status string       `json:"status"`
	Data   *ChannelData `json:"data"`
}

// ListChannelsResponse wraps list channels response
type ListChannelsResponse struct {
	Status string         `json:"status"`
	Data   []*ChannelData `json:"data"`
}

// CreateChannel creates a new notification channel
func (c *Client) CreateChannel(ctx context.Context, channel *ChannelData) (*ChannelData, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/channels", channel)
	if err != nil {
		return nil, err
	}

	var result ChannelResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// GetChannel retrieves a channel by ID
func (c *Client) GetChannel(ctx context.Context, id string) (*ChannelData, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/channels/%s", id), nil)
	if err != nil {
		return nil, err
	}

	var result ChannelResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// UpdateChannel updates an existing channel
func (c *Client) UpdateChannel(ctx context.Context, id string, channel *ChannelData) (*ChannelData, error) {
	resp, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/v1/channels/%s", id), channel)
	if err != nil {
		return nil, err
	}

	var result ChannelResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// DeleteChannel deletes a channel
func (c *Client) DeleteChannel(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%s", id), nil)
	return err
}

// ListChannels lists all channels
func (c *Client) ListChannels(ctx context.Context) ([]*ChannelData, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/channels", nil)
	if err != nil {
		return nil, err
	}

	var result ListChannelsResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// TestChannel tests a notification channel
func (c *Client) TestChannel(ctx context.Context, channelData *ChannelData) error {
	_, err := c.doRequest(ctx, http.MethodPost, "/api/v1/testChannel", channelData)
	return err
}

// IsNotFound returns true if the error indicates a resource was not found
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found")
}
