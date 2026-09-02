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

package v1beta1

import (
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

	// Thresholds defines SigNoz's v5 multi-level threshold block (rules API
	// condition.thresholds, kind "basic"). When set, this replaces
	// CompareOp/Target/MatchType entirely on the wire - those only express a
	// single flat threshold and cannot represent multiple severity levels
	// each with their own notification channels, which the flat form
	// silently gets wrong for any rule created with multi-level thresholds.
	// +optional
	Thresholds []Threshold `json:"thresholds,omitempty"`
}

// Threshold defines a single severity level within a v5 multi-level
// threshold block (RuleCondition.Thresholds).
type Threshold struct {
	// Name is the severity level name (e.g. "critical", "warning").
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Target is the threshold value for this level.
	// +kubebuilder:validation:Required
	Target float64 `json:"target"`

	// TargetUnit is the unit of the target value.
	// +optional
	TargetUnit string `json:"targetUnit,omitempty"`

	// RecoveryTarget is the value at which this level recovers.
	// +optional
	RecoveryTarget *float64 `json:"recoveryTarget,omitempty"`

	// MatchType defines how to match the condition, as SigNoz's raw v5
	// enum string (not the same numbering as RuleCondition.MatchType).
	// +kubebuilder:validation:Required
	MatchType string `json:"matchType"`

	// Op is the comparison operator, as SigNoz's raw v5 enum string.
	// +kubebuilder:validation:Required
	Op string `json:"op"`

	// Channels is a list of notification channel names for this severity
	// level.
	// +optional
	Channels []string `json:"channels,omitempty"`
}

// CompositeQuery defines a composite query for alerts.
type CompositeQuery struct {
	// QueryType defines the type of query (PromQL|ClickHouse|Builder).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum="PromQL";"ClickHouse";"Builder";"1";"2";"3";"promql";"clickhouse_sql";"builder"
	QueryType string `json:"queryType"`

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

	// PanelType is the panel type for the alert query (e.g. graph, value).
	// +optional
	// +kubebuilder:validation:Enum=graph;value;table;list;trace
	PanelType string `json:"panelType,omitempty"`

	// Unit is the unit of the resulting time series.
	// +optional
	Unit string `json:"unit,omitempty"`
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

// QueryBuilder defines a v5 builder query for alerts. Each QueryBuilder
// maps to one entry in the SigNoz CompositeQuery.builderQueries map keyed
// by QueryName.
type QueryBuilder struct {
	// QueryName is the identifier for this builder query (e.g. "A").
	// Defaults to "A" if empty.
	// +optional
	// +kubebuilder:validation:MaxLength=1
	QueryName string `json:"queryName,omitempty"`

	// DataSource defines the data source (metrics, logs, traces).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=metrics;logs;traces
	DataSource string `json:"dataSource"`

	// StepInterval is the step interval in seconds used for the query.
	// Defaults to 60s.
	// +optional
	StepInterval *int64 `json:"stepInterval,omitempty"`

	// Expression is the formula expression for this query. Defaults to
	// QueryName when empty (i.e. a simple builder query, not a formula).
	// +optional
	Expression string `json:"expression,omitempty"`

	// AggregateOperator defines the aggregation function (e.g. sum, avg,
	// rate, p99). Required for non-metric data sources and for some
	// metric operators.
	// +optional
	AggregateOperator string `json:"aggregateOperator,omitempty"`

	// AggregationExpression is the aggregation for logs/traces data
	// sources, expressed as a single SigNoz expression string (e.g.
	// "count()", "sum(bytes)") rather than the metric-style metricName +
	// time/space aggregation split. Ignored for metrics data sources;
	// defaults to "count()" for logs/traces when empty.
	// +optional
	AggregationExpression string `json:"aggregationExpression,omitempty"`

	// AggregateAttribute defines what to aggregate on.
	// +optional
	AggregateAttribute *KeyAttribute `json:"aggregateAttribute,omitempty"`

	// TimeAggregation is the aggregation across the time dimension
	// (e.g. rate, sum, avg, increase). Required for metric data sources
	// that use the v5 space/time aggregation split.
	// +optional
	TimeAggregation string `json:"timeAggregation,omitempty"`

	// SpaceAggregation is the aggregation across the label/series
	// dimension (e.g. sum, avg, min, max, p99). Required for metric data
	// sources that use the v5 space/time aggregation split.
	// +optional
	SpaceAggregation string `json:"spaceAggregation,omitempty"`

	// Temporality is the metric temporality hint (Delta, Cumulative,
	// Unspecified). SigNoz auto-detects this if omitted.
	// +optional
	// +kubebuilder:validation:Enum=Delta;Cumulative;Unspecified
	Temporality string `json:"temporality,omitempty"`

	// ReduceTo reduces a multi-series result to a single value
	// (last, sum, avg, min, max).
	// +optional
	// +kubebuilder:validation:Enum=last;sum;avg;min;max
	ReduceTo string `json:"reduceTo,omitempty"`

	// Filters define the query filters.
	// +optional
	Filters *FilterSet `json:"filters,omitempty"`

	// FilterExpression is the raw v5 filter expression string emitted as
	// compositeQuery.queries[].spec.filter.expression (e.g.
	// "job_name = 'dns-internal-validation'"). Prefer this over the
	// structured Filters block when the live SigNoz rule carries an
	// expression that the structured form cannot represent.
	// +optional
	FilterExpression string `json:"filterExpression,omitempty"`

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

	// SelectColumns restricts the columns returned for logs/traces
	// queries.
	// +optional
	SelectColumns []KeyAttribute `json:"selectColumns,omitempty"`

	// Legend overrides the legend format for the resulting series.
	// +optional
	Legend string `json:"legend,omitempty"`

	// Disabled indicates whether this builder query is disabled.
	// +optional
	Disabled bool `json:"disabled,omitempty"`
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
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              AlertParameters `json:"forProvider"`
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
	xpv1.ConditionedStatus `json:",inline"`
	AtProvider             AlertObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion

// Alert is the Schema for the Alerts API
// +crossplane:generate:reference:type=github.com/rossigee/provider-signoz/apis/channel/v1beta1.NotificationChannel
// +crossplane:generate:reference:extractor=github.com/crossplane/crossplane-runtime/pkg/reference.ExternalName()
// +crossplane:generate:reference:refFieldName=ChannelIDsRef
// +crossplane:generate:reference:selectorFieldName=ChannelIDsSelector
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="STATE",type="string",JSONPath=".status.atProvider.state"
// +kubebuilder:printcolumn:name="SEVERITY",type="string",JSONPath=".spec.forProvider.severity"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,signoz}
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
