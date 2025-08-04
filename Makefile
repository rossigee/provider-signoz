# Project Setup
PROJECT_NAME := provider-signoz
PROJECT_REPO := github.com/rossigee/$(PROJECT_NAME)

PLATFORMS ?= linux_amd64 linux_arm64
-include build/makelib/common.mk

# Setup Output
-include build/makelib/output.mk

# Setup Go
# Override golangci-lint version for modern Go support
GOLANGCILINT_VERSION ?= 2.3.1
NPROCS ?= 1
GO_TEST_PARALLEL := $(shell echo $$(( $(NPROCS) / 2 )))
GO_STATIC_PACKAGES = $(GO_PROJECT)/cmd/provider
GO_LDFLAGS += -X $(GO_PROJECT)/internal/version.Version=$(VERSION)
GO_SUBDIRS += cmd internal apis
GO111MODULE = on
-include build/makelib/golang.mk

# Setup Kubernetes tools
-include build/makelib/k8s_tools.mk

# Setup Images
IMAGES = provider-signoz
-include build/makelib/imagelight.mk

# Setup XPKG - Standardized registry configuration
# Primary registry: GitHub Container Registry under rossigee
XPKG_REG_ORGS ?= ghcr.io/rossigee
XPKG_REG_ORGS_NO_PROMOTE ?= ghcr.io/rossigee

# Optional registries (can be enabled via environment variables)
# To enable Harbor: export ENABLE_HARBOR_PUBLISH=true make publish XPKG_REG_ORGS=harbor.golder.lan/library
# To enable Upbound: export ENABLE_UPBOUND_PUBLISH=true make publish XPKG_REG_ORGS=xpkg.upbound.io/crossplane-contrib
XPKGS = provider-signoz
-include build/makelib/xpkg.mk

# NOTE: we force image building to happen prior to xpkg build so that we ensure
# image is present in daemon.
xpkg.build.provider-signoz: do.build.images

# Setup Local Dev
-include build/makelib/local.mk

# Targets

# run `make submodules` after cloning the repo
submodules:
	@git submodule sync
	@git submodule update --init --recursive

# install CRDs into a cluster
install: $(KIND) $(KUBECTL)
	@$(KUBECTL) apply -f package/crds

# uninstall CRDs from a cluster
uninstall:
	@$(KUBECTL) delete -f package/crds

# Generate manifests e.g. CRD, RBAC etc.
manifests:
	@$(INFO) Generating CRD manifests
	@$(KUBECTL) create -f package/crds 2>/dev/null || $(KUBECTL) replace -f package/crds
	@$(OK) Generating CRD manifests

# Additional targets that don't conflict with build system

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate manifests
	@$(INFO) Running provider
	@$(GO) run ./cmd/provider/main.go
	@$(OK) Running provider

# Generate code
dev: generate
	@$(INFO) Creating CRDs
	@$(KUBECTL) create -f package/crds 2>/dev/null || $(KUBECTL) replace -f package/crds
	@$(OK) Created CRDs

.PHONY: submodules install uninstall manifests run dev

# Custom targets for development

# Update the submodules, such as the common build scripts.
update-submodules:
	@git submodule update --remote

# We want submodules to be set up the first time running make (or else the build will fail)
build.init: submodules

# Update CI images
ci-update-images:
	@sed -i -E "s|(GO_VERSION ?=) .+|\1 $(shell cat .github/workflows/ci.yml | yq '.env.GO_VERSION' -r)|g" build/makelib/golang.mk

.PHONY: submodules reviewable check-diff update-submodules build.init ci-update-images