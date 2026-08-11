# Destroy graph run-input cache and ownership generations

`ExecuteDestroyGraph` produces one full render before dispatch and passes it to
the graph as immutable run input. That render is the only owner of shared
storage assets. A task cache miss writes only inventory and vars below
`runs/history/<run>/inputs/<sha256>` and points its `StorageAssets` result at
the immutable full-render root; task overlays must never recreate or rewrite
those assets.

The overlay key is the canonical combination of the task's desired-state
projection, a cloned and deterministically sorted ownership snapshot, and its
materialized KubeVirt host-kubeconfig path map. Parallel requests for one key
share one render completion. A graph task whose desired and recorded host sets
are empty still stops before an overlay render, secret materialization, or
runner launch.

Ownership is cached only for one mutation generation, not frozen for the whole
graph. Each task receives a clone. Every Ansible runner return, including a
failure, invalidates the shared generation; controller-side storage ownership
transitions invalidate it before the nested release run. The next task reloads
the context before deriving hosts and its overlay key. Keep invalidation at
every ownership-changing boundary so a later task cannot render or prove
completion from pre-mutation evidence.

Referenced context secrets are materialized once per graph below
`runs/history/<run>/runtime/secrets`, using the immutable full render to select
them. All tasks share that read-only runtime set. Normal graph completion
removes the whole runtime root, a cleanup error is returned rather than hidden,
and the post-lease stale-runtime sweep reclaims it after a killed process.
KubeVirt host kubeconfigs remain separate: each machine-infra destroy task
derives only the host clusters in its machine/cluster dependency closure,
materializes those kubeconfigs for that invocation, includes their paths in
the overlay key, and removes them when the callback returns. Records-only and
unrelated tasks materialize none.

`DestroyRunInputCounters` is test instrumentation and stays atomic because
independent graph branches run concurrently. `Renders` counts task-overlay
cache misses, excluding the initial full render; `OwnershipLoads` counts
generation load attempts; `SecretMaterializations` counts the one graph-secret
materialization attempt; `KubeconfigScopes` counts requested task host-cluster
scopes; and `RunnerLaunches` counts actual Ansible runner calls. Keep the
counters at these boundaries: collapsing them into a generic task count would
hide repeated setup or make legitimate no-host skips look like launches.
