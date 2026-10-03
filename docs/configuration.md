# Configuration

Guide for configuring the provider.

## ProviderConfig

Create a ProviderConfig to configure connection settings:

```yaml
apiVersion: signoz.m.crossplane.io/v1beta1
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
