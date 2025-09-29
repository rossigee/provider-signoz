# Crossplane Provider for SigNoz

A [Crossplane](https://crossplane.io/) provider for managing [SigNoz](https://signoz.io/) observability resources through Kubernetes.

## Overview

The SigNoz provider enables platform teams to manage SigNoz dashboards, alerts, and notification channels as Kubernetes resources. This allows for:

- Declarative configuration of observability infrastructure
- GitOps workflows for monitoring and alerting
- Integration with existing Kubernetes tooling
- Consistent lifecycle management across environments

## Features

- **Dashboard Management**: Create, update, and delete SigNoz dashboards
- **Alert Rules**: Manage threshold and anomaly-based alerts
- **Notification Channels**: Configure Slack, PagerDuty, Webhook, and other notification integrations
- **Cross-references**: Link alerts to notification channels using Kubernetes selectors
- **Import Support**: Import existing SigNoz resources

## Prerequisites

- Kubernetes cluster with Crossplane installed
- SigNoz instance (self-hosted or cloud)
- API token with appropriate permissions

## Installation

### Quick Start

```bash
kubectl crossplane install provider ghcr.io/rossigee/provider-signoz:v0.3.0
```

### Declarative Installation

```yaml
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-signoz
spec:
  package: ghcr.io/rossigee/provider-signoz:v0.3.0
```

## Configuration

### 1. Create API Token

In your SigNoz instance:
1. Navigate to Settings > Access Tokens
2. Create a new token with appropriate scopes
3. Copy the token value

### 2. Create Secret

```bash
kubectl create secret generic signoz-credentials \
  --from-literal=credentials='{"apiKey":"YOUR_API_TOKEN_HERE"}' \
  -n crossplane-system
```

### 3. Configure Provider

```yaml
apiVersion: signoz.m.crossplane.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
spec:
  endpoint: "https://your-signoz-instance.com"  # For self-hosted instances
  # endpoint: "https://api.signoz.cloud"        # For SigNoz Cloud
  credentials:
    source: Secret
    secretRef:
      name: signoz-credentials
      namespace: crossplane-system
      key: credentials
```

## Usage Examples

### Create a Dashboard

```yaml
apiVersion: dashboard.signoz.m.crossplane.io/v1beta1
kind: Dashboard
metadata:
  name: application-metrics
  namespace: default
spec:
  forProvider:
    title: "Application Metrics"
    description: "Key metrics for our application"
    tags:
      - "production"
      - "metrics"
    widgets:
      - title: "Request Rate"
        panelType: "graph"
        query:
          queryType: 1  # PromQL
          promQL:
            - query: "sum(rate(http_requests_total[5m]))"
              legend: "Requests/sec"
  providerConfigRef:
    name: default
```

### Create an Alert Rule

```yaml
apiVersion: alert.signoz.m.crossplane.io/v1beta1
kind: Alert
metadata:
  name: high-error-rate
  namespace: default
spec:
  forProvider:
    alertName: "High Error Rate"
    alertType: "METRIC_BASED_ALERT"
    condition:
      compositeQuery:
        queryType: 1
        promQL:
          - query: "sum(rate(http_requests_total{status=~'5..'}[5m])) / sum(rate(http_requests_total[5m])) > 0.05"
    evalWindow: "5m"
    frequency: "1m"
    severity: "warning"
    channelIdsRef:
      - name: slack-alerts
    labels:
      team: "platform"
      service: "api"
  providerConfigRef:
    name: default
```

### Create a Notification Channel

```yaml
apiVersion: channel.signoz.m.crossplane.io/v1beta1
kind: NotificationChannel
metadata:
  name: slack-alerts
  namespace: default
spec:
  forProvider:
    name: "Slack Alerts"
    type: "slack"
    slackConfigs:
      - channel: "#alerts"
        webhook_url: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
  providerConfigRef:
    name: default
```

## Resource Reference

### Dashboard Resource

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | Yes | Dashboard title |
| `description` | string | No | Dashboard description |
| `tags` | []string | No | List of tags |
| `widgets` | []Widget | Yes | Dashboard widgets/panels |
| `variables` | map[string]Variable | No | Dashboard variables |

### Alert Resource

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `alertName` | string | Yes | Alert rule name |
| `alertType` | string | Yes | Type of alert (METRIC_BASED_ALERT, LOG_BASED_ALERT, etc.) |
| `condition` | RuleCondition | Yes | Alert condition/query |
| `evalWindow` | string | Yes | Evaluation window (e.g., "5m") |
| `frequency` | string | Yes | Check frequency (e.g., "1m") |
| `severity` | string | Yes | Alert severity (info, warning, error, critical) |
| `channelIdsRef` | []Reference | No | References to notification channels |

### NotificationChannel Resource

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Channel name |
| `type` | string | Yes | Channel type (slack, pagerduty, webhook, etc.) |
| `*Configs` | object | Conditional | Type-specific configuration |

## Development

### Prerequisites

- Go 1.21+
- Docker
- kubectl
- crossplane CLI

### Building from Source

```bash
# Clone the repository
git clone https://github.com/rossigee/provider-signoz.git
cd provider-signoz

# Initialize build system
make submodules

# Download dependencies
go mod download

# Generate code
make generate

# Build binary
make build

# Run tests
make test

# Build image
make docker-build

# Build Crossplane package
make xpkg.build
```

## Troubleshooting

### Common Issues

#### 401 Unauthorized
- Verify API token is valid
- Check token has required permissions
- Ensure token is correctly formatted in secret

#### Connection Errors
- Verify `endpoint` in ProviderConfig
- Check network connectivity to SigNoz instance
- For self-hosted instances, ensure API is exposed

### Debug Mode

Enable debug logging:

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: debug-config
spec:
  deploymentTemplate:
    spec:
      template:
        spec:
          containers:
          - name: package-runtime
            args:
            - --debug
```

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.