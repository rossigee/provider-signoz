# Crossplane Provider for SigNoz

[![CI](https://img.shields.io/github/actions/workflow/status/rossigee/provider-signoz/ci.yml?branch=master)][build]
[![Version](https://img.shields.io/github/v/release/rossigee/provider-signoz)][releases]
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

[build]: https://github.com/rossigee/provider-signoz/actions/workflows/ci.yml
[releases]: https://github.com/rossigee/provider-signoz/releases

A [Crossplane](https://crossplane.io/) provider for managing [SigNoz](https://signoz.io/) observability resources through Kubernetes.

## Container Registry

- **Primary**: `ghcr.io/rossigee/provider-signoz:v0.4.16`

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

## Getting Started

### Quick Start

```bash
kubectl crossplane install provider ghcr.io/rossigee/provider-signoz:v0.4.16
```

### Declarative Installation

```yaml
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-signoz
spec:
  package: ghcr.io/rossigee/provider-signoz:v0.4.16
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
          queryType: PromQL
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
        queryType: PromQL
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

## Resource Types

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
- Inspect the ProviderConfig's `CredentialsValid` condition (see below)

#### Connection Errors
- Verify `endpoint` in ProviderConfig
- Check network connectivity to SigNoz instance
- For self-hosted instances, ensure API is exposed

### ProviderConfig Readiness

The provider validates every `ProviderConfig` against the upstream SigNoz
API before accepting work for any resource that references it. The result is
recorded on **`status.conditions.CredentialsValid`** (a condition type
distinct from `Ready` so operators can route alerts on it independently).

| Reason | Meaning | Operator action |
|---|---|---|
| `CredentialsAccepted` | Probe succeeded. | None; healthy. |
| `CredentialsRejected` | Upstream returned 401/403. | Rotate the API key in the Secret referenced by `spec.credentials.secretRef`. |
| `CredentialsEmpty` | `apiKey` is empty. | Check Secret content and `spec.credentials.secretRef.key`. |
| `CredentialsTooShort` | `apiKey` is shorter than `--min-api-key-length` (default 8). | Almost certainly a placeholder/mis-paste. Replace. |
| `SecretMissing` | Secret referenced by ProviderConfig not found, or `apiKey` JSON key absent. | Verify Secret exists and contains valid JSON. |
| `UpstreamTransient` | Upstream returned 5xx or the probe timed out. | Usually self-healing; ensure SigNoz is healthy. |
| `EndpointUnreachable` | Probe could not reach the configured `endpoint`. | Verify `endpoint` and DNS. |

When `CredentialsValid=False`, the provider:

1. **Stays running** — the pod does not CrashLoop. This is intentional so a
   single bad ProviderConfig doesn't take the provider down (CrashLoop would
   StormLoop probes against the upstream).
2. **Requeues the probe with exponential backoff**: 30s → 1m → 5m → 5m …
   (capped at 15m) for auth-class failures. Transient failures use a smaller
   cap (60s) so the probe recovers quickly once upstream recovers.
3. **Records every managed-resource Observe error** in the managed resource's
   own `status.conditions.UpstreamAuth` so a UI/alert rule can see which
   resources are currently blocked on auth.

### Defending against misconfigured credentials (`--*` flags)

The provider ships with several flags that hard-gate a misconfigured
ProviderConfig before it can hammer the upstream SigNoz API:

| Flag | Default | Purpose |
|---|---|---|
| `--min-api-key-length` | `8` | Reject `apiKey` values shorter than this from the Secret. |
| `--auth-failure-window` | `60s` | Sliding window for counting consecutive auth failures. |
| `--auth-failure-threshold` | `5` | Auth failures within the window that trip the breaker. |
| `--auth-failure-cooldown` | `5m` | Time the breaker stays open before allowing a probe. |
| `--probe-conn-timeout` | `10s` | Per-attempt timeout for the ProviderConfig credentials probe. |

A misconfigured ProviderConfig (empty/short/expired API key) is rejected at
three increasingly strict layers:

1. **Empty-key short-circuit** in `Client.doRequest` — the request is
   rejected before any TCP connection. Every managed-resource Observe
   triggers 0 outbound bytes to the upstream.
2. **Extraction-time validation** in `clients.GetConfigFromProviderConfig` —
   the provider does not even build a `Client` until the API key passes
   non-empty + min-length checks.
3. **Per-PC upstream breaker** — if upstream auth still fails despite valid
   syntax, a per-`(baseURL, apiKey-fingerprint)` breaker opens after 5
   failures in 60s, holding closed for 5m. Only the ProviderConfig probe
   is permitted while the breaker is open; other managed-resource calls
   return `ErrBreakerOpen` immediately.

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
## Implementation

This provider is a native Crossplane controller that directly implements the provider APIs without using Terraform or upjet scaffolding. This approach yields smaller binaries, simpler code, and reduced dependencies.
