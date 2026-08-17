---
name: soha-operator
description: >-
  Implement or review the standalone Soha Kubernetes operator, WorkloadCronJob
  API, reconcilers, generated CRDs and RBAC, native deployment manifests,
  container image, tests, and operator documentation.
---

# Soha Operator

## Workflow

1. Read `references/go-engineering-standards.md` before production Go changes.
2. Keep `cmd/operator/main.go` limited to flags, schemes, manager wiring, probes, and lifecycle.
3. Put public Kubernetes APIs in `api/<version>` and reconciliation in `internal/controller`.
4. Run `make generate manifests` after API types or RBAC markers change; never hand-edit generated deep-copy code or CRD schemas.
5. Preserve the controller contract: same-namespace source references, one owned native CronJob, selected-container runtime synchronization (image, environment, mounts, and referenced volumes), safe suspension when the source is invalid, and no adoption of foreign targets.

## Boundaries

- Do not import `github.com/opensoha/soha/internal/**` or depend on the Soha control plane at runtime.
- Add focused CRDs rather than a universal operation schema. New APIs need a concrete controller, status contract, least-privilege RBAC, tests, and an installation path.
- Use controller-runtime watches and owner references. Do not add polling loops, databases, webhooks, or finalizers without a demonstrated lifecycle requirement.
- Keep the native Kubernetes manifests in this repository. Helm chart source belongs in `opensoha/soha-helm`.

## Verification

Use Go 1.26.6 and run:

```sh
GOWORK=off go mod tidy
git diff --exit-code -- go.mod go.sum
GOWORK=off go mod verify
make generate manifests
git diff --exit-code
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@v1.3.0 ./...
GOWORK=off CGO_ENABLED=0 go build -trimpath -o /tmp/soha-operator ./cmd/operator
kubectl kustomize config/default | go run github.com/yannh/kubeconform/cmd/kubeconform@v0.8.0 -kubernetes-version 1.35.1 -strict -summary -
docker build -f deploy/Dockerfile -t ghcr.io/opensoha/soha-operator:test .
git diff --check
```
