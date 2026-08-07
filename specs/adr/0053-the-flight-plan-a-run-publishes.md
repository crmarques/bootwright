# ADR 0053: The Flight Plan a Run Publishes

## Status

Accepted

Extends [ADR 0022](0022-cluster-wait-bootstrap-boundary.md) (the bootstrap
boundary that split one wait into two activities) and the run-tree phase
taxonomy landed in `6c46f611`. It changes no capability edge and no task kind.

## Context

Apply already plans a DAG. `ActivityGraph.Lower` turns each activity's
`Requires`/`Provides` into dependencies, so an activity waits for the tasks
that produce what it consumes and for nothing else; the scheduler in
`RunPreparedApplyTaskGraph` is work-conserving, re-scanning every ready task
each pass and dispatching up to `limits.Parallelism`. Two independent container
roots already carry no edge between them, and a `ceph -> ocp` edge exists only
where `stepCrossClusterDependencies` resolves a declared Data Foundation input.

Three things are missing around that DAG, and all three are the same missing
thing: the graph's *structure* is never written down.

**The steps are re-derived, not planned.** The run tree and `status --watch`
share one mapper, `applyRunFrame`, which reconstructs step rows from
`Entry.Kind` and cluster lanes from `Entry.Cluster`. The rows are required to
be true DAG strata — *a row may only hold kinds that are graph ancestors of
every kind in the row below it, within one cluster group* — but nothing in the
lowering enforces that. It is held by a Kind-to-label table and a fitness test,
which is why a `deps`-stage `virtctl` task that requires its host's
`cluster.installed` capability once printed in the host's *first* row while
being the last task in its subtree.

**Nothing fails when parallelism is lost.** An over-broad edge — one added for
a reason that no longer holds, or copied from a neighbouring planner — silently
serializes two clusters that should fly side by side. The plan stays valid, the
tests stay green, and the only symptom is wall clock.

Two edges were reviewed as suspected over-approximations and both proved
correctly scoped; they are recorded here so the next reader does not re-open
them. `exclusiveOLMOwner` keys only on add-ons that *create* the shared object —
`Namespace.Create || OperatorGroup != nil` for a namespace, a declared
`CatalogSource` for a catalog — so add-ons that merely subscribe into an
existing namespace already fly together, and removing the chain would race two
creators of the same object. `phaseTaskIDsInScope` already restricts a
CustomPlaybook's anchor to the playbook's own resolved target clusters and
widens to the fleet only for a genuinely fleet-wide playbook.

**Add-on logs sit outside their cluster.** `TaskLogPath` puts every task's
output at `history/<run>/tasks/<taskID>/output.log`, beside every other task of
every other cluster, while the per-cluster narration lives at
`history/<run>/bootwright-<cluster>.log`. An operator reading one cluster's
install has to know task-ID grammar to find the add-on that failed inside it.

## Decision

**The plan is lowered into a `FlightPlan` and published, and every consumer
reads it instead of re-deriving it.**

`graph.Lower` gains a companion that computes, from the lowered dependency
edges alone:

- **stages** — the DAG's strata, so stage *n* holds exactly the activities
  whose longest path from a root is *n*. Two activities in one stage are
  provably independent; an activity in stage *n+1* provably has an ancestor in
  stage *n*. The invariant the phase rows assert becomes a property of the
  computation rather than a rule reviewers must keep.
- **lanes** — one per cluster, plus one leading non-cluster lane for fabric and
  infra work that owns no cluster, ordered by `orderClusterNames` so a KubeVirt
  child follows its host.
- **steps** — the existing high-level step labels, now carried on the plan
  rather than recomputed from `Entry.Kind`.

The `FlightPlan` is written to `history/<run>/flight-plan.json` beside the
ledger, rendered by `bootwright apply --plan` before anything runs, and read by
`applyRunFrame` so the live tree and `status --watch` display the stages that
were planned rather than a reconstruction of them.

**Independence is a contract with a test.** A fitness test lowers the reference
`bootwright-template-inputs` fleet and asserts that the independent container
roots share a stage, that every `ceph -> ocp` edge traces to a declared input,
and that add-ons without a `requires` relationship share a stage. An edge added
without a declared reason fails the build.

**No edge is removed.** The review found no over-approximation to narrow; the
guard above is what keeps that true as the planners grow.

**Logs nest under the cluster they belong to.** A run writes
`history/<run>/clusters/<cluster>/cluster.log` for the cluster narration,
`.../steps/<step>.log` per step, and `.../addons/<addon>.log` per add-on. The
run log keeps the cross-cluster narration and the initiated/finished markers.

**Grouped `TASK` output stays scoped to where one process owns the machines.**
Ansible can only print

```text
TASK [...] ***
ok: [machine-01]
ok: [machine-02]
```

when those machines share one `ansible-playbook` process under the `linear`
strategy. That holds today for KubeVirt (one task per `(cluster, host)` with
`Forks: len(group)`), for bare-metal OS install, and for node boot (one task
per cluster over every machine, `linear` since the `free` strategy was retired
fleet-wide). It is now pinned by a fitness test. libvirt and vSphere keep one
task per machine: their per-hypervisor and per-vCenter slot budgets are charged
per task, and collapsing them would clamp a nine-VM cluster to
`ParallelismPerHost` while `hostSlotDispatchFloor` silently unbounds the very
budget that protects the hypervisor.

## Consequences

- The step rows an operator reads are the strata the scheduler will actually
  fly. A row can no longer imply an ordering the graph does not have.
- `bootwright apply --plan` answers "what will run in parallel" before the run,
  from the same value the run publishes — not a second implementation.
- No fleet gets shorter from this ADR by itself: the parallelism was already
  there. What changes is that losing it now fails a test instead of only
  showing up as wall clock.
- The log tree moves. `TaskLogPath` keeps its shape for non-cluster tasks;
  cluster-owned tasks move under `clusters/<cluster>/`, so anything that
  hardcoded the flat path (docs, the `logs` command, `status`) is updated with
  it.
- `flight-plan.json` is a new per-run artifact. It carries no secret material —
  task IDs, kinds, labels, cluster names and edges only.
