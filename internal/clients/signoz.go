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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/rossigee/provider-signoz/apis/v1beta1"
)

const (
	errNoProviderConfig = "no providerConfig specified"
	errGetProviderConfig = "cannot get providerConfig"
	errTrackUsage = "cannot track ProviderConfig usage"
	errExtractCredentials = "cannot extract credentials"
	errUnmarshalCredentials = "cannot unmarshal signoz credentials as JSON"
)

// Config holds SigNoz client configuration
type Config struct {
	BaseURL string
	APIKey  string
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
	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetConfig extracts SigNoz configuration from a ProviderConfig
func GetConfig(ctx context.Context, c resource.ClientApplicator, mg resource.Managed) (*Config, error) {
	// Get provider config reference from the managed resource's ResourceSpec
	var pcRef *xpv1.Reference

	// Type assert to extract the ProviderConfigReference from the managed resource
	switch mr := mg.(type) {
	case interface{ GetProviderConfigReference() *xpv1.Reference }:
		pcRef = mr.GetProviderConfigReference()
	default:
		return nil, errors.New(errGetProviderConfig)
	}

	if pcRef == nil {
		return nil, errors.New(errGetProviderConfig)
	}

	pc := &v1beta1.ProviderConfig{}
	if err := c.Get(ctx, types.NamespacedName{Name: pcRef.Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetProviderConfig)
	}

	// Use no-op tracker for v2.0.0 compatibility
	t := resource.TrackerFn(func(ctx context.Context, mg resource.Managed) error { return nil })
	if err := t.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackUsage)
	}

	data, err := resource.CommonCredentialExtractor(ctx, pc.Spec.Credentials.Source, c, pc.Spec.Credentials.CommonCredentialSelectors)
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

	return &Config{
		BaseURL: strings.TrimSuffix(endpoint, "/"),
		APIKey:  creds.APIKey,
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

// DashboardData represents a dashboard in SigNoz
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
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/dashboards", dashboard)
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
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/dashboards/%s", id), nil)
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
	resp, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/v1/dashboards/%s", id), dashboard)
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
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/dashboards/%s", id), nil)
	return err
}

// ListDashboards lists all dashboards
func (c *Client) ListDashboards(ctx context.Context) ([]*DashboardData, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/dashboards", nil)
	if err != nil {
		return nil, err
	}

	var result ListDashboardsResponse
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// Alert/Rule API methods

// RuleData represents an alert rule in SigNoz
type RuleData struct {
	ID               string                 `json:"id,omitempty"`
	AlertName        string                 `json:"alert"`
	AlertType        string                 `json:"alertType"`
	RuleType         string                 `json:"ruleType,omitempty"`
	EvalWindow       string                 `json:"evalWindow"`
	Frequency        string                 `json:"frequency"`
	Condition        map[string]interface{} `json:"condition"`
	Labels           map[string]string      `json:"labels,omitempty"`
	Annotations      map[string]string      `json:"annotations,omitempty"`
	PreferredChannels []string              `json:"preferredChannels,omitempty"`
	Disabled         bool                   `json:"disabled"`
	Version          string                 `json:"version,omitempty"`
	CreatedAt        string                 `json:"created_at,omitempty"`
	UpdatedAt        string                 `json:"updated_at,omitempty"`
	State            string                 `json:"state,omitempty"`
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
	ID        int                    `json:"id,omitempty"`
	CreatedAt string                 `json:"created_at,omitempty"`
	UpdatedAt string                 `json:"updated_at,omitempty"`
	Name      string                 `json:"name"`
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data"`
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