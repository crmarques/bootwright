---
title: Operations and Recovery
description: The three apply modes including break-glass --converge-drifted and greenfield --expect-new, full-lifecycle and staged destroy, destroyProtection and the destroy-authorization boundary, focused --stage/--clusters recovery, managed-OS reinstall and owned-Ceph rebuild, removing the artifact server, diff drift, --include-unowned / --skip-unreachable, run timings and the critical path, and the concurrency caps.
---

# Operations and recovery

Day-2 work on an applied context is teardown and recovery, not re-authoring.
This page covers the three apply modes, tearing a platform down by stage or in
full, the `destroyProtection` safety gate and the destroy-authorization
boundary, recovering a single component, and the destructive rebuilds Bootwright
performs on resources it owns.

For the user-facing apply, stage, and convergence model — bare `apply`,
`--expect-new`, and `--converge-drifted` as modes, and the stage families and
sub-phases — see [The desired-state model](../concepts/index.md). For the
execution pipeline, locking, and the four-outcome classifier in depth, see
[Architecture](../contributing/architecture.md). This page is the how-to and
does not restate those models.

## The three apply modes

`apply` reconciles by default; two mutually exclusive modifier flags change that
— `--expect-new` (greenfield assertion: refuse if any selected object already
exists) and `--converge-drifted` (break-glass recovery for Bootwright-owned drift
it knows how to rebuild — managed-OS reinstall, owned-Ceph wipe-and-rebuild,
drifted owned-object rebuild). See
[Apply modes](../concepts/index.md#apply-modes) for the full model; the rest of
this section covers the operational contract that this page owns.

!!! note "`--expect-new` and `--converge-drifted` cannot be combined"
    `--expect-new` asserts greenfield (fail if any selected object exists);
    `--converge-drifted` authorizes rebuilding objects that already exist. They express
    opposite intents.

!!! tip "Growing a Ceph cluster's OSDs is a plain `apply`, not `--converge-drifted`"
    Adding an OSD device to a `spec.ceph.topology` node reconciles **in place**:
    a bare `apply` classifies an OSD-device-only change as reconcilable (not
    destructive) drift, so `ceph orch apply` adds the new OSD without a
    wipe-and-rebuild. `--converge-drifted` is only for a change to cluster identity
    (seedHost/monIP/network), which is a genuine rebuild. Because cephadm never
    auto-removes an OSD (removal must drain data first), **removing** a device
    that still hosts an OSD is refused with guidance to drain it —
    `cephadm shell -- ceph orch osd rm <id>` — before removing it from the spec.

### The fail-closed `--converge-drifted` contract

`apply --converge-drifted` authorizes Bootwright-owned destructive *rebuilds* — drifted
owned objects, a managed-OS machine reinstall, an owned-Ceph wipe-and-rebuild.
It never bypasses active-run leases, validation, secret checks, or
foreign-resource ownership failures, and it never touches a resource a
non-Bootwright owner holds.

`--converge-drifted` authorizes the rebuild; a **separate** acknowledgement
authorizes the *data loss* it entails. When a run would destroy data, it stops at
an interactive prompt — `Confirm this DESTRUCTIVE action (accept data loss)?` —
and proceeds only on `y`. For automation, pass `--confirm-data-loss` alongside
`--yes`; `--yes` on its own skips only the ordinary confirmation and **never**
authorizes data loss, so a destructive rebuild under `--yes` without
`--confirm-data-loss` fails closed. The same acknowledgement authorizes the two
storage data-loss operations: the `all: true` OSD auto-reclaim that zaps dirty
declared disks before an OSD apply, and an explicit `--reclaim-devices` run
(below). `--confirm-data-loss` has no effect on a run that plans no data-loss
action.

A failed probe is not an authorization. If a cluster whose recorded install
inputs match desired state cannot be probed at all — the API is unreachable, the
kubeconfig is unusable, `oc` is missing — `--converge-drifted` **fails closed**
naming that cluster instead of scheduling a reinstall, because unknown state is
not evidence of a broken cluster. Restore reachability and re-run, exclude it
with `--clusters`, or tear it down deliberately with `destroy --clusters <name>`
and re-apply.

The fail-closed interaction with destroy protection is the part operators most
often miss:

!!! warning "`apply --converge-drifted` is rejected on a protected context"
    When the selected state sets `destroyProtection: requiredOverride`,
    `apply --converge-drifted` **fails closed before any mutation** instead of rebuilding
    the protected resources. Destruction of protected state must cross the
    destroy authorization boundary. (`--dry-run` still previews the override
    plan.)

So there are two distinct rebuild paths, and which one you use depends on
whether the context is protected:

- **Protected context** — rebuild crosses the destroy boundary. Run
  `destroy --force` for the affected scope, then re-apply:

  ```text
  bootwright destroy --stage clusters --clusters ocp-3node --force
  bootwright apply --stage clusters --clusters ocp-3node --yes
  ```

- **Unprotected context** — a single `apply --converge-drifted` performs the
  Bootwright-owned destructive rebuild in place:

  ```text
  bootwright apply --clusters ocp-3node --converge-drifted
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
ordered task chain. `--yes` skips the confirmation prompt and nothing more.

### Bounded, ownership-gated cleanup

`destroy` is deliberately conservative about what it removes:

- **Disks.** Managed-machine disk cleanup is limited to provider-owned disks or
  devices you declared as Bootwright-managed. Bootwright never wipes arbitrary
  visible disks.
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
`runs/history/`. Add `--purge-history` to also delete that once a component's
teardown actually succeeds:

```text
bootwright destroy --clusters ocp-3node --purge-history --yes
bootwright destroy --machines ceph-osd-1 --purge-history --force
```

`--purge-history` is scoped identically to `--clusters`/`--machines` — the
whole context on an unscoped `destroy` — and never reaches a component outside
that scope. A cluster left partially destroyed by `--skip-unreachable` keeps
its history so you can still diagnose and retry; a run that also covers a
still-live component keeps its shared ledger and run log, pruning only the
purged component's own task directories and log. It leaves the destroy
authorization trail (the substrate-release record that lets a later `apply`
reinstall the same name) and unrelated context state (the ownership store,
input-history rollback snapshots) untouched, and has nothing to remove for the
artifact-server literal.

!!! warning "Not recoverable"
    Purged history is gone for good — there is no undo. Skip `--purge-history`
    if you might need the installer inputs or run logs for a post-mortem.

## Destroy protection and the authorization boundary

Set `Environment.spec.safety.destroyProtection: requiredOverride` to guard a
context against accidental teardown. The field accepts `allow` or
`requiredOverride`; empty means `allow`, and protection is **never** inferred
from environment, context, label, or cluster names.

When the selected state sets `requiredOverride`, `destroy --stage infra` and
`destroy --stage clusters` both require `--force` to proceed:

```text
bootwright destroy --stage clusters --force
```

This is the **destroy authorization boundary**: destruction of protected state
can be authorized only through `destroy --force`, never through
`apply --converge-drifted` (which fails closed on a protected context, as above).

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
`destroyProtection: requiredOverride`, but scoped to that kind. Even when
`destroyProtection` is `allow`, a `destroy` whose scope tears down a protected
kind requires `--force`, and an `apply --converge-drifted` that would
destructively rebuild one fails closed and directs you to `destroy --force` that
scope first. Reconfigure-only drift — an in-place service re-apply that touches
no data, OS, or VM — does not trip it; only destructive rebuilds of the protected
kind do. `destroyProtection` and `protectedKinds` combine: a resource is
protected if either covers it.

!!! warning "`--yes` does not imply `--force`"
    `--yes` skips only the confirmation prompt; it never implies `--force`.
    On a protected context a `destroy` without `--force` fails closed,
    regardless of `--yes`.

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
  They are accepted by `apply`, `plan`, and `diff` (not `destroy`) and take the
  same family and sub-phase names as `--stage`. A range that starts past the
  first phase assumes the earlier phases already applied and reports which ones:

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
  bootwright destroy --machines ceph-osd-1 --force
  ```

!!! note "Check before you rebuild"
    `bootwright diff` is **live by default**: it probes the selected clusters
    read-only — Ceph discovery on the seed, a shallow `ClusterVersion` check per
    container cluster — and prints the desired-vs-real differences, exiting `3` on
    any difference. So plain `diff` **does** catch an out-of-band change — a wiped
    disk, an undefined VM, a deleted namespace — wherever discovery reaches it.
    For a fast offline check, `bootwright diff --recorded` skips all cluster
    contact and classifies each root as `missing`, `match`, `drift`, or `foreign`
    against the last recorded apply; that variant is the one blind to out-of-band
    changes until the next apply refreshes the record. `bootwright plan` (or
    `apply --dry-run`) shows the task graph a selection would run. Use them to
    confirm the scope before applying.

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
| A whole `ContainerCluster` / `StorageCluster`, `Machine`, `InfraProvider`, or `InfraComponent` | The live resource keeps running; `diff` and `destroy --dry-run` list it under **"Owned but no longer declared"**. | `destroy --clusters <name>` (or a full `destroy`) *before* deleting the file — destroy is ownership-record driven and can reach a resource already gone from desired state. |
| A `ContainerCluster.spec.nodes[]` entry (node scale-in) | The next apply classifies the installed cluster as drift and fails closed; `--converge-drifted` reinstalls the whole cluster rather than removing one node. | Not a day-2 operation today: remove the node out of band with `oc`, and expect the cluster to report drift. |
| A `ClusterAddonBinding` or one bound add-on | The live operator/manifests keep running and are **not** orphan-tracked (add-ons carry no ownership record). | Uninstall out of band with OLM/`oc`, then delete the binding. |
| A `StoragePool` / `StorageFilesystem` / `StorageObjectGateway` / `StorageNFSExport` / `services[]` entry | Additive-only: the live Ceph object keeps running and is not orphan-listed (below object granularity). | Remove it on the cluster with the `ceph`/`cephadm` CLI. |

Removal always crosses the destroy authorization boundary or goes out of band;
`apply` alone never prunes.

## Managed-OS reinstall and owned-Ceph rebuild

Two destructive rebuilds run through `apply --converge-drifted` and are gated by
Bootwright ownership markers, so they apply only to resources Bootwright owns.

- **Managed-OS machine reinstall.** `apply --converge-drifted` bypasses the
  skip-if-already-installed check, undefines the substrate VM, wipes its disks,
  and rebuilds the machine from its `Machine`, `MachineImage`, and
  `MachineInstallProfile` desired state. (FIPS and other install-time
  customizations are reinstall-only by nature, so this is the path to change
  them on an installed machine.) See [Managed OS installs](managed-os.md) for the
  install model.

- **Owned-Ceph cluster rebuild.** `apply --converge-drifted` cleanly rebuilds a managed
  Ceph cluster with `cephadm rm-cluster --zap-osds`, but **only** when a
  Bootwright ownership marker proves the live cluster is the one Bootwright
  created. A foreign or co-resident cluster fails closed.

!!! note "Override rebuilds still-declared structure; it does not prune"
    Ceph convergence is additive-only across the whole storage domain. `apply`
    never removes a live Ceph object whose declaration was deleted, and
    `--converge-drifted` does not prune undeclared objects either — it rebuilds only
    still-declared objects whose structural identity changed: a pool's `type` or
    erasure profile, or a CephFS metadata pool (a data-destroying `ceph fs rm`
    recreate). Remove undeclared Ceph objects on the cluster out of band.
    See [Ceph storage topologies](ceph-topologies.md#convergence-is-additive-only)
    for the full convergence contract.

!!! warning "Reclaiming OSD disks after a managed-OS reinstall"
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
report described under [Ownership & safety](ownership-and-safety.md)).

## Force-destroying renamed or unmarked machines

Each machine substrate (libvirt, KubeVirt, vSphere) refuses to tear down a
VM that matches the Bootwright `<cluster>-<machine>` naming but carries no
ownership marker for **this** context, cluster, and machine — a foreign VM that
merely shares the name must never be destroyed. The same guard fires when you
rename a machine or cluster after applying: the live VM still carries the *old*
marker, so the teardown no longer recognizes it as its own and stops with
"it carries no Bootwright ownership marker for this context/cluster/machine".

`--include-unowned` is the recovery path: it tells the machine-substrate teardown
to remove a matching VM despite a missing or mismatched marker. Machine
substrate is torn down by the infra stage, so combine it with `--stage infra`
(a clusters-only destroy never runs the machine-substrate teardown and refuses
the flag).

```text
bootwright destroy --stage infra --clusters ceph-storage --include-unowned --yes
```

!!! warning "`--include-unowned` is scoped to machine VMs"
    It relaxes only the libvirt/KubeVirt/vSphere per-VM ownership-marker
    refusals. It does **not** relax the Ceph cluster or OSD-device ownership
    gates, and it never relaxes the device data-safety checks (a mounted,
    in-use, or unprobeable device still fails closed). It is independent of
    `--force` (protected-environment teardown) and does not imply `--yes`.
    Because it removes a VM Bootwright cannot positively confirm it owns,
    confirm the target VM is yours before using it.

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
implies neither `--force` nor `--yes`. The normative contract is in the
[CLI specification](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md#cli-contract).

## Tearing down with a node powered off

The node-targeting teardown plays (managed Ceph storage and OpenShift agent
clusters) connect to every node over SSH. When a node is powered off the
affected step stops with `UNREACHABLE!`; the overall teardown remains
incomplete even when independent cleanup steps can continue.

`--skip-unreachable` lets the teardown proceed on the nodes it can reach: an
unreachable node is skipped rather than aborting the play. It **requires
`--force`**, because a skipped node leaves the cluster only *partially*
destroyed.

```text
bootwright destroy --clusters ceph-nprd --force --skip-unreachable --yes
```

What "partial" means: a skipped storage node keeps its OSD device signatures and
local Ceph state (`/etc/ceph`, `/var/lib/ceph`) — they are **not** wiped. The
cluster's ownership record is kept and marked partially destroyed, so it is not
treated as fully gone; `bootwright status` flags it, and the teardown prints a
warning naming the skipped nodes. Re-run `destroy` once the nodes are back up, or
wipe them manually, before reusing the hardware.

When the flag is enabled but every managed storage node is reachable, the
completion report proves that the storage teardown finished and the next
`apply` remains authorized to reinstall its machines. A report that names any
skipped node withholds that authorization; infra-only, machine-scoped, and
non-storage teardown with `--skip-unreachable` also withhold it because they do
not produce the same per-node completion proof.

!!! danger "Storage teardown fails closed when the Ceph **seed** host is down"
    Cluster ownership is proven on the seed host before any node wipes its OSD
    devices. If the seed host itself is unreachable, `--skip-unreachable` does
    **not** proceed — the storage teardown fails closed with a clear message, so
    no node wipes a cluster whose ownership could not be verified. Power the seed
    on (or remove the cluster manually after verifying it is safe) and retry.
    `--skip-unreachable` does not relax any device data-safety check, and like
    `--include-unowned` it does not imply `--yes`.

### Never-provisioned clusters tear down automatically

A cluster that was **never provisioned** — for example a nested KubeVirt cluster
whose host cluster never came up — has no per-machine ownership record, because
Bootwright writes that record only after the substrate is actually created. Its
substrate teardown is a clean no-op that consults the local record set and never
contacts the (possibly nonexistent) host, so a plain `destroy` completes it
without `--skip-unreachable` and without requiring the host to be reachable.

A host cluster that holds **no captured kubeconfig** counts as unavailable in
exactly the same way, whether it never finished installing or an earlier destroy
already removed its install state. Machine-substrate teardown continues past it
rather than failing on the missing file, so a partially failed destroy stays
re-runnable. `apply` is the opposite: it refuses before running anything and
names the host cluster to converge first, because guests cannot be created
through an API it cannot reach.

`--skip-unreachable` is therefore only needed for a node substrate Bootwright
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

`apply` and `destroy` redact credential-handling task output by default: secret
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
    opt-in debugging aid only; default runs stay redacted. Avoid it on shared
    terminals and scrub any logs captured during a verbose run.

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
    one. Without `--run`, the report covers the current run ledger.

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
    The per-host cap counts every task targeting the same host, so on a lab where
    one hypervisor backs the whole fleet, virtual machines are now created four
    at a time instead of all at once. That is deliberate — an unbounded fan-out
    onto a single libvirtd is how storage pools and `virsh` calls start timing
    out — but it is a change in behaviour for large single-hypervisor labs. If
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
  apply-mode model (reconcile, `--expect-new`, `--converge-drifted`), plus the destroy
  stages.
- [Architecture](../contributing/architecture.md) — the execution pipeline,
  resource locking, and the four-outcome classifier in depth.
- [Ceph storage topologies](ceph-topologies.md) — additive-only convergence and
  the owned-Ceph rebuild details.
- [Managed OS installs](managed-os.md) — the managed-OS install and reinstall
  model.
- [Troubleshooting](../troubleshooting.md) — what to do when a run fails closed.
