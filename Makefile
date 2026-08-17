GO ?= go
CONTROLLER_GEN_VERSION ?= v0.20.0
IMAGE ?= ghcr.io/opensoha/soha-operator:local

.DEFAULT_GOAL := verify

.PHONY: generate manifests test test-race vet build docker-build verify

generate:
	$(GO) run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) object paths=./api/...

manifests:
	$(GO) run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) crd:maxDescLen=0,generateEmbeddedObjectMeta=true paths=./api/... output:crd:artifacts:config=config/crd/bases
	$(GO) run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) rbac:roleName=soha-operator-manager-role paths=./... output:rbac:artifacts:config=config/rbac

test:
	GOWORK=off $(GO) test ./...

test-race:
	GOWORK=off $(GO) test -race ./...

vet:
	GOWORK=off $(GO) vet ./...

build:
	GOWORK=off CGO_ENABLED=0 $(GO) build -trimpath -o bin/soha-operator ./cmd/operator

docker-build:
	docker build -f deploy/Dockerfile -t $(IMAGE) .

verify: generate manifests test vet build
