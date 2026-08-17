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

	queries, ok := compositeQuery["queries"].([]interface{})
	if !ok || len(queries) != 1 {
		t.Fatalf("expected queries array with 1 envelope, got %T", compositeQuery["queries"])
	}
	env, ok := queries[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected query envelope map, got %T", queries[0])
	}
	if env["type"] != "promql" {
		t.Errorf("expected envelope type promql, got %v", env["type"])
	}
}

func TestConvertQueryBuilder_MetricAggregation(t *testing.T) {
	// This test exercises the v5 builder_query spec for a metric alert:
	//   metricName=coredns_panics_total, timeAggregation=rate,
	//   spaceAggregation=sum.
	// The rules API (POST/PUT /api/v1/rules) expects the v5 QueryEnvelope
	// schema: compositeQuery.queries[] spec carries name/stepInterval/signal/
	// source/aggregations[] (metricName, temporality, timeAggregation,
	// spaceAggregation), NOT the legacy v3 builderQueries fields.
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

	if result["name"] != "A" {
		t.Errorf("Expected name A, got %v", result["name"])
	}
	if result["signal"] != "metrics" {
		t.Errorf("Expected signal metrics, got %v", result["signal"])
	}
	if result["source"] != "meter" {
		t.Errorf("Expected source meter, got %v", result["source"])
	}
	if step, ok := result["stepInterval"].(int64); !ok || step != 60 {
		t.Errorf("Expected stepInterval 60 (default), got %v", result["stepInterval"])
	}

	rawAggs, ok := result["aggregations"].([]interface{})
	if !ok || len(rawAggs) != 1 {
		t.Fatalf("expected aggregations array with 1 entry, got %T %v", result["aggregations"], result["aggregations"])
	}
	agg, ok := rawAggs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected aggregation map, got %T", rawAggs[0])
	}
	if agg["metricName"] != "coredns_panics_total" {
		t.Errorf("Expected metricName coredns_panics_total, got %v", agg["metricName"])
	}
	if agg["timeAggregation"] != "rate" {
		t.Errorf("Expected timeAggregation rate, got %v", agg["timeAggregation"])
	}
	if agg["spaceAggregation"] != "sum" {
		t.Errorf("Expected spaceAggregation sum, got %v", agg["spaceAggregation"])
	}
}

func TestConvertQueryBuilder_LegacyAggregateOperator(t *testing.T) {
	// A builder query that only sets the legacy aggregateOperator (no
	// explicit time/space aggregation) must still yield a usable v5
	// aggregation (spaceAggregation defaulting to sum).
	builder := v1beta1.QueryBuilder{
		QueryName:         "A",
		DataSource:        "metrics",
		AggregateOperator: "rate",
		AggregateAttribute: &v1beta1.KeyAttribute{
			Key: "coredns_panics_total",
		},
	}

	result := convertQueryBuilder(builder)

	rawAggs, ok := result["aggregations"].([]interface{})
	if !ok || len(rawAggs) != 1 {
		t.Fatalf("expected aggregations array, got %T %v", result["aggregations"], result["aggregations"])
	}
	agg := rawAggs[0].(map[string]interface{})
	if agg["timeAggregation"] != "rate" {
		t.Errorf("Expected timeAggregation rate from aggregateOperator, got %v", agg["timeAggregation"])
	}
	if agg["spaceAggregation"] != "sum" {
		t.Errorf("Expected spaceAggregation sum (default), got %v", agg["spaceAggregation"])
	}
}

func TestConvertCompositeQuery_BuilderQueriesEnvelope(t *testing.T) {
	// Verify the converter emits the v5 QueryEnvelope contract: compositeQuery
	// carries a "queries" array of {type, spec} envelopes for builder queries,
	// not the legacy builderQueries map.
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

	rawQueries, ok := result["queries"]
	if !ok {
		t.Fatalf("expected queries key in compositeQuery, got keys=%v", keysOf(result))
	}
	queries, ok := rawQueries.([]interface{})
	if !ok || len(queries) != 1 {
		t.Fatalf("expected queries array with 1 envelope, got %T %v", rawQueries, rawQueries)
	}
	env, ok := queries[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected query envelope map, got %T", queries[0])
	}
	if env["type"] != "builder_query" {
		t.Errorf("Expected envelope type builder_query, got %v", env["type"])
	}
	spec, ok := env["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected spec map, got %T", env["spec"])
	}
	if spec["name"] != "A" {
		t.Errorf("Expected spec name A, got %v", spec["name"])
	}
	if _, present := result["builderQueries"]; present {
		t.Errorf("did not expect legacy builderQueries key in v5 payload")
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
			"queries": []interface{}{
				map[string]interface{}{
					"type": "builder_query",
					"spec": map[string]interface{}{
						"name":         "A",
						"stepInterval": float64(60),
						"signal":       "metrics",
						"source":       "meter",
						"aggregations": []interface{}{
							map[string]interface{}{
								"metricName":       "coredns_panics_total",
								"temporality":      "",
								"timeAggregation":  "rate",
								"spaceAggregation": "sum",
							},
						},
						"disabled": false,
						"legend":   "",
					},
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
			"queries": []interface{}{
				map[string]interface{}{
					"type": "builder_query",
					"spec": map[string]interface{}{
						"name":         "A",
						"stepInterval": float64(60),
						"signal":       "metrics",
						"source":       "meter",
						"aggregations": []interface{}{
							map[string]interface{}{
								"metricName":       "coredns_panics_total",
								"timeAggregation":  "rate",
								"spaceAggregation": "sum",
							},
						},
						"disabled": false,
						"legend":   "",
					},
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

func TestConvertQueryBuilder_FilterExpression(t *testing.T) {
	builder := v1beta1.QueryBuilder{
		QueryName:         "A",
		DataSource:        "metrics",
		AggregateOperator: "rate",
		AggregateAttribute: &v1beta1.KeyAttribute{
			Key: "coredns_dns_responses_total",
		},
		FilterExpression: "job_name = 'dns-internal-validation'",
	}

	result := convertQueryBuilder(builder)

	rawFilter, ok := result["filter"]
	if !ok {
		t.Fatalf("expected filter key in spec")
	}
	filter, ok := rawFilter.(map[string]interface{})
	if !ok {
		t.Fatalf("expected filter map, got %T", rawFilter)
	}
	if filter["expression"] != "job_name = 'dns-internal-validation'" {
		t.Errorf("expected filter.expression to carry raw expression, got %v", filter["expression"])
	}
}

func TestConvertQueryBuilder_FilterExpressionTakesPrecedence(t *testing.T) {
	// The raw FilterExpression must win over the structured Filters block
	// when both are present, so the live expression is never lost.
	builder := v1beta1.QueryBuilder{
		QueryName:  "A",
		DataSource: "metrics",
		Filters: &v1beta1.FilterSet{
			Operator: "AND",
			Items:    []v1beta1.FilterItem{},
		},
		FilterExpression: "job_name = 'blackbox_dns_external' AND zone = 'bankrut'",
	}

	result := convertQueryBuilder(builder)

	filter := result["filter"].(map[string]interface{})
	if filter["expression"] != "job_name = 'blackbox_dns_external' AND zone = 'bankrut'" {
		t.Errorf("expected raw expression to take precedence, got %v", filter["expression"])
	}
}
