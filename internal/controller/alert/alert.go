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
	"fmt"
	"strconv"
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
	"github.com/rossigee/provider-signoz/apis/alert/v1beta1"
	channelv1beta1 "github.com/rossigee/provider-signoz/apis/channel/v1beta1"
	apisv1beta1 "github.com/rossigee/provider-signoz/apis/v1beta1"
	"github.com/rossigee/provider-signoz/internal/clients"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	opts := []managed.ReconcilerOption{
		managed.WithExternalConnector(&connector{
			kube:         resource.ClientApplicator{Client: mgr.GetClient(), Applicator: resource.NewAPIPatchingApplicator(mgr.GetClient())},
			usage:        resource.ModernTrackerFn(func(ctx context.Context, mg resource.ModernManaged) error { return nil }),
			newServiceFn: clients.NewClient,
		}),
		managed.WithReferenceResolver(managed.NewAPISimpleReferenceResolver(mgr.GetClient())),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(name))),
	}

	if o.Features != nil && o.Features.Enabled(feature.EnableBetaManagementPolicies) {
		opts = append(opts, managed.WithManagementPolicies())
	}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1beta1.Alert_GroupVersionKind),
		opts...,
	)

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
	usage        resource.ModernTracker
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
		// Generate deterministic UUID-v7 for new resources
		alertID = clients.GenerateExternalName(cr.GetNamespace(), cr.GetName())
		if cr.GetAnnotations() == nil {
			cr.SetAnnotations(make(map[string]string))
		}
		cr.GetAnnotations()["crossplane.io/external-name"] = alertID
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	// Validate external-name is a valid UUID - if not, generate a new one
	if _, err := uuid.Parse(alertID); err != nil {
		// Not a valid UUID, generate a new one
		alertID = clients.GenerateExternalName(cr.GetNamespace(), cr.GetName())
		if cr.GetAnnotations() == nil {
			cr.SetAnnotations(make(map[string]string))
		}
		cr.GetAnnotations()["crossplane.io/external-name"] = alertID
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	alert, err := c.service.GetRule(ctx, alertID)
	if err != nil {
		if clients.IsNotFound(err) {
			clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, nil, true)
			return managed.ExternalObservation{
				ResourceExists: false,
			}, nil
		}
		clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, err, false)
		return managed.ExternalObservation{}, errors.Wrap(err, errGetAlert)
	}
	clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, nil, true)

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

	// Set Ready condition since the resource exists
	cr.Status.SetConditions(xpv1.Available())

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

	ruleData := buildRuleData(cr)

	created, err := c.service.CreateRule(ctx, ruleData)
	if err != nil {
		clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, err, false)
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateAlert)
	}
	clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, nil, true)

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

	ruleData := buildRuleData(cr)

	_, err := c.service.UpdateRule(ctx, alertID, ruleData)
	if err != nil {
		clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, err, false)
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateAlert)
	}
	clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, nil, true)

	return managed.ExternalUpdate{}, nil
}

// buildRuleData builds the API payload shared by Create and Update.
//
// evaluation/schemaVersion/notificationSettings are only populated for
// alerts using a v5 multi-level threshold condition - confirmed live by
// isolating this exact variable against two different alert kinds:
// http-auth-failures (a real threshold_rule) requires the block and is
// rejected without it, while high-cpu-usage (a promql_rule, condition has
// no Thresholds) is rejected *with* it and succeeds without it. RuleType
// is left hardcoded to "threshold_rule" for every alert regardless of
// actual kind - also confirmed live: the API tolerates that mismatch as
// long as the evaluation block is absent, so it is out of scope for this
// fix (changing it would need to correctly discriminate promql_rule/
// threshold_rule/anomaly_rule from the condition shape, which only
// exercises the flat-condition and Thresholds cases seen so far).
func buildRuleData(cr *v1beta1.Alert) *clients.RuleData {
	ruleData := &clients.RuleData{
		AlertName:         cr.Spec.ForProvider.AlertName,
		AlertType:         convertAlertType(cr.Spec.ForProvider.AlertType),
		RuleType:          "threshold_rule",
		EvalWindow:        cr.Spec.ForProvider.EvalWindow,
		Frequency:         cr.Spec.ForProvider.Frequency,
		Condition:         convertCondition(cr.Spec.ForProvider.Condition),
		Labels:            cr.Spec.ForProvider.Labels,
		Annotations:       cr.Spec.ForProvider.Annotations,
		PreferredChannels: cr.Status.AtProvider.ResolvedChannelIDs,
		Disabled:          cr.Spec.ForProvider.Disabled,
		Severity:          cr.Spec.ForProvider.Severity,
		Version:           "v5",
	}

	if len(cr.Spec.ForProvider.Condition.Thresholds) > 0 {
		ruleData.Evaluation = &clients.RuleEvaluation{
			Kind: "rolling",
			Spec: clients.RuleEvaluationSpec{
				EvalWindow: cr.Spec.ForProvider.EvalWindow,
				Frequency:  cr.Spec.ForProvider.Frequency,
			},
		}
		ruleData.SchemaVersion = "v2alpha1"
		ruleData.NotificationSettings = &clients.RuleNotificationSettings{
			Renotify:  clients.RuleRenotify{Enabled: false, Interval: "30m"},
			UsePolicy: false,
		}
	}

	return ruleData
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
		clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, err, false)
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteAlert)
	}
	clients.RecordUpstreamCondition(ctx, &cr.Status.ConditionedStatus, nil, true)

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

	if convertAlertType(spec.AlertType) != alert.AlertType {
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

	// Compare the rendered condition against the live condition. This is
	// what catches a builder_query schema mismatch (e.g. provider vs SigNoz
	// v5) - if we always claimed "up to date" the user would never see
	// drift in the form of repeated PUT attempts.
	desiredCondition := convertCondition(spec.Condition)
	return conditionEqual(desiredCondition, alert.Condition)
}

// conditionEqual compares two condition maps while tolerating known
// fields that SigNoz may normalise (int vs string for op/matchType,
// nil vs absent for unit/legend, etc).
func conditionEqual(desired, observed map[string]interface{}) bool {
	if desired == nil && observed == nil {
		return true
	}
	if desired == nil || observed == nil {
		return false
	}

	for k, dv := range desired {
		ov, ok := observed[k]
		if !ok {
			// Tolerate empty/zero values that SigNoz may strip.
			if isZeroish(dv) {
				continue
			}
			return false
		}
		if !valueEqual(dv, ov) {
			return false
		}
	}

	for k, ov := range observed {
		if _, ok := desired[k]; ok {
			continue
		}
		if isZeroish(ov) {
			continue
		}
		return false
	}

	return true
}

func valueEqual(a, b interface{}) bool {
	if a == nil || b == nil {
		return a == b
	}
	// Numeric comparisons. The converter builds desired values as Go ints/
	// floats while the rules API returns JSON numbers that decode to
	// float64 (and occasionally stringified ints). Compare all numeric
	// kinds by reducing both sides to float64 so that, e.g., an int64
	// desired stepInterval matches a float64 observed one - this is the
	// "op-as-array"/"type-shape" drift class.
	if af, aok := toFloat64(a); aok {
		if bf, bok := toFloat64(b); bok {
			return af == bf
		}
		// b is a stringified number (e.g. matchType).
		if bs, ok := b.(string); ok {
			if bf, fok := toFloat64FromString(bs); fok {
				return af == bf
			}
		}
	}
	// Maps.
	if am, aok := a.(map[string]interface{}); aok {
		bm, bok := b.(map[string]interface{})
		if !bok {
			return false
		}
		return conditionEqual(am, bm)
	}
	// Slices.
	if as, aok := a.([]interface{}); aok {
		bs, bok := b.([]interface{})
		if !bok || len(as) != len(bs) {
			return false
		}
		for i := range as {
			if !valueEqual(as[i], bs[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// toFloat64 reduces a Go numeric value to float64, recognizing the kinds the
// converter may emit (float64/int/int32/int64/uint/uint64/float32). It returns
// (value, true) when the input is numeric, (0, false) otherwise. This lets
// valueEqual compare a desired numeric against the float64 that JSON decoding
// produces from the SigNoz GET response.
func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case string:
		return 0, false
	}
	return 0, false
}

// toFloat64FromString handles SigNoz's habit of stringifying small ints (e.g.
// matchType "1"). It parses a numeric string to float64 and reports whether the
// value was numeric.
func toFloat64FromString(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != '-' && r != '+' {
			return 0, false
		}
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, true
	}
	return 0, false
}

func isZeroish(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case int:
		return x == 0
	case int64:
		return x == 0
	case float64:
		return x == 0
	case uint64:
		return x == 0
	case []interface{}:
		return len(x) == 0
	case map[string]interface{}:
		return len(x) == 0
	}
	return false
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

// convertAlertType translates the CRD's alertType enum to the value
// SigNoz's rules API actually expects on the wire. SigNoz's naming is
// inconsistent across signal types - METRIC_BASED_ALERT is accepted as-is
// (singular "METRIC", confirmed by every metrics alert in the fleet
// syncing successfully), but a logs-signal alert with LOG_BASED_ALERT is
// rejected with 400 "alert rule is not valid"; only LOGS_BASED_ALERT
// (plural "LOGS") is accepted - confirmed live by isolating this single
// variable against http-auth-failures. Only this one confirmed mismatch is
// translated; TRACE_BASED_ALERT/ANOMALY_BASED_ALERT are passed through
// unchanged since there is no live rule to confirm either way, and
// guessing would risk breaking a currently-working alert type.
func convertAlertType(alertType string) string {
	if alertType == "LOG_BASED_ALERT" {
		return "LOGS_BASED_ALERT"
	}
	return alertType
}

func convertCondition(condition v1beta1.RuleCondition) map[string]interface{} {
	result := map[string]interface{}{
		"compositeQuery":    convertCompositeQuery(condition.CompositeQuery),
		"selectedQueryName": "A",
	}

	// Multi-level thresholds replace the flat op/target/matchType entirely
	// on the wire - a rule created with thresholds has no top-level
	// op/target/matchType keys at all (confirmed against a live v5 rule).
	if len(condition.Thresholds) > 0 {
		result["thresholds"] = convertThresholds(condition.Thresholds)
		return result
	}

	result["op"] = "1"
	result["target"] = 0
	result["matchType"] = "1"

	if condition.CompareOp != "" {
		result["op"] = condition.CompareOp
	}
	if condition.Target != nil {
		result["target"] = *condition.Target
	}
	if condition.MatchType != nil {
		result["matchType"] = fmt.Sprintf("%d", *condition.MatchType)
	}

	return result
}

// convertThresholds converts the CRD's Thresholds slice into SigNoz's v5
// condition.thresholds block (kind "basic", spec as an array of per-level
// objects), matching the shape confirmed against a live rule fetched via
// GET /api/v1/rules/{id}.
func convertThresholds(thresholds []v1beta1.Threshold) map[string]interface{} {
	specs := make([]interface{}, len(thresholds))
	for i, t := range thresholds {
		spec := map[string]interface{}{
			"name":           t.Name,
			"target":         t.Target,
			"targetUnit":     t.TargetUnit,
			"recoveryTarget": nil,
			"matchType":      t.MatchType,
			"op":             t.Op,
			"channels":       []interface{}{},
		}
		if t.RecoveryTarget != nil {
			spec["recoveryTarget"] = *t.RecoveryTarget
		}
		if len(t.Channels) > 0 {
			channels := make([]interface{}, len(t.Channels))
			for j, ch := range t.Channels {
				channels[j] = ch
			}
			spec["channels"] = channels
		}
		specs[i] = spec
	}

	return map[string]interface{}{
		"kind": "basic",
		"spec": specs,
	}
}

func convertCompositeQuery(query v1beta1.CompositeQuery) map[string]interface{} {
	// Normalise QueryType. The user may supply either the numeric legacy
	// Normalize various input formats to the canonical form (PromQL/ClickHouse/Builder).
	// Support legacy numeric ("1"/"2"/"3") and lowercase formats for backwards compatibility.
	queryType := query.QueryType
	if queryType != "" {
		lower := strings.ToLower(queryType)
		switch lower {
		case "1", "promql":
			queryType = "PromQL"
		case "2", "clickhouse_sql":
			queryType = "ClickHouse"
		case "3", "builder":
			queryType = "Builder"
		}
	}
	if queryType == "" {
		if len(query.PromQL) > 0 {
			queryType = "PromQL"
		} else if len(query.ClickHouse) > 0 {
			queryType = "ClickHouse"
		} else if query.Builder != nil {
			queryType = "Builder"
		} else {
			queryType = "Builder"
		}
	}

	panelType := query.PanelType
	if panelType == "" {
		panelType = "graph"
	}

	result := map[string]interface{}{
		"queryType": queryType,
		"panelType": panelType,
		"unit":      query.Unit,
	}

	// The rules API (POST/PUT /api/v1/rules) expects the v5 QueryEnvelope
	// schema: compositeQuery.queries is an array of {type, spec} envelopes,
	// NOT the legacy query-range v3 builderQueries/promQueries maps.
	// SigNoz v0.137.1's ruletypes.AlertCompositeQuery.Queries is
	// []qbtypes.QueryEnvelope (see pkg/types/ruletypes/alerting.go).
	queries := make([]interface{}, 0, len(query.PromQL)+len(query.ClickHouse)+1)

	if len(query.PromQL) > 0 {
		for i, pq := range query.PromQL {
			name := pq.Name
			if name == "" {
				name = fmt.Sprintf("A%d", i)
			}
			queries = append(queries, map[string]interface{}{
				"type": "promql",
				"spec": map[string]interface{}{
					"name":     name,
					"query":    pq.Query,
					"legend":   pq.Legend,
					"disabled": pq.Disabled,
					"stats":    nil,
				},
			})
		}
	}

	if len(query.ClickHouse) > 0 {
		for i, chq := range query.ClickHouse {
			name := chq.Name
			if name == "" {
				name = fmt.Sprintf("A%d", i)
			}
			queries = append(queries, map[string]interface{}{
				"type": "clickhouse_sql",
				"spec": map[string]interface{}{
					"name":     name,
					"query":    chq.Query,
					"legend":   chq.Legend,
					"disabled": chq.Disabled,
				},
			})
		}
	}

	if query.Builder != nil {
		queries = append(queries, map[string]interface{}{
			"type": "builder_query",
			"spec": convertQueryBuilder(*query.Builder),
		})
	}

	if len(queries) > 0 {
		result["queries"] = queries
	}

	if query.Expression != "" {
		result["expression"] = query.Expression
	}

	return result
}

func convertQueryBuilder(builder v1beta1.QueryBuilder) map[string]interface{} {
	name := builder.QueryName
	if name == "" {
		name = "A"
	}

	stepInterval := int64(60)
	if builder.StepInterval != nil {
		stepInterval = *builder.StepInterval
	}

	signal := builder.DataSource
	if signal == "" {
		signal = "metrics"
	}

	// "source" is required on the wire for metrics ("meter"); logs/traces
	// queries carry no source (confirmed against a live logs-signal rule
	// fetched via GET /api/v1/rules/{id}, which has source: "").
	source := ""
	if signal == "metrics" {
		source = "meter"
	}

	// The rules API v5 envelope expects compositeQuery.queries[].spec to be
	// a QueryBuilderQuery with a "signal" discriminator and an
	// "aggregations" array. The aggregation shape itself differs by signal:
	// metrics use metricName/temporality/timeAggregation/spaceAggregation;
	// logs/traces use a single "expression" string (e.g. "count()") instead
	// - confirmed against a live logs-signal rule, which has
	// aggregations: [{"expression": "count()"}] with none of the metric
	// fields present at all.
	//
	// stepInterval is emitted as float64 rather than int64 because the rules
	// API returns JSON numbers (which unmarshal to float64) and the
	// conditionEqual drift comparator compares the desired map against the
	// float64 values decoded from the GET response. Emitting int64 here
	// caused a perpetual int64-vs-float64 mismatch (the "op-as-array"-class
	// type-shape drift) and a PUT on every reconcile. float64 is the
	// canonical JSON number type and matches the observed shape exactly.
	spec := map[string]interface{}{
		"name":         name,
		"stepInterval": float64(stepInterval),
		"signal":       signal,
		"source":       source,
		"disabled":     builder.Disabled,
		"legend":       builder.Legend,
	}

	if signal == "metrics" {
		aggregation := map[string]interface{}{
			"metricName":       "",
			"temporality":      "",
			"timeAggregation":  "",
			"spaceAggregation": "",
		}

		if builder.AggregateAttribute != nil {
			aggregation["metricName"] = builder.AggregateAttribute.Key
		}
		if builder.Temporality != "" {
			aggregation["temporality"] = builder.Temporality
		}
		if builder.TimeAggregation != "" {
			aggregation["timeAggregation"] = builder.TimeAggregation
		} else if builder.AggregateOperator != "" {
			// Legacy single-operator form (v3): map to the v5 time/space split.
			aggregation["timeAggregation"] = builder.AggregateOperator
		}
		if builder.SpaceAggregation != "" {
			aggregation["spaceAggregation"] = builder.SpaceAggregation
		} else if builder.AggregateOperator != "" {
			aggregation["spaceAggregation"] = "sum"
		}
		if builder.ReduceTo != "" {
			aggregation["reduceTo"] = builder.ReduceTo
		}

		spec["aggregations"] = []interface{}{aggregation}
	} else {
		expr := builder.AggregationExpression
		if expr == "" {
			expr = "count()"
		}
		spec["aggregations"] = []interface{}{
			map[string]interface{}{"expression": expr},
		}
	}

	if builder.FilterExpression != "" {
		spec["filter"] = map[string]interface{}{
			"expression": builder.FilterExpression,
		}
	} else if builder.Filters != nil {
		spec["filter"] = convertFilterSet(*builder.Filters)
	}
	if len(builder.GroupBy) > 0 {
		groupBy := make([]interface{}, len(builder.GroupBy))
		for i, gb := range builder.GroupBy {
			groupBy[i] = map[string]interface{}{
				"name":          gb.Key,
				"fieldDataType": gb.DataType,
				"fieldContext":  gb.Type,
			}
		}
		spec["groupBy"] = groupBy
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
		spec["having"] = having
	}
	if len(builder.OrderBy) > 0 {
		orderBy := make([]interface{}, len(builder.OrderBy))
		for i, ob := range builder.OrderBy {
			orderBy[i] = map[string]interface{}{
				"key": map[string]interface{}{
					"name": ob.ColumnName,
				},
				"direction": ob.Order,
			}
		}
		spec["order"] = orderBy
	}
	if len(builder.SelectColumns) > 0 {
		selectFields := make([]interface{}, len(builder.SelectColumns))
		for i, sc := range builder.SelectColumns {
			selectFields[i] = map[string]interface{}{
				"name": sc.Key,
			}
		}
		spec["selectFields"] = selectFields
	}
	if builder.Limit != nil {
		spec["limit"] = uint64(*builder.Limit)
	}
	if builder.Offset != nil {
		spec["offset"] = uint64(*builder.Offset)
	}

	return spec
}

func convertFilterSet(filterSet v1beta1.FilterSet) map[string]interface{} {
	// The v5 rules API expects filter as {expression: string}. Build the
	// expression from the structured items following SigNoz's filter
	// syntax ("key op value", joined by AND/OR). Empty items yield an
	// empty expression, which SigNoz treats as "no filter".
	parts := make([]string, 0, len(filterSet.Items))
	for _, item := range filterSet.Items {
		if item.Key.Key == "" {
			continue
		}
		if item.Value == nil || *item.Value == "" {
			parts = append(parts, fmt.Sprintf("%s %s", item.Key.Key, item.Op))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s '%s'", item.Key.Key, item.Op, *item.Value))
	}

	expression := strings.Join(parts, " "+filterSet.Operator+" ")
	return map[string]interface{}{
		"expression": expression,
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
			if err := c.kube.Get(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: ref.Name}, channel); err != nil {
				return errors.Wrapf(err, "cannot get notification channel %s", ref.Name)
			}

			// SigNoz rules API validates preferredChannels against the channel's
			// display name, not its UUID.
			if name := channel.Spec.ForProvider.Name; name != "" {
				channelIDs = append(channelIDs, name)
			} else if channelID := channel.GetAnnotations()["crossplane.io/external-name"]; channelID != "" {
				channelIDs = append(channelIDs, channelID)
			}
		}
	}

	// Resolve selector-based references
	if cr.Spec.ForProvider.ChannelIDsSelector != nil {
		selector := cr.Spec.ForProvider.ChannelIDsSelector
		channelList := &channelv1beta1.NotificationChannelList{}

		listOptions := []client.ListOption{
			client.InNamespace(cr.GetNamespace()),
		}
		if selector.MatchLabels != nil {
			listOptions = append(listOptions, client.MatchingLabels(selector.MatchLabels))
		}

		if err := c.kube.List(ctx, channelList, listOptions...); err != nil {
			return errors.Wrap(err, "cannot list notification channels")
		}

		for _, channel := range channelList.Items {
			if name := channel.Spec.ForProvider.Name; name != "" {
				channelIDs = append(channelIDs, name)
			} else if channelID := channel.GetAnnotations()["crossplane.io/external-name"]; channelID != "" {
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
