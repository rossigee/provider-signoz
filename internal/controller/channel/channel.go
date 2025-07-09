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

package channel

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"

	"github.com/crossplane-contrib/provider-signoz/apis/channel/v1alpha1"
	apisv1beta1 "github.com/crossplane-contrib/provider-signoz/apis/v1beta1"
	"github.com/crossplane-contrib/provider-signoz/internal/clients"
)

const (
	errNotChannel       = "managed resource is not a NotificationChannel custom resource"
	errTrackPCUsage     = "cannot track ProviderConfig usage"
	errGetPC            = "cannot get ProviderConfig"
	errGetCreds         = "cannot get credentials"
	errNewClient        = "cannot create new Service"
	errCreateChannel    = "cannot create notification channel"
	errUpdateChannel    = "cannot update notification channel"
	errDeleteChannel    = "cannot delete notification channel"
	errGetChannel       = "cannot get notification channel"
	errGetSecret        = "cannot get secret"
	errInvalidChannelID = "invalid channel ID"
)

// Setup adds a controller that reconciles NotificationChannel managed resources.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(v1alpha1.NotificationChannel_GroupVersionKind.Kind)

	cps := []managed.ConnectionPublisher{managed.NewAPISecretPublisher(mgr.GetClient(), mgr.GetScheme())}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.NotificationChannel_GroupVersionKind),
		managed.WithExternalConnecter(&connector{
			kube:         resource.ClientApplicator{Client: mgr.GetClient(), Applicator: resource.NewAPIPatchingApplicator(mgr.GetClient())},
			usage:        resource.NewProviderConfigUsageTracker(mgr.GetClient(), &apisv1beta1.ProviderConfigUsage{}),
			newServiceFn: clients.NewClient,
		}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithConnectionPublishers(cps...))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		WithEventFilter(resource.DesiredStateChanged()).
		For(&v1alpha1.NotificationChannel{}).
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
	cr, ok := mg.(*v1alpha1.NotificationChannel)
	if !ok {
		return nil, errors.New(errNotChannel)
	}

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
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
		kube:    c.kube,
	}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	service *clients.Client
	kube    client.Client
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha1.NotificationChannel)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotChannel)
	}

	// Get the channel ID from the external-name annotation
	channelIDStr := cr.GetAnnotations()["crossplane.io/external-name"]
	if channelIDStr == "" {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	channel, err := c.service.GetChannel(ctx, channelIDStr)
	if err != nil {
		if clients.IsNotFound(err) {
			return managed.ExternalObservation{
				ResourceExists: false,
			}, nil
		}
		return managed.ExternalObservation{}, errors.Wrap(err, errGetChannel)
	}

	// Update the status with observed values
	cr.Status.AtProvider.ID = channel.ID
	
	if channel.CreatedAt != "" {
		if createdAt, err := time.Parse(time.RFC3339, channel.CreatedAt); err == nil {
			cr.Status.AtProvider.CreatedAt = &metav1.Time{Time: createdAt}
		}
	}
	
	if channel.UpdatedAt != "" {
		if updatedAt, err := time.Parse(time.RFC3339, channel.UpdatedAt); err == nil {
			cr.Status.AtProvider.UpdatedAt = &metav1.Time{Time: updatedAt}
		}
	}

	// Check if the channel is up to date
	upToDate, err := c.isChannelUpToDate(ctx, cr.Spec.ForProvider, channel)
	if err != nil {
		return managed.ExternalObservation{}, err
	}

	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: upToDate,
	}, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha1.NotificationChannel)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotChannel)
	}

	channelData, err := c.convertToChannelData(ctx, cr.Spec.ForProvider)
	if err != nil {
		return managed.ExternalCreation{}, err
	}

	created, err := c.service.CreateChannel(ctx, channelData)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateChannel)
	}

	// Set the external-name annotation to the channel ID
	if cr.GetAnnotations() == nil {
		cr.SetAnnotations(make(map[string]string))
	}
	cr.GetAnnotations()["crossplane.io/external-name"] = strconv.Itoa(created.ID)

	return managed.ExternalCreation{}, nil
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1alpha1.NotificationChannel)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotChannel)
	}

	channelIDStr := cr.GetAnnotations()["crossplane.io/external-name"]
	if channelIDStr == "" {
		return managed.ExternalUpdate{}, errors.New("channel ID not found")
	}

	channelData, err := c.convertToChannelData(ctx, cr.Spec.ForProvider)
	if err != nil {
		return managed.ExternalUpdate{}, err
	}

	_, err = c.service.UpdateChannel(ctx, channelIDStr, channelData)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateChannel)
	}

	return managed.ExternalUpdate{}, nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1alpha1.NotificationChannel)
	if !ok {
		return errors.New(errNotChannel)
	}

	channelIDStr := cr.GetAnnotations()["crossplane.io/external-name"]
	if channelIDStr == "" {
		return nil // Nothing to delete
	}

	err := c.service.DeleteChannel(ctx, channelIDStr)
	if err != nil && !clients.IsNotFound(err) {
		return errors.Wrap(err, errDeleteChannel)
	}

	return nil
}

// Helper functions

func (c *external) isChannelUpToDate(ctx context.Context, spec v1alpha1.NotificationChannelParameters, channel *clients.ChannelData) (bool, error) {
	if spec.Name != channel.Name {
		return false, nil
	}

	if spec.Type != channel.Type {
		return false, nil
	}

	// For simplicity, we'll consider the channel up to date if basic fields match
	// In a more sophisticated implementation, we would deeply compare the channel data
	return true, nil
}

func (c *external) convertToChannelData(ctx context.Context, spec v1alpha1.NotificationChannelParameters) (*clients.ChannelData, error) {
	channelData := &clients.ChannelData{
		Name: spec.Name,
		Type: spec.Type,
		Data: make(map[string]interface{}),
	}

	switch spec.Type {
	case "slack":
		if len(spec.SlackConfigs) > 0 {
			slackData, err := c.convertSlackConfig(ctx, spec.SlackConfigs[0])
			if err != nil {
				return nil, err
			}
			channelData.Data = slackData
		}
	case "webhook":
		if len(spec.WebhookConfigs) > 0 {
			webhookData, err := c.convertWebhookConfig(ctx, spec.WebhookConfigs[0])
			if err != nil {
				return nil, err
			}
			channelData.Data = webhookData
		}
	case "pagerduty":
		if len(spec.PagerDutyConfigs) > 0 {
			pagerDutyData, err := c.convertPagerDutyConfig(ctx, spec.PagerDutyConfigs[0])
			if err != nil {
				return nil, err
			}
			channelData.Data = pagerDutyData
		}
	case "email":
		if len(spec.EmailConfigs) > 0 {
			emailData := c.convertEmailConfig(spec.EmailConfigs[0])
			channelData.Data = emailData
		}
	case "opsgenie":
		if len(spec.OpsGenieConfigs) > 0 {
			opsGenieData, err := c.convertOpsGenieConfig(ctx, spec.OpsGenieConfigs[0])
			if err != nil {
				return nil, err
			}
			channelData.Data = opsGenieData
		}
	case "msteams":
		if len(spec.MSTeamsConfigs) > 0 {
			msTeamsData, err := c.convertMSTeamsConfig(ctx, spec.MSTeamsConfigs[0])
			if err != nil {
				return nil, err
			}
			channelData.Data = msTeamsData
		}
	case "sns":
		if len(spec.SNSConfigs) > 0 {
			snsData, err := c.convertSNSConfig(ctx, spec.SNSConfigs[0])
			if err != nil {
				return nil, err
			}
			channelData.Data = snsData
		}
	default:
		return nil, fmt.Errorf("unsupported channel type: %s", spec.Type)
	}

	return channelData, nil
}

func (c *external) convertSlackConfig(ctx context.Context, config v1alpha1.SlackConfig) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"channel": config.Channel,
	}

	if config.Title != nil {
		data["title"] = *config.Title
	}
	if config.SendResolved != nil {
		data["send_resolved"] = *config.SendResolved
	}

	// Get webhook URL from secret or direct value
	webhookURL := ""
	if config.WebhookURLSecretRef != nil {
		secret, err := c.getSecretValue(ctx, config.WebhookURLSecretRef)
		if err != nil {
			return nil, errors.Wrap(err, errGetSecret)
		}
		webhookURL = secret
	} else if config.WebhookURL != nil {
		webhookURL = *config.WebhookURL
	}

	if webhookURL != "" {
		data["webhook_url"] = webhookURL
	}

	return data, nil
}

func (c *external) convertWebhookConfig(ctx context.Context, config v1alpha1.WebhookConfig) (map[string]interface{}, error) {
	data := map[string]interface{}{}

	if config.Method != nil {
		data["http_method"] = *config.Method
	}
	if config.MaxAlerts != nil {
		data["max_alerts"] = *config.MaxAlerts
	}
	if config.SendResolved != nil {
		data["send_resolved"] = *config.SendResolved
	}

	// Get URL from secret or direct value
	url := ""
	if config.URLSecretRef != nil {
		secret, err := c.getSecretValue(ctx, config.URLSecretRef)
		if err != nil {
			return nil, errors.Wrap(err, errGetSecret)
		}
		url = secret
	} else if config.URL != nil {
		url = *config.URL
	}

	if url != "" {
		data["url"] = url
	}

	return data, nil
}

func (c *external) convertPagerDutyConfig(ctx context.Context, config v1alpha1.PagerDutyConfig) (map[string]interface{}, error) {
	data := map[string]interface{}{}

	if config.Severity != nil {
		data["severity"] = *config.Severity
	}
	if config.SendResolved != nil {
		data["send_resolved"] = *config.SendResolved
	}

	// Get routing key from secret or direct value
	routingKey := ""
	if config.RoutingKeySecretRef != nil {
		secret, err := c.getSecretValue(ctx, config.RoutingKeySecretRef)
		if err != nil {
			return nil, errors.Wrap(err, errGetSecret)
		}
		routingKey = secret
	} else if config.RoutingKey != nil {
		routingKey = *config.RoutingKey
	}

	if routingKey != "" {
		data["routing_key"] = routingKey
	}

	// Get service key from secret or direct value
	serviceKey := ""
	if config.ServiceKeySecretRef != nil {
		secret, err := c.getSecretValue(ctx, config.ServiceKeySecretRef)
		if err != nil {
			return nil, errors.Wrap(err, errGetSecret)
		}
		serviceKey = secret
	} else if config.ServiceKey != nil {
		serviceKey = *config.ServiceKey
	}

	if serviceKey != "" {
		data["service_key"] = serviceKey
	}

	return data, nil
}

func (c *external) convertEmailConfig(config v1alpha1.EmailConfig) map[string]interface{} {
	data := map[string]interface{}{
		"to": config.To,
	}

	if config.SendResolved != nil {
		data["send_resolved"] = *config.SendResolved
	}

	return data
}

func (c *external) convertOpsGenieConfig(ctx context.Context, config v1alpha1.OpsGenieConfig) (map[string]interface{}, error) {
	data := map[string]interface{}{}

	if config.Priority != nil {
		data["priority"] = *config.Priority
	}
	if config.SendResolved != nil {
		data["send_resolved"] = *config.SendResolved
	}

	// Get API key from secret or direct value
	apiKey := ""
	if config.APIKeySecretRef != nil {
		secret, err := c.getSecretValue(ctx, config.APIKeySecretRef)
		if err != nil {
			return nil, errors.Wrap(err, errGetSecret)
		}
		apiKey = secret
	} else if config.APIKey != nil {
		apiKey = *config.APIKey
	}

	if apiKey != "" {
		data["api_key"] = apiKey
	}

	return data, nil
}

func (c *external) convertMSTeamsConfig(ctx context.Context, config v1alpha1.MSTeamsConfig) (map[string]interface{}, error) {
	data := map[string]interface{}{}

	if config.Title != nil {
		data["title"] = *config.Title
	}
	if config.SendResolved != nil {
		data["send_resolved"] = *config.SendResolved
	}

	// Get webhook URL from secret or direct value
	webhookURL := ""
	if config.WebhookURLSecretRef != nil {
		secret, err := c.getSecretValue(ctx, config.WebhookURLSecretRef)
		if err != nil {
			return nil, errors.Wrap(err, errGetSecret)
		}
		webhookURL = secret
	} else if config.WebhookURL != nil {
		webhookURL = *config.WebhookURL
	}

	if webhookURL != "" {
		data["webhook_url"] = webhookURL
	}

	return data, nil
}

func (c *external) convertSNSConfig(ctx context.Context, config v1alpha1.SNSConfig) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"topic_arn": config.TopicARN,
		"region":    config.Region,
	}

	if config.SendResolved != nil {
		data["send_resolved"] = *config.SendResolved
	}

	// Get AWS credentials from secrets
	if config.AccessKeySecretRef != nil {
		accessKey, err := c.getSecretValue(ctx, config.AccessKeySecretRef)
		if err != nil {
			return nil, errors.Wrap(err, errGetSecret)
		}
		data["access_key"] = accessKey
	}

	if config.SecretKeySecretRef != nil {
		secretKey, err := c.getSecretValue(ctx, config.SecretKeySecretRef)
		if err != nil {
			return nil, errors.Wrap(err, errGetSecret)
		}
		data["secret_key"] = secretKey
	}

	return data, nil
}

func (c *external) getSecretValue(ctx context.Context, secretRef *xpv1.SecretKeySelector) (string, error) {
	secret := &corev1.Secret{}
	if err := c.kube.Get(ctx, types.NamespacedName{
		Name:      secretRef.Name,
		Namespace: secretRef.Namespace,
	}, secret); err != nil {
		return "", err
	}

	value, ok := secret.Data[secretRef.Key]
	if !ok {
		return "", fmt.Errorf("key %s not found in secret %s/%s", secretRef.Key, secretRef.Namespace, secretRef.Name)
	}

	return string(value), nil
}