# Day-2 node config reconciles by re-applying, not by pruning

**Symptom:** A node demoted from `role: infra` to `role: worker` keeps
`node-role.kubernetes.io/infra` and its `NoSchedule` taint forever, so nothing
schedules on it and no bootwright output says why. Deleting a `nodes[].labels`
entry has the same effect. Separately, authoring
`nodes[].taints: [{key: node-role.kubernetes.io/infra, effect: NoSchedule}]` on a
node that is already `role: infra` fails the whole `nodeconfig.<cluster>.apply`
task with `spec.taints[1]: Duplicate value`, taking every other node in that
cluster down with it. A pre-existing `infra` MachineConfigPool created with a
client-side `oc create` fails the same task with `Apply failed with 1 conflict`.

**Root cause:** The step is a single `oc apply --server-side --field-manager
bootwright` over a multi-doc manifest. Server-side apply only relinquishes fields
on objects **still present in the applied file** — there is no `--prune`. Emitting
a document only for nodes that currently carry labels/taints therefore makes
removal invisible: the cleared node drops out of the manifest and keeps whatever
Bootwright last set. Planning the task only for clusters that declare config has
the same shape one level up — clear the last `infra` role and no task is planned
at all. Nothing else compensates: `containerClusterInstallStructuralHashVars`
deliberately nulls `Labels`/`Taints` and coerces the role before hashing, so a
demotion produces no install-hash change and no reinstall, and `OwnershipOrphans`
has no concept of node fields.

**Fix:** Reconcile by re-applying, in three parts.

- Plan the task for **every** container cluster that declares nodes, not just
  those declaring config. This is what closes the clear-everything case, and it
  is why an otherwise idle cluster now shows one extra `nodeconfig.<cluster>.apply`
  row that reports `no node config to apply`.
- Emit a document for every node carrying declared config **plus** every node that
  already lists `bootwright` in `metadata.managedFields`. The second set is what
  relinquishes stale fields. Probe managed-fields rather than mere existence, or
  Bootwright registers itself as a field manager on nodes it never configured; a
  bare `kind: Node` doc for an unregistered node would also make `oc apply`
  **create a phantom Node object**, which is why absent nodes are skipped.
- Delete the `infra` MachineConfigPool once no `infra` node remains, selecting on
  the `bootwright.io/managed-by: bootwright` label the pool is stamped with. Never
  delete by name: an operator's own `infra` pool must survive. A cluster with no
  MachineConfigPool kind at all must not fail the task.

**Constraint — taints dedupe by `key`+`effect`, not by whole value.** Kubernetes
rejects a Node whose `spec.taints` repeats a `<key, effect>` pair, so an authored
taint that differs only in `value` still collides with the synthesized infra
taint. Deduplicate before rendering and let the authored entry win, so its `value`
survives. The validator deliberately does not reject duplicates — being forgiving
here is better than failing an apply over redundant input.

**Constraint — the apply passes `--force-conflicts`.** `Node.spec.taints` is
`+listType=atomic`, so it is one owned field, and kube-controller-manager's
node-lifecycle controller writes `node.kubernetes.io/not-ready` and
`unreachable` into it via Update. Without the flag any co-ownership is an
unrecoverable task failure — there is no retry and no per-document isolation.
Forcing means Bootwright takes the whole list; the node-lifecycle controller
re-adds its own taints on its next sweep. `--force-conflicts` is deliberately NOT
applied to the add-on path (`ApplyArgs` in `internal/addons/oc/execute.go`), where
manifests are user-authored and clobbering an operator's edit is the worse
outcome.

Pinned by `TestNodeConfigApplySetCoversOnlyManagedNodes`,
`TestNodeConfigManifestsRelinquishesClearedNode`,
`TestNodeConfigManifestsDedupesTaintKeyEffectPairAcrossValues` and the
`TestPruneInfraMachineConfigPool*` set in
`internal/converge/workflow/node_config_test.go`.
