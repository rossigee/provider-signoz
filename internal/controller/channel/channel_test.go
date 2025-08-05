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

package channel

import (
	"context"
	"testing"

	"github.com/rossigee/provider-signoz/apis/channel/v1alpha1"
)

func TestConvertToChannelData(t *testing.T) {
	e := &external{}
	
	spec := v1alpha1.NotificationChannelParameters{
		Name: "Test Channel",
		Type: "slack",
		SlackConfigs: []v1alpha1.SlackConfig{
			{
				Channel:    "#test",
				WebhookURL: stringPtr("https://hooks.slack.com/test"),
				Title:      stringPtr("Test Alert"),
			},
		},
	}
	
	result, err := e.convertToChannelData(context.Background(), spec)
	if err != nil {
		t.Fatalf("convertToChannelData failed: %v", err)
	}
	
	if result.Name != "Test Channel" {
		t.Errorf("Expected name 'Test Channel', got %s", result.Name)
	}
	
	if result.Type != "slack" {
		t.Errorf("Expected type 'slack', got %s", result.Type)
	}
	
	if result.Data["channel"] != "#test" {
		t.Errorf("Expected channel '#test', got %v", result.Data["channel"])
	}
	
	if result.Data["webhook_url"] != "https://hooks.slack.com/test" {
		t.Errorf("Expected webhook_url 'https://hooks.slack.com/test', got %v", result.Data["webhook_url"])
	}
	
	if result.Data["title"] != "Test Alert" {
		t.Errorf("Expected title 'Test Alert', got %v", result.Data["title"])
	}
}

func TestConvertSlackConfig(t *testing.T) {
	e := &external{}
	
	config := v1alpha1.SlackConfig{
		Channel:      "#test",
		WebhookURL:   stringPtr("https://hooks.slack.com/test"),
		Title:        stringPtr("Test Alert"),
		SendResolved: boolPtr(true),
	}
	
	result, err := e.convertSlackConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("convertSlackConfig failed: %v", err)
	}
	
	if result["channel"] != "#test" {
		t.Errorf("Expected channel '#test', got %v", result["channel"])
	}
	
	if result["webhook_url"] != "https://hooks.slack.com/test" {
		t.Errorf("Expected webhook_url 'https://hooks.slack.com/test', got %v", result["webhook_url"])
	}
	
	if result["title"] != "Test Alert" {
		t.Errorf("Expected title 'Test Alert', got %v", result["title"])
	}
	
	if result["send_resolved"] != true {
		t.Errorf("Expected send_resolved true, got %v", result["send_resolved"])
	}
}

func TestConvertWebhookConfig(t *testing.T) {
	e := &external{}
	
	config := v1alpha1.WebhookConfig{
		URL:          stringPtr("https://webhook.example.com"),
		Method:       stringPtr("POST"),
		MaxAlerts:    intPtr(5),
		SendResolved: boolPtr(false),
	}
	
	result, err := e.convertWebhookConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("convertWebhookConfig failed: %v", err)
	}
	
	if result["url"] != "https://webhook.example.com" {
		t.Errorf("Expected url 'https://webhook.example.com', got %v", result["url"])
	}
	
	if result["http_method"] != "POST" {
		t.Errorf("Expected http_method 'POST', got %v", result["http_method"])
	}
	
	if result["max_alerts"] != 5 {
		t.Errorf("Expected max_alerts 5, got %v", result["max_alerts"])
	}
	
	if result["send_resolved"] != false {
		t.Errorf("Expected send_resolved false, got %v", result["send_resolved"])
	}
}

func TestConvertEmailConfig(t *testing.T) {
	e := &external{}
	
	config := v1alpha1.EmailConfig{
		To:           []string{"test@example.com", "admin@example.com"},
		SendResolved: boolPtr(true),
	}
	
	result := e.convertEmailConfig(config)
	
	to := result["to"].([]string)
	if len(to) != 2 {
		t.Errorf("Expected 2 email addresses, got %d", len(to))
	}
	
	if to[0] != "test@example.com" {
		t.Errorf("Expected first email 'test@example.com', got %s", to[0])
	}
	
	if to[1] != "admin@example.com" {
		t.Errorf("Expected second email 'admin@example.com', got %s", to[1])
	}
	
	if result["send_resolved"] != true {
		t.Errorf("Expected send_resolved true, got %v", result["send_resolved"])
	}
}

func TestUnsupportedChannelType(t *testing.T) {
	e := &external{}
	
	spec := v1alpha1.NotificationChannelParameters{
		Name: "Test Channel",
		Type: "unsupported",
	}
	
	_, err := e.convertToChannelData(context.Background(), spec)
	if err == nil {
		t.Fatal("Expected error for unsupported channel type")
	}
	
	if err.Error() != "unsupported channel type: unsupported" {
		t.Errorf("Expected error message 'unsupported channel type: unsupported', got %v", err.Error())
	}
}

func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

func intPtr(i int) *int {
	return &i
}