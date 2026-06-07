# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a **Crossplane provider for SigNoz** that enables managing SigNoz observability resources (dashboards, alerts, notification channels) through Kubernetes. It's written in Go 1.21+ and follows standard Crossplane provider patterns.

## Essential Setup Commands

**First time setup (required):**
```bash
make submodules  # MUST run after cloning - downloads Crossplane build system
go mod download
```

**Development workflow:**
```bash
make generate    # Generate deepcopy methods and code
make build       # Build provider binary
make test        # Run tests with coverage
make dev         # Generate + install/update CRDs in cluster
```

## Key Development Commands

### Building and Testing
```bash
make build                    # Build provider binary to bin/provider
make test                     # Run unit tests with coverage (creates cover.out)
make generate                 # Generate deepcopy, conversion, runtime.Object code
make manifests               # Generate and apply CRD manifests
```

### Kubernetes Operations
```bash
make install                 # Install CRDs into cluster
make uninstall              # Remove CRDs from cluster
make run                    # Run provider locally against cluster
make dev                    # Generate + create/update CRDs in cluster
```

### Code Quality
```bash
make reviewable             # Prepare for PR (generate + go mod tidy)
make check-diff            # Ensure no uncommitted changes
```

### Docker and Packaging
```bash
make docker-build          # Build Docker image
make xpkg.build.provider-signoz  # Build Crossplane package
```

## Architecture

### Resource Types
- **Dashboard** (`dashboard.signoz.crossplane.io/v1alpha1`): Manages SigNoz dashboards with widgets and queries
- **Alert** (`alert.signoz.crossplane.io/v1alpha1`): Manages alert rules with conditions and notification channels
- **NotificationChannel** (`channel.signoz.crossplane.io/v1alpha1`): Manages Slack, PagerDuty, webhook integrations
- **ProviderConfig** (`signoz.crossplane.io/v1beta1`): Authentication and endpoint configuration

### Key Directories
- `apis/*/v1alpha1/`: CRD type definitions and API schemas
- `internal/controller/`: Crossplane controller implementations (Observe/Create/Update/Delete)
- `internal/clients/`: SigNoz API client implementation
- `examples/`: Sample resource manifests for testing
- `package/crds/`: Generated CRD manifests
- `cmd/provider/`: Main provider entry point

### Cross-references
Alert resources can reference NotificationChannel resources using `channelIdsRef` with Kubernetes-native selectors, enabling declarative linking between resources.

## Testing Strategy

- **Unit Tests**: Focus on controller logic and API client
- **Integration**: Use example manifests in `examples/` directory
- **Coverage**: Tests generate `cover.out` for coverage analysis
- **Race Detection**: Tests run with race detection enabled

## Development Notes

- This provider follows Crossplane's managed resource lifecycle patterns
- All code generation is handled by the Crossplane build system included via git submodules
- The project uses standard Kubernetes controller-runtime patterns
- SigNoz API client handles authentication, rate limiting, and error handling
- Provider supports both self-hosted and SigNoz Cloud instances

## Important Files

- `internal/clients/signoz.go`: Core SigNoz API client implementation
- `apis/*/v1alpha1/types.go`: Resource type definitions and schemas
- `internal/controller/*/controller.go`: Crossplane controller implementations
- `examples/`: Working examples for each resource type