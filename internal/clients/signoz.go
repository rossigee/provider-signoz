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
	"strings"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/pkg/errors"
	"github.com/rossigee/provider-signoz/apis/v1beta1"

	"k8s.io/apimachinery/pkg/types"
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
)

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

	data, err := resource.CommonCredentialExtractor(ctx, pc.Spec.Credentials.Source, c.Client, pc.Spec.Credentials.CommonCredentialSelectors)
	if err != nil {
		return nil, errors.Wrap(err, errExtractCredentials)
	}

	creds := &Credentials{}
	if err := json.Unmarshal(data, creds); err != nil {
		return nil, errors.Wrap(err, errUnmarshalCredentials)
	}

	// Set default endpoint if not specified
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

// doRequest performs an HTTP request with authentication
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	logger := log.FromContext(ctx)

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

	if resp.StatusCode >= 400 {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				// Ignore close error in error path
				_ = err
			}
		}()
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(bodyBytes))
	}

	return resp, nil
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
	Layouts   []interface{}          `json:"layouts,omitempty"`
	Panels    map[string]interface{} `json:"panels,omitempty"`
	Variables []interface{}          `json:"variables,omitempty"`
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
