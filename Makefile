PROJECT := longhorn-instance-manager
MACHINE := longhorn
# Define the target platforms that can be used across the ecosystem.
# Note that what would actually be used for a given project will be
# defined in TARGET_PLATFORMS, and must be a subset of the below:
DEFAULT_PLATFORMS := linux/amd64,linux/arm64

export SRC_BRANCH := v1.11.x-linkpool
export SRC_TAG := $(shell git tag --points-at HEAD | head -n 1)

export CACHEBUST := $(shell date +%s)

.PHONY: build validate test ci package
build:
	docker buildx build --platform linux/amd64 --target build-artifacts --output type=local,dest=. -f Dockerfile .

validate:
	docker buildx build --target validate -f Dockerfile .

test:
	docker buildx build --target test-artifacts --output type=local,dest=. -f Dockerfile .

ci:
	docker buildx build --target ci-artifacts --output type=local,dest=. -f Dockerfile .

package:
	@file bin/longhorn-instance-manager 2>/dev/null | grep -q 'x86-64' || { echo "bin/longhorn-instance-manager missing or wrong arch (expected x86-64); run 'make build' first" >&2; exit 1; }
	bash scripts/package

.PHONY: buildx-machine
buildx-machine:
	@docker buildx create --name=$(MACHINE) --platform=$(DEFAULT_PLATFORMS) 2>/dev/null || true
	docker buildx inspect $(MACHINE)

# variables needed from GHA caller:
# - REPO: image repo, include $registry/$repo_path
# - TAG: image tag
# - TARGET_PLATFORMS: optional, to be passed for buildx's --platform option
# - IID_FILE_FLAG: optional, options to generate image ID file
.PHONY: workflow-image-build-push workflow-image-build-push-secure workflow-manifest-image
workflow-image-build-push: buildx-machine
	MACHINE=$(MACHINE) PUSH='true' IMAGE_NAME=$(PROJECT) bash scripts/package
workflow-image-build-push-secure: buildx-machine
	MACHINE=$(MACHINE) PUSH='true' IMAGE_NAME=$(PROJECT) IS_SECURE=true bash scripts/package
workflow-manifest-image:
	docker pull --platform linux/amd64 ${REPO}/longhorn-instance-manager:${TAG}-amd64
	docker pull --platform linux/arm64 ${REPO}/longhorn-instance-manager:${TAG}-arm64
	docker buildx imagetools create -t ${REPO}/longhorn-instance-manager:${TAG} \
	  ${REPO}/longhorn-instance-manager:${TAG}-amd64 \
	  ${REPO}/longhorn-instance-manager:${TAG}-arm64

.DEFAULT_GOAL := ci
