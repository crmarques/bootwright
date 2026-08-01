---
title: Operations and Recovery
description: How to recover, rerun, and tear down an applied context safely — the apply modes, the destroy stages and their authorization gates, and the run observability that shows what a run did.
---

# Operations and recovery

Day-2 work on an applied context is teardown and recovery, not re-authoring.
This page covers the three apply modes, the `--authorize` risk vocabulary,
tearing a platform down by stage or in full, the `destroyProtection` safety gate
and the destroy-authorization boundary, recovering a single component, and the
destructive rebuilds Bootwright performs on resources it owns.

For the user-facing apply, stage, and convergence model — the `--mode` values
and the stage families and sub-phases — see
[The desired-state model](../concepts/index.md). For the
execution pipeline, locking, and the four-outcome classifier in depth, see
[Architecture](../contributing/architecture.md). This page is the how-to and
does not restate those models.

## The two axes: intent and authorization

Every destructive decision on `apply`, `plan` and `destroy` is one of two
orthogonal choices.

**`--mode create|reconcile|rebuild`** (on `apply`/`plan`) states *intent*:
`create` asserts a greenfield run and fails if any selected object already
exists; `reconcile` (the default) creates what is missing, skips what matches,
and fails closed on drift; `rebuild` is break-glass recovery for
Bootwright-owned drift it knows how to rebuild — managed-OS reinstall,
owned-Ceph wipe-and-rebuild, drifted owned-object rebuild. See
[Apply modes](../concepts/index.md#apply-modes) for the full model.

**`--authorize <token>`** (on `apply`, `plan` and `destroy`, repeatable and
comma-separated) states which *risk* you accept. Every token but `all` unblocks
exactly one refusal and nothing else:

| token | authorizes |
| --- | --- |
| `all` | every other token the command accepts, in one word — the apply-side tokens on `apply`/`plan`, the whole table on `destroy`. It never clears a refusal that has no token of its own, and never answers a confirmation prompt |
| `data-loss` | any disk wipe or Ceph OSD zap, on `apply` and on `destroy` |
| `protected` | acting on state whose Environment sets `destroyProtection: protected` or lists the kind in `protectedKinds` |
| `installed-cluster-node` | `destroy --machines` naming a node of an installed cluster — a `ContainerCluster` with an install record or a provisioned managed `StorageCluster` |
| `unowned-vms` | tearing down VMs that match the Bootwright naming but carry no ownership marker |
| `unowned-networks` | removing an unowned libvirt network or KubeVirt DataVolume, which may still be in use by another context |
| `unowned-devices` | wiping a declared OSD device that carries data signatures or LVM/dm-crypt holders while the node holds no Bootwright OSD ownership record for it — on `apply` under `--reclaim-devices`, and on `destroy` |
| `foreign-daemons` | removing another Ceph cluster's cephadm daemons, units and `/var/lib/ceph` state from a storage node this apply enrolls — fsid-scoped, zaps no disk, `apply` only |
| `unreachable-nodes` | skipping nodes that do not answer at all during teardown, leaving the cluster partially destroyed — never a node that answers SSH and then rejects its teardown identity |
| `unreadable-records` | proceeding when ownership records cannot be read |
| `shared-infra` | storage-consumer conflicts and infra components owned or referenced by another context |
| `stale-input` | planning a teardown from input whose documents no longer decode or validate against this build, skipping exactly those documents |

An unknown token is a usage error listing the tokens the command accepts. So is a
token the command has no gate for: `all`, `data-loss` and `unowned-devices` are
accepted by both verbs, `foreign-daemons` by `apply` alone, and every other token
is destroy-only,
and `apply --authorize protected` is refused with the guidance that resolves it on
`apply` rather than accepted and ignored. A token the command accepts but this run
never used prints a warning naming it, so you learn you authorized the wrong risk
instead of assuming a gate was cleared.

!!! warning "`--authorize all` is a blast radius, not a shortcut"
    `all` is the one token that stands for others. Reach for it in a lab you
    intend to flatten, or when a teardown keeps surfacing one more refusal and
    you have already read the ones before it — not as a habit on state you care
    about, where naming the risk is the point. It is bounded in three ways: it
    expands only to the tokens the invoked verb accepts, so `apply --authorize
    all` never gains a `destroy` token; it clears no refusal that has no token
    (scope conflicts, the KubeVirt tenant gate, a mounted or in-use device, a
    `protectedKinds` rebuild on `apply`); and it never answers a confirmation
    prompt — `--yes` still does that, separately. A real run that used it prints
    which tokens it stood in for, so the blast radius stays on the record.

!!! warning "`--yes` authorizes nothing"
    `--yes` answers the ordinary confirmation prompt on either verb and never
    authorizes data loss or any other named risk. A `destroy` that would destroy
    a managed Ceph cluster's OSD data needs `--authorize data-loss` (or the
    interactive data-loss prompt when you omit `--yes`), exactly as `apply` has
    always required. That covers both ways the data dies: the clusters stage
    running `cephadm rm-cluster --zap-osds`, and the machine layer
    (`--stage infra`, `--machines`, or a full teardown) deleting the
    libvirt/KubeVirt/vSphere machines whose disks hold the OSDs. A Ceph cluster on
    bare metal keeps its disks through a machine-layer teardown, so that case
    needs no token — the teardown preview tells you which of the two you have.

!!! tip "Growing a Ceph cluster's OSDs is a plain `apply`, not `--mode rebuild`"
    Adding an OSD device to a `spec.ceph.topology` node reconciles **in place**:
    a bare `apply` classifies an OSD-device-only change as reconcilable (not
    destructive) drift, so `ceph orch apply` adds the new OSD without a
    wipe-and-rebuild. `--mode rebuild` is only for a change to cluster identity
    (seedHost/monIP/network), which is a genuine rebuild. Because cephadm never
    auto-removes an OSD (removal must drain data first), **removing** a device
    that still hosts an OSD is refused with guidance to drain it —
    `cephadm shell -- ceph orch osd rm <id>` — before removing it from the spec.

### The fail-closed `--mode rebuild` contract

`apply --mode rebuild` authorizes Bootwright-owned destructive *rebuilds* — drifted
owned objects, a managed-OS machine reinstall, an owned-Ceph wipe-and-rebuild.
It never bypasses active-run leases, validation, secret checks, or
foreign-resource ownership failures, and it never touches a resource a
non-Bootwright owner holds.

`--mode rebuild` authorizes the rebuild; a **separate** token authorizes the
*data loss* it entails. When a run would destroy data, it stops at an
interactive prompt — `Confirm this DESTRUCTIVE action (accept data loss)?` — and
proceeds only on `y`. For automation, pass `--authorize data-loss` alongside
`--yes`; `--yes` on its own answers only the ordinary confirmation and **never**
authorizes data loss, so a destructive rebuild under `--yes` without
`--authorize data-loss` fails closed. The same token authorizes the two storage
data-loss operations: the `all: true` OSD auto-reclaim that zaps dirty declared
disks before an OSD apply, and an explicit `--reclaim-devices` run (below).
`--authorize data-loss` warns that it had no effect on a run that plans no
data-loss action.

A failed probe is not an authorization. If a cluster whose recorded install
inputs match desired state cannot be probed at all — the API is unreachable, the
kubeconfig is unusable, `oc` is missing — `--mode rebuild` **fails closed**
naming that cluster instead of scheduling a reinstall, because unknown state is
not evidence of a broken cluster. Restore reachability and re-run, exclude it
with `--clusters`, or tear it down deliberately with `destroy --clusters <name>`
and re-apply.

The fail-closed interaction with destroy protection is the part operators most
often miss:

!!! warning "`apply --mode rebuild` is rejected on a protected context"
    When the selected state sets `destroyProtection: protected`,
    `apply --mode rebuild` **fails closed before any mutation** instead of rebuilding
    the protected resources. Destruction of protected state must cross the
    destroy authorization boundary. (`--dry-run` still previews the rebuild
    plan.)

    The single exception is `--reclaim-devices`: on a protected context it
    *requires* `--authorize data-loss` as the protected-data-loss authorization,
    because routing a single-disk reclaim through `destroy` would demand
    destroying the whole cluster. See
    [Reclaiming OSD disks](#managed-os-reinstall-and-owned-ceph-rebuild).

So there are two distinct rebuild paths, and which one you use depends on
whether the context is protected:

- **Protected context** — rebuild crosses the destroy boundary. Which `destroy`
  depends on what the rebuild would touch, because machine substrate is torn
  down by the infra stage and never by the clusters stage. Run the destroy the
  refusal names, then re-apply:

  ```text
  # A cluster rebuild
  bootwright destroy --clusters ocp-3node --authorize protected

  # A machine or managed-OS rebuild
  bootwright destroy --stage infra --clusters ocp-3node --authorize protected

  # A scope covering both: the infra destroy first, then the clusters destroy
  bootwright destroy --stage infra --clusters ocp-3node --authorize protected
  bootwright destroy --clusters ocp-3node --authorize protected

  # Then re-apply
  bootwright apply --clusters ocp-3node --yes
  ```

  A rebuild routed to the wrong stage loops: a `--stage clusters` destroy never
  reaches the machine substrate, so the next apply finds the same drift. Add
  `--authorize unreachable-nodes` when a machine's host substrate was never
  provisioned or is powered off — the refusal prints that hint too.

- **Unprotected context** — a single `apply --mode rebuild` performs the
  Bootwright-owned destructive rebuild in place:

  ```text
  bootwright apply --clusters ocp-3node --mode rebuild
  ```

## Tearing down with `destroy`

`destroy` tears down a previously applied target for the current context, using
the current desired state plus the root-managed ownership records under
`/var/lib/bootwright/contexts/<context>/ownership/`. Those records let it remove
resources Bootwright created or configured, including ones no longer present in
the input YAML.

`destroy` accepts only two stage values — the two stage families. The
single-phase sub-phases (`fabric`, `machines`, `deps`, `base`, `add-ons`) are
apply/plan reruns only; `destroy` does not accept them.

| Invocation | Tears down |
| --- | --- |
| `destroy --stage clusters` | Cluster-stage runtime for selected or all `ContainerCluster` and `StorageCluster` names: OpenShift install runtime, add-on records, generated storage attachment records, and managed storage cluster services and runtime. Does not touch provider infrastructure. |
| `destroy --stage infra` | Provider, infra-component, machine-infra, and storage-infra state. Without `--clusters` it also sweeps every context-owned VM that provider adapters can identify. |
| `destroy` (no `--stage`) | Full lifecycle for the work set, as the inverse of build-up: cluster installer and add-on runtime together with storage runtime, then registration and node access, then machine infrastructure with guests before their KubeVirt hosts, then cluster records, then exclusively owned infra-component and provider services. With no selector it covers the context and also runs the context-wide VM and orphan-ownership sweep; with `--clusters` it is limited to those roots. |

!!! note "The no-stage default is full teardown"
    Omitting `--stage` tears down the full lifecycle of the work set. With no
    selector that is the whole context. Adding `--clusters` narrows the same
    lifecycle to those roots: positively owned libvirt, vSphere, and KubeVirt
    machines are deleted, while bare-metal hardware and its installed OS are
    retained. Use explicit `--stage clusters --clusters <names>` to keep virtual
    machines standing.

```text
# Full context lifecycle teardown, with confirmation prompt.
bootwright destroy

# Full teardown of one cluster, including its owned virtual machines.
bootwright destroy --clusters child-ocp

# Tear down only the cluster stage; leave provider infrastructure standing.
bootwright destroy --stage clusters

# Tear down infrastructure including the context-wide VM sweep.
bootwright destroy --stage infra
```

`destroy --dry-run` renders and prints the ordered teardown commands without
executing; `--output json` is accepted only with `--dry-run` and reports the
ordered task chain. `--yes` answers the ordinary confirmation prompt and nothing
more: when the teardown destroys a managed storage cluster's OSD data, it needs
`--authorize data-loss` on top. Without it, an interactive run gets the data-loss
prompt (`Confirm this DESTRUCTIVE action (accept data loss)?`) and a `--yes` run
fails closed naming the token — the same contract `apply` enforces. The gate
follows the data rather than the stage: the clusters stage runs
`cephadm rm-cluster --zap-osds`, and `--stage infra` (or `--machines`) deletes the
libvirt/KubeVirt/vSphere machines whose disks hold those OSDs, so both cross it.
A Ceph cluster whose OSD hosts are bare metal keeps its disks through the machine
layer — the reinstall on a later `apply` is where those disks go, and it crosses
`apply`'s own data-loss gate.

### Bounded, ownership-gated cleanup

`destroy` is deliberately conservative about what it removes:

- **Disks.** Managed-machine disk cleanup is limited to provider-owned disks or
  devices you declared as Bootwright-managed. Bootwright never wipes arbitrary
  visible disks.

    A Ceph OSD host that declares its devices through a *filter* — `data_devices.all`,
    or a `model`/`size`/`rotational` selection — names no device path, so the
    declared-device wipe covers none of its disks. On an `all`-devices host the
    teardown therefore also reclaims the disks that carry a **Ceph signature**:
    a `ceph_bluestore` filesystem, or an LVM physical volume in a `ceph-*` volume
    group. The root disk, any disk mounted anywhere in its tree, and any disk with
    no Ceph signature are left untouched, and a host whose local disk scan fails
    stops the teardown rather than guessing. Without this the bluestore signatures
    outlive the cluster and the next `apply` finds every device unavailable to
    ceph-volume — a zero-OSD install that only
    `apply --mode rebuild --authorize data-loss` can clear.
- **Host packages.** A package is removed only when ownership records prove
  Bootwright installed it *and* no remaining ownership record on that host still
  requires it.
- **VM scope.** The context-wide VM sweep runs only for unscoped full teardown
  and unscoped `destroy --stage infra`. With `--clusters`, full-lifecycle or
  infra teardown deletes only positively owned machines of the selected roots
  and runs no context-wide cleanup.

!!! warning "Scoped infra destroy refuses shared-service conflicts"
    A scoped full-lifecycle destroy or `destroy --stage infra --clusters …`
    refuses to proceed when the selected clusters share a provider service with
    clusters left out of the selection, rather than tearing down state another
    cluster still depends on. Widen or narrow the selection so the shared
    service is unambiguous.

### Removing only the artifact server

`destroy --stage infra --clusters` accepts one special literal token,
`artifact-server`, which removes only the generated artifact publication service
and leaves the rest of the infrastructure standing:

```text
bootwright destroy --stage infra --clusters artifact-server
```

### Leaving no trace of a destroyed component

Clearing those records is part of the teardown, not bookkeeping after it: if a
`destroy` finishes its remote work but cannot remove a converge or install
record — or cannot write the substrate-release record that authorizes the
rebuild — it reports each problem and **exits non-zero**. A record that outlives
its resource makes the next `apply` read it as already converged and skip
re-provisioning. Remove the reported files, or re-run the same `destroy`, before
applying again.

By default `destroy` clears the runtime records it needs to for correctness —
converge-safety records, install/connection records, kubeconfig — but leaves
the rest of a destroyed component's history on disk: its installer working
directory (`install-config.yaml`/`agent-config.yaml`, rendered manifests, the
`openshift-install` log) and its per-run task and flow logs under
`runs/history/`. What it never leaves is an empty skeleton: a directory under
`clusters/<name>/` or `provider-state/` that the teardown emptied is removed
along with its contents, down to `clusters/<name>` itself when nothing of the
component remains. A directory that still holds state is always kept.

Add `--purge-history` to also delete the retained history once a component's
teardown actually succeeds:

```text
bootwright destroy --clusters ocp-3node --purge-history --yes
bootwright destroy --machines ceph-osd-1 --purge-history --authorize installed-cluster-node
```

`--purge-history` removes the destroyed component's whole state tree under
`clusters/<name>/` — the installer working directory, install and connection
records, kubeconfig, and captured cluster secrets such as a Ceph
`dashboard-password` — for every kind of cluster it tore down, `StorageCluster`
and `ContainerCluster` alike.

It is scoped identically to `--clusters`/`--machines` — the whole context on an
unscoped `destroy` — and never reaches a component outside that scope. A
`--stage clusters` teardown leaves the machine layer standing, so it keeps that
layer's provider state (`clusters/<name>/runtime/provider-state/`) and purges
only the cluster's own history. A cluster left partially destroyed by
`--authorize unreachable-nodes` keeps its history and its state tree so you can
still diagnose and retry; a run that also covers a still-live component keeps
its shared ledger and run log, pruning only the purged component's own task
directories and log. It leaves the destroy authorization trail (the
substrate-release record that lets a later `apply` reinstall the same name) and
unrelated context state (the ownership store, input-history rollback snapshots)
untouched, and is **rejected** with the artifact-server literal
(`--stage infra --clusters artifact-server`), which has no per-component
history — drop the flag.

!!! warning "Not recoverable"
    Purged history is gone for good — there is no undo. Skip `--purge-history`
    if you might need the installer inputs or run logs for a post-mortem.

## Destroy protection and the authorization boundary

Set `Environment.spec.safety.destroyProtection: protected` to guard a
context against accidental teardown. The field accepts `allow` or
`protected`; empty means `allow`, and protection is **never** inferred
from environment, context, label, or cluster names.

When the selected state sets `protected`, `destroy` requires
`--authorize protected` to proceed — with or without `--stage`, so the unscoped
full-lifecycle teardown is covered first, as are `--stage infra` and
`--stage clusters`:

```text
bootwright destroy --authorize protected
bootwright destroy --stage clusters --authorize protected
```

This is the **destroy authorization boundary**: destruction of protected state
can be authorized only through `destroy --authorize protected`, never through
`apply --mode rebuild` (which fails closed on a protected context, as above).

### Protecting specific kinds

`destroyProtection` guards the **whole** context. To protect only certain kinds —
leaving the rest at `allow` — list them in
`Environment.spec.safety.protectedKinds[]`. The accepted kinds are
`ContainerCluster`, `StorageCluster`, and `Machine`:

```yaml
spec:
  safety:
    protectedKinds:
      - StorageCluster
      - Machine
```

A listed kind crosses the same destroy authorization boundary as
`destroyProtection: protected`, but scoped to that kind. Even when
`destroyProtection` is `allow`, a `destroy` whose scope tears down a protected
kind requires `--authorize protected`, and an `apply --mode rebuild` that would
destructively rebuild one fails closed and directs you to
`destroy --authorize protected` for that scope first. Reconfigure-only drift — an in-place service re-apply that touches
no data, OS, or VM — does not trip it; only destructive rebuilds of the protected
kind do. `destroyProtection` and `protectedKinds` combine: a resource is
protected if either covers it.

!!! warning "`--yes` authorizes no token"
    `--yes` answers only the confirmation prompt; it never implies any
    `--authorize` token. On a protected context a `destroy` without
    `--authorize protected` fails closed, regardless of `--yes`.

## Whole-input validation

`apply` and `destroy` validate the **whole** input before doing any work, even
when you narrow the run with `--clusters`/`--stage`, so a desired-state error
anywhere — including in a cluster you did not select — blocks the run. Fix the
offending object (the error names it) before retrying the scoped run.

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

- **A range of stages with `--stage` and `--through`.** `--stage` names the
  first stage of a run and `--through` names the last; together they run every
  phase in that inclusive range. `--stage` alone runs exactly what it names — a
  sub-phase name runs that one phase, a family name runs that family's phases;
  `--through` alone runs from the very beginning up to and including the named
  phase (a cumulative build-out); `--through end` runs through to the last phase.
  `--through` — and the sub-phase names — are accepted by `apply`, `plan`, and
  `diff`; `destroy` accepts `--stage` with the two family values only. A family
  name used as a range endpoint resolves to that family's boundary sub-phase: as
  `--stage` it starts at the family's first sub-phase, as `--through` it ends at
  the family's last — so `--through infra` equals `--through machines`,
  `--stage clusters` starts at `deps`, and `--through clusters` is the full
  graph. A range that starts past the first phase assumes the earlier phases
  already applied and reports which ones:

  ```text
  # From the beginning up to and including machines
  bootwright apply --through machines --yes

  # A contiguous mid-graph range: deps through base, inclusive
  bootwright apply --stage deps --through base --yes

  # From a phase through to the end of the graph
  bootwright apply --stage deps --through end --yes
  ```

- **A surgical sub-phase rerun.** `apply`, `plan`, and `diff` additionally accept
  the single-phase selectors `fabric`, `machines`, `deps`, `base`, and `add-ons`
  for surgical reruns within a family. These are reruns, not peers of the `infra`
  and `clusters` families; `destroy` does not accept them.

- **One machine.** `--machines <names>` narrows the run to individual `Machine`s
  instead of whole clusters (mutually exclusive with `--clusters`). It runs only
  the `fabric` and `machines` phases, so it rebuilds or tears down one machine's
  substrate — a replaced node or a shared bastion — without touching the rest.
  See [Multi-cluster fleets](fleets.md#narrow-to-machines) and
  [Machines](../concepts/machines.md).

  ```text
  bootwright apply --machines ceph-osd-1 --yes
  bootwright destroy --machines ceph-osd-1 --authorize installed-cluster-node
  ```

!!! note "Check before you rebuild"
    `bootwright diff` is **live by default** and exits `3` on any difference;
    `diff --recorded` is the fast offline variant — see
    [Comparing against live cluster state](#comparing-against-live-cluster-state).
    `bootwright plan` (or `apply --dry-run`) shows the task graph a selection
    would run. Use them to confirm the scope before applying.

### KubeVirt child clusters do not auto-include their parent

A scoped apply of a KubeVirt-backed child `ContainerCluster` that references a
Bootwright-managed virtualization cluster does **not** expand the selection to
install the parent. It fails before mutation unless the parent is selected too,
or local runtime records already prove the parent install and its `kubevirt`
add-on are ready. Select both roots together when the parent is not yet
installed.

## Removing declared objects

`apply` is additive: it never deletes or deprovisions a live resource whose
declaration you removed from desired state (see
[Troubleshooting](../troubleshooting.md#resources-no-longer-in-desired-state-orphans)).
What a deletion means depends on the kind:

| You delete… | What happens | Supported removal |
| --- | --- | --- |
| A whole `ContainerCluster` / `StorageCluster`, `Machine`, `InfraProvider`, or `InfraComponent` | The live resource keeps running; `diff` and `destroy --dry-run` list it under **"Owned but no longer declared"**. | `destroy --clusters <name>` (or a full `destroy`) *before* deleting the file. Destroy is ownership-record driven for the kinds whose control plane it can reach from the provider or infra host it already contacts — `libvirt-domain`, `libvirt-network`, `managed-os-install`, `bmc-emulator`, `infra-component` — and those it reclaims even after the declaration is gone. For `kubevirt-machine`, `vsphere-machine`, `vsphere-vmedia`, `controller-name-resolver`, and `storage-cluster` it cannot: re-declare the object under its original names first. |
| A `ContainerCluster.spec.nodes[]` entry (node scale-in) | The next apply classifies the installed cluster as drift and fails closed; `--mode rebuild` reinstalls the whole cluster rather than removing one node. | Not a day-2 operation today: remove the node out of band with `oc`, and expect the cluster to report drift. |
| A `ClusterAddonBinding` or one bound add-on | The live operator/manifests keep running and are **not** orphan-tracked (add-ons carry no ownership record). | Uninstall out of band with OLM/`oc`, then delete the binding. |
| A `StoragePool` / `StorageFilesystem` / `StorageObjectGateway` / `StorageNFSExport` / `services[]` entry | Additive-only: the live Ceph object keeps running and is not orphan-listed (below object granularity). | Remove it on the cluster with the `ceph`/`cephadm` CLI. |
| A `topology.osdDrivegroups[]` / `devices[]` device path that still hosts an OSD | `apply` **fails closed** — cephadm never auto-removes an OSD, so dropping the device would orphan a running one (see the OSD-growth tip above). | Drain it first — `cephadm shell -- ceph orch osd rm <id>` — then remove the path. |

Removal always crosses the destroy authorization boundary or goes out of band;
`apply` alone never prunes.

## Managed-OS reinstall and owned-Ceph rebuild

Two destructive rebuilds run through `apply --mode rebuild` and are gated by
Bootwright ownership markers, so they apply only to resources Bootwright owns.

- **Managed-OS machine reinstall.** `apply --mode rebuild` bypasses the
  skip-if-already-installed check, undefines the substrate VM, wipes its disks,
  and rebuilds the machine from its `Machine`, `MachineImage`, and
  `MachineInstallProfile` desired state. (FIPS and other install-time
  customizations are reinstall-only by nature, so this is the path to change
  them on an installed machine.) See [Managed OS installs](managed-os.md) for the
  install model.

- **Owned-Ceph cluster rebuild.** `apply --mode rebuild` cleanly rebuilds a managed
  Ceph cluster with `cephadm rm-cluster --zap-osds`, but **only** when a
  Bootwright ownership marker proves the live cluster is the one Bootwright
  created. A foreign or co-resident cluster fails closed.

!!! note "Override rebuilds still-declared structure; it does not prune"
    Ceph convergence is additive-only across the whole storage domain. `apply`
    never removes a live Ceph object whose declaration was deleted, and
    `--mode rebuild` does not prune undeclared objects either — it rebuilds only
    still-declared objects whose structural identity changed: a pool's `type` or
    erasure profile, or a CephFS metadata pool (a data-destroying `ceph fs rm`
    recreate). Remove undeclared Ceph objects on the cluster out of band.
    See [Ceph storage topologies](ceph-topologies.md#convergence-is-additive-only)
    for the full convergence contract.

!!! warning "Reclaiming OSD disks after a managed-OS reinstall"
    On a protected context this is the one path where `--authorize data-loss` is
    the authorization rather than a refusal — see
    [The two axes: intent and authorization](#the-two-axes-intent-and-authorization).

    Bootwright records which disks it provisioned as OSDs in an on-node marker.
    A managed-OS reinstall wipes the OS disk (and that marker) while the separate
    OSD **data** disks keep their ceph LVM, so a plain re-apply would refuse those
    now-unrecognized signed disks. When the controller still records the cluster as
    Bootwright-owned, authorize an in-band wipe of the specific disks:

    ```
    bootwright apply --clusters ceph-storage --reclaim-devices /dev/disk/by-id/wwn-0x...,/dev/disk/by-id/wwn-0x...
    ```

    Only a named device that is a declared OSD device of an owned cluster, is
    **not mounted or a system disk**, and is on a host whose OSD marker does not
    already record it is wiped (irreversible); everything else fails closed. A
    device the host's marker still records is skipped, so passing a cluster-wide
    device name (for example the same `/dev/sdb` on every node) reclaims only the
    marker-lost node and leaves the healthy nodes untouched. This re-provisions the
    disks from scratch — it does not preserve the old OSD data.

    If the controller no longer records the cluster as Bootwright-owned — for
    example the context's `runs/` records were lost and you are driving from a
    fresh checkout — a reclaim run reports **"no device will be reclaimed"** and
    the device-empty gate keeps refusing, referring you back to reclaim. Break the
    loop by re-establishing ownership first, by either route:

    - restore the context's `runs/` records (converge-safety records) from backup,
      then re-run the `--reclaim-devices` apply; or
    - apply once with the data-carrying device **removed** from the `StorageCluster`
      declaration (this records ownership without touching the disk), then re-add
      the device and re-run the `--reclaim-devices` apply.

!!! warning "Wiping an orphan the marker never recorded"
    A reclaim refuses a device carrying LVM or dm-crypt holders, because that is
    what a live bluestore OSD looks like — an in-use OSD has no mountpoint, so
    the mount probe cannot catch it. When the cluster that wrote those holders is
    **gone**, that refusal has no remedy: its advice is to drain the OSD, and
    there is no cluster to drain it from. Bootwright detects the difference and
    says which case you are in — the refusal reports whether the node still
    carries a Ceph daemon tree at `/var/lib/ceph`.

    For the orphan, `--authorize unowned-devices` is the token that proceeds:

    ```
    bootwright apply --clusters ceph-storage --reclaim-devices /dev/nvme0n1 --authorize data-loss,unowned-devices
    ```

    Both tokens are required and neither implies the other: `data-loss`
    authorizes the wipe, `unowned-devices` authorizes wiping a device this node
    holds no ownership record for. The same token clears the equivalent refusal
    on `destroy`, where an undeclared-at-the-time device can survive teardown
    with its signatures intact.

    Confirm what the volume group actually is first — `pvs -o pv_name,vg_name`
    and `lvs -o lv_name,vg_name,lv_tags` on the node. A `ceph-<uuid>` group whose
    LV tags carry `ceph.osd_id=`/`ceph.cluster_fsid=` is prior-install residue.
    On a node whose Ceph daemons are still running, this token wipes a **live**
    OSD and its data.

    What it never relaxes: a **mounted or in-use** device, and a device whose
    probe failed for any reason other than not being present, still fail closed
    under every token — that is what keeps a root disk out of a reclaim. A device
    that is simply **absent** is skipped and reported rather than refused; if a
    declared device is absent on every node, the declaration does not match the
    hardware, and the OSD readiness check will fail later with a count short of
    the declaration.

### Comparing against live cluster state

`diff` is **live by default**: it discovers the real state of each managed Ceph
cluster read-only on the seed (hosts, services and placements, OSDs, CRUSH rules,
pools and replication, config, mgr modules, and health) and prints a git-style
diff of desired-vs-real, and runs a shallow `ClusterVersion` reachability check
for each container cluster. It exits `3` when anything differs.

```
bootwright diff --clusters ceph-storage
```

To fold the live state back into desired-state YAML — so a re-apply reproduces
the cluster as it actually runs — add `--adopt`. It edits declared objects in
place (preserving comments), creates a file for a pool that exists only on the
cluster, and snapshots the prior input to the context's input history first (so
the change is recoverable); differences it cannot safely represent are reported
for a manual edit rather than dropped.

```
bootwright diff --clusters ceph-storage --adopt
```

For a fast, no-contact check in automation, `--recorded` compares desired state
against the last recorded apply instead of the live clusters (the classification
report described under [Ownership & safety](ownership-and-safety.md)). That
variant is the one blind to out-of-band changes — a wiped disk, an undefined VM,
a deleted namespace — until the next apply refreshes the record.

## Destroying renamed or unmarked machines

Each machine substrate (libvirt, KubeVirt, vSphere) refuses to tear down a
VM that matches the Bootwright `<cluster>-<machine>` naming but carries no
ownership marker for **this** context, cluster, and machine — a foreign VM that
merely shares the name must never be destroyed. The same guard fires when you
rename a machine or cluster after applying: the live VM still carries the *old*
marker, so the teardown no longer recognizes it as its own and stops with
"it carries no Bootwright ownership marker for this context/cluster/machine".

`--authorize unowned-vms` is the recovery path: it tells the machine-substrate
teardown to remove a matching VM despite a missing or mismatched marker. Use it
on a scope that tears down machine substrate — `--stage infra` (optionally with
`--clusters`), or the default no-`--stage` full destroy. A `--stage clusters`
run never reaches the machine-substrate teardown, so the token reports that it
had no effect.

```text
bootwright destroy --stage infra --clusters ceph-storage --authorize unowned-vms --yes
```

The cluster's **libvirt network** and its **KubeVirt DataVolumes** are a
separate token, `--authorize unowned-networks`, because their blast radius is
wider: removing an unowned libvirt network can strand VMs of another context
that use it. Authorizing VMs never authorizes networks.

```text
bootwright destroy --stage infra --clusters ceph-storage \
  --authorize unowned-vms,unowned-networks --yes
```

!!! warning "Neither token relaxes anything else"
    They relax only the ownership-marker refusals they name. Neither relaxes
    the Ceph cluster or OSD-device ownership gates, and neither relaxes the
    device data-safety checks (a mounted, in-use, or unprobeable device still
    fails closed). Because they remove a resource Bootwright cannot positively
    confirm it owns, confirm the target is yours before using them.

## Recovering lost Ceph cluster ownership evidence

A managed Ceph destroy refuses when its controller ownership record is missing,
when `/etc/ceph/.bootwright-owned` is missing, or when the marker fsid differs
from `/etc/ceph/ceph.conf`. After independently verifying the cluster and its
on-disk fsid, let Bootwright validate and restore both ownership proofs before
teardown:

```text
bootwright destroy \
  --clusters ceph-storage \
  --recover-ceph-ownership ceph-storage=2088ddee-875b-11f1-9b98-303ea72d7724 \
  --yes
```

The flag is an explicit `<StorageCluster>=<fsid>` mapping; repeat entries with
commas when a destroy covers several storage clusters. Bootwright accepts only
selected managed clusters and compares the supplied UUID with the fsid in the
declared seed's `/etc/ceph/ceph.conf`. Any existing controller owner record must
agree with that cluster and seed. After the remote match, Bootwright reconstructs
a missing controller record, writes the normal owner-only host marker, and
re-reads both through the existing destroy gate. Contradictory controller
evidence is refused rather than overwritten. If the cluster is reachable, its
live fsid must also match the on-disk fsid; Bootwright uses that only as a
contradiction check, never as authorization.

The mapping explicitly attests that this exact on-disk cluster belongs to the
declared `StorageCluster`; verify that fact independently before using it.
Recovery does not trust whichever fsid a live cluster reports or relax
OSD-device ownership, mounted-device, system-disk, or probe-failure checks. It
authorizes no `--authorize` token and no `--yes`. The normative contract is in the
[CLI specification](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md#cli-contract).

## Enrolling a node another Ceph cluster was left running on

An apply refuses a storage node that still carries the systemd units of a
cephadm cluster it does not own, naming each leftover identity and its units.
That residue is what a teardown which skipped the node
(`--authorize unreachable-nodes`) or failed partway through it leaves behind,
and no `--mode rebuild` clears it: a rebuild removes the one fsid the seed
carries, so an identity only this node still has is outside its reach. Left
running, those daemons hold their ports, and cephadm's own placement of the same
daemon type onto the node fails with `Address already in use` — a failure that
names neither the node nor the cluster holding the port, and that surfaces only
when service readiness times out.

Authorize the removal and re-run the same apply:

```text
bootwright apply --clusters ceph-storage --authorize foreign-daemons --yes
```

Each foreign identity is removed with `cephadm rm-cluster --force --fsid <fsid>`
on the node that carries it — that cluster's daemons, units and `/var/lib/ceph`
state, and nothing belonging to the cluster being applied. No disk is zapped, so
the other cluster's OSD data survives; what is destroyed is its presence on this
node. The node is then probed again, and the apply still refuses if any of those
units outlived the removal.

Because the token destroys state Bootwright does not own, nothing implies it —
not `--yes`, not `data-loss`, not `--mode rebuild`. If that other cluster is
still live on nodes elsewhere, it loses this node's daemons, and a monitor among
them leaves its quorum. Clearing the node by hand first, with the same
`cephadm rm-cluster --force --fsid <fsid>`, is the same operation without the
gate or the audit trail.

## Tearing down with a node powered off

The node-targeting teardown plays (managed Ceph storage and OpenShift agent
clusters) connect to every node over SSH. When a node is powered off the
affected step stops with `UNREACHABLE!`; the overall teardown remains
incomplete even when independent cleanup steps can continue.

`--authorize unreachable-nodes` lets the teardown proceed on the nodes it can
reach: an unreachable node is skipped rather than aborting the play. The token
names exactly the risk it accepts — a skipped node leaves the cluster only
*partially* destroyed — and needs no second flag.

It covers a node that does not answer at all: power, route, or an sshd that
never answers. A node that **answers over SSH and then rejects every teardown
identity** — an unauthorized key, an untrusted host key, a refused `sudo`
escalation — is an identity fault, not an absent node, and the token does not
skip it. The teardown fails closed on it and reports which identity refused,
with the exit status and message, instead of telling you to power the node on.
Fix the identity — re-authorize the key, restore the account's NOPASSWD grant,
or pass `--ssh-sudo-password` — and re-run. Skipping such a node would leave its
Ceph daemons running and its OSD devices holding cluster data while the run
reported the cluster destroyed.

```text
bootwright destroy --clusters ceph-nprd --authorize unreachable-nodes,data-loss --yes
```

What "partial" means: a skipped storage node keeps its OSD device signatures,
its running Ceph daemons, and its local Ceph state (`/etc/ceph`,
`/var/lib/ceph`) — none of it is wiped or stopped, so that node keeps serving
the cluster the run reported destroyed. The
cluster's ownership record is kept and marked partially destroyed, so it is not
treated as fully gone; `bootwright status` flags it, and the teardown prints a
warning naming the skipped nodes. Re-run `destroy` once the nodes are back up, or
wipe them manually, before reusing the hardware.

When the flag is enabled but every managed storage node is reachable, the
completion report proves that the storage teardown finished and the next
`apply` remains authorized to reinstall its machines. A report that names any
skipped node withholds that authorization; infra-only, machine-scoped, and
non-storage teardown with `--authorize unreachable-nodes` also withhold it because they do
not produce the same per-node completion proof.

!!! danger "Storage teardown fails closed when the Ceph **seed** host is down"
    Cluster ownership is proven on the seed host before any node wipes its OSD
    devices. If the seed host itself is unreachable, `--authorize
    unreachable-nodes` does **not** proceed — the storage teardown fails closed
    with a clear message, so no node wipes a cluster whose ownership could not
    be verified. Power the seed on (or remove the cluster manually after
    verifying it is safe) and retry. The token does not relax any device
    data-safety check, and like the unowned tokens it authorizes nothing else.

### Never-provisioned clusters tear down automatically

A cluster that was **never provisioned** — for example a nested KubeVirt cluster
whose host cluster never came up — has no per-machine ownership record, because
Bootwright writes that record only after the substrate is actually created. Its
substrate teardown is a clean no-op that consults the local record set and never
contacts the (possibly nonexistent) host, so a plain `destroy` completes it
without `--authorize unreachable-nodes` and without requiring the host to be reachable.

A host cluster that holds **no captured kubeconfig** counts as unavailable in
exactly the same way, whether it never finished installing or an earlier destroy
already removed its install state. Machine-substrate teardown continues past it
rather than failing on the missing file, so a partially failed destroy stays
re-runnable. `apply` is the opposite: it refuses before running anything and
names the host cluster to converge first, because guests cannot be created
through an API it cannot reach.

`--authorize unreachable-nodes` is therefore only needed for a node substrate Bootwright
**did** record but can no longer reach. A KubeVirt host-cluster API is not a
skippable node: if it is unreachable — or its kubeconfig is gone — while
Bootwright still records a guest, destroy cannot prove the VM or its DataVolumes
absent, so it fails closed and retains the ownership and cluster runtime records
for retry.

### Managed bare-metal is torn down locally, wiped on reinstall

Bare-metal machines are operator-owned hardware, so Bootwright never deletes a
physical machine at destroy time. For a managed-OS bare-metal node, the substrate
teardown is **local state cleanup**: it drops the managed-OS install record and
provider state so the next apply performs a fresh install, whose kickstart
`clearpart --all` reclaims the OS disk on reinstall. Destroy never fails closed
here and never contacts the node — so `destroy --stage infra` completes and a
re-apply from scratch is unblocked.

Because the OS disk is reclaimed on reinstall rather than at destroy time, a node
that is destroyed but **not** re-applied keeps its old OS on disk. If you are
decommissioning the hardware rather than rebuilding it, erase the disks out of
band. The data-bearing Ceph **OSD** disks are a separate concern, wiped by the
storage-cluster teardown under its own ownership and device-safety gates (above).

## Surfacing redacted output with `--verbose`

Every command that drives Ansible — `preflight` (all scopes), `diff`, `apply`,
and `destroy` — redacts credential-handling task output by default: secret
material shows as `censored due to no_log` in both the terminal and the persisted
run log. `--verbose`/`-v` disables that `no_log` redaction so the same output is
printed in full.

```text
bootwright apply --clusters ceph-nprd --verbose
```

!!! danger "`--verbose` prints secrets to the terminal **and** the run log"
    With `--verbose` set, secret bytes that are normally censored — BMC, registry,
    RHSM, and proxy credentials, tokens, and generated Ceph keys — reach both the
    terminal and the `0600` run log under `runs/history/<run-id>/`. It is an
    opt-in debugging aid only; default runs stay redacted. This applies equally
    to a verbose `preflight`, which reads the same credentials. Avoid it on
    shared terminals and scrub any logs captured during a verbose run.

## Timing a run and reading its critical path

Every mutating run records per-task timestamps in its ledger. `status --timings`
reports them for the current run, or for any recorded run with `--run <runID>`:

```text
bootwright status --timings
bootwright status --run apply-20260727T101500.000000000Z --timings
bootwright status --run apply-20260727T101500.000000000Z --timings --output json
```

```text
Bootwright: status --timings

Run
  Run: apply-20260727T101500.000000000Z
  Target: clusters
  Status: ok
  Wall clock: 2m0s
  Parallelism: tasks 16, per host 4, Redfish 8

Critical path
  [DONE] Create ISO demo: 10s (cumulative 10s)
  [DONE] Boot demo nodes: 1m0s (cumulative 1m10s)
  [DONE] Install demo: 10s (cumulative 1m20s)
  Total: 1m20s of 2m0s wall clock (67%)

Task timings
  [DONE] Boot demo nodes: 1m0s  queue 0s  blocked 0s  cluster demo  kind nodeBoot
  [DONE] Infra demo: 30s  queue 5s  blocked 4s on host slot host:bastion:machine  cluster demo  kind infra
```

- **Run** — wall clock plus the concurrency caps the run actually ran under. A
  cap that never constrained this graph — its value already covered every task
  that could run at once — prints as `unbounded`.
- **Critical path** — the longest dependency chain through the run's task graph,
  each hop with its own duration and the running total.
- **Task timings** — every task, longest first, with its duration, `queue` (ready
  but waiting for a free slot), `blocked` (waiting on a dependency or a named
  slot), and what it was blocked on.

**Compare the critical-path total, not the wall clock.** A run cannot finish
before its longest dependency chain does, so speeding up thirty tasks that all
run concurrently *beside* that chain buys nothing — the total is unchanged and
only the idle headroom grew. Work that shortens the run either removes a hop
from the critical path, shortens a hop on it, or removes an edge that put it
there. Queue and blocked time reported *on a critical-path hop* is the exception
worth chasing directly: it means a concurrency cap or an ordering edge, not the
task itself, is holding the run.

`--output json` emits the same report machine-readably (`criticalPath.hops[]`
with `durationSeconds`/`cumulativeSeconds`, and `tasks[]` with
`queueWaitSeconds`, `blockedWaitSeconds`, and `blockedOn`), which is the form to
record if you want to track run cost across releases.

!!! note "Flag combinations"
    `--run` is only accepted together with `--timings`, and `--timings` cannot be
    combined with `--watch` — a timing report describes a run, `--watch` follows
    one. Without `--run`, the report covers the current run ledger. `--watch`
    takes `--watch-interval <duration>` for its refresh interval and is text-only:
    combining it with `--output json` is rejected.

### Profiling the Ansible tasks inside a run

`--timings` resolves down to the Bootwright task. To see which *Ansible* task
inside one of them is slow, set `BOOTWRIGHT_ANSIBLE_PROFILE=1` for the run:

```text
BOOTWRIGHT_ANSIBLE_PROFILE=1 bootwright apply --clusters demo --yes
```

Each Ansible task result is then appended as one JSON line to
`runs/history/<run-id>/tasks/<task-id>/artifacts/task-profile.jsonl`, alongside
that task's ordinary Ansible output log. Nothing else about the run changes —
the callback swallows its own errors, so profiling cannot alter the outcome of a
play.

!!! note "Profiling is opt-in on purpose"
    It works by enabling an extra Ansible callback plugin, and callback dispatch
    has disagreed with a callback's hook signature before on an unstable
    `ansible-core` pin — printing `Callback dispatch … failed for plugin` noise
    across an otherwise healthy run. Leaving profiling off keeps every normal run
    on the shipped callback set; enable it for the run you are investigating
    rather than as a standing setting.

## Tuning apply and destroy concurrency

Independent tasks run in parallel under three caps. Each has a default and an
environment-variable escape hatch:

| Environment variable | Caps | Default |
| --- | --- | --- |
| `BOOTWRIGHT_APPLY_PARALLELISM` | Tasks in flight across the whole run | `NumCPU × 2`, clamped to the range 8–32 |
| `BOOTWRIGHT_APPLY_PARALLELISM_PER_HOST` | Tasks in flight against any single host (hypervisor, bastion, vCenter) | 4 |
| `BOOTWRIGHT_APPLY_PARALLELISM_REDFISH` | Concurrent Redfish/BMC operations | 8 |

Despite the `APPLY` in the names, `destroy` plans its task graph under the same
three caps.

These are environment variables and not flags by design: the parallelism CLI
flags were removed deliberately, because concurrency is a property of the
machine and estate you run from, not of an individual invocation. Keep the value
in your shell profile or CI job, not in the command you paste into a runbook.

A requested value is clamped down to what the task graph can actually use — ask
for 64 in a run that has 9 tasks and you get 9 — and raised to the floor a single
task needs to be dispatchable at all. A value that is not a positive integer is
ignored and the default applies. `status --timings` prints what the run really
used, and prints `unbounded` for a cap that never constrained the graph — a cap
reported that way is not the thing making the run slow.

!!! warning "The per-host cap of 4 throttles libvirt VM creation"
    The per-host cap counts every task targeting the same host, so a lab where
    one hypervisor backs the whole fleet creates four virtual machines at a time.
    That is deliberate — an unbounded fan-out onto a single libvirtd is how
    storage pools and `virsh` calls start timing out. If
    the hypervisor has the headroom, raise
    `BOOTWRIGHT_APPLY_PARALLELISM_PER_HOST`; `status --timings` shows what the
    cap is costing as `blocked … on host slot host:<host>:machine`.

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
| `runs/history/<run-id>/tasks/<task-id>/artifacts/` | One task's Ansible output log, plus `task-profile.jsonl` when `BOOTWRIGHT_ANSIBLE_PROFILE` was set for the run. |
| `runs/last-destroy-input/` | The forensic snapshot of the input a `destroy` loaded. |
| `runs/safety/` | Convergence-safety records (the non-secret desired hash plus Bootwright owner identity) that `diff` classifies against. |
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

- [The desired-state model](../concepts/index.md) — apply stages and the
  apply-mode model (`--mode create|reconcile|rebuild`), plus the destroy
  stages.
- [Architecture](../contributing/architecture.md) — the execution pipeline,
  resource locking, and the four-outcome classifier in depth.
- [Ceph storage topologies](ceph-topologies.md) — additive-only convergence and
  the owned-Ceph rebuild details.
- [Managed OS installs](managed-os.md) — the managed-OS install and reinstall
  model.
- [Troubleshooting](../troubleshooting.md) — what to do when a run fails closed.
