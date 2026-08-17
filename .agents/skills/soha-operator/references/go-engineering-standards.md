# Go Engineering Standards

- Keep the main package thin and wire concrete dependencies explicitly.
- Keep interfaces at consumer boundaries only; do not add an interface for one implementation.
- Reconciliation must be idempotent, context-aware, and reconstructable from Kubernetes state.
- Use owner references for garbage collection and status conditions for user-visible failures.
- Never adopt or mutate a same-name object that is not controlled by the current custom resource.
- Watch referenced resources instead of polling. Index references when a source event can affect multiple custom resources.
- Avoid hot loops: permanent specification or target conflicts update status and wait for a watched change; transient API errors return an error for controller-runtime backoff.
- Generated code and manifests must be reproducible from pinned tools.
- Tests must cover create, update, conflict, source loss, and every supported source kind.
