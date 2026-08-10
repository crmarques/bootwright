# ADR 0023: Teardown Is the Inverse of Build-Up

## Status

Accepted

Supersedes the teardown-ordering section of
[ADR 0007](0007-apply-destroy-safety-model.md). The safety model in ADR 0007 is
otherwise unchanged; only the ordering rule and the set of fail-closed edges are
restated here. The managed-storage clauses of A2 and the fail-closed edge set
are revised by
[ADR 0058](0058-storage-destroy-completion-is-positive-proof.md).

## Context

Apply builds a capability graph. `ActivityGraph` resolves `Requires` against
`Provides`, lowers the result to task dependencies, and rejects cycles. Destroy
did none of that: `PlanDestroyTasks` returned a hand-written list of steps whose
order was maintained by hand, and the only graph reasoning left was
`machineInfraDestroyLevels`, which levelled clusters by KubeVirt
`hostClusterRef` and handed the result to Ansible as two extra vars.

That chain was not the inverse of build-up. It ran, in order, storage clusters,
machine registration, storage node access, infra components, machine
infrastructure, container clusters, provider services. Container-cluster
teardown — the inverse of the terminal add-ons phase — was second to last, and
was the only step carrying a hard dependency, on machine infrastructure. Three
consequences followed:

- A single machine-infrastructure failure blocked controller-local cleanup for
  every cluster in the fleet, because neither step was fanned out.
- Infra components were removed while the machines they serve were still being
  torn down, even though apply gives every machines-phase task an explicit
  dependency on those same fabric services.
- The nesting barrier lived in Ansible include-result arrival order rather than
  in the scheduler, so the Go planner could not run independent clusters
  concurrently, and `machineInfraDestroyGraph` missed `StorageCluster` guests
  entirely because it only walked `state.ContainerClusters`.

The Ansible monolith wrapper had already drifted the other way:
`workflow_infra_destroy.yml` ran machine infrastructure before infra components,
the opposite of the Go chain, and nothing checked the two agreed.

## Decision

The destroy graph is derived from the apply graph by reversing it. Where the
reversal is not decidable from the apply graph alone, the residual constraint is
named here as an axiom rather than hidden in a hand-written order.

### Derived order

    T0  cluster runtime (per container cluster)
    T1  storage clusters (per storage cluster, after the runtime of every
        container cluster consuming its export — A1)
    T2  machine registration (per storage cluster)
    T3  storage node access (per storage cluster)
    T4  machine infrastructure (per cluster): a container cluster's task
        follows cluster runtime; a storage cluster's follows that cluster's
        storage, registration, and node-access teardown; a KubeVirt host's
        task hard-depends on each of its guests', so the fan spans nest
        levels 0..D
    T5  container-cluster records; machine-infrastructure records sweep
    T6  infra component services
    T7  provider services

A cluster hosting an infra component — and every transitive KubeVirt host of
one — inverts the T4/T6 order per A3.

Container-cluster teardown splits in two. The runtime half — installer runtime,
add-on runtime, generated add-on secrets — is the inverse of the add-ons and
base phases and is a graph root with no predecessors. The records half —
install record, connection record, captured `kubeconfig` and
`kubeadmin-password` — is the inverse of the credentials apply captured, and
runs last. ADR 0055 moves the controller resolver lifetime to its owning managed
name-resolution service at T6; ContainerCluster teardown never changes it.

### Axioms

- **A1.** A container cluster consuming a `StorageExport` is torn down before
  the storage cluster backing it. The apply graph has no edge between
  `storage.<cluster>` and the container chain, so this cannot be derived.
  Enforcement against an out-of-scope consumer remains the selection-time
  `StorageConsumerDestroyConflicts` guard; the graph edge only orders work
  already in scope.
- **A2.** On-node work — Ceph wipe, RHSM deregistration, node-access revoke —
  precedes deletion of the substrate that hosts it. These steps have no apply
  counterpart. For selected managed storage, the Ceph task's positive
  completion proof is fail-closed: registration cleanup, access revoke, and
  deletion of that cluster's substrate require it; access revoke additionally
  requires registration cleanup. A skipped task with selected work is not
  proof. Other on-node ordering remains skip-tolerant, including the infra-only
  and machine-scoped registration-to-substrate chain. A controller-side
  substrate-release record cannot replace the managed-storage edges: it can
  withhold future apply authorization but cannot undo a machine deletion or
  restore a login the failed run already removed.
- **A3.** Infra components outlive the machines they serve, except for their own
  placement closure. The managed name-resolution component serves the machine
  addresses teardown connects through, its controller split-DNS route remains
  in place through all machine teardown, and the proxy carries RHSM egress, so
  fabric teardown is last. Only the owning name-resolution service removes that
  route; releasing one shared-service reference does not. A cluster hosting an
  infra component, or any
  transitive KubeVirt host of one, is carved out and torn down after infra
  components instead. The closure over ancestors is what makes the carve-out
  provably acyclic.

### Fail-closed edges

Four edge families are fail-closed; every other edge is ordering-only.

- Managed-storage completion before that cluster's registration cleanup,
  node-access revoke, and machine substrate; registration cleanup before that
  cluster's node-access revoke. The storage task is successful only after its
  exact per-node terminal attestation validates, so this edge protects the
  evidence and access an incomplete destroy needs for retry. An authorized
  partial attestation is retained evidence but is not a successful storage
  result: its positively absent nodes remain outside host-local cleanup, its
  retained ownership marker withholds substrate-release authorization, and the
  branch keeps the registration, access, and substrate required by an exact
  retry.

- Guest machine infrastructure before its KubeVirt host's. The KubeVirt
  substrate role fails closed on an unreachable host API, so a failed guest
  teardown must block the host's substrate deletion rather than strand the
  guests.
- Container-cluster records on the whole machine-infrastructure set, not
  per cluster. `kubeVirtHostClustersForRun` derives its host list from the
  entire run state, so every machine-infrastructure task materialises every
  KubeVirt host kubeconfig. Gating on the base ID means no kubeconfig can be
  deleted while any machine teardown is still runnable.
- The terminal machine-infrastructure records sweep on the same set. It must be
  hard, not ordering: `taskTerminal` releases ordering dependencies on `Blocked`
  as well as `Failed`, which would let the sweep bypass the graph's own
  fail-closed edges.

### Fan-out

Storage steps fan per storage cluster, as before. Machine infrastructure and
container-cluster steps now fan per cluster when at least two clusters render an
inventory host group; below that the plan falls back to the single whole-group
task. The fan-out predicate reads the rendered inventory rather than the desired
state, because a group is emitted only for machines that actually have a
provider host — limiting onto a group Ansible never rendered aborts the run, and
a step that matches nothing while recording a positive substrate release is
worse.

Each fanned machine-infrastructure task replaces the run-level
`bootwright_destroy_cluster_scope` with its own cluster and drops
`bootwright_infra_destroy_context_sweep`. That scope is the only gate on the
ownership-record loop, and an unscoped run never sets it, so an ungated fanned
task would delete every other cluster's recorded domains and disks. The sweep
and the reap of records whose cluster is blank or deleted from desired state
move to the terminal records task, which carries no cluster scope.

### Cycles

`destroyChain` runs an explicit Kahn pass over hard and ordering edges together
and fails planning with the cycle members named. The scheduler breaks silently
on `running == 0 && !startedAny`, so an undetected cycle would leave tasks
Pending with no diagnostic and withhold every substrate release.

## Consequences

- A failed cluster blocks only its own dependents. Nested guests, bare-metal
  clusters and Ceph start concurrently.
- The nesting barrier is a scheduler edge, so the Ansible levels vars carry a
  single cluster per fanned task and exist only for the unfanned fallback.
- The monolith `workflow_*_destroy.yml` wrappers are checked to be a topological
  order of the graph the split path generates.
- `--machines` keeps its own two-step unfanned chain.
  `ResetMachineConvergeRecordsAfterDestroy` gates on the bare kind key, which
  fan-out would leave false, silently withholding every substrate-release write.
- `destroyKindIncluded` still treats a successful machine teardown as covering
  container-cluster cleanup. Under the split the records half runs after machine
  teardown, so a successful machine teardown with a failed records task still
  claims coverage and the Go-side `RemoveClusterInstallState` runs anyway. That
  is idempotent controller-local cleanup doing the same work, and is retained
  deliberately as a backstop.
