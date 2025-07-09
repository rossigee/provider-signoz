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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
)

// AlertParameters are the configurable fields of an Alert.
type AlertParameters struct {
	// AlertName is the name of the alert rule.
	// +kubebuilder:validation:Required
	AlertName string `json:"alertName"`

	// AlertType defines the type of alert.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=METRIC_BASED_ALERT;LOG_BASED_ALERT;TRACE_BASED_ALERT;ANOMALY_BASED_ALERT
	AlertType string `json:"alertType"`

	// Condition defines the alert condition.
	// +kubebuilder:validation:Required
	Condition RuleCondition `json:"condition"`

	// EvalWindow is the time window for evaluating the alert.
	// Format: "5m", "1h", etc.
	// +kubebuilder:validation:Required
	EvalWindow string `json:"evalWindow"`

	// Frequency is how often to evaluate the alert.
	// Format: "1m", "5m", etc.
	// +kubebuilder:validation:Required
	Frequency string `json:"frequency"`

	// Severity of the alert.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=info;warning;error;critical
	Severity string `json:"severity"`

	// Labels are key-value pairs associated with the alert.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are key-value pairs that provide additional information.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// PreferredChannels is a list of notification channel names to send alerts to.
	// +optional
	PreferredChannels []string `json:"preferredChannels,omitempty"`

	// ChannelIDsRef are references to NotificationChannel resources.
	// +optional
	ChannelIDsRef []xpv1.Reference `json:"channelIdsRef,omitempty"`

	// ChannelIDsSelector selects NotificationChannels by labels.
	// +optional
	ChannelIDsSelector *xpv1.Selector `json:"channelIdsSelector,omitempty"`

	// Disabled indicates if the alert is disabled.
	// +optional
	Disabled bool `json:"disabled,omitempty"`
}

// RuleCondition defines the condition for triggering an alert.
type RuleCondition struct {
	// CompositeQuery defines the query for the alert condition.
	// +kubebuilder:validation:Required
	CompositeQuery CompositeQuery `json:"compositeQuery"`

	// CompareOp is the comparison operator for the condition.
	// +kubebuilder:validation:Enum=>;>=;<;<=;==;!=
	// +optional
	CompareOp string `json:"compareOp,omitempty"`

	// Target is the threshold value for comparison.
	// +optional
	Target *float64 `json:"target,omitempty"`

	// MatchType defines how to match the condition (1=at least once, 2=all the time).
	// +kubebuilder:validation:Enum=1;2
	// +optional
	MatchType *int `json:"matchType,omitempty"`
}

// CompositeQuery defines a composite query for alerts.
type CompositeQuery struct {
	// QueryType defines the type of query (1=PromQL, 2=ClickHouse, 3=Builder).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=1;2;3
	QueryType int `json:"queryType"`

	// PromQL contains PromQL queries.
	// +optional
	PromQL []AlertPromQuery `json:"promQL,omitempty"`

	// ClickHouse contains ClickHouse SQL queries.
	// +optional
	ClickHouse []AlertClickHouseQuery `json:"clickHouse,omitempty"`

	// Builder contains query builder configuration.
	// +optional
	Builder *QueryBuilder `json:"builder,omitempty"`

	// Expression combines multiple queries with mathematical operations.
	// +optional
	Expression string `json:"expression,omitempty"`
}

// AlertPromQuery defines a PromQL query for alerts.
type AlertPromQuery struct {
	// Query is the PromQL query string.
	// +kubebuilder:validation:Required
	Query string `json:"query"`

	// Name is the query identifier (e.g., "A", "B").
	// +optional
	Name string `json:"name,omitempty"`

	// Legend is an optional legend format.
	// +optional
	Legend string `json:"legend,omitempty"`

	// Disabled indicates if this query is disabled.
	// +optional
	Disabled bool `json:"disabled,omitempty"`
}

// AlertClickHouseQuery defines a ClickHouse SQL query for alerts.
type AlertClickHouseQuery struct {
	// Query is the SQL query string.
	// +kubebuilder:validation:Required
	Query string `json:"query"`

	// Name is the query identifier (e.g., "A", "B").
	// +optional
	Name string `json:"name,omitempty"`

	// Legend is an optional legend format.
	// +optional
	Legend string `json:"legend,omitempty"`

	// Disabled indicates if this query is disabled.
	// +optional
	Disabled bool `json:"disabled,omitempty"`
}

// QueryBuilder defines query builder configuration for alerts.
type QueryBuilder struct {
	// DataSource defines the data source (metrics, logs, traces).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=metrics;logs;traces
	DataSource string `json:"dataSource"`

	// AggregateOperator defines the aggregation function.
	// +optional
	AggregateOperator string `json:"aggregateOperator,omitempty"`

	// AggregateAttribute defines what to aggregate on.
	// +optional
	AggregateAttribute *KeyAttribute `json:"aggregateAttribute,omitempty"`

	// Filters define the query filters.
	// +optional
	Filters *FilterSet `json:"filters,omitempty"`

	// GroupBy defines the grouping attributes.
	// +optional
	GroupBy []KeyAttribute `json:"groupBy,omitempty"`

	// Having defines post-aggregation filters.
	// +optional
	Having []Having `json:"having,omitempty"`

	// OrderBy defines the sort order.
	// +optional
	OrderBy []OrderBy `json:"orderBy,omitempty"`

	// Limit defines the result limit.
	// +optional
	Limit *int `json:"limit,omitempty"`

	// Offset defines the result offset.
	// +optional
	Offset *int `json:"offset,omitempty"`
}

// KeyAttribute defines an attribute for grouping or aggregation.
type KeyAttribute struct {
	// Key is the attribute key.
	Key string `json:"key"`

	// Type is the attribute type.
	Type string `json:"type"`

	// DataType is the data type of the attribute.
	// +optional
	DataType string `json:"dataType,omitempty"`
}

// FilterSet defines a set of filters.
type FilterSet struct {
	// Operator is the logical operator (AND, OR).
	// +kubebuilder:validation:Enum=AND;OR
	Operator string `json:"operator"`

	// Items are the filter conditions.
	Items []FilterItem `json:"items"`
}

// FilterItem defines a single filter condition.
type FilterItem struct {
	// Key is the attribute to filter on.
	Key KeyAttribute `json:"key"`

	// Op is the comparison operator.
	Op string `json:"op"`

	// Value is the filter value.
	// +kubebuilder:pruning:PreserveUnknownFields
	Value *string `json:"value,omitempty"`
}

// Having defines a post-aggregation filter.
type Having struct {
	// ColumnName is the column to filter on.
	ColumnName string `json:"columnName"`

	// Op is the comparison operator.
	Op string `json:"op"`

	// Value is the filter value.
	// +kubebuilder:pruning:PreserveUnknownFields
	Value *string `json:"value,omitempty"`
}

// OrderBy defines sort order.
type OrderBy struct {
	// ColumnName is the column to sort by.
	ColumnName string `json:"columnName"`

	// Order is the sort direction (ASC, DESC).
	// +kubebuilder:validation:Enum=ASC;DESC
	Order string `json:"order"`
}

// AlertSpec defines the desired state of Alert
type AlertSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       AlertParameters `json:"forProvider"`
}

// AlertObservation are the observable fields of an Alert.
type AlertObservation struct {
	// ID is the unique identifier of the alert in SigNoz.
	ID string `json:"id,omitempty"`

	// State is the current state of the alert (inactive, pending, firing, etc.).
	State string `json:"state,omitempty"`

	// LastFiredTime is when the alert last fired.
	LastFiredTime *metav1.Time `json:"lastFiredTime,omitempty"`

	// CreatedAt is when the alert was created.
	CreatedAt *metav1.Time `json:"createdAt,omitempty"`

	// UpdatedAt is when the alert was last updated.
	UpdatedAt *metav1.Time `json:"updatedAt,omitempty"`

	// ResolvedChannelIDs contains the IDs of resolved notification channels.
	ResolvedChannelIDs []string `json:"resolvedChannelIds,omitempty"`
}

// AlertStatus represents the observed state of an Alert.
type AlertStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          AlertObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true

// Alert is the Schema for the Alerts API
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="STATE",type="string",JSONPath=".status.atProvider.state"
// +kubebuilder:printcolumn:name="SEVERITY",type="string",JSONPath=".spec.forProvider.severity"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,signoz}
type Alert struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AlertSpec   `json:"spec"`
	Status            AlertStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AlertList contains a list of Alerts
type AlertList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Alert `json:"items"`
}

// Alert type metadata.
var (
	Alert_Kind             = "Alert"
	Alert_GroupKind        = schema.GroupKind{Group: Group, Kind: Alert_Kind}.String()
	Alert_KindAPIVersion   = Alert_Kind + "." + SchemeGroupVersion.String()
	Alert_GroupVersionKind = SchemeGroupVersion.WithKind(Alert_Kind)
)

func init() {
	SchemeBuilder.Register(&Alert{}, &AlertList{})
}