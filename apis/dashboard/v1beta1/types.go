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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
)

// DashboardParameters are the configurable fields of a Dashboard.
type DashboardParameters struct {
	// Title is the title of the dashboard.
	// +kubebuilder:validation:Required
	Title string `json:"title"`

	// Description is an optional description of the dashboard.
	// +optional
	Description *string `json:"description,omitempty"`

	// Tags is a list of tags associated with the dashboard.
	// +optional
	Tags []string `json:"tags,omitempty"`

	// Layout defines the grid layout of widgets on the dashboard.
	// +optional
	Layout []Layout `json:"layout,omitempty"`

	// Widgets defines the panels/widgets on the dashboard.
	// +kubebuilder:validation:Required
	Widgets []Widget `json:"widgets"`

	// Variables defines dashboard variables for dynamic queries.
	// +optional
	Variables map[string]Variable `json:"variables,omitempty"`
}

// Layout defines the position and size of a widget on the dashboard grid.
type Layout struct {
	// I is the widget ID this layout applies to.
	I string `json:"i"`

	// X is the horizontal position on the grid.
	X int `json:"x"`

	// Y is the vertical position on the grid.
	Y int `json:"y"`

	// W is the width in grid units.
	W int `json:"w"`

	// H is the height in grid units.
	H int `json:"h"`

	// Moved indicates if the widget has been moved.
	// +optional
	Moved bool `json:"moved,omitempty"`

	// Static indicates if the widget is static (can't be dragged).
	// +optional
	Static bool `json:"static,omitempty"`
}

// Widget defines a panel on the dashboard.
type Widget struct {
	// ID is the unique identifier for the widget.
	// +kubebuilder:validation:Required
	ID string `json:"id"`

	// Title is the title of the widget.
	// +kubebuilder:validation:Required
	Title string `json:"title"`

	// Description is an optional description of the widget.
	// +optional
	Description *string `json:"description,omitempty"`

	// PanelType defines the visualization type (e.g., "graph", "table", "value").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=graph;table;value;list;pie
	PanelType string `json:"panelType"`

	// Query defines the data query for this widget.
	// +kubebuilder:validation:Required
	Query Query `json:"query"`

	// IsStacked indicates if the graph should be stacked.
	// +optional
	IsStacked *bool `json:"isStacked,omitempty"`

	// NullZeroValues defines how to handle null/zero values.
	// +optional
	NullZeroValues *string `json:"nullZeroValues,omitempty"`

	// YAxisUnit defines the unit for the Y-axis.
	// +optional
	YAxisUnit *string `json:"yAxisUnit,omitempty"`

	// TimePreference allows overriding the dashboard time range for this widget.
	// +optional
	TimePreference *string `json:"timePreference,omitempty"`
}

// Query defines the data query for a widget.
type Query struct {
	// QueryType defines the type of query (1=PromQL, 2=ClickHouse, 3=Builder).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=1;2;3
	QueryType int `json:"queryType"`

	// PromQL contains PromQL queries.
	// +optional
	PromQL []PromQuery `json:"promQL,omitempty"`

	// ClickHouse contains ClickHouse SQL queries.
	// +optional
	ClickHouse []ClickHouseQuery `json:"clickHouse,omitempty"`

	// Builder contains query builder configuration.
	// +optional
	Builder *MetricsBuilder `json:"builder,omitempty"`
}

// PromQuery defines a PromQL query.
type PromQuery struct {
	// Query is the PromQL query string.
	// +kubebuilder:validation:Required
	Query string `json:"query"`

	// Name is an optional name for this query.
	// +optional
	Name *string `json:"name,omitempty"`

	// Legend is an optional legend format.
	// +optional
	Legend *string `json:"legend,omitempty"`

	// Disabled indicates if this query is disabled.
	// +optional
	Disabled bool `json:"disabled,omitempty"`
}

// ClickHouseQuery defines a ClickHouse SQL query.
type ClickHouseQuery struct {
	// Query is the SQL query string.
	// +kubebuilder:validation:Required
	Query string `json:"query"`

	// Name is an optional name for this query.
	// +optional
	Name *string `json:"name,omitempty"`

	// Legend is an optional legend format.
	// +optional
	Legend *string `json:"legend,omitempty"`

	// Disabled indicates if this query is disabled.
	// +optional
	Disabled bool `json:"disabled,omitempty"`
}

// MetricsBuilder defines query builder configuration.
type MetricsBuilder struct {
	// QueryBuilder contains individual metric queries.
	QueryBuilder []QueryBuilder `json:"queryBuilder"`

	// Formulas contains formula expressions combining queries.
	// +optional
	Formulas []string `json:"formulas,omitempty"`
}

// QueryBuilder defines a single metric query in the builder.
type QueryBuilder struct {
	// Name is the query identifier (e.g., "A", "B").
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// MetricName is the name of the metric to query.
	// +kubebuilder:validation:Required
	MetricName string `json:"metricName"`

	// AggregateOperator defines the aggregation function.
	// +optional
	AggregateOperator *string `json:"aggregateOperator,omitempty"`

	// GroupBy defines the grouping dimensions.
	// +optional
	GroupBy []string `json:"groupBy,omitempty"`

	// Legend is an optional legend format.
	// +optional
	Legend *string `json:"legend,omitempty"`

	// Disabled indicates if this query is disabled.
	// +optional
	Disabled bool `json:"disabled,omitempty"`
}

// Variable defines a dashboard variable.
type Variable struct {
	// Type defines the variable type (e.g., "query", "custom", "textbox").
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// Description is an optional description.
	// +optional
	Description *string `json:"description,omitempty"`

	// QueryValue contains the query for query-type variables.
	// +optional
	QueryValue *string `json:"queryValue,omitempty"`

	// CustomValue contains the value for custom-type variables.
	// +optional
	CustomValue *string `json:"customValue,omitempty"`

	// TextboxValue contains the default value for textbox variables.
	// +optional
	TextboxValue *string `json:"textboxValue,omitempty"`

	// MultiSelect indicates if multiple values can be selected.
	// +optional
	MultiSelect bool `json:"multiSelect,omitempty"`

	// ShowAllOption indicates if an "All" option should be shown.
	// +optional
	ShowAllOption bool `json:"showAllOption,omitempty"`

	// SelectedValue contains the currently selected value(s).
	// +optional
	SelectedValue *string `json:"selectedValue,omitempty"`

	// Sort defines the sort order for values.
	// +optional
	Sort *string `json:"sort,omitempty"`
}

// DashboardSpec defines the desired state of Dashboard
type DashboardSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       DashboardParameters `json:"forProvider"`
}

// DashboardObservation are the observable fields of a Dashboard.
type DashboardObservation struct {
	// ID is the unique identifier of the dashboard in SigNoz.
	ID string `json:"id,omitempty"`

	// UUID is the UUID of the dashboard.
	UUID string `json:"uuid,omitempty"`

	// CreatedAt is the timestamp when the dashboard was created.
	CreatedAt *metav1.Time `json:"createdAt,omitempty"`

	// UpdatedAt is the timestamp when the dashboard was last updated.
	UpdatedAt *metav1.Time `json:"updatedAt,omitempty"`
}

// DashboardStatus represents the observed state of a Dashboard.
type DashboardStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          DashboardObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion

// Dashboard is the Schema for the Dashboards API
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="EXTERNAL-NAME",type="string",JSONPath=".metadata.annotations.crossplane\\.io/external-name"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,signoz}
type Dashboard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DashboardSpec   `json:"spec"`
	Status            DashboardStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DashboardList contains a list of Dashboards
type DashboardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dashboard `json:"items"`
}

// Dashboard type metadata.
var (
	Dashboard_Kind             = "Dashboard"
	Dashboard_GroupKind        = schema.GroupKind{Group: Group, Kind: Dashboard_Kind}.String()
	Dashboard_KindAPIVersion   = Dashboard_Kind + "." + SchemeGroupVersion.String()
	Dashboard_GroupVersionKind = SchemeGroupVersion.WithKind(Dashboard_Kind)
)

func init() {
	SchemeBuilder.Register(&Dashboard{}, &DashboardList{})
}