# Local host guard; standalone clones and GitHub Actions keep their existing toolchain.
LOCAL_VERIFY :=
LOCAL_DOCKER_BUILD := docker build
ifneq ($(GITHUB_ACTIONS),true)
ifneq ($(shell command -v opensoha-verify 2>/dev/null)$(OPENSOHA_LOCAL_VERIFY_REQUIRED),)
LOCAL_VERIFY := opensoha-verify run --
LOCAL_DOCKER_BUILD := opensoha-verify build-image --
endif
endif

GO ?= go
CONTROLLER_GEN_VERSION ?= v0.22.0
IMAGE ?= ghcr.io/opensoha/soha-operator:local

.DEFAULT_GOAL := verify

.PHONY: generate manifests test test-race vet build docker-build verify

generate:
	GOWORK=off $(LOCAL_VERIFY) $(GO) run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) object paths=./api/...

manifests:
	GOWORK=off $(LOCAL_VERIFY) $(GO) run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) crd:maxDescLen=0,generateEmbeddedObjectMeta=true paths=./api/... output:crd:artifacts:config=config/crd/bases
	GOWORK=off $(LOCAL_VERIFY) $(GO) run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) rbac:roleName=soha-operator-manager-role paths=./... output:rbac:artifacts:config=config/rbac

test:
	GOWORK=off $(LOCAL_VERIFY) $(GO) test ./...

test-race:
	GOWORK=off $(LOCAL_VERIFY) $(GO) test -race ./...

vet:
	GOWORK=off $(LOCAL_VERIFY) $(GO) vet ./...

build:
	GOWORK=off CGO_ENABLED=0 $(LOCAL_VERIFY) $(GO) build -trimpath -o bin/soha-operator ./cmd/operator

docker-build:
	$(LOCAL_DOCKER_BUILD) -f deploy/Dockerfile -t $(IMAGE) .

.PHONY: verify-image
verify-image:
	opensoha-verify image -- -f deploy/Dockerfile .

verify: generate manifests test vet build
