# Project Setup
PROJECT_NAME := provider-signoz
PROJECT_REPO := github.com/crossplane-contrib/$(PROJECT_NAME)

PLATFORMS ?= linux_amd64 linux_arm64
-include build/makelib/common.mk

# Setup Output
-include build/makelib/output.mk

# Setup Go
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

# Setup XPKG
XPKG_REG_ORGS ?= xpkg.upbound.io/crossplane-contrib
# NOTE(hasheddan): skip promoting on xpkg.upbound.io as channel tags are
# inferred.
XPKG_REG_ORGS_NO_PROMOTE ?= xpkg.upbound.io/crossplane-contrib
XPKGS = provider-signoz
-include build/makelib/xpkg.mk

# NOTE(hasheddan): we force image building to happen prior to xpkg build so that
# we ensure image is present in daemon.
xpkg.build.provider-signoz: do.build.images

# Override xpkg.build to use modern Crossplane CLI syntax
xpkg.build.provider-signoz: $(CROSSPLANE_CLI)
	@$(INFO) Building package provider-signoz-$(VERSION).xpkg for $(PLATFORM)
	@mkdir -p $(OUTPUT_DIR)/xpkg/$(PLATFORM)
	@controller_arg=$$(grep -E '^kind:\s+Provider\s*$$' $(XPKG_DIR)/crossplane.yaml > /dev/null && echo "--embed-runtime-image $(BUILD_REGISTRY)/provider-signoz-$(ARCH)"); \
	$(CROSSPLANE_CLI) xpkg build \
		$${controller_arg} \
		--package-root $(XPKG_DIR) \
		--examples-root $(XPKG_EXAMPLES_DIR) \
		--ignore $(XPKG_IGNORE) \
		--package-file $(XPKG_OUTPUT_DIR)/$(PLATFORM)/provider-signoz-$(VERSION).xpkg || $(FAIL)
	@$(OK) Built package provider-signoz-$(VERSION).xpkg for $(PLATFORM)

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