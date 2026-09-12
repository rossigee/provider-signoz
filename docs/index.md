# Provider SigNoz Documentation

A Crossplane v2 provider for managing SigNoz observability resources. All managed resources are namespaced (`*.signoz.m.crossplane.io/v1beta1`) with multi-tenancy support.

## Quick Links

- [Configuration](configuration.md) — Authentication and connection setup
- [Getting Started](getting-started.md) — Installation and first resources
- [Development](development.md) — Building, testing, and contributing

## Resource Documentation

### Observability

| Resource | API Group | Description |
|----------|-----------|-------------|
| Dashboard | `dashboard.signoz.m.crossplane.io/v1beta1` | Dashboards |
| Channel | `channel.signoz.m.crossplane.io/v1beta1` | Notification channels |

### Alerts

| Resource | API Group | Description |
|----------|-----------|-------------|
| Alert | `alert.signoz.m.crossplane.io/v1beta1` | Alert rules |

### Provider

| Resource | API Group | Description |
|----------|-----------|-------------|
| ProviderConfig | `signoz.m.crossplane.io/v1beta1` | Credentials (cluster-scoped) |

## API Coverage Gaps

SigNoz API surface not yet modeled: saved views/filters, traceExplorer query templates, logs pipelines and parsing rules, alertmanager routes/silences beyond channels, user/team management, license/ingestion-key rotation, and dashboard variable templates beyond inline maps.
