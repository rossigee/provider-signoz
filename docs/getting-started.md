# Getting Started

Guide to getting started with the SigNoz provider.

## Installation

Install the SigNoz provider:

```bash
kubectl crossplane install provider ghcr.io/rossigee/provider-signoz:v0.6.4
```

## Prerequisites

- Kubernetes cluster with Crossplane installed

## Quick Start

1. Create a ProviderConfig:

```yaml
apiVersion: signoz.m.crossplane.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      name: signoz-credentials
      namespace: crossplane-system
```
