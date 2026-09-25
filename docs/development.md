# Development

Guide for developing the provider.

## Prerequisites

- Go 1.27+
- Kubernetes cluster
- kubectl configured

## Building

```bash
make build
```

## Testing

```bash
make test
```

## Running Locally

```bash
make run
```

## Release Process

1. Update `VERSION`, `package/crossplane.yaml`, and current installation references.
2. Add the release entry to `CHANGELOG.md`.
3. Open a release PR from `release/v0.6.4` based on `origin/master`.
4. After merge, create and push the exact release tag:

   ```bash
   git tag v0.6.4
   git push origin v0.6.4
   ```

5. The tag-only workflow builds and publishes `linux_amd64` and `linux_arm64` packages, aliases `latest`, verifies equal digests and both architectures, and creates the GitHub Release.
