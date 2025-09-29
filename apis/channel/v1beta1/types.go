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

// NotificationChannelParameters are the configurable fields of a NotificationChannel.
type NotificationChannelParameters struct {
	// Name is the name of the notification channel.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Type is the type of notification channel.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=slack;webhook;pagerduty;email;opsgenie;msteams;sns
	Type string `json:"type"`

	// SlackConfigs contains configuration for Slack channels.
	// Required when Type is "slack".
	// +optional
	SlackConfigs []SlackConfig `json:"slackConfigs,omitempty"`

	// WebhookConfigs contains configuration for webhook channels.
	// Required when Type is "webhook".
	// +optional
	WebhookConfigs []WebhookConfig `json:"webhookConfigs,omitempty"`

	// PagerDutyConfigs contains configuration for PagerDuty channels.
	// Required when Type is "pagerduty".
	// +optional
	PagerDutyConfigs []PagerDutyConfig `json:"pagerdutyConfigs,omitempty"`

	// EmailConfigs contains configuration for email channels.
	// Required when Type is "email".
	// +optional
	EmailConfigs []EmailConfig `json:"emailConfigs,omitempty"`

	// OpsGenieConfigs contains configuration for OpsGenie channels.
	// Required when Type is "opsgenie".
	// +optional
	OpsGenieConfigs []OpsGenieConfig `json:"opsgenieConfigs,omitempty"`

	// MSTeamsConfigs contains configuration for Microsoft Teams channels.
	// Required when Type is "msteams".
	// +optional
	MSTeamsConfigs []MSTeamsConfig `json:"msteamsConfigs,omitempty"`

	// SNSConfigs contains configuration for AWS SNS channels.
	// Required when Type is "sns".
	// +optional
	SNSConfigs []SNSConfig `json:"snsConfigs,omitempty"`
}

// SlackConfig defines configuration for Slack notifications.
type SlackConfig struct {
	// Channel is the Slack channel to send notifications to.
	// +kubebuilder:validation:Required
	Channel string `json:"channel"`

	// WebhookURL is the Slack webhook URL.
	// This field should be provided via a secret reference.
	// +optional
	WebhookURL *string `json:"webhook_url,omitempty"`

	// WebhookURLSecretRef references a secret containing the webhook URL.
	// +optional
	WebhookURLSecretRef *xpv1.SecretKeySelector `json:"webhookUrlSecretRef,omitempty"`

	// Title is an optional title for notifications.
	// +optional
	Title *string `json:"title,omitempty"`

	// SendResolved indicates whether to send notifications when alerts resolve.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
}

// WebhookConfig defines configuration for webhook notifications.
type WebhookConfig struct {
	// URL is the webhook endpoint URL.
	// This field should be provided via a secret reference.
	// +optional
	URL *string `json:"url,omitempty"`

	// URLSecretRef references a secret containing the webhook URL.
	// +optional
	URLSecretRef *xpv1.SecretKeySelector `json:"urlSecretRef,omitempty"`

	// Method is the HTTP method to use (GET, POST, PUT).
	// +kubebuilder:validation:Enum=GET;POST;PUT
	// +optional
	Method *string `json:"http_method,omitempty"`

	// MaxAlerts is the maximum number of alerts to include in a single webhook call.
	// +optional
	MaxAlerts *int `json:"max_alerts,omitempty"`

	// SendResolved indicates whether to send notifications when alerts resolve.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
}

// PagerDutyConfig defines configuration for PagerDuty notifications.
type PagerDutyConfig struct {
	// RoutingKey is the PagerDuty integration routing key.
	// This field should be provided via a secret reference.
	// +optional
	RoutingKey *string `json:"routing_key,omitempty"`

	// RoutingKeySecretRef references a secret containing the routing key.
	// +optional
	RoutingKeySecretRef *xpv1.SecretKeySelector `json:"routingKeySecretRef,omitempty"`

	// ServiceKey is the PagerDuty service key (for legacy integrations).
	// This field should be provided via a secret reference.
	// +optional
	ServiceKey *string `json:"service_key,omitempty"`

	// ServiceKeySecretRef references a secret containing the service key.
	// +optional
	ServiceKeySecretRef *xpv1.SecretKeySelector `json:"serviceKeySecretRef,omitempty"`

	// Severity is the default severity for incidents.
	// +kubebuilder:validation:Enum=critical;error;warning;info
	// +optional
	Severity *string `json:"severity,omitempty"`

	// SendResolved indicates whether to send notifications when alerts resolve.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
}

// EmailConfig defines configuration for email notifications.
type EmailConfig struct {
	// To is the list of email addresses to send notifications to.
	// +kubebuilder:validation:Required
	To []string `json:"to"`

	// SendResolved indicates whether to send notifications when alerts resolve.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
}

// OpsGenieConfig defines configuration for OpsGenie notifications.
type OpsGenieConfig struct {
	// APIKey is the OpsGenie API key.
	// This field should be provided via a secret reference.
	// +optional
	APIKey *string `json:"api_key,omitempty"`

	// APIKeySecretRef references a secret containing the API key.
	// +optional
	APIKeySecretRef *xpv1.SecretKeySelector `json:"apiKeySecretRef,omitempty"`

	// Priority is the default priority for alerts.
	// +kubebuilder:validation:Enum=P1;P2;P3;P4;P5
	// +optional
	Priority *string `json:"priority,omitempty"`

	// SendResolved indicates whether to send notifications when alerts resolve.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
}

// MSTeamsConfig defines configuration for Microsoft Teams notifications.
type MSTeamsConfig struct {
	// WebhookURL is the Microsoft Teams webhook URL.
	// This field should be provided via a secret reference.
	// +optional
	WebhookURL *string `json:"webhook_url,omitempty"`

	// WebhookURLSecretRef references a secret containing the webhook URL.
	// +optional
	WebhookURLSecretRef *xpv1.SecretKeySelector `json:"webhookUrlSecretRef,omitempty"`

	// Title is an optional title for notifications.
	// +optional
	Title *string `json:"title,omitempty"`

	// SendResolved indicates whether to send notifications when alerts resolve.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
}

// SNSConfig defines configuration for AWS SNS notifications.
type SNSConfig struct {
	// TopicARN is the SNS topic ARN.
	// +kubebuilder:validation:Required
	TopicARN string `json:"topic_arn"`

	// Region is the AWS region.
	// +kubebuilder:validation:Required
	Region string `json:"region"`

	// AccessKeySecretRef references a secret containing the AWS access key.
	// +optional
	AccessKeySecretRef *xpv1.SecretKeySelector `json:"accessKeySecretRef,omitempty"`

	// SecretKeySecretRef references a secret containing the AWS secret key.
	// +optional
	SecretKeySecretRef *xpv1.SecretKeySelector `json:"secretKeySecretRef,omitempty"`

	// SendResolved indicates whether to send notifications when alerts resolve.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
}

// NotificationChannelSpec defines the desired state of NotificationChannel
type NotificationChannelSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       NotificationChannelParameters `json:"forProvider"`
}

// NotificationChannelObservation are the observable fields of a NotificationChannel.
type NotificationChannelObservation struct {
	// ID is the unique identifier of the channel in SigNoz.
	ID int `json:"id,omitempty"`

	// CreatedAt is when the channel was created.
	CreatedAt *metav1.Time `json:"createdAt,omitempty"`

	// UpdatedAt is when the channel was last updated.
	UpdatedAt *metav1.Time `json:"updatedAt,omitempty"`
}

// NotificationChannelStatus represents the observed state of a NotificationChannel.
type NotificationChannelStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          NotificationChannelObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion

// NotificationChannel is the Schema for the NotificationChannels API
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="TYPE",type="string",JSONPath=".spec.forProvider.type"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,signoz}
type NotificationChannel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              NotificationChannelSpec   `json:"spec"`
	Status            NotificationChannelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NotificationChannelList contains a list of NotificationChannels
type NotificationChannelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NotificationChannel `json:"items"`
}

// NotificationChannel type metadata.
var (
	NotificationChannel_Kind             = "NotificationChannel"
	NotificationChannel_GroupKind        = schema.GroupKind{Group: Group, Kind: NotificationChannel_Kind}.String()
	NotificationChannel_KindAPIVersion   = NotificationChannel_Kind + "." + SchemeGroupVersion.String()
	NotificationChannel_GroupVersionKind = SchemeGroupVersion.WithKind(NotificationChannel_Kind)
)

func init() {
	SchemeBuilder.Register(&NotificationChannel{}, &NotificationChannelList{})
}