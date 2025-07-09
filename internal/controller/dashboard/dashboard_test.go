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

package dashboard

import (
	"testing"

	"github.com/crossplane-contrib/provider-signoz/apis/dashboard/v1alpha1"
	"github.com/crossplane-contrib/provider-signoz/internal/clients"
)

func TestConvertWidgets(t *testing.T) {
	widgets := []v1alpha1.Widget{
		{
			ID:        "widget-1",
			Title:     "Test Widget",
			PanelType: "graph",
			Query: v1alpha1.Query{
				QueryType: 1,
				PromQL: []v1alpha1.PromQuery{
					{
						Query: "up",
						Name:  stringPtr("A"),
					},
				},
			},
		},
	}

	result := convertWidgets(widgets)
	
	if len(result) != 1 {
		t.Errorf("Expected 1 widget, got %d", len(result))
	}
	
	widget := result[0].(map[string]interface{})
	
	if widget["id"] != "widget-1" {
		t.Errorf("Expected widget ID widget-1, got %v", widget["id"])
	}
	
	if widget["title"] != "Test Widget" {
		t.Errorf("Expected widget title 'Test Widget', got %v", widget["title"])
	}
	
	if widget["panelType"] != "graph" {
		t.Errorf("Expected panel type 'graph', got %v", widget["panelType"])
	}
	
	query := widget["query"].(map[string]interface{})
	if query["queryType"] != 1 {
		t.Errorf("Expected query type 1, got %v", query["queryType"])
	}
}

func TestIsDashboardUpToDate(t *testing.T) {
	spec := v1alpha1.DashboardParameters{
		Title:       "Test Dashboard",
		Description: stringPtr("Test description"),
		Tags:        []string{"test"},
	}
	
	dashboard := &clients.DashboardData{
		Title:       "Test Dashboard",
		Description: "Test description",
		Tags:        []string{"test"},
	}
	
	if !isDashboardUpToDate(spec, dashboard) {
		t.Error("Expected dashboard to be up to date")
	}
	
	// Test with different title
	dashboard.Title = "Different Title"
	if isDashboardUpToDate(spec, dashboard) {
		t.Error("Expected dashboard to not be up to date with different title")
	}
}

func stringPtr(s string) *string {
	return &s
}