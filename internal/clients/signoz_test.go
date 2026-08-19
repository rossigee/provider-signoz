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
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestNewClient(t *testing.T) {
	cfg := Config{
		BaseURL: "https://api.signoz.io",
		APIKey:  "test-key",
	}

	client := NewClient(cfg)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.config.BaseURL != cfg.BaseURL {
		t.Errorf("Expected BaseURL %s, got %s", cfg.BaseURL, client.config.BaseURL)
	}

	if client.config.APIKey != cfg.APIKey {
		t.Errorf("Expected APIKey %s, got %s", cfg.APIKey, client.config.APIKey)
	}

	if client.httpClient == nil {
		t.Error("HTTP client is nil")
	}

	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", client.httpClient.Timeout)
	}
}

func TestClient_CreateDashboard(t *testing.T) {
	// Create a test server that returns a successful response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		if r.URL.Path != "/api/v2/dashboards" {
			t.Errorf("Expected path /api/v2/dashboards, got %s", r.URL.Path)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		if r.Header.Get("SIGNOZ-API-KEY") != "test-key" {
			t.Errorf("Expected SIGNOZ-API-KEY test-key, got %s", r.Header.Get("SIGNOZ-API-KEY"))
		}

		response := DashboardResponse{
			Status: "success",
			Data: &DashboardData{
				ID:          "dashboard-123",
				UUID:        "uuid-456",
				Title:       "Test Dashboard",
				Description: "Test description",
				Tags:        []string{"test"},
				CreatedAt:   "2023-01-01T00:00:00Z",
				UpdatedAt:   "2023-01-01T00:00:00Z",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
	}

	client := NewClient(cfg)

	dashboard := &DashboardData{
		Title:       "Test Dashboard",
		Description: "Test description",
		Tags:        []string{"test"},
		Widgets:     []interface{}{},
	}

	result, err := client.CreateDashboard(context.Background(), dashboard)
	if err != nil {
		t.Fatalf("CreateDashboard failed: %v", err)
	}

	if result.ID != "dashboard-123" {
		t.Errorf("Expected ID dashboard-123, got %s", result.ID)
	}

	if result.Title != "Test Dashboard" {
		t.Errorf("Expected title 'Test Dashboard', got %s", result.Title)
	}
}

func TestClient_GetDashboard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET method, got %s", r.Method)
		}

		if r.URL.Path != "/api/v2/dashboards/dashboard-123" {
			t.Errorf("Expected path /api/v2/dashboards/dashboard-123, got %s", r.URL.Path)
		}

		response := DashboardResponse{
			Status: "success",
			Data: &DashboardData{
				ID:          "dashboard-123",
				UUID:        "uuid-456",
				Title:       "Test Dashboard",
				Description: "Test description",
				Tags:        []string{"test"},
				CreatedAt:   "2023-01-01T00:00:00Z",
				UpdatedAt:   "2023-01-01T00:00:00Z",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
	}

	client := NewClient(cfg)

	result, err := client.GetDashboard(context.Background(), "dashboard-123")
	if err != nil {
		t.Fatalf("GetDashboard failed: %v", err)
	}

	if result.ID != "dashboard-123" {
		t.Errorf("Expected ID dashboard-123, got %s", result.ID)
	}
}

func TestClient_DeleteDashboard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("Expected DELETE method, got %s", r.Method)
		}

		if r.URL.Path != "/api/v2/dashboards/dashboard-123" {
			t.Errorf("Expected path /api/v2/dashboards/dashboard-123, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
	}

	client := NewClient(cfg)

	err := client.DeleteDashboard(context.Background(), "dashboard-123")
	if err != nil {
		t.Fatalf("DeleteDashboard failed: %v", err)
	}
}

func TestClient_CreateRule(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		if r.URL.Path != "/api/v1/rules" {
			t.Errorf("Expected path /api/v1/rules, got %s", r.URL.Path)
		}

		response := RuleResponse{
			Status: "success",
			Data: &RuleData{
				ID:         "rule-123",
				AlertName:  "Test Alert",
				AlertType:  "METRIC_BASED_ALERT",
				EvalWindow: "5m",
				Frequency:  "1m",
				Disabled:   false,
				CreatedAt:  "2023-01-01T00:00:00Z",
				UpdatedAt:  "2023-01-01T00:00:00Z",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
	}

	client := NewClient(cfg)

	rule := &RuleData{
		AlertName:  "Test Alert",
		AlertType:  "METRIC_BASED_ALERT",
		EvalWindow: "5m",
		Frequency:  "1m",
		Condition:  map[string]interface{}{"test": "condition"},
		Disabled:   false,
	}

	result, err := client.CreateRule(context.Background(), rule)
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	if result.ID != "rule-123" {
		t.Errorf("Expected ID rule-123, got %s", result.ID)
	}

	if result.AlertName != "Test Alert" {
		t.Errorf("Expected AlertName 'Test Alert', got %s", result.AlertName)
	}
}

// TestClient_CreateRule_RequiresEvaluationBlock reproduces a bug found live
// against http-auth-failures: the rules API (POST/PUT /api/v1/rules)
// rejects any request lacking the nested evaluation/schemaVersion/
// notificationSettings fields with 400 "alert rule is not valid" - even
// though the top-level evalWindow/frequency fields are also accepted.
// Confirmed by round-tripping a live GET response through PUT: it failed
// with only evalWindow/frequency present, and succeeded once evaluation,
// schemaVersion, and notificationSettings were all three added back
// (individually omitting any one of the three still failed). This test
// asserts the client actually serializes all three onto the wire, not
// just that RuleData has the fields.
func TestClient_CreateRule_RequiresEvaluationBlock(t *testing.T) {
	var captured map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RuleResponse{
			Status: "success",
			Data:   &RuleData{ID: "rule-123"},
		})
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, APIKey: "test-key"})

	rule := &RuleData{
		AlertName:  "Test Alert",
		AlertType:  "LOG_BASED_ALERT",
		EvalWindow: "5m",
		Frequency:  "1m",
		Condition:  map[string]interface{}{"test": "condition"},
		Evaluation: &RuleEvaluation{
			Kind: "rolling",
			Spec: RuleEvaluationSpec{EvalWindow: "5m", Frequency: "1m"},
		},
		SchemaVersion: "v2alpha1",
		NotificationSettings: &RuleNotificationSettings{
			Renotify:  RuleRenotify{Enabled: false, Interval: "30m"},
			UsePolicy: false,
		},
	}

	if _, err := client.CreateRule(context.Background(), rule); err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	evaluation, ok := captured["evaluation"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected evaluation object on the wire, got %v", captured["evaluation"])
	}
	if evaluation["kind"] != "rolling" {
		t.Errorf("expected evaluation.kind 'rolling', got %v", evaluation["kind"])
	}
	spec, ok := evaluation["spec"].(map[string]interface{})
	if !ok || spec["evalWindow"] != "5m" || spec["frequency"] != "1m" {
		t.Errorf("expected evaluation.spec {evalWindow:5m, frequency:1m}, got %v", evaluation["spec"])
	}

	if captured["schemaVersion"] != "v2alpha1" {
		t.Errorf("expected schemaVersion 'v2alpha1' on the wire, got %v", captured["schemaVersion"])
	}

	if _, ok := captured["notificationSettings"].(map[string]interface{}); !ok {
		t.Errorf("expected notificationSettings object on the wire, got %v", captured["notificationSettings"])
	}
}

func TestClient_CreateChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		if r.URL.Path != "/api/v1/channels" {
			t.Errorf("Expected path /api/v1/channels, got %s", r.URL.Path)
		}

		response := ChannelResponse{
			Status: "success",
			Data: &ChannelData{
				ID:        "1",
				Name:      "Test Channel",
				Type:      "slack",
				CreatedAt: "2023-01-01T00:00:00Z",
				UpdatedAt: "2023-01-01T00:00:00Z",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
	}

	client := NewClient(cfg)

	channel := &ChannelData{
		Name: "Test Channel",
		Type: "slack",
	}

	result, err := client.CreateChannel(context.Background(), channel)
	if err != nil {
		t.Fatalf("CreateChannel failed: %v", err)
	}

	if result.ID != "1" {
		t.Errorf("Expected ID 1, got %s", result.ID)
	}

	if result.Name != "Test Channel" {
		t.Errorf("Expected Name 'Test Channel', got %s", result.Name)
	}
}

func TestClient_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "Invalid request"}`))
	}))
	defer server.Close()

	cfg := Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
	}

	client := NewClient(cfg)

	_, err := client.GetDashboard(context.Background(), "non-existent")
	if err == nil {
		t.Fatal("Expected error for 400 status code")
	}

	if !contains(err.Error(), "API error") {
		t.Errorf("Expected API error, got %v", err)
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "404 error",
			err:      errors.New("API error: 404 Not Found"),
			expected: true,
		},
		{
			name:     "not found error",
			err:      errors.New("resource not found"),
			expected: true,
		},
		{
			name:     "other error",
			err:      errors.New("API error: 500 Internal Server Error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNotFound(tt.err)
			if result != tt.expected {
				t.Errorf("IsNotFound(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}
