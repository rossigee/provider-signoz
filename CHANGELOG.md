# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `DeleteDashboardV2` API method for proper V2 dashboard deletion
- Comprehensive ProviderConfig controller tests (70% coverage)
- ProviderConfig credentials validation tests covering:
  - Successful authentication probes
  - Auth failure scenarios (invalid/rejected credentials)
  - Empty/missing API key validation
  - Secret reference resolution
  - Transient error handling
  - Condition type assertions
- Client-layer tests for `DeleteDashboardV2`
- Fingerprinting and failure tracking tests

### Fixed
- **CRITICAL**: Dashboard deletion now uses V2 API instead of V1
  - Previously: Create/Update/Read used V2, Delete used V1 (API mismatch)
  - Now: All dashboard operations consistently use V2 API
  - Resolves potential issues deleting V2 dashboards
- ProviderConfig status condition reporting
- API version consistency across dashboard CRUD operations

### Changed
- Dashboard controller updated to use `DeleteDashboardV2` for deletion

### Test Coverage
- ProviderConfig: 0% → 70.1% (NEW)
- Dashboard: 54.1% (maintained)
- Alert: 44.5% (maintained)
- Channel: 14.5% (maintained)

## [v0.6.7] - 2024-10-07

### Added
- Dashboard adoption by name when external-name is lost
- Dashboard persist-id functionality for adoption recovery
- Rule evaluation block guidance documentation

### Fixed
- Inverted guidance on rule evaluation block requirements
- Dashboard adoption workflow to properly persist IDs

## Previous Releases
See git history for previous releases prior to v0.6.7.
