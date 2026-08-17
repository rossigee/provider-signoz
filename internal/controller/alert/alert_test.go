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

package alert

import (
	"github.com/rossigee/provider-signoz/apis/alert/v1beta1"
	"github.com/rossigee/provider-signoz/internal/clients"
	"testing"
)

func TestIsAlertUpToDate(t *testing.T) {
	spec := v1beta1.AlertParameters{
		AlertName:   "Test Alert",
		AlertType:   "METRIC_BASED_ALERT",
		EvalWindow:  "5m",
		Frequency:   "1m",
		Disabled:    false,
		Labels:      map[string]string{"team": "backend"},
		Annotations: map[string]string{"description": "Test alert"},
	}

	alert := &clients.RuleData{
		AlertName:   "Test Alert",
		AlertType:   "METRIC_BASED_ALERT",
		EvalWindow:  "5m",
		Frequency:   "1m",
		Disabled:    false,
		Labels:      map[string]string{"team": "backend"},
		Annotations: map[string]string{"description": "Test alert"},
		// Mirror the default condition that convertCondition emits for
		// a zero-value RuleCondition (op="1", target=0, matchType="1",
		// compositeQuery with builder query A).
		Condition: convertCondition(spec.Condition),
	}

	if !isAlertUpToDate(spec, alert) {
		t.Error("Expected alert to be up to date")
	}

	// Test when alert name differs
	alert.AlertName = "Different Name"
	if isAlertUpToDate(spec, alert) {
		t.Error("Expected alert to not be up to date due to different name")
	}
	alert.AlertName = spec.AlertName // Reset

	// Test when disabled differs
	alert.Disabled = true
	if isAlertUpToDate(spec, alert) {
		t.Error("Expected alert to not be up to date due to different disabled state")
	}
	alert.Disabled = spec.Disabled // Reset

	// Test when labels differ
	alert.Labels = map[string]string{"team": "frontend"}
	if isAlertUpToDate(spec, alert) {
		t.Error("Expected alert to not be up to date due to different labels")
	}
}

func TestMapsEqual(t *testing.T) {
	a := map[string]string{"key1": "value1", "key2": "value2"}
	b := map[string]string{"key1": "value1", "key2": "value2"}
	c := map[string]string{"key1": "value1", "key2": "different"}
	d := map[string]string{"key1": "value1", "key3": "value2"}

	if !mapsEqual(a, b) {
		t.Error("Expected maps a and b to be equal")
	}

	if mapsEqual(a, c) {
		t.Error("Expected maps a and c to not be equal")
	}

	if mapsEqual(a, d) {
		t.Error("Expected maps a and d to not be equal")
	}
}

func TestRemoveDuplicates(t *testing.T) {
	input := []string{"a", "b", "a", "c", "b"}
	expected := []string{"a", "b", "c"}

	result := removeDuplicates(input)
	if len(result) != len(expected) {
		t.Errorf("Expected length %d, got %d", len(expected), len(result))
	}

	for i, v := range expected {
		if result[i] != v {
			t.Errorf("Expected %s at index %d, got %s", v, i, result[i])
		}
	}
}

func TestConvertCondition(t *testing.T) {
	condition := v1beta1.RuleCondition{
		CompositeQuery: v1beta1.CompositeQuery{
			QueryType: "1", // PromQL
			PromQL: []v1beta1.AlertPromQuery{
				{
					Query:    "up == 0",
					Name:     "A",
					Legend:   "Service Down",
					Disabled: false,
				},
			},
		},
		CompareOp: "VALUE",
		Target:    float64Ptr(1.0),
		MatchType: intPtr(1),
	}

	result := convertCondition(condition)

	if result["op"] != "VALUE" {
		t.Errorf("Expected op 'VALUE', got %v", result["op"])
	}

	if result["target"] != 1.0 {
		t.Errorf("Expected target 1.0, got %v", result["target"])
	}

	if result["matchType"] != "1" {
		t.Errorf("Expected matchType 1, got %v", result["matchType"])
	}

	compositeQuery := result["compositeQuery"].(map[string]interface{})
	if compositeQuery["queryType"] != "promql" {
		t.Errorf("Expected queryType promql, got %v", compositeQuery["queryType"])
	}

	promQueries, ok := compositeQuery["promQueries"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected promQueries map, got %T", compositeQuery["promQueries"])
	}
	if _, ok := promQueries["A"]; !ok {
		t.Errorf("expected promQueries to contain entry 'A'")
	}
}

func TestConvertQueryBuilder_MetricAggregation(t *testing.T) {
	// This test exercises the v5 builder_query schema for a metric alert:
	//   metricName=coredns_panics_total, timeAggregation=rate,
	//   spaceAggregation=sum.
	// Prior to the v0.4.0 fix this produced a payload SigNoz would reject
	// (queries[] vs builderQueries{}, missing queryName/expression/
	// stepInterval/timeAggregation/spaceAggregation).
	builder := v1beta1.QueryBuilder{
		QueryName:         "A",
		DataSource:        "metrics",
		AggregateOperator: "rate",
		AggregateAttribute: &v1beta1.KeyAttribute{
			Key:      "coredns_panics_total",
			Type:     "Gauge",
			DataType: "float64",
		},
		TimeAggregation:  "rate",
		SpaceAggregation: "sum",
	}

	result := convertQueryBuilder(builder)

	if result["queryName"] != "A" {
		t.Errorf("Expected queryName A, got %v", result["queryName"])
	}
	if result["dataSource"] != "metrics" {
		t.Errorf("Expected dataSource metrics, got %v", result["dataSource"])
	}
	if result["timeAggregation"] != "rate" {
		t.Errorf("Expected timeAggregation rate, got %v", result["timeAggregation"])
	}
	if result["spaceAggregation"] != "sum" {
		t.Errorf("Expected spaceAggregation sum, got %v", result["spaceAggregation"])
	}
	if result["aggregateOperator"] != "rate" {
		t.Errorf("Expected aggregateOperator rate, got %v", result["aggregateOperator"])
	}
	if result["expression"] != "A" {
		t.Errorf("Expected expression A (default), got %v", result["expression"])
	}
	if step, ok := result["stepInterval"].(int64); !ok || step != 60 {
		t.Errorf("Expected stepInterval 60 (default), got %v", result["stepInterval"])
	}
}

func TestConvertCompositeQuery_BuilderQueriesMap(t *testing.T) {
	// Verify the converter emits builderQueries as a map keyed by query
	// name (SigNoz v5 contract), not as an array under "queries".
	cq := v1beta1.CompositeQuery{
		QueryType: "3",
		Builder: &v1beta1.QueryBuilder{
			QueryName:         "A",
			DataSource:        "metrics",
			AggregateOperator: "rate",
			AggregateAttribute: &v1beta1.KeyAttribute{
				Key: "coredns_panics_total",
			},
			TimeAggregation:  "rate",
			SpaceAggregation: "sum",
		},
	}

	result := convertCompositeQuery(cq)

	if result["queryType"] != "builder" {
		t.Errorf("Expected queryType builder, got %v", result["queryType"])
	}

	rawBQ, ok := result["builderQueries"]
	if !ok {
		t.Fatalf("expected builderQueries key in compositeQuery, got keys=%v", keysOf(result))
	}
	bq, ok := rawBQ.(map[string]interface{})
	if !ok {
		t.Fatalf("expected builderQueries to be a map, got %T", rawBQ)
	}
	if _, ok := bq["A"]; !ok {
		t.Errorf("expected builderQueries to contain entry 'A', got keys=%v", keysOf(bq))
	}
	if _, present := result["queries"]; present {
		t.Errorf("did not expect legacy 'queries' array key in v5 payload")
	}
}

func TestConvertCompositeQuery_NumericQueryType(t *testing.T) {
	// Legacy numeric queryType values ("1"/"2"/"3") must be normalised to
	// the symbolic forms ("promql"/"clickhouse_sql"/"builder").
	cases := []struct {
		in   string
		want string
	}{
		{"1", "promql"},
		{"2", "clickhouse_sql"},
		{"3", "builder"},
		{"builder", "builder"},
		{"PROMQL", "promql"},
	}
	for _, tc := range cases {
		got := convertCompositeQuery(v1beta1.CompositeQuery{QueryType: tc.in})
		if got["queryType"] != tc.want {
			t.Errorf("QueryType %q -> %q, want %q", tc.in, got["queryType"], tc.want)
		}
	}
}

func TestConditionEqual_BuilderQueryDrift(t *testing.T) {
	// Two conditions that should be considered equal modulo known
	// SigNoz normalisations (nil vs empty, int vs float64 for op).
	a := map[string]interface{}{
		"compositeQuery": map[string]interface{}{
			"queryType": "builder",
			"panelType": "graph",
			"unit":      nil,
			"builderQueries": map[string]interface{}{
				"A": map[string]interface{}{
					"queryName":    "A",
					"dataSource":   "metrics",
					"stepInterval": float64(60),
					"expression":   "A",
					"aggregateOperator": "rate",
					"aggregateAttribute": map[string]interface{}{
						"key": "coredns_panics_total",
					},
					"timeAggregation":  "rate",
					"spaceAggregation": "sum",
					"limit":            float64(0),
					"offset":           float64(0),
				},
			},
		},
		"op":        "1",
		"target":    float64(0),
		"matchType": "1",
	}
	b := map[string]interface{}{
		"compositeQuery": map[string]interface{}{
			"queryType": "builder",
			"panelType": "graph",
			"builderQueries": map[string]interface{}{
				"A": map[string]interface{}{
					"queryName":    "A",
					"dataSource":   "metrics",
					"stepInterval": float64(60),
					"expression":   "A",
					"aggregateOperator": "rate",
					"aggregateAttribute": map[string]interface{}{
						"key": "coredns_panics_total",
					},
					"timeAggregation":  "rate",
					"spaceAggregation": "sum",
				},
			},
		},
		"op":        "1",
		"target":    float64(0),
		"matchType": "1",
	}
	if !conditionEqual(a, b) {
		t.Errorf("Expected conditions to be considered equal")
	}

	c := map[string]interface{}{}
	c["op"] = "2" // mismatch on op
	if conditionEqual(a, c) {
		t.Errorf("Expected op mismatch to be detected")
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func intPtr(i int) *int {
	return &i
}

func float64Ptr(f float64) *float64 {
	return &f
}
