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
	"context"
	"time"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"

	"github.com/rossigee/provider-signoz/apis/alert/v1beta1"
	channelv1beta1 "github.com/rossigee/provider-signoz/apis/channel/v1beta1"
	apisv1beta1 "github.com/rossigee/provider-signoz/apis/v1beta1"
	"github.com/rossigee/provider-signoz/internal/clients"
)

const (
	errNotAlert     = "managed resource is not an Alert custom resource"
	errTrackPCUsage = "cannot track ProviderConfig usage"
	errGetPC        = "cannot get ProviderConfig"
	errGetCreds     = "cannot get credentials"
	errNewClient    = "cannot create new Service"
	errCreateAlert  = "cannot create alert"
	errUpdateAlert  = "cannot update alert"
	errDeleteAlert  = "cannot delete alert"
	errGetAlert     = "cannot get alert"
	errResolveRefs  = "cannot resolve channel references"
)

// Setup adds a controller that reconciles Alert managed resources.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(v1beta1.Alert_GroupVersionKind.Kind)

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1beta1.Alert_GroupVersionKind),
		managed.WithExternalConnecter(&connector{
			kube:         resource.ClientApplicator{Client: mgr.GetClient(), Applicator: resource.NewAPIPatchingApplicator(mgr.GetClient())},
			usage:        resource.TrackerFn(func(ctx context.Context, mg resource.Managed) error { return nil }),
			newServiceFn: clients.NewClient,
		}),
		managed.WithReferenceResolver(managed.NewAPISimpleReferenceResolver(mgr.GetClient())),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		WithEventFilter(resource.DesiredStateChanged()).
		For(&v1beta1.Alert{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

// A connector is expected to produce an ExternalClient when its Connect method
// is called.
type connector struct {
	kube         resource.ClientApplicator
	usage        resource.Tracker
	newServiceFn func(cfg clients.Config) *clients.Client
}

// Connect typically produces an ExternalClient by:
// 1. Tracking that the managed resource is using a ProviderConfig.
// 2. Getting the managed resource's ProviderConfig.
// 3. Getting the credentials specified by the ProviderConfig.
// 4. Using the credentials to form a client.
func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1beta1.Alert)
	if !ok {
		return nil, errors.New(errNotAlert)
	}

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	if cr.GetProviderConfigReference() == nil {
		return nil, errors.New("no providerConfigRef provided")
	}

	pc := &apisv1beta1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	cfg, err := clients.GetConfig(ctx, c.kube, mg)
	if err != nil {
		return nil, errors.Wrap(err, errGetCreds)
	}

	return &external{
		service: c.newServiceFn(*cfg),
		kube:    c.kube.Client,
	}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	service *clients.Client
	kube    client.Client
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1beta1.Alert)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotAlert)
	}

	// Get the alert ID from the external-name annotation
	alertID := cr.GetAnnotations()["crossplane.io/external-name"]
	if alertID == "" {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	alert, err := c.service.GetRule(ctx, alertID)
	if err != nil {
		if clients.IsNotFound(err) {
			return managed.ExternalObservation{
				ResourceExists: false,
			}, nil
		}
		return managed.ExternalObservation{}, errors.Wrap(err, errGetAlert)
	}

	// Update the status with observed values
	cr.Status.AtProvider.ID = alert.ID
	cr.Status.AtProvider.State = alert.State

	if alert.CreatedAt != "" {
		if createdAt, err := time.Parse(time.RFC3339, alert.CreatedAt); err == nil {
			cr.Status.AtProvider.CreatedAt = &metav1.Time{Time: createdAt}
		}
	}

	if alert.UpdatedAt != "" {
		if updatedAt, err := time.Parse(time.RFC3339, alert.UpdatedAt); err == nil {
			cr.Status.AtProvider.UpdatedAt = &metav1.Time{Time: updatedAt}
		}
	}

	// Resolve channel references and update status
	if err := c.resolveChannelReferences(ctx, cr); err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errResolveRefs)
	}

	// Check if the alert is up to date
	upToDate := isAlertUpToDate(cr.Spec.ForProvider, alert)

	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: upToDate,
	}, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1beta1.Alert)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotAlert)
	}

	// Resolve channel references
	if err := c.resolveChannelReferences(ctx, cr); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errResolveRefs)
	}

	ruleData := &clients.RuleData{
		AlertName:         cr.Spec.ForProvider.AlertName,
		AlertType:         cr.Spec.ForProvider.AlertType,
		EvalWindow:        cr.Spec.ForProvider.EvalWindow,
		Frequency:         cr.Spec.ForProvider.Frequency,
		Condition:         convertCondition(cr.Spec.ForProvider.Condition),
		Labels:            cr.Spec.ForProvider.Labels,
		Annotations:       cr.Spec.ForProvider.Annotations,
		PreferredChannels: cr.Status.AtProvider.ResolvedChannelIDs,
		Disabled:          cr.Spec.ForProvider.Disabled,
	}

	created, err := c.service.CreateRule(ctx, ruleData)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateAlert)
	}

	// Set the external-name annotation to the alert ID
	if cr.GetAnnotations() == nil {
		cr.SetAnnotations(make(map[string]string))
	}
	cr.GetAnnotations()["crossplane.io/external-name"] = created.ID

	return managed.ExternalCreation{}, nil
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1beta1.Alert)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotAlert)
	}

	alertID := cr.GetAnnotations()["crossplane.io/external-name"]
	if alertID == "" {
		return managed.ExternalUpdate{}, errors.New("alert ID not found")
	}

	// Resolve channel references
	if err := c.resolveChannelReferences(ctx, cr); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errResolveRefs)
	}

	ruleData := &clients.RuleData{
		AlertName:         cr.Spec.ForProvider.AlertName,
		AlertType:         cr.Spec.ForProvider.AlertType,
		EvalWindow:        cr.Spec.ForProvider.EvalWindow,
		Frequency:         cr.Spec.ForProvider.Frequency,
		Condition:         convertCondition(cr.Spec.ForProvider.Condition),
		Labels:            cr.Spec.ForProvider.Labels,
		Annotations:       cr.Spec.ForProvider.Annotations,
		PreferredChannels: cr.Status.AtProvider.ResolvedChannelIDs,
		Disabled:          cr.Spec.ForProvider.Disabled,
	}

	_, err := c.service.UpdateRule(ctx, alertID, ruleData)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateAlert)
	}

	return managed.ExternalUpdate{}, nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr, ok := mg.(*v1beta1.Alert)
	if !ok {
		return managed.ExternalDelete{}, errors.New(errNotAlert)
	}

	alertID := cr.GetAnnotations()["crossplane.io/external-name"]
	if alertID == "" {
		return managed.ExternalDelete{}, nil // Nothing to delete
	}

	err := c.service.DeleteRule(ctx, alertID)
	if err != nil && !clients.IsNotFound(err) {
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteAlert)
	}

	return managed.ExternalDelete{}, nil
}

func (c *external) Disconnect(ctx context.Context) error {
	// Nothing to disconnect for SigNoz API client
	return nil
}

// Helper functions

func isAlertUpToDate(spec v1beta1.AlertParameters, alert *clients.RuleData) bool {
	if spec.AlertName != alert.AlertName {
		return false
	}

	if spec.AlertType != alert.AlertType {
		return false
	}

	if spec.EvalWindow != alert.EvalWindow {
		return false
	}

	if spec.Frequency != alert.Frequency {
		return false
	}

	if spec.Disabled != alert.Disabled {
		return false
	}

	// Compare labels
	if !mapsEqual(spec.Labels, alert.Labels) {
		return false
	}

	// Compare annotations
	if !mapsEqual(spec.Annotations, alert.Annotations) {
		return false
	}

	// For simplicity, we'll consider the alert up to date if basic fields match
	// In a more sophisticated implementation, we would deeply compare the condition
	return true
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func convertCondition(condition v1beta1.RuleCondition) map[string]interface{} {
	result := map[string]interface{}{
		"compositeQuery": convertCompositeQuery(condition.CompositeQuery),
	}

	if condition.CompareOp != "" {
		result["compareOp"] = condition.CompareOp
	}
	if condition.Target != nil {
		result["target"] = *condition.Target
	}
	if condition.MatchType != nil {
		result["matchType"] = *condition.MatchType
	}

	return result
}

func convertCompositeQuery(query v1beta1.CompositeQuery) map[string]interface{} {
	result := map[string]interface{}{
		"queryType": query.QueryType,
	}

	if len(query.PromQL) > 0 {
		promQueries := make([]interface{}, len(query.PromQL))
		for i, pq := range query.PromQL {
			promQuery := map[string]interface{}{
				"query":    pq.Query,
				"name":     pq.Name,
				"legend":   pq.Legend,
				"disabled": pq.Disabled,
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
				"name":     chq.Name,
				"legend":   chq.Legend,
				"disabled": chq.Disabled,
			}
			chQueries[i] = chQuery
		}
		result["clickHouse"] = chQueries
	}

	if query.Builder != nil {
		result["builder"] = convertQueryBuilder(*query.Builder)
	}

	if query.Expression != "" {
		result["expression"] = query.Expression
	}

	return result
}

func convertQueryBuilder(builder v1beta1.QueryBuilder) map[string]interface{} {
	result := map[string]interface{}{
		"dataSource": builder.DataSource,
	}

	if builder.AggregateOperator != "" {
		result["aggregateOperator"] = builder.AggregateOperator
	}
	if builder.AggregateAttribute != nil {
		result["aggregateAttribute"] = convertKeyAttribute(*builder.AggregateAttribute)
	}
	if builder.Filters != nil {
		result["filters"] = convertFilterSet(*builder.Filters)
	}
	if len(builder.GroupBy) > 0 {
		groupBy := make([]interface{}, len(builder.GroupBy))
		for i, gb := range builder.GroupBy {
			groupBy[i] = convertKeyAttribute(gb)
		}
		result["groupBy"] = groupBy
	}
	if len(builder.Having) > 0 {
		having := make([]interface{}, len(builder.Having))
		for i, h := range builder.Having {
			having[i] = map[string]interface{}{
				"columnName": h.ColumnName,
				"op":         h.Op,
				"value":      h.Value,
			}
		}
		result["having"] = having
	}
	if len(builder.OrderBy) > 0 {
		orderBy := make([]interface{}, len(builder.OrderBy))
		for i, ob := range builder.OrderBy {
			orderBy[i] = map[string]interface{}{
				"columnName": ob.ColumnName,
				"order":      ob.Order,
			}
		}
		result["orderBy"] = orderBy
	}
	if builder.Limit != nil {
		result["limit"] = *builder.Limit
	}
	if builder.Offset != nil {
		result["offset"] = *builder.Offset
	}

	return result
}

func convertKeyAttribute(attr v1beta1.KeyAttribute) map[string]interface{} {
	result := map[string]interface{}{
		"key":  attr.Key,
		"type": attr.Type,
	}
	if attr.DataType != "" {
		result["dataType"] = attr.DataType
	}
	return result
}

func convertFilterSet(filterSet v1beta1.FilterSet) map[string]interface{} {
	items := make([]interface{}, len(filterSet.Items))
	for i, item := range filterSet.Items {
		items[i] = map[string]interface{}{
			"key":   convertKeyAttribute(item.Key),
			"op":    item.Op,
			"value": item.Value,
		}
	}

	return map[string]interface{}{
		"operator": filterSet.Operator,
		"items":    items,
	}
}

func (c *external) resolveChannelReferences(ctx context.Context, cr *v1beta1.Alert) error {
	var channelIDs []string

	// Add preferred channels directly
	channelIDs = append(channelIDs, cr.Spec.ForProvider.PreferredChannels...)

	// Resolve explicit references
	for _, ref := range cr.Spec.ForProvider.ChannelIDsRef {
		if ref.Name != "" {
			// Get the NotificationChannel resource
			channel := &channelv1beta1.NotificationChannel{}
			if err := c.kube.Get(ctx, types.NamespacedName{Name: ref.Name}, channel); err != nil {
				return errors.Wrapf(err, "cannot get notification channel %s", ref.Name)
			}

			// Get the channel ID from the external-name annotation
			if channelID := channel.GetAnnotations()["crossplane.io/external-name"]; channelID != "" {
				channelIDs = append(channelIDs, channelID)
			}
		}
	}

	// Resolve selector-based references
	if cr.Spec.ForProvider.ChannelIDsSelector != nil {
		selector := cr.Spec.ForProvider.ChannelIDsSelector
		channelList := &channelv1beta1.NotificationChannelList{}

		listOptions := []client.ListOption{}
		if selector.MatchLabels != nil {
			listOptions = append(listOptions, client.MatchingLabels(selector.MatchLabels))
		}

		if err := c.kube.List(ctx, channelList, listOptions...); err != nil {
			return errors.Wrap(err, "cannot list notification channels")
		}

		for _, channel := range channelList.Items {
			if channelID := channel.GetAnnotations()["crossplane.io/external-name"]; channelID != "" {
				channelIDs = append(channelIDs, channelID)
			}
		}
	}

	// Remove duplicates and update status
	uniqueChannelIDs := removeDuplicates(channelIDs)
	cr.Status.AtProvider.ResolvedChannelIDs = uniqueChannelIDs

	return nil
}

func removeDuplicates(slice []string) []string {
	keys := make(map[string]bool)
	result := []string{}
	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}
	return result
}
