# Soha Operator

Soha Operator is the optional, cluster-local controller for Kubernetes-native Soha extensions. Its first API is `workloads.soha.io/v1alpha1 WorkloadCronJob`.

- Source: [github.com/opensoha/soha-operator](https://github.com/opensoha/soha-operator)
- Container image: [ghcr.io/opensoha/soha-operator](https://github.com/orgs/opensoha/packages/container/package/soha-operator)

`WorkloadCronJob` owns a native `batch/v1 CronJob`. It copies the declared `cronJobSpec`, then continuously synchronizes one target container with a selected regular container from a same-namespace `Deployment`, `StatefulSet`, or `DaemonSet`.

The selected container's image, `env`, `envFrom`, `volumeMounts`, and referenced Pod volumes follow the source. Commands, arguments, scheduling, security context, sidecars, volume devices, and unrelated volumes remain the snapshot stored in `spec.cronJobSpec`. ConfigMap and Secret contents are resolved by each Job at runtime; the operator copies references and does not read their data. A source volume cannot replace a different same-name volume used by another CronJob container. StatefulSet `volumeClaimTemplates` also cannot be mapped to a specific Job PVC. These cases, a missing source/container, or an unmappable volume set `Ready=False` and suspend an already-owned CronJob. The operator never adopts a foreign same-name CronJob.

## Install

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
kubectl apply -k "https://github.com/opensoha/soha-operator//config/default?ref=v0.1.0"
```

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

```sh
make verify
make test-race
make docker-build
```

Use Go 1.26.6. Generated deep-copy code, CRD schemas, and RBAC are produced with `controller-gen v0.20.0`.

## License

Apache License 2.0. See [LICENSE](./LICENSE).
