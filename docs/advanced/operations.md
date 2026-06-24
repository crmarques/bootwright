---
title: Operations and Recovery
description: Destroy stages and full-context teardown, the destroyProtection requiredOverride safety, the fail-closed apply --override pattern, focused --stage/--clusters recovery, managed-OS reinstall and owned-Ceph rebuild, and where run logs, the ledger, and leases live.
---

# Operations and recovery

Day-2 work on an applied context is teardown and recovery, not re-authoring.
This page covers tearing a platform down by stage or in full, the
`destroyProtection` safety gate, the fail-closed `apply --override` pattern,
recovering a single component, and where Bootwright keeps the run logs, ledger,
and leases that make all of this resumable.

For the user-facing apply, stage, and convergence model — bare `apply`,
`--expect-new`, and `--override` as modes — see [Concepts](../concepts.md). For
the execution pipeline, locking, and the four-outcome classifier in depth, see
[Architecture](../concepts/architecture.md). This page is the how-to and does
not restate that model.

## Tearing down with `destroy`

`destroy` tears down a previously applied target for the current context, using
the current desired state plus the root-managed ownership records under
`/var/lib/bootwright/contexts/<context>/ownership/`. Those records let it remove
resources Bootwright created or configured, including ones no longer present in
the input YAML.

`destroy` accepts only two stage values:

| Invocation | Tears down |
| --- | --- |
| `destroy --stage clusters` | Cluster-stage runtime for selected or all `ContainerCluster` and `StorageCluster` names: OpenShift install runtime, add-on records, generated storage attachment records, and managed storage cluster services and runtime. Does not touch provider infrastructure. |
| `destroy --stage infra` | Provider, infra-component, machine-infra, and storage-infra state. Without `--clusters` it also sweeps every context-owned VM that provider adapters can identify. |
| `destroy` (no `--stage`) | The whole context: clusters teardown then infra teardown as one ordered graph (the reverse of apply order), plus the same context-wide VM and orphan-ownership sweep as unscoped `destroy --stage infra`. |

!!! note "The no-stage default is full teardown"
    Omitting `--stage` tears down the **whole context**, clusters first and then
    the infrastructure they ran on. This is the no-selector form. Adding
    `--clusters` with no `--stage` narrows to `destroy --stage clusters` — the
    full whole-context teardown only happens when no selector is given.

```text
# Full context teardown (clusters, then infra), with confirmation prompt.
bootwright destroy

# Tear down only the cluster stage; leave provider infrastructure standing.
bootwright destroy --stage clusters

# Tear down infrastructure including the context-wide VM sweep.
bootwright destroy --stage infra
```

`destroy --dry-run` renders and prints the ordered teardown commands without
executing; `--output json` is accepted only with `--dry-run` and reports the
ordered task chain. `--yes` skips the confirmation prompt and nothing more.

### Bounded, ownership-gated cleanup

`destroy` is deliberately conservative about what it removes:

- **Disks.** Managed-machine disk cleanup is limited to provider-owned disks or
  devices you declared as Bootwright-managed. Bootwright never wipes arbitrary
  visible disks.
- **Host packages.** A package is removed only when ownership records prove
  Bootwright installed it *and* no remaining ownership record on that host still
  requires it.
- **VM sweep scope.** The context-wide VM sweep runs only for the no-selector
  full teardown and unscoped `destroy --stage infra`. With `--clusters`, infra
  teardown is limited to the selected roots and runs no context-wide VM cleanup.

!!! warning "Scoped infra destroy refuses shared-service conflicts"
    A scoped `destroy --stage infra --clusters …` refuses to proceed when the
    selected clusters share a provider service with clusters left out of the
    selection, rather than tearing down state another cluster still depends on.
    Widen or narrow the selection so the shared service is unambiguous.

### Removing only the artifact server

`destroy --stage infra --clusters` accepts one special literal token,
`artifact-server`, which removes only the generated artifact publication service
and leaves the rest of the infrastructure standing:

```text
bootwright destroy --stage infra --clusters artifact-server
```

## Validating only the selected scope

By default `apply` and `destroy` validate the **whole** input before doing any
work, so a desired-state error anywhere — even in a cluster you did not select —
blocks the run. When you are deliberately acting on part of the workspace,
`--scoped-validation` narrows that check to the resources the
`--clusters`/`--stage` selection will actually touch:

```text
bootwright apply --clusters ocp1,ocp2 --scoped-validation
```

With the flag set, a desired-state error in an out-of-scope object — for example
a broken `StorageCluster` you are not applying — no longer blocks the scoped run.
It has no effect without a narrowing selector: a run over the whole graph still
validates everything. Dependencies are still validated — if a selected cluster
pulls another object in transitively (such as a Data Foundation storage
attachment), that object stays in scope and is validated with it.

## Destroy protection

Set `Environment.spec.safety.destroyProtection: requiredOverride` to guard a
context against accidental teardown. The field accepts `allow` or
`requiredOverride`; empty means `allow`, and protection is **never** inferred
from environment, context, label, or cluster names.

When the selected state sets `requiredOverride`, `destroy --stage infra` and
`destroy --stage clusters` both require `--override` to proceed:

```text
bootwright destroy --stage clusters --override
```

!!! warning "`--yes` does not imply `--override`"
    `--yes` skips only the confirmation prompt; it never implies `--override`.
    On a protected context a `destroy` without `--override` fails closed,
    regardless of `--yes`.

## Force-destroying renamed or unmarked machines

Each machine substrate (libvirt, KubeVirt, vSphere) refuses to tear down a
VM that matches the Bootwright `<cluster>-<machine>` naming but carries no
ownership marker for **this** context, cluster, and machine — a foreign VM that
merely shares the name must never be destroyed. The same guard fires when you
rename a machine or cluster after applying: the live VM still carries the *old*
marker, so the teardown no longer recognizes it as its own and stops with
"it carries no Bootwright ownership marker for this context/cluster/machine".

`--force-unowned` is the recovery path: it tells the machine-substrate teardown
to remove a matching VM despite a missing or mismatched marker.

```text
bootwright destroy --clusters ceph-storage --force-unowned --yes
```

!!! warning "`--force-unowned` is scoped to machine VMs"
    It relaxes only the libvirt/KubeVirt/vSphere per-VM ownership-marker
    refusals. It does **not** relax the Ceph cluster or OSD-device ownership
    gates, and it never relaxes the device data-safety checks (a mounted,
    in-use, or unprobeable device still fails closed). It is independent of
    `--override` (protected-environment teardown) and does not imply `--yes`.
    Because it removes a VM Bootwright cannot positively confirm it owns,
    confirm the target VM is yours before using it.

## The fail-closed `apply --override` pattern

`apply --override` authorizes Bootwright-owned destructive *rebuilds* — drifted
owned objects, a managed-OS machine reinstall, an owned-Ceph wipe-and-rebuild.
It is command-scoped and bypasses only safe-mismatch gates that have an explicit
override path. It never bypasses active-run leases, validation, secret checks,
or foreign-resource ownership failures, and it never touches a resource a
non-Bootwright owner holds.

The fail-closed interaction with destroy protection is the part operators most
often miss:

!!! warning "`apply --override` is rejected on a protected context"
    When the selected state sets `destroyProtection: requiredOverride`,
    `apply --override` **fails closed before any mutation** instead of rebuilding
    the protected resources. Destruction of protected state must cross the
    destroy authorization boundary. (`--dry-run` still previews the override
    plan.)

So there are two distinct rebuild paths, and which one you use depends on
whether the context is protected:

- **Protected context** — rebuild crosses the destroy boundary. Run
  `destroy --override` for the affected scope, then re-apply:

  ```text
  bootwright destroy --stage clusters --clusters ocp-3node --override
  bootwright apply --stage clusters --clusters ocp-3node --yes
  ```

- **Unprotected context** — a single `apply --override` performs the
  Bootwright-owned destructive rebuild in place:

  ```text
  bootwright apply --clusters ocp-3node --override
  ```

!!! note "`--expect-new` and `--override` are mutually exclusive"
    `--expect-new` asserts greenfield (fail if any selected object exists);
    `--override` authorizes rebuilding objects that already exist. They express
    opposite intents and cannot be combined.

## Focused recovery of one component

Recovery does not require touching the whole graph. Combine `--stage` and
`--clusters` to converge a single component for build-out, recovery, or
maintenance.

- **One cluster root.** `--clusters` takes a comma-separated list of
  `ContainerCluster` and `StorageCluster` names. The two kinds share one
  selection namespace, so a bare name must be unique across both and selects
  exactly one cluster root.

  ```text
  bootwright apply --clusters ceph-stretch --yes
  ```

- **One stage.** Narrow to a stage family when only that layer needs work:

  ```text
  bootwright apply --stage infra --yes
  ```

- **A surgical sub-phase rerun.** `apply` and `plan` additionally accept the
  single-phase selectors `fabric`, `machines`, `deps`, `base`, and `addons` for
  surgical reruns within a family. These are reruns, not peers of the `infra`
  and `clusters` families; `destroy` does not accept them.

!!! note "Check before you rebuild"
    `bootwright state-check` reports which roots are `missing`, `match`,
    `drift`, or `foreign` against the last recorded apply, without contacting
    hosts. `bootwright plan` (or `apply --dry-run`) shows the task graph a
    selection would run. Use them to confirm the scope before applying. Note
    that `state-check` compares against *recorded* evidence only — an out-of-band
    change (a wiped disk, an undefined VM, a deleted namespace) is not detected
    until the next apply refreshes the record. `state-check --override` is
    rejected.

### KubeVirt child clusters do not auto-include their parent

A scoped apply of a KubeVirt-backed child `ContainerCluster` that references a
Bootwright-managed virtualization cluster does **not** expand the selection to
install the parent. It fails before mutation unless the parent is selected too,
or local runtime records already prove the parent install and its `kubevirt`
add-on are ready. Select both roots together when the parent is not yet
installed.

## Managed-OS reinstall and owned-Ceph rebuild

Two destructive rebuilds run through `apply --override` and are gated by
Bootwright ownership markers, so they apply only to resources Bootwright owns.

- **Managed-OS machine reinstall.** `apply --override` bypasses the
  skip-if-already-installed check, undefines the substrate VM, wipes its disks,
  and rebuilds the machine from its `Machine`, `MachineImage`, and
  `MachineInstallProfile` desired state. (FIPS and other install-time
  customizations are reinstall-only by nature, so this is the path to change
  them on an installed machine.)

- **Owned-Ceph cluster rebuild.** `apply --override` cleanly rebuilds a managed
  Ceph cluster with `cephadm rm-cluster --zap-osds`, but **only** when a
  Bootwright ownership marker proves the live cluster is the one Bootwright
  created. A foreign or co-resident cluster fails closed.

!!! note "Override rebuilds still-declared structure; it does not prune"
    Ceph convergence is additive-only across the whole storage domain. `apply`
    never removes a live Ceph object whose declaration was deleted, and
    `--override` does not prune undeclared objects either — it rebuilds only
    still-declared pools whose structural identity (pool `type` or erasure
    profile) changed. Remove undeclared Ceph objects on the cluster out of band.
    See [Ceph Storage Clusters](storage-ceph.md) for the full convergence
    contract.

## Where run logs, the ledger, and leases live

Mutating runs write their state under the root-managed context tree at
`/var/lib/bootwright/contexts/<context>/runs/`. Read-only verbs never write
there.

| Path under `…/contexts/<context>/` | Holds |
| --- | --- |
| `runs/` | Apply and destroy logs in restrictive modes, the durable run ledger, and the short-lived local lease for the updating process. |
| `runs/history/<run-id>/bootwright.log` | The shared apply flow log: shared-stage tool output plus one `<cluster> apply initiated … / finished successfully \| failed` marker per cluster that points at its split-out log. |
| `runs/history/<run-id>/bootwright-<cluster>.log` | One cluster's apply flow log, split out of the shared log so each cluster's output reads on its own and the shared log stays a legible index. |
| `runs/history/<run-id>/input/` | The forensic snapshot of the input YAML an `apply` loaded, written at the start of the mutating run. |
| `runs/last-destroy-input/` | The forensic snapshot of the input a `destroy` loaded. |
| `runs/safety/` | Convergence-safety records (the non-secret desired hash plus Bootwright owner identity) that `state-check` classifies against. |
| `ownership/` | Root-managed non-secret JSON ownership records used to scope destroy, gate host package removal, and report orphans. |
| `clusters/<cluster>/runtime/install-record.json` | Per-cluster install record with the non-secret desired-input fingerprint that install reconcile reads. |

A run records a durable ledger entry plus a short-lived lease that marks the
updating process; cluster install tasks additionally record per-cluster install
state. The forensic input snapshots capture what was applied — nothing reads
them back, and `plan` / `--dry-run` never write them.

!!! note "Commands re-exec through `sudo` for context state"
    The context tree under `/var/lib/bootwright/` is root-managed. Commands that
    need to read or write it re-exec through `sudo` when run as a non-root user;
    `apply` always does, and `destroy` does when it selects a rootful target.

## See also

- [Concepts](../concepts.md) — apply stages and the apply-mode model
  (reconcile, `--expect-new`, `--override`).
- [Architecture](../concepts/architecture.md) — the execution pipeline,
  resource locking, and the four-outcome classifier in depth.
- [Ceph Storage Clusters](storage-ceph.md) — additive-only convergence and the
  owned-Ceph rebuild details.
- [Troubleshooting](../troubleshooting.md) — what to do when a run fails closed.
