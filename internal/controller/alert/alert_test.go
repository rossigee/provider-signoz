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
	"testing"

	"github.com/rossigee/provider-signoz/apis/alert/v1beta1"
	"github.com/rossigee/provider-signoz/internal/clients"
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
	if compositeQuery["queryType"] != "PromQL" {
		t.Errorf("Expected queryType PromQL, got %v", compositeQuery["queryType"])
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
	// stepInterval is emitted as float64 (the canonical JSON number type) so it
	// matches the float64 decoded from the SigNoz GET response; see the
	// int64-vs-float64 drift fix in convertQueryBuilder.
	if step, ok := result["stepInterval"].(float64); !ok || step != 60 {
		t.Errorf("Expected stepInterval float64 60 (default), got %T(%v)", result["stepInterval"], result["stepInterval"])
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

	if result["queryType"] != "Builder" {
		t.Errorf("Expected queryType Builder, got %v", result["queryType"])
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
	// Legacy numeric queryType values ("1"/"2"/"3") and lowercase forms must be
	// normalised to the canonical forms (PromQL/ClickHouse/Builder).
	cases := []struct {
		in   string
		want string
	}{
		{"1", "PromQL"},
		{"2", "ClickHouse"},
		{"3", "Builder"},
		{"builder", "Builder"},
		{"PROMQL", "PromQL"},
		{"promql", "PromQL"},
		{"clickhouse_sql", "ClickHouse"},
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
			"queryType": "Builder",
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
			"queryType": "Builder",
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

// TestValueEqual_NumericKindTolerance locks in the int64-vs-float64 drift fix.
// The converter emits numeric fields (e.g. stepInterval) as Go int64/int while
// the rules API returns JSON numbers that decode to float64; matching must
// succeed across all numeric kinds, otherwise every observe loop detects a
// phantom diff and re-PUTs the rule ("type-shape" / "op-as-array" drift class).
func TestValueEqual_NumericKindTolerance(t *testing.T) {
	cases := []struct {
		name string
		a, b interface{}
		want bool
	}{
		{"int64 vs float64", int64(60), float64(60), true},
		{"float64 vs int64", float64(60), int64(60), true},
		{"int vs float64", int(60), float64(60), true},
		{"float64 vs float64", float64(1), float64(1), true},
		{"int64 unequal", int64(60), float64(61), false},
		{"number vs stringified number", float64(1), "1", true},
		{"mismatch kind", "x", 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := valueEqual(tc.a, tc.b); got != tc.want {
				t.Errorf("valueEqual(%T(%v), %T(%v)) = %v, want %v", tc.a, tc.a, tc.b, tc.b, got, tc.want)
			}
		})
	}
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

// TestConvertCondition_OpEmittedAsString guards against the feared "op-as-array"
// drift. SigNoz 0.137.x (schemaVersion v1 / "v5") reports condition.op, target
// and matchType as scalar values (op is a string such as ">", target is a
// number, matchType is a stringified number). If the converter ever emitted op
// as a slice (e.g. []string{">"}) the Observe comparison would perpetually
// drift and the reconciler would fight itself. This test pins the scalar wire
// shape so a regression is caught at build time instead of in production.
func TestConvertCondition_OpEmittedAsString(t *testing.T) {
	cond := v1beta1.RuleCondition{
		CompositeQuery: v1beta1.CompositeQuery{QueryType: "3"},
		CompareOp:      ">",
		Target:         float64Ptr(1500),
		MatchType:      intPtr(1),
	}

	got := convertCondition(cond)

	// op must be a scalar string, never []interface{}.
	op, ok := got["op"].(string)
	if !ok || op != ">" {
		t.Fatalf("expected op to be string \">\", got %T(%v)", got["op"], got["op"])
	}
	if _, isSlice := got["op"].([]interface{}); isSlice {
		t.Fatalf("op must not be emitted as an array (op-as-array drift)")
	}

	mt, ok := got["matchType"].(string)
	if !ok || mt != "1" {
		t.Fatalf("expected matchType string \"1\", got %T(%v)", got["matchType"], got["matchType"])
	}

	// target must be a numeric scalar, not a slice.
	switch got["target"].(type) {
	case float64, int, int64:
	default:
		t.Fatalf("expected target numeric scalar, got %T(%v)", got["target"], got["target"])
	}

	// selectedQueryName default must be a string.
	if got["selectedQueryName"] != "A" {
		t.Errorf("expected selectedQueryName \"A\", got %v", got["selectedQueryName"])
	}
}

// TestConditionEqual_FilterExpressionRoundTrip verifies that a desired
// condition carrying a filterExpression compares equal to the observed v1 GET
// response shape (which carries compositeQuery.queries[].spec.filter.expression
// alongside scalar op/target/matchType). This is the canary-coredns-panics
// round-trip in bug-free form: no drift on every reconcile.
func TestConditionEqual_FilterExpressionRoundTrip(t *testing.T) {
	cond := v1beta1.RuleCondition{
		CompareOp: ">",
		Target:    float64Ptr(0),
		MatchType: intPtr(1),
		CompositeQuery: v1beta1.CompositeQuery{
			QueryType: "3",
			Builder: &v1beta1.QueryBuilder{
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
				FilterExpression: "kubernetes_namespace = 'kube-system'",
			},
		},
	}

	desired := convertCondition(cond)

	// observed mirrors the live SigNoz v1 GET response for this rule.
	observed := map[string]interface{}{
		"compositeQuery": map[string]interface{}{
			"queryType": "Builder",
			"panelType": "graph",
			"unit":      "",
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
						"filter":   map[string]interface{}{"expression": "kubernetes_namespace = 'kube-system'"},
						"legend":   "",
					},
				},
			},
		},
		"op":                ">",
		"target":            float64(0),
		"matchType":         "1",
		"selectedQueryName": "A",
	}

	if !conditionEqual(desired, observed) {
		t.Errorf("expected desired condition (with filterExpression) to match observed live shape; desired=%v", desired)
	}

	// A divergent expression must register as drift.
	observed["compositeQuery"].(map[string]interface{})["queries"].([]interface{})[0].(map[string]interface{})["spec"].(map[string]interface{})["filter"].(map[string]interface{})["expression"] = "kubernetes_namespace = 'other'"
	if conditionEqual(desired, observed) {
		t.Error("expected differing filter expression to be detected as drift")
	}
}

// TestConvertCondition_Thresholds reproduces http-auth-failures: a
// LOGS_BASED_ALERT created in SigNoz with a v5 multi-level threshold block.
// Before Thresholds existed on RuleCondition, convertCondition always
// emitted a flat op/target/matchType condition for every alert, which
// SigNoz's rules API rejects outright for a threshold_rule that actually
// uses condition.thresholds - confirmed live (400 "alert rule is not
// valid"). This locks in the shape convertThresholds must produce, matched
// field-for-field against a live rule fetched via GET /api/v1/rules/{id}.
func TestConvertCondition_Thresholds(t *testing.T) {
	condition := v1beta1.RuleCondition{
		CompositeQuery: v1beta1.CompositeQuery{
			QueryType: "3",
			Builder: &v1beta1.QueryBuilder{
				DataSource:            "logs",
				AggregationExpression: "count()",
				FilterExpression:      "body CONTAINS '401 Unauth'",
			},
		},
		Thresholds: []v1beta1.Threshold{
			{
				Name:      "critical",
				Target:    0,
				MatchType: "3",
				Op:        "1",
				Channels:  []string{"Discord #infra (golder)"},
			},
		},
	}

	result := convertCondition(condition)

	if _, hasOp := result["op"]; hasOp {
		t.Error("expected no top-level op key when thresholds are set")
	}
	if _, hasTarget := result["target"]; hasTarget {
		t.Error("expected no top-level target key when thresholds are set")
	}
	if _, hasMatchType := result["matchType"]; hasMatchType {
		t.Error("expected no top-level matchType key when thresholds are set")
	}

	thresholds, ok := result["thresholds"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thresholds map, got %T", result["thresholds"])
	}
	if thresholds["kind"] != "basic" {
		t.Errorf("expected kind 'basic', got %v", thresholds["kind"])
	}
	specs, ok := thresholds["spec"].([]interface{})
	if !ok || len(specs) != 1 {
		t.Fatalf("expected 1 threshold spec, got %v", thresholds["spec"])
	}
	level := specs[0].(map[string]interface{})
	if level["name"] != "critical" || level["target"] != float64(0) || level["matchType"] != "3" || level["op"] != "1" {
		t.Errorf("unexpected threshold level: %v", level)
	}
	channels, ok := level["channels"].([]interface{})
	if !ok || len(channels) != 1 || channels[0] != "Discord #infra (golder)" {
		t.Errorf("expected 1 channel 'Discord #infra (golder)', got %v", level["channels"])
	}
}

// TestConvertQueryBuilder_LogsSignal reproduces the other half of the same
// bug: a logs/traces builder query was always emitted with metric-shaped
// aggregations (metricName/temporality/time+spaceAggregation) and
// source="meter", regardless of signal. A live logs-signal rule has
// aggregations: [{"expression": "count()"}] and source: "" instead.
func TestConvertQueryBuilder_LogsSignal(t *testing.T) {
	builder := v1beta1.QueryBuilder{
		DataSource:            "logs",
		AggregationExpression: "count()",
	}

	result := convertQueryBuilder(builder)

	if result["signal"] != "logs" {
		t.Errorf("expected signal 'logs', got %v", result["signal"])
	}
	if result["source"] != "" {
		t.Errorf("expected empty source for logs signal, got %v", result["source"])
	}
	aggregations, ok := result["aggregations"].([]interface{})
	if !ok || len(aggregations) != 1 {
		t.Fatalf("expected 1 aggregation, got %v", result["aggregations"])
	}
	agg := aggregations[0].(map[string]interface{})
	if agg["expression"] != "count()" {
		t.Errorf("expected expression 'count()', got %v", agg)
	}
	if _, hasMetricName := agg["metricName"]; hasMetricName {
		t.Error("expected no metricName field on a logs-signal aggregation")
	}
}

// TestConvertQueryBuilder_LogsSignal_DefaultExpression checks the fallback
// when AggregationExpression is left empty on a non-metrics query.
func TestConvertQueryBuilder_LogsSignal_DefaultExpression(t *testing.T) {
	builder := v1beta1.QueryBuilder{DataSource: "logs"}

	result := convertQueryBuilder(builder)

	aggregations := result["aggregations"].([]interface{})
	agg := aggregations[0].(map[string]interface{})
	if agg["expression"] != "count()" {
		t.Errorf("expected default expression 'count()', got %v", agg["expression"])
	}
}

// TestConvertAlertType reproduces the third bug found while fixing
// http-auth-failures: SigNoz's rules API rejects LOG_BASED_ALERT (the
// CRD's enum value) with 400 "alert rule is not valid" for a logs-signal
// alert, and only accepts LOGS_BASED_ALERT - confirmed live by isolating
// this single variable. METRIC_BASED_ALERT and other enum values must
// pass through unchanged, since every metrics alert in the fleet already
// syncs successfully with that exact value.
func TestConvertAlertType(t *testing.T) {
	cases := map[string]string{
		"LOG_BASED_ALERT":     "LOGS_BASED_ALERT",
		"METRIC_BASED_ALERT":  "METRIC_BASED_ALERT",
		"TRACE_BASED_ALERT":   "TRACE_BASED_ALERT",
		"ANOMALY_BASED_ALERT": "ANOMALY_BASED_ALERT",
	}
	for in, want := range cases {
		if got := convertAlertType(in); got != want {
			t.Errorf("convertAlertType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIsAlertUpToDate_LogAlertTypeTranslation guards against comparing the
// CRD's raw LOG_BASED_ALERT against the observed LOGS_BASED_ALERT
// directly, which would report drift forever on an otherwise-correct log
// alert and force a spurious Update on every reconcile.
func TestIsAlertUpToDate_LogAlertTypeTranslation(t *testing.T) {
	spec := v1beta1.AlertParameters{
		AlertName:  "Test Alert",
		AlertType:  "LOG_BASED_ALERT",
		EvalWindow: "5m",
		Frequency:  "1m",
	}
	alert := &clients.RuleData{
		AlertName:  "Test Alert",
		AlertType:  "LOGS_BASED_ALERT",
		EvalWindow: "5m",
		Frequency:  "1m",
		Condition:  convertCondition(spec.Condition),
	}

	if !isAlertUpToDate(spec, alert) {
		t.Error("expected LOG_BASED_ALERT spec to match observed LOGS_BASED_ALERT without drift")
	}
}

// TestBuildRuleData_EvaluationOnlyForThresholds reproduces a regression
// found live right after Thresholds support shipped: sending the
// evaluation/schemaVersion/notificationSettings block unconditionally
// broke every alert that does NOT use Thresholds (a promql_rule like
// high-cpu-usage, condition = flat compareOp/target/matchType) with the
// same 400 "alert rule is not valid" - confirmed by isolating this exact
// variable against both a real threshold_rule (http-auth-failures, which
// requires the block) and a real promql_rule (high-cpu-usage, which
// rejects it). buildRuleData must only populate the block when the
// condition actually carries Thresholds.
func TestBuildRuleData_EvaluationOnlyForThresholds(t *testing.T) {
	withThresholds := &v1beta1.Alert{
		Spec: v1beta1.AlertSpec{
			ForProvider: v1beta1.AlertParameters{
				AlertName:  "Thresholds Alert",
				EvalWindow: "5m",
				Frequency:  "1m",
				Condition: v1beta1.RuleCondition{
					Thresholds: []v1beta1.Threshold{
						{Name: "critical", Target: 0, MatchType: "3", Op: "1"},
					},
				},
			},
		},
	}

	rd := buildRuleData(withThresholds)
	if rd.Evaluation == nil {
		t.Error("expected Evaluation to be set for an alert using Thresholds")
	}
	if rd.SchemaVersion == "" {
		t.Error("expected SchemaVersion to be set for an alert using Thresholds")
	}
	if rd.NotificationSettings == nil {
		t.Error("expected NotificationSettings to be set for an alert using Thresholds")
	}

	withoutThresholds := &v1beta1.Alert{
		Spec: v1beta1.AlertSpec{
			ForProvider: v1beta1.AlertParameters{
				AlertName:  "Flat Condition Alert",
				EvalWindow: "5m",
				Frequency:  "5m",
				Condition: v1beta1.RuleCondition{
					CompareOp: ">",
					Target:    float64Ptr(80),
				},
			},
		},
	}

	rd2 := buildRuleData(withoutThresholds)
	if rd2.Evaluation != nil {
		t.Error("expected Evaluation to be nil for a flat-condition alert (no Thresholds)")
	}
	if rd2.SchemaVersion != "" {
		t.Error("expected SchemaVersion to be empty for a flat-condition alert (no Thresholds)")
	}
	if rd2.NotificationSettings != nil {
		t.Error("expected NotificationSettings to be nil for a flat-condition alert (no Thresholds)")
	}
}
