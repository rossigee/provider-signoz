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
			QueryType: 1, // PromQL
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

	if result["compareOp"] != "VALUE" {
		t.Errorf("Expected compareOp 'VALUE', got %v", result["compareOp"])
	}

	if result["target"] != 1.0 {
		t.Errorf("Expected target 1.0, got %v", result["target"])
	}

	if result["matchType"] != 1 {
		t.Errorf("Expected matchType 1, got %v", result["matchType"])
	}

	compositeQuery := result["compositeQuery"].(map[string]interface{})
	if compositeQuery["queryType"] != 1 {
		t.Errorf("Expected queryType 1, got %v", compositeQuery["queryType"])
	}
}

func intPtr(i int) *int {
	return &i
}

func float64Ptr(f float64) *float64 {
	return &f
}
