# ====================================================================================
# Setup Project

PLATFORMS := linux_amd64 linux_arm64
include ../../../build/makelib/common.mk

# ====================================================================================
# Options
IMAGE ?= $(BUILD_REGISTRY)/provider-signoz:$(VERSION)
TARGETOS := $(word 1, $(subst _, ,$(PLATFORM)))
include ../../../build/makelib/imagelight.mk

# ====================================================================================
# Image Build
img.build:
	@$(INFO) docker build $(IMAGE)
	@$(MAKE) BUILD_ARGS="--load" img.build.shared
	@$(OK) docker build $(IMAGE)

img.build.shared:
	@cp Dockerfile $(IMAGE_TEMP_DIR)
	@cp -r $(OUTPUT_DIR)/bin/ $(IMAGE_TEMP_DIR)/
	@docker build $(BUILD_ARGS) --build-arg TARGETOS=$(TARGETOS) --build-arg TARGETARCH=$(TARGETARCH) -t $(IMAGE) $(IMAGE_TEMP_DIR)