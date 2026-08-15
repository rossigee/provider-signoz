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
	"strings"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/pkg/errors"
	"github.com/rossigee/provider-signoz/apis/dashboard/v1beta1"
	apisv1beta1 "github.com/rossigee/provider-signoz/apis/v1beta1"
	"github.com/rossigee/provider-signoz/internal/clients"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"github.com/google/uuid"
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

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1beta1.Dashboard_GroupVersionKind),
		managed.WithExternalConnector(&connector{
			kube:         resource.ClientApplicator{Client: mgr.GetClient(), Applicator: resource.NewAPIPatchingApplicator(mgr.GetClient())},
			usage:        resource.ModernTrackerFn(func(ctx context.Context, mg resource.ModernManaged) error { return nil }),
			newServiceFn: clients.NewClient,
		}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(name))),
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

	// For simplicity, we'll consider the dashboard up to date if basic fields match
	return true
}

func convertLayout(layout []v1beta1.Layout) []interface{} {
	result := make([]interface{}, len(layout))
	for i, l := range layout {
		result[i] = map[string]interface{}{
			"i":      l.I,
			"x":      l.X,
			"y":      l.Y,
			"w":      l.W,
			"h":      l.H,
			"moved":  l.Moved,
			"static": l.Static,
		}
	}
	return result
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

func convertVariables(variables map[string]v1beta1.Variable) map[string]interface{} {
	if variables == nil {
		return nil
	}

	result := make(map[string]interface{})
	for k, v := range variables {
		variable := map[string]interface{}{
			"type":          v.Type,
			"multiSelect":   v.MultiSelect,
			"showAllOption": v.ShowAllOption,
		}

		if v.Description != nil {
			variable["description"] = *v.Description
		}
		if v.QueryValue != nil {
			variable["queryValue"] = *v.QueryValue
		}
		if v.CustomValue != nil {
			variable["customValue"] = *v.CustomValue
		}
		if v.TextboxValue != nil {
			variable["textboxValue"] = *v.TextboxValue
		}
		if v.SelectedValue != nil {
			variable["selectedValue"] = *v.SelectedValue
		}
		if v.Sort != nil {
			variable["sort"] = *v.Sort
		}

		result[k] = variable
	}
	return result
}

func convertToV2(title, description string, tags []string, widgets []v1beta1.Widget, layout []v1beta1.Layout) *clients.DashboardV2Data {
	v2name := strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	v2 := &clients.DashboardV2Data{
		Name:          v2name,
		SchemaVersion: "v6",
		Tags: make([]clients.DashboardTag, len(tags)),
		Spec: clients.DashboardV2Spec{
			Display: &clients.DashboardV2Display{
				Name:        title,
				Description: description,
			},
			Panels:     make(map[string]interface{}),
			Variables:  []interface{}{},
			Layouts:    []interface{}{},
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
			queries[i] = map[string]interface{}{
				"type": "promql",
				"spec": map[string]interface{}{
					"query":    pq.Query,
					"legend":   "",
					"name":     name,
					"disabled": false,
				},
			}
		}
		compQuery["spec"].(map[string]interface{})["plugin"].(map[string]interface{})["spec"].(map[string]interface{})["queries"] = queries
	}

	return compQuery
}
