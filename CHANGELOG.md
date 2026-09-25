# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.6.4] - 2026-09-24

### Changed

- Refreshed the Crossplane APIs fork dependency to `v2.5.0-rc.0`.
- Hardened tag-only, xpkg-only release publishing for exact `vMAJOR.MINOR.PATCH` tags at the current `origin/master` commit.
- Publishes `linux_amd64` and `linux_arm64` xpkg artifacts, aliases the version as `latest`, and verifies matching digests and both platform manifests before creating the GitHub Release.
