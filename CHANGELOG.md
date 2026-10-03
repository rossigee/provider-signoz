# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.6.7] - 2026-10-03

### Fixed

- Adoption now persists the id it discovers. Setting `external-name` during `Observe` was silently discarded: once a resource is observed up to date the reconciler writes only the status subresource and never the object, so the adopted id was lost every reconcile. Adoption was correct but had to be repeated indefinitely, re-listing every dashboard through the API on each poll, and the resource only read as Ready because adoption rescued it each time rather than because its id was recorded. `Observe` now writes the id itself.

## [v0.6.6] - 2026-10-03

### Fixed

- Dashboard adoption now matches on the name SigNoz actually stores. v0.6.5 compared against `spec.forProvider.title`, but SigNoz stores a slug of it — lower cased with whitespace collapsed to hyphens — so `CoreDNS Monitoring` is held as `coredns-monitoring` and `Bitcoin Knots Node` as `bitcoin-knots-node`. Neither equals the title, so the lookup never matched and adoption never fired. Verified against all 29 dashboards managed by flux-crossplane-signoz: the slug of the title matches every one. The raw title and CR name are still tried as fallbacks.

## [v0.6.5] - 2026-10-03

### Fixed

- A `Dashboard` whose `external-name` annotation is lost no longer sticks in a permanent create/409 loop. `Observe` now looks the dashboard up by `spec.forProvider.title` when the recorded id 404s, re-adopts it, and writes the real id back. SigNoz enforces uniqueness on name and reports a freshly minted id in its `already_exists` error, so the previous behaviour could never recover on its own.
- A failure to search for the dashboard is reported as an error rather than as absence, so a transient list failure can no longer trigger a create and wedge the resource.
- `ListDashboardsV2` decoded the wrong response shape and silently returned an empty list. The v2 endpoint nests results under `data.dashboards` with the unpaginated count in `data.total`.
- `ListDashboardsV2` now requests an explicit `limit=`. Without it SigNoz returns 20 results while still reporting the full total, making a truncated list indistinguishable from absent dashboards.

## [v0.6.4] - 2026-09-24

### Changed

- Refreshed the Crossplane APIs fork dependency to `v2.5.0-rc.0`.
- Hardened tag-only, xpkg-only release publishing for exact `vMAJOR.MINOR.PATCH` tags at the current `origin/master` commit.
- Publishes `linux_amd64` and `linux_arm64` xpkg artifacts, aliases the version as `latest`, and verifies matching digests and both platform manifests before creating the GitHub Release.
