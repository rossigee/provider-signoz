# Configuration

Guide for configuring the provider.

## ProviderConfig

Create a ProviderConfig to configure connection settings:

```yaml
apiVersion: $p.crossplane.io/v1
kind: ProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      name: provider-credentials
      namespace: crossplane-system
```
