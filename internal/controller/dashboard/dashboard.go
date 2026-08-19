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
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/feature"
	"github.com/crossplane/crossplane-runtime/v2/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/rossigee/provider-signoz/apis/dashboard/v1beta1"
	apisv1beta1 "github.com/rossigee/provider-signoz/apis/v1beta1"
	"github.com/rossigee/provider-signoz/internal/clients"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	errNotDashboard    = "managed resource is not a Dashboard custom resource"
	errTrackPCUsage    = "cannot track ProviderConfig usage"
	errGetPC           = "cannot get ProviderConfig"
	errGetCreds        = "cannot get credentials"
	errNewClient       = "cannot create new Service"
	errCreateDashboard = "cannot create dashboard"
	errUpdateDashboard = "cannot update dashboard"
	errDeleteDashboard = "cannot delete dashboard"
	errGetDashboard    = "cannot get dashboard"
)

// Setup adds a controller that reconciles Dashboard managed resources.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(v1beta1.Dashboard_GroupVersionKind.Kind)

	opts := []managed.ReconcilerOption{
		managed.WithExternalConnector(&connector{
			kube:         resource.ClientApplicator{Client: mgr.GetClient(), Applicator: resource.NewAPIPatchingApplicator(mgr.GetClient())},
			usage:        resource.ModernTrackerFn(func(ctx context.Context, mg resource.ModernManaged) error { return nil }),
			newServiceFn: clients.NewClient,
		}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(name))),
	}

	if o.Features != nil && o.Features.Enabled(feature.EnableBetaManagementPolicies) {
		opts = append(opts, managed.WithManagementPolicies())
	}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1beta1.Dashboard_GroupVersionKind),
		opts...,
	)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		WithEventFilter(resource.DesiredStateChanged()).
		For(&v1beta1.Dashboard{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

// A connector is expected to produce an ExternalClient when its Connect method
// is called.
type connector struct {
	kube         resource.ClientApplicator
	usage        resource.ModernTracker
	newServiceFn func(cfg clients.Config) *clients.Client
}

// Connect typically produces an ExternalClient by:
// 1. Tracking that the managed resource is using a ProviderConfig.
// 2. Getting the managed resource's ProviderConfig.
// 3. Getting the credentials specified by the ProviderConfig.
// 4. Using the credentials to form a client.
func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1beta1.Dashboard)
	if !ok {
		return nil, errors.New(errNotDashboard)
	}

	if err := c.usage.Track(ctx, mg.(resource.ModernManaged)); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	if cr.GetProviderConfigReference() == nil {
		return nil, errors.New("no providerConfigRef provided")
	}

	pc := &apisv1beta1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Namespace: "", Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	cfg, err := clients.GetConfig(ctx, c.kube, mg)
	if err != nil {
		return nil, errors.Wrap(err, errGetCreds)
	}

	return &external{service: c.newServiceFn(*cfg)}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	service *clients.Client
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1beta1.Dashboard)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotDashboard)
	}

	// Get the dashboard ID from the external-name annotation
	dashboardID := cr.GetAnnotations()["crossplane.io/external-name"]
	// Generate new UUID if external-name is empty or not a valid UUID
	if dashboardID == "" {
		// Generate deterministic UUID-v7 for new resources
		dashboardID = clients.GenerateExternalName(cr.GetNamespace(), cr.GetName())
		if cr.GetAnnotations() == nil {
			cr.SetAnnotations(make(map[string]string))
		}
		cr.GetAnnotations()["crossplane.io/external-name"] = dashboardID
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	// Validate external-name is a valid UUID - if not, generate a new one
	if _, err := uuid.Parse(dashboardID); err != nil {
		// Not a valid UUID, generate a new one
		dashboardID = clients.GenerateExternalName(cr.GetNamespace(), cr.GetName())
		if cr.GetAnnotations() == nil {
			cr.SetAnnotations(make(map[string]string))
		}
		cr.GetAnnotations()["crossplane.io/external-name"] = dashboardID
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	dashboard, err := c.service.GetDashboardV2(ctx, dashboardID)
	if err != nil {
		if clients.IsNotFound(err) {
			clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, nil, true)
			return managed.ExternalObservation{
				ResourceExists: false,
			}, nil
		}
		clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, err, false)
		return managed.ExternalObservation{}, errors.Wrap(err, errGetDashboard)
	}
	clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, nil, true)

	// Update the status with observed values
	cr.Status.AtProvider.ID = dashboard.ID
	cr.Status.AtProvider.UUID = dashboard.UUID

	if dashboard.CreatedAt != "" {
		if createdAt, err := time.Parse(time.RFC3339, dashboard.CreatedAt); err == nil {
			cr.Status.AtProvider.CreatedAt = &metav1.Time{Time: createdAt}
		}
	}

	if dashboard.UpdatedAt != "" {
		if updatedAt, err := time.Parse(time.RFC3339, dashboard.UpdatedAt); err == nil {
			cr.Status.AtProvider.UpdatedAt = &metav1.Time{Time: updatedAt}
		}
	}

	// Set Ready condition since the resource exists
	cr.Status.SetConditions(xpv1.Available())

	// Check if the dashboard is up to date (V2 version)
	upToDate := isDashboardV2UpToDate(cr.Spec.ForProvider, dashboard)

	logger := log.FromContext(ctx)
	logger.V(1).Info("Dashboard observe", "name", cr.Name, "widgets_count", len(cr.Spec.ForProvider.Widgets), "panels_count", len(dashboard.Spec.Panels), "upToDate", upToDate)

	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: upToDate,
	}, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1beta1.Dashboard)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotDashboard)
	}

	description := ""
	if cr.Spec.ForProvider.Description != nil {
		description = *cr.Spec.ForProvider.Description
	}

	dashboardV2 := convertToV2(
		cr.Spec.ForProvider.Title,
		description,
		cr.Spec.ForProvider.Tags,
		cr.Spec.ForProvider.Widgets,
		cr.Spec.ForProvider.Layout,
		cr.Spec.ForProvider.Variables,
	)

	created, err := c.service.CreateDashboardV2(ctx, dashboardV2)
	if err != nil {
		clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, err, false)
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateDashboard)
	}
	clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, nil, true)

	// Set the external-name annotation to the dashboard ID (UUID from V2 API)
	if cr.GetAnnotations() == nil {
		cr.SetAnnotations(make(map[string]string))
	}
	// V2 API returns ID or UUID - use whichever is available
	externalID := created.ID
	if externalID == "" {
		externalID = created.UUID
	}
	cr.GetAnnotations()["crossplane.io/external-name"] = externalID

	return managed.ExternalCreation{}, nil
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1beta1.Dashboard)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotDashboard)
	}

	dashboardID := cr.GetAnnotations()["crossplane.io/external-name"]
	if dashboardID == "" {
		return managed.ExternalUpdate{}, errors.New("dashboard ID not found")
	}

	description := ""
	if cr.Spec.ForProvider.Description != nil {
		description = *cr.Spec.ForProvider.Description
	}

	dashboardV2 := convertToV2(
		cr.Spec.ForProvider.Title,
		description,
		cr.Spec.ForProvider.Tags,
		cr.Spec.ForProvider.Widgets,
		cr.Spec.ForProvider.Layout,
		cr.Spec.ForProvider.Variables,
	)

	_, err := c.service.UpdateDashboardV2(ctx, dashboardID, dashboardV2)
	if err != nil {
		clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, err, false)
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateDashboard)
	}
	clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, nil, true)

	return managed.ExternalUpdate{}, nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr, ok := mg.(*v1beta1.Dashboard)
	if !ok {
		return managed.ExternalDelete{}, errors.New(errNotDashboard)
	}

	dashboardID := cr.GetAnnotations()["crossplane.io/external-name"]
	if dashboardID == "" {
		return managed.ExternalDelete{}, nil // Nothing to delete
	}

	err := c.service.DeleteDashboard(ctx, dashboardID)
	if err != nil && !clients.IsNotFound(err) {
		clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, err, false)
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteDashboard)
	}
	clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, nil, true)

	return managed.ExternalDelete{}, nil
}

func (c *external) Disconnect(ctx context.Context) error {
	// Nothing to disconnect for SigNoz API client
	return nil
}

// Helper functions

func isDashboardUpToDate(spec v1beta1.DashboardParameters, dashboard *clients.DashboardData) bool {
	if spec.Title != dashboard.Title {
		return false
	}

	expectedDesc := ""
	if spec.Description != nil {
		expectedDesc = *spec.Description
	}
	if expectedDesc != dashboard.Description {
		return false
	}

	// Compare tags
	if len(spec.Tags) != len(dashboard.Tags) {
		return false
	}
	for i, tag := range spec.Tags {
		if i >= len(dashboard.Tags) || tag != dashboard.Tags[i] {
			return false
		}
	}

	// For simplicity, we'll consider the dashboard up to date if basic fields match
	// In a more sophisticated implementation, we would deeply compare widgets, layout, etc.
	return true
}

func isDashboardV2UpToDate(spec v1beta1.DashboardParameters, dashboard *clients.DashboardV2Data) bool {
	if spec.Title != dashboard.Spec.Display.Name {
		return false
	}

	expectedDesc := ""
	if spec.Description != nil {
		expectedDesc = *spec.Description
	}
	if dashboard.Spec.Display != nil && expectedDesc != dashboard.Spec.Display.Description {
		return false
	}

	// Compare tags
	if len(spec.Tags) != len(dashboard.Tags) {
		return false
	}
	for i, tag := range spec.Tags {
		if i >= len(dashboard.Tags) || tag != dashboard.Tags[i].Key {
			return false
		}
	}

	// Compare widget count
	if len(spec.Widgets) != len(dashboard.Spec.Panels) {
		return false
	}

	// Compare each widget's content against the observed panel. Matching
	// on ID alone is not enough: editing a query string inside an existing
	// widget keeps the same ID and count, so that drift must be detected
	// here or it is silently never applied (Update is never called).
	for _, w := range spec.Widgets {
		panel, exists := dashboard.Spec.Panels[w.ID]
		if !exists {
			return false
		}
		if !isPanelUpToDate(w, panel) {
			return false
		}
	}

	return isVariablesUpToDate(spec.Variables, dashboard.Spec.Variables)
}

// isVariablesUpToDate compares the desired Variables map against the
// observed SigNoz v6 variables array, keyed by each variable's spec.name.
// Same rationale as isPanelUpToDate: without this, editing an existing
// variable's query/value while nothing else on the dashboard changes would
// be invisible to Observe() and Update() would never be called.
func isVariablesUpToDate(variables map[string]v1beta1.Variable, observed []interface{}) bool {
	if len(variables) != len(observed) {
		return false
	}

	observedByName := make(map[string]interface{}, len(observed))
	for _, ov := range observed {
		ovMap, ok := ov.(map[string]interface{})
		if !ok {
			return false
		}
		name, ok := nestedString(ovMap, "spec", "name")
		if !ok {
			return false
		}
		observedByName[name] = ov
	}

	for name, v := range variables {
		ov, exists := observedByName[name]
		if !exists {
			return false
		}
		if !isVariableUpToDate(name, v, ov) {
			return false
		}
	}

	return true
}

// isVariableUpToDate compares one desired Variable against its observed
// SigNoz representation, using convertVariableToV2 as the single source of
// truth for what Create/Update actually sends so the comparison can't drift
// from it. Only compares fields convertVariableToV2 sets - name, and either
// the textbox value or the plugin kind + query/custom value + allowMultiple/
// allowAllValue - since the API fills in additional defaults (display,
// sort, capturingRegexp, etc.) this provider never sends.
func isVariableUpToDate(name string, v v1beta1.Variable, observed interface{}) bool {
	expected := convertVariableToV2(name, v)

	observedMap, ok := observed.(map[string]interface{})
	if !ok {
		return false
	}
	if expected["kind"] != observedMap["kind"] {
		return false
	}

	expectedSpec, ok := expected["spec"].(map[string]interface{})
	if !ok {
		return false
	}
	observedSpec, ok := observedMap["spec"].(map[string]interface{})
	if !ok {
		return false
	}
	if expectedSpec["name"] != observedSpec["name"] {
		return false
	}

	if v.Type == "textbox" {
		return expectedSpec["value"] == observedSpec["value"]
	}

	if expectedSpec["allowMultiple"] != observedSpec["allowMultiple"] {
		return false
	}
	if expectedSpec["allowAllValue"] != observedSpec["allowAllValue"] {
		return false
	}

	expectedPlugin, ok := expectedSpec["plugin"].(map[string]interface{})
	if !ok {
		return false
	}
	observedPlugin, ok := observedSpec["plugin"].(map[string]interface{})
	if !ok {
		return false
	}
	if expectedPlugin["kind"] != observedPlugin["kind"] {
		return false
	}

	expectedPluginSpec, ok := expectedPlugin["spec"].(map[string]interface{})
	if !ok {
		return false
	}
	observedPluginSpec, ok := observedPlugin["spec"].(map[string]interface{})
	if !ok {
		return false
	}

	if v.Type == "custom" {
		return expectedPluginSpec["customValue"] == observedPluginSpec["customValue"]
	}
	return expectedPluginSpec["queryValue"] == observedPluginSpec["queryValue"]
}

// isPanelUpToDate compares a desired widget against the observed SigNoz v2
// panel returned by the API. It only compares the fields convertToV2 /
// convertQueryToV2 actually set - display name, Y-axis unit, and each
// sub-query's type/query/legend/name/disabled - since the live API response
// fills in many additional default fields (chart appearance, thresholds,
// legend position, etc.) that this provider never sends and must not be
// diffed against.
func isPanelUpToDate(w v1beta1.Widget, panel interface{}) bool {
	panelMap, ok := panel.(map[string]interface{})
	if !ok {
		return false
	}
	specMap, ok := panelMap["spec"].(map[string]interface{})
	if !ok {
		return false
	}

	displayMap, ok := specMap["display"].(map[string]interface{})
	if !ok {
		return false
	}
	if name, _ := displayMap["name"].(string); name != w.Title {
		return false
	}

	if w.YAxisUnit != nil {
		unit, ok := nestedString(specMap, "plugin", "spec", "formatting", "unit")
		if !ok || unit != *w.YAxisUnit {
			return false
		}
	}

	observedQueries, ok := extractQuerySpecs(specMap)
	if !ok {
		return false
	}

	// convertQueryToV2 returns a single composite-query object; wrap it the
	// same way convertToV2 does (panel.spec.queries = [compositeQuery]) so
	// extractQuerySpecs can walk both observed and expected the same way.
	expectedWrapped := map[string]interface{}{
		"queries": []interface{}{convertQueryToV2(w.Query)},
	}
	expectedQueries, ok := extractQuerySpecs(expectedWrapped)
	if !ok {
		return false
	}

	if len(observedQueries) != len(expectedQueries) {
		return false
	}
	for i := range expectedQueries {
		if expectedQueries[i] != observedQueries[i] {
			return false
		}
	}

	return true
}

// extractQuerySpecs walks a panel/query spec map down to the list of
// individual sub-queries (compositeQuery.spec.plugin.spec.queries) and
// returns a normalized, comparable representation of each one's type,
// query string, legend, name and disabled flag.
func extractQuerySpecs(widgetSpecMap map[string]interface{}) ([]string, bool) {
	queriesRaw, ok := widgetSpecMap["queries"].([]interface{})
	if !ok || len(queriesRaw) == 0 {
		return nil, false
	}
	compQuery, ok := queriesRaw[0].(map[string]interface{})
	if !ok {
		return nil, false
	}

	innerQueries, ok := nestedSlice(compQuery, "spec", "plugin", "spec", "queries")
	if !ok {
		return nil, false
	}

	result := make([]string, 0, len(innerQueries))
	for _, iq := range innerQueries {
		iqMap, ok := iq.(map[string]interface{})
		if !ok {
			return nil, false
		}
		qType, _ := iqMap["type"].(string)
		qSpec, ok := iqMap["spec"].(map[string]interface{})
		if !ok {
			return nil, false
		}
		result = append(result, fmt.Sprintf("%s|%v|%v|%v|%v",
			qType, qSpec["query"], qSpec["legend"], qSpec["name"], qSpec["disabled"]))
	}

	return result, true
}

// nestedString descends a chain of map[string]interface{} keys and returns
// the final value as a string, or false if any step of the path is missing
// or not the expected type.
func nestedString(m map[string]interface{}, keys ...string) (string, bool) {
	v, ok := nestedValue(m, keys...)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// nestedSlice descends a chain of map[string]interface{} keys and returns
// the final value as a []interface{}, or false if any step of the path is
// missing or not the expected type.
func nestedSlice(m map[string]interface{}, keys ...string) ([]interface{}, bool) {
	v, ok := nestedValue(m, keys...)
	if !ok {
		return nil, false
	}
	s, ok := v.([]interface{})
	return s, ok
}

// nestedValue descends a chain of map[string]interface{} keys, returning
// the value at the end of the path or false if any intermediate step is
// missing or not a map.
func nestedValue(m map[string]interface{}, keys ...string) (interface{}, bool) {
	var cur interface{} = m
	for _, k := range keys {
		curMap, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur, ok = curMap[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func convertWidgets(widgets []v1beta1.Widget) []interface{} {
	result := make([]interface{}, len(widgets))
	for i, w := range widgets {
		widget := map[string]interface{}{
			"id":        w.ID,
			"title":     w.Title,
			"panelType": w.PanelType,
			"query":     convertQuery(w.Query),
		}

		if w.Description != nil {
			widget["description"] = *w.Description
		}
		if w.IsStacked != nil {
			widget["isStacked"] = *w.IsStacked
		}
		if w.NullZeroValues != nil {
			widget["nullZeroValues"] = *w.NullZeroValues
		}
		if w.YAxisUnit != nil {
			widget["yAxisUnit"] = *w.YAxisUnit
		}
		if w.TimePreference != nil {
			widget["timePreference"] = *w.TimePreference
		}

		result[i] = widget
	}
	return result
}

func convertQuery(query v1beta1.Query) map[string]interface{} {
	result := map[string]interface{}{
		"queryType": query.QueryType,
	}

	if len(query.PromQL) > 0 {
		promQueries := make([]interface{}, len(query.PromQL))
		for i, pq := range query.PromQL {
			promQuery := map[string]interface{}{
				"query":    pq.Query,
				"disabled": pq.Disabled,
			}
			if pq.Name != nil {
				promQuery["name"] = *pq.Name
			}
			if pq.Legend != nil {
				promQuery["legend"] = *pq.Legend
			}
			promQueries[i] = promQuery
		}
		result["promQL"] = promQueries
	}

	if len(query.ClickHouse) > 0 {
		chQueries := make([]interface{}, len(query.ClickHouse))
		for i, chq := range query.ClickHouse {
			chQuery := map[string]interface{}{
				"query":    chq.Query,
				"disabled": chq.Disabled,
			}
			if chq.Name != nil {
				chQuery["name"] = *chq.Name
			}
			if chq.Legend != nil {
				chQuery["legend"] = *chq.Legend
			}
			chQueries[i] = chQuery
		}
		result["clickHouse"] = chQueries
	}

	if query.Builder != nil {
		result["builder"] = convertMetricsBuilder(*query.Builder)
	}

	return result
}

func convertMetricsBuilder(builder v1beta1.MetricsBuilder) map[string]interface{} {
	result := map[string]interface{}{}

	if len(builder.QueryBuilder) > 0 {
		queryBuilders := make([]interface{}, len(builder.QueryBuilder))
		for i, qb := range builder.QueryBuilder {
			queryBuilder := map[string]interface{}{
				"name":       qb.Name,
				"metricName": qb.MetricName,
				"disabled":   qb.Disabled,
			}
			if qb.AggregateOperator != nil {
				queryBuilder["aggregateOperator"] = *qb.AggregateOperator
			}
			if len(qb.GroupBy) > 0 {
				queryBuilder["groupBy"] = qb.GroupBy
			}
			if qb.Legend != nil {
				queryBuilder["legend"] = *qb.Legend
			}
			queryBuilders[i] = queryBuilder
		}
		result["queryBuilder"] = queryBuilders
	}

	if len(builder.Formulas) > 0 {
		result["formulas"] = builder.Formulas
	}

	return result
}

func convertToV2(title, description string, tags []string, widgets []v1beta1.Widget, layout []v1beta1.Layout, variables map[string]v1beta1.Variable) *clients.DashboardV2Data {
	v2name := strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	v2 := &clients.DashboardV2Data{
		Name:          v2name,
		SchemaVersion: "v6",
		Tags:          make([]clients.DashboardTag, len(tags)),
		Spec: clients.DashboardV2Spec{
			Display: &clients.DashboardV2Display{
				Name:        title,
				Description: description,
			},
			Panels:    make(map[string]interface{}),
			Variables: convertVariablesToV2(variables),
			Layouts:   []interface{}{},
		},
	}

	if description != "" {
		v2.Spec.Display.Description = description
	}

	for i, tag := range tags {
		v2.Tags[i] = clients.DashboardTag{Key: tag, Value: tag}
	}

	panels := make(map[string]interface{})
	var layoutItems []interface{}

	for i, w := range widgets {
		panelID := w.ID
		if panelID == "" {
			panelID = fmt.Sprintf("panel-%d", i)
		}

		panel := map[string]interface{}{
			"kind": "Panel",
			"spec": map[string]interface{}{
				"display": map[string]interface{}{
					"name": w.Title,
				},
				"plugin": map[string]interface{}{
					"kind": "signoz/TimeSeriesPanel",
					"spec": map[string]interface{}{
						"visualization": map[string]interface{}{
							"timePreference": "global_time",
						},
					},
				},
				"queries": []interface{}{
					convertQueryToV2(w.Query),
				},
			},
		}

		if w.YAxisUnit != nil {
			panel["spec"].(map[string]interface{})["plugin"].(map[string]interface{})["spec"].(map[string]interface{})["formatting"] = map[string]interface{}{
				"unit": *w.YAxisUnit,
			}
		}

		panels[panelID] = panel

		layoutItems = append(layoutItems, map[string]interface{}{
			"x":      i % 2 * 6,
			"y":      (i / 2) * 6,
			"width":  6,
			"height": 6,
			"content": map[string]interface{}{
				"$ref": "#/spec/panels/" + panelID,
			},
		})
	}

	v2.Spec.Panels = panels
	v2.Spec.Layouts = []interface{}{
		map[string]interface{}{
			"kind": "Grid",
			"spec": map[string]interface{}{
				"items": layoutItems,
			},
		},
	}

	return v2
}

// convertVariablesToV2 converts the CRD's Variables map into the []interface{}
// array the SigNoz v6 API expects. Sorted by name for deterministic output -
// map iteration order is otherwise random, which would make every Create/
// Update payload differ from the last for no reason.
func convertVariablesToV2(variables map[string]v1beta1.Variable) []interface{} {
	if len(variables) == 0 {
		return []interface{}{}
	}

	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]interface{}, 0, len(names))
	for _, name := range names {
		result = append(result, convertVariableToV2(name, variables[name]))
	}
	return result
}

// convertVariableToV2 converts a single CRD Variable into a SigNoz v6
// dashboard variable object. The v6 API distinguishes "TextVariable" (a
// plain text/constant value) from "ListVariable" (a dropdown backed by
// either a query or a fixed custom value list, via a nested plugin.kind of
// signoz/QueryVariable or signoz/CustomVariable respectively) - this
// mapping and the exact field names (queryValue, customValue, value,
// allowMultiple, allowAllValue) were confirmed against the live SigNoz v2
// dashboards API, since the API rejects unknown fields outright.
func convertVariableToV2(name string, v v1beta1.Variable) map[string]interface{} {
	if v.Type == "textbox" {
		value := ""
		if v.TextboxValue != nil {
			value = *v.TextboxValue
		}
		return map[string]interface{}{
			"kind": "TextVariable",
			"spec": map[string]interface{}{
				"name":  name,
				"value": value,
			},
		}
	}

	pluginKind := "signoz/QueryVariable"
	pluginSpec := map[string]interface{}{}
	if v.Type == "custom" {
		pluginKind = "signoz/CustomVariable"
		customValue := ""
		if v.CustomValue != nil {
			customValue = *v.CustomValue
		}
		pluginSpec["customValue"] = customValue
	} else {
		queryValue := ""
		if v.QueryValue != nil {
			queryValue = *v.QueryValue
		}
		pluginSpec["queryValue"] = queryValue
	}

	spec := map[string]interface{}{
		"name": name,
		"plugin": map[string]interface{}{
			"kind": pluginKind,
			"spec": pluginSpec,
		},
		"allowMultiple": v.MultiSelect,
		"allowAllValue": v.ShowAllOption,
	}
	if v.Sort != nil {
		spec["sort"] = *v.Sort
	}

	return map[string]interface{}{
		"kind": "ListVariable",
		"spec": spec,
	}
}

func convertQueryToV2(query v1beta1.Query) map[string]interface{} {
	compQuery := map[string]interface{}{
		"kind": "time_series",
		"spec": map[string]interface{}{
			"plugin": map[string]interface{}{
				"kind": "signoz/CompositeQuery",
				"spec": map[string]interface{}{
					"queries": []interface{}{},
				},
			},
		},
	}

	if len(query.PromQL) > 0 {
		queries := make([]interface{}, len(query.PromQL))
		for i, pq := range query.PromQL {
			name := fmt.Sprintf("A%d", i)
			if pq.Name != nil {
				name = *pq.Name
			}
			legend := ""
			if pq.Legend != nil {
				legend = *pq.Legend
			}
			queries[i] = map[string]interface{}{
				"type": "promql",
				"spec": map[string]interface{}{
					"query":    pq.Query,
					"legend":   legend,
					"name":     name,
					"disabled": pq.Disabled,
				},
			}
		}
		compQuery["spec"].(map[string]interface{})["plugin"].(map[string]interface{})["spec"].(map[string]interface{})["queries"] = queries
	}

	if len(query.ClickHouse) > 0 {
		queries := make([]interface{}, len(query.ClickHouse))
		for i, chq := range query.ClickHouse {
			name := fmt.Sprintf("A%d", i)
			if chq.Name != nil {
				name = *chq.Name
			}
			legend := ""
			if chq.Legend != nil {
				legend = *chq.Legend
			}
			queries[i] = map[string]interface{}{
				"type": "clickhouse_sql",
				"spec": map[string]interface{}{
					"query":    chq.Query,
					"legend":   legend,
					"name":     name,
					"disabled": chq.Disabled,
				},
			}
		}
		compQuery["spec"].(map[string]interface{})["plugin"].(map[string]interface{})["spec"].(map[string]interface{})["queries"] = queries
	}

	return compQuery
}
