# Soha Operator

Soha Operator is the optional, cluster-local controller for Kubernetes-native Soha extensions. Its first API is `workloads.soha.io/v1alpha1 WorkloadCronJob`.

- Source: [github.com/opensoha/soha-operator](https://github.com/opensoha/soha-operator)
- Container image: [ghcr.io/opensoha/soha-operator](https://github.com/orgs/opensoha/packages/container/package/soha-operator)

`WorkloadCronJob` owns a native `batch/v1 CronJob`. It copies the declared `cronJobSpec`, then continuously synchronizes one target container with a selected regular container from a same-namespace `Deployment`, `StatefulSet`, or `DaemonSet`.

The selected container's image, `env`, `envFrom`, `volumeMounts`, and referenced Pod volumes follow the source. Commands, arguments, scheduling, security context, sidecars, volume devices, and unrelated volumes remain the snapshot stored in `spec.cronJobSpec`. ConfigMap and Secret contents are resolved by each Job at runtime; the operator copies references and does not read their data. A source volume cannot replace a different same-name volume used by another CronJob container. StatefulSet `volumeClaimTemplates` also cannot be mapped to a specific Job PVC. These cases, a missing source/container, or an unmappable volume set `Ready=False` and suspend an already-owned CronJob. The operator never adopts a foreign same-name CronJob.

## Install

Default installation inputs pair the released `0.1.1` controller with its exact
Kubernetes 0.35.1 CRD. The checked-out `config/default` profile uses the preserved
CRD in `config/release/v0.1.1`; `config/crd/bases` remains the current development
schema and is not part of that profile.

**Existing installations:** keep your installed CRD and controller on their matching
version. Do not apply the stable CRD over an existing Kubernetes 0.37 schema or
replace its controller with `0.1.1`: that would downgrade the API/runtime pair and
can lose fields. Do not delete/recreate CRDs or prune stored WorkloadCronJobs to
switch profiles. See Development for the current schema and a matching custom
image. Helm does not upgrade existing CRDs automatically.

Install the released Helm chart:

```sh
helm repo add opensoha https://opensoha.github.io/soha-helm
helm repo update
helm install soha-operator opensoha/soha-operator \
  --namespace soha-operator \
  --create-namespace
```

Or apply the versioned Kustomize manifests:

```sh
kubectl apply -k "https://github.com/opensoha/soha-operator//config/default?ref=v0.1.1"
```

For a fresh installation from this checkout, `kubectl apply -k config/default`
uses the same stable schema/image pair. The release CRD is copied unchanged from
Git tag `v0.1.1` (commit `1ab625355a8555fd54f363dac6edbcf0e5e9b334`), not regenerated
from the current Go API.

The install creates the CRD, controller Deployment, ServiceAccount, and least-privilege cluster RBAC. Helm packaging lives in [`opensoha/soha-helm`](https://github.com/opensoha/soha-helm).

Releases use `vX.Y.Z` Git tags. Each release publishes `vX.Y.Z`, `X.Y.Z`, and `latest` image tags to GHCR. Deployment manifests and the Helm chart use the versioned `X.Y.Z` tag.

## Example

```sh
kubectl apply -f config/samples/workloads_v1alpha1_workloadcronjob.yaml
kubectl get workloadcronjobs -A
kubectl get cronjobs -n default
```

Soha can generate this CR from the existing Job/CronJob workload snapshot form. Native Job and CronJob creation remains available without installing the Operator.

The `soha-http-periodic` deployment template also bundles a service Deployment, Service, and WorkloadCronJob under the same versioned release. Its periodic task starts suspended; configure its command, schedule, and time zone before enabling it. Saving this template binding does not deploy resources.

`Ready=True` means the current CR generation and source runtime have been synchronized to the owned CronJob. It does not mean a scheduled Job has completed. Status records the source UID/resourceVersion and the CronJob UID/resourceVersion so Soha can detect stale observations and replacement resources. Older controller versions without the CronJob resourceVersion cannot satisfy this stronger health check.

When a CR is being deleted, the controller suspends its owned CronJob and never recreates it. Soha requests foreground deletion with UID/resourceVersion preconditions, reports deletion as pending while finalizers remain, and never removes another controller's finalizer. Canceling a deployment stops further Soha writes; use an explicit rollback to restore previous configuration, and manage existing Jobs separately.

For a namespace-scoped installation, run the controller with `--watch-namespace=<namespace>` and matching namespace RBAC. The default empty value watches all namespaces; changing watch scope does not change installed RBAC automatically.

## Development

Operator remains a standalone repository, Go module and runtime. Core and its
shared HTTP contracts are maintained in `opensoha/soha` (root and `contracts/`),
while this repository owns the CRDs, native manifests and
[development skill](.agents/skills/soha-operator/SKILL.md). Helm chart sources
remain in the independent `opensoha/soha-helm` repository; an Operator build does
not require the Core source tree.

```sh
make verify
make test-race
make docker-build
```

Use Go 1.27.4 and Kubernetes Go modules 0.37.1 with controller-runtime 0.25.2. Generated deep-copy code, CRD schemas, and RBAC are produced with `controller-gen v0.22.0`.

Use the Makefile commands or `GOWORK=off` in the multi-repository workspace so sibling modules do not override the Operator's types during generation or builds.

### Current schema and custom image

The current source and generated `config/crd/bases` schema use Kubernetes 0.37.1.
They include `schedulingGroup` and `defaultUser`, which the released `0.1.1`
controller cannot preserve. Build a matching image from the same source checkout;
do not retag or overwrite the published `0.1.1` image.

```sh
make docker-build  # builds ghcr.io/opensoha/soha-operator:local
# Load that image into your development cluster before applying this profile.
kubectl apply -k config/development
```

`config/development` includes the current CRD and explicitly selects the `local`
image. For a registry-backed build, use `make docker-build IMAGE=<your-image>`,
push that custom image, and set its name/tag in your own Kustomize overlay of
`config/development`. Use a Kubernetes cluster that supports the declared native
CronJob fields. Review stored CRs and CRD changes before upgrading an existing
installation; keep its matching controller image and do not apply `config/default`
as a rollback. No profile performs data migration or CRD deletion.

To use the Helm chart with this current source schema, first review and explicitly
apply `config/crd` from the same checkout, then install/upgrade with `--skip-crds`
and an explicit matching custom image. See the chart README for that workflow;
never use the chart's default `0.1.1` image with this schema.

Run the offline installation-pairing checks with:

```sh
python3 -B -m unittest discover -s config -p 'test_*.py'
```

The current CRD embeds native `CronJobSpec`. The former Pod `workloadRef` field belonged to Kubernetes GenericWorkload group scheduling and was replaced upstream by `schedulingGroup` in Kubernetes 1.36. It is unrelated to the Operator's `spec.sourceRef`, which still selects the source workload and container. Repository consumers do not set the removed field; external manifests that used it must migrate before upgrading the CRD. The Operator retains the CronJob's declared scheduling snapshot and does not copy the source Pod's scheduling group.

## License

Apache License 2.0. See [LICENSE](./LICENSE).
