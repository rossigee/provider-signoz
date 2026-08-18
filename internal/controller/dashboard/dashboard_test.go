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
	"github.com/rossigee/provider-signoz/apis/dashboard/v1beta1"
	"github.com/rossigee/provider-signoz/internal/clients"
	"testing"
)

func TestConvertWidgets(t *testing.T) {
	widgets := []v1beta1.Widget{
		{
			ID:        "widget-1",
			Title:     "Test Widget",
			PanelType: "graph",
			Query: v1beta1.Query{
				QueryType: "1",
				PromQL: []v1beta1.PromQuery{
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
	if query["queryType"] != "1" {
		t.Errorf("Expected query type 1, got %v", query["queryType"])
	}
}

func TestIsDashboardUpToDate(t *testing.T) {
	spec := v1beta1.DashboardParameters{
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

// TestIsDashboardV2UpToDate_DetectsQueryDrift reproduces the bug where
// editing a widget's query string (same widget ID, same widget count) was
// invisible to Observe(): isDashboardV2UpToDate previously only compared
// widget IDs and count, never the query content, so Update() was never
// called and the live SigNoz dashboard silently diverged from spec forever.
func TestIsDashboardV2UpToDate_DetectsQueryDrift(t *testing.T) {
	widget := v1beta1.Widget{
		ID:        "dns-query-rate",
		Title:     "DNS Query Rate",
		PanelType: "graph",
		Query: v1beta1.Query{
			QueryType: "1",
			PromQL: []v1beta1.PromQuery{
				{Query: "rate(coredns_dns_requests_total[5m])", Name: stringPtr("Queries/sec")},
			},
		},
		YAxisUnit: stringPtr("requests/sec"),
	}
	spec := v1beta1.DashboardParameters{
		Title:   "CoreDNS Monitoring",
		Widgets: []v1beta1.Widget{widget},
	}

	// Observed panel matches the desired widget exactly.
	matching := &clients.DashboardV2Data{
		Spec: clients.DashboardV2Spec{
			Display: &clients.DashboardV2Display{Name: "CoreDNS Monitoring"},
			Panels: map[string]interface{}{
				"dns-query-rate": panelFixture(
					"DNS Query Rate", "requests/sec",
					"promql", "rate(coredns_dns_requests_total[5m])", "", "Queries/sec", false,
				),
			},
		},
	}
	if !isDashboardV2UpToDate(spec, matching) {
		t.Error("expected dashboard to be up to date when observed panel matches spec")
	}

	// Same widget ID and widget count, but the live query string is the
	// stale/incorrect one - this is exactly the scenario that went
	// undetected before the fix.
	drifted := &clients.DashboardV2Data{
		Spec: clients.DashboardV2Spec{
			Display: &clients.DashboardV2Display{Name: "CoreDNS Monitoring"},
			Panels: map[string]interface{}{
				"dns-query-rate": panelFixture(
					"DNS Query Rate", "requests/sec",
					"promql", "rate(coredns_dns_request_count_total[5m])", "", "Queries/sec", false,
				),
			},
		},
	}
	if isDashboardV2UpToDate(spec, drifted) {
		t.Error("expected dashboard to be detected as out of date when the live query string differs from spec")
	}
}

// panelFixture builds a minimal V2 panel map matching the shape convertToV2
// produces, for use as observed API state in tests.
func panelFixture(title, unit, queryType, query, legend, name string, disabled bool) map[string]interface{} {
	return map[string]interface{}{
		"kind": "Panel",
		"spec": map[string]interface{}{
			"display": map[string]interface{}{
				"name": title,
			},
			"plugin": map[string]interface{}{
				"kind": "signoz/TimeSeriesPanel",
				"spec": map[string]interface{}{
					"formatting": map[string]interface{}{
						"unit": unit,
					},
				},
			},
			"queries": []interface{}{
				map[string]interface{}{
					"kind": "time_series",
					"spec": map[string]interface{}{
						"plugin": map[string]interface{}{
							"kind": "signoz/CompositeQuery",
							"spec": map[string]interface{}{
								"queries": []interface{}{
									map[string]interface{}{
										"type": queryType,
										"spec": map[string]interface{}{
											"query":    query,
											"legend":   legend,
											"name":     name,
											"disabled": disabled,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestConvertQueryToV2_ClickHouse(t *testing.T) {
	query := v1beta1.Query{
		QueryType: "2",
		ClickHouse: []v1beta1.ClickHouseQuery{
			{
				Query:  "SELECT 1",
				Name:   stringPtr("A"),
				Legend: stringPtr("{{host_name}}"),
			},
		},
	}

	result := convertQueryToV2(query)

	spec := result["spec"].(map[string]interface{})
	plugin := spec["plugin"].(map[string]interface{})
	pluginSpec := plugin["spec"].(map[string]interface{})
	queries := pluginSpec["queries"].([]interface{})

	if len(queries) != 1 {
		t.Fatalf("Expected 1 query, got %d", len(queries))
	}

	q := queries[0].(map[string]interface{})
	if q["type"] != "clickhouse_sql" {
		t.Errorf("Expected type 'clickhouse_sql', got %v", q["type"])
	}

	qSpec := q["spec"].(map[string]interface{})
	if qSpec["query"] != "SELECT 1" {
		t.Errorf("Expected query 'SELECT 1', got %v", qSpec["query"])
	}
	if qSpec["name"] != "A" {
		t.Errorf("Expected name 'A', got %v", qSpec["name"])
	}
	if qSpec["legend"] != "{{host_name}}" {
		t.Errorf("Expected legend '{{host_name}}', got %v", qSpec["legend"])
	}
}

func TestConvertQueryToV2_PromQL(t *testing.T) {
	query := v1beta1.Query{
		QueryType: "1",
		PromQL: []v1beta1.PromQuery{
			{
				Query:  "up",
				Name:   stringPtr("A"),
				Legend: stringPtr("{{instance}}"),
			},
		},
	}

	result := convertQueryToV2(query)

	spec := result["spec"].(map[string]interface{})
	plugin := spec["plugin"].(map[string]interface{})
	pluginSpec := plugin["spec"].(map[string]interface{})
	queries := pluginSpec["queries"].([]interface{})

	if len(queries) != 1 {
		t.Fatalf("Expected 1 query, got %d", len(queries))
	}

	q := queries[0].(map[string]interface{})
	if q["type"] != "promql" {
		t.Errorf("Expected type 'promql', got %v", q["type"])
	}

	qSpec := q["spec"].(map[string]interface{})
	if qSpec["legend"] != "{{instance}}" {
		t.Errorf("Expected legend '{{instance}}' to be preserved, got %v", qSpec["legend"])
	}
}

func stringPtr(s string) *string {
	return &s
}
