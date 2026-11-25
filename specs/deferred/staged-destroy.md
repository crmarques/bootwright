# Staged destroy

This page holds the design of `bootwright destroy --stage` and of the apply
that restores what it takes back. It authorizes nothing: only an item in an
active slice does, and the owner's decision
[D288](../milestones/backlog.md#d288) names the program. Each slice promotes
its part into the owning spec in its own commit and deletes that part from this
page. A rule that the code already holds is not stated here: slice 0, the
frozen inverse of every destroy step, slice 1, its branches once decided at
run time, slice 2, the ownership fold, slice 3, `destroy --stage` and the
trimmed resting state, slice 4, the restoring apply, and slice 5, its changed
input, are implemented and
are owned by [state reconciliation](../state-reconciliation.md#staged-destroy),
its [restoring apply](../state-reconciliation.md#restoring-apply) and the
[delivered records](../milestones/delivered.md#x757--the-restoring-apply-restores-what-a-staged-destroy-took-back).

Words used below:

- A **stage** is one of `substrates`, `machines`, `clusters` and `add-ons`, in
  the order the engine realizes them.
- A **staged destroy** takes back one stage and everything built on it. The
  context then **rests trimmed**.
- A **kept** block is a block of the context that a staged destroy leaves in
  place. A **kept Machine** is a Machine whose own step is not a deletion.
- **Live** is the set of blocks a context owns.
- A **restoring apply** is the next `apply` over a trimmed context.
- The **step contract** is the pairing of every apply step with one destroy
  step that states its inverse.

## Why

An apply that fails late, such as in cluster provisioning, leaves the operator
only the whole destroy. The whole destroy deletes the Machines that were
already installed and proved, and the operator pays for the whole build again.
The owner asked for a way to take back only the latest stage and keep the
Machines below it, and asked that every apply and destroy flow be backed by a
state machine whose steps each have an equivalent destroy step, so that a later
reconciliation of one changed attribute can build on the same contract.

## Program

The order below is the order of delivery. Each row is one item with its own
slice, and each slice names its decision and the contract and digest moves it
causes.

| Order | Item | Outcome |
| --- | --- | --- |
| 0 | [B1855](../milestones/delivered.md#x753--every-destroy-step-states-its-inverse) | Every destroy step states its inverse, frozen with the removal plan |
| 1 | [B1856](../milestones/delivered.md#x754--run-time-inverse-branches-frozen) | The remaining inverse branches decided at run time are frozen |
| 2 | [B1857](../milestones/delivered.md#x755--one-fold-answers-what-a-contexts-operations-own) | One ownership fold answers what a context owns |
| 3 | [B166](../milestones/delivered.md#x756--a-staged-destroy-takes-back-a-stage-and-the-context-rests-trimmed) | `destroy --stage` and the trimmed resting state |
| 3b | [B1867](../milestones/delivered.md#x758--a-destroy-widens-an-unfinished-staged-destroy) | A `destroy` without `--stage`, or a lower floor, supersedes an unfinished staged destroy |
| 4 | [B1858](../milestones/delivered.md#x757--the-restoring-apply-restores-what-a-staged-destroy-took-back) | The restoring apply over unchanged input |
| 5 | [B1859](../milestones/delivered.md#x759--a-restore-admits-changed-input-inside-what-a-staged-destroy-took-back) | A restore admits changed input inside the taken-back part |
| 5b | [B1868](../milestones/m1.md#b1868) | A restore binds its added steps' Secrets, and a whole removal reopens each member's binding |
| 6 | [B1860](../milestones/m1.md#b1860) | An agent installation whose nodes never booted is taken back on kept nodes |
| 7 | [B1861](../milestones/m1.md#b1861) | Root disks of kept virtual nodes are blanked through their substrate |
| 8 | [B1862](../milestones/m2.md#b1862) | A Ceph cluster is taken back on kept hosts |

Two further outcomes follow the restoring apply and stay parked: physical
installs in a staged scope
([B1864](../milestones/backlog.md#b1864)) and `--clusters` narrowing of a
staged destroy ([B1865](../milestones/backlog.md#b1865)). Online removal of
one add-on instance is [B769](../milestones/backlog.md#b769).

## The trimmed resting state

What lands with later items:

- After every registration the log area still fits one whole removal of what
  the context owns. A staged destroy and its restore cost two operation
  directories, and nothing is pruned. At the bound the command refuses and
  names the whole destroy.

## The restoring apply

The restore, changed input included, is owned by
[state reconciliation](../state-reconciliation.md#restoring-apply). Next, the
restore binds the current versions of the Secrets its added steps name in a
binding of its own, which also admits an added step whose Secret only an
additive member's binding pins; while trimmed, the holds keep only the pinned
Secrets kept steps name, so a Secret only taken-back steps used may rotate
before the restore; and a whole removal reopens, for each step, the binding of
the member that last created it ([B1868](../milestones/m1.md#b1868)).

## Safety gates

- **Power.** The entry check reads the Machine blocks of the scope and every
  kept Machine a scoped block names as its host or node, in the state its
  declared removal needs: off for an agent installation, on and reachable for
  an online teardown. A kept host in the wrong state stops with the exact
  `machine start` or `machine stop` command.
- **Kept power targets.** The first scoped step that acts on a kept Machine
  ([B1860](../milestones/m1.md#b1860)) freezes each such Machine with the power
  state it needs in `stage`, and every invocation probes them.

## CLI

- `--dry-run` shows the steps taken back under a header per stage.
- The ask rules that cover `destroy` cover `destroy --stage`.

## Scenarios

| Scenario | Result |
| --- | --- |
| Cluster provisioning failed and the nodes are virtual, installed and proved | `--stage clusters` is admitted once the node Machines are kept and the root-disk reset exists ([B1861](../milestones/m1.md#b1861)); before that it refuses with `destroy.stage-keeps-host` |
| A cluster install failed before any node booted the image | `--stage clusters` takes back the agent installation on kept nodes ([B1860](../milestones/m1.md#b1860)) |
| A staged destroy fails halfway and the operator widens it | A lower floor or no `--stage` supersedes it behind the widening confirmation ([staged destroy](../state-reconciliation.md#staged-destroy)) |
| A kept host is in the wrong power state | Stops with the `machine start` or `machine stop` command |
| A physical install is in the scope | Refuses; the physical restore is parked ([B1864](../milestones/backlog.md#b1864)) |
| `--clusters` narrows a staged destroy | Parked ([B1865](../milestones/backlog.md#b1865)) |
| A Ceph cluster no data client can reach is in the scope | Taken back on kept hosts through the unwind below ([B1862](../milestones/m2.md#b1862)) |

## Capabilities

Each row names what a staged destroy does with the capability and the item that
unlocks the part that refuses.

| Capability | In a staged scope | Unlocked by |
| --- | --- | --- |
| libvirt and KubeVirt Machines | Deleted by the scope, or kept with their steps below the floor | [B166](../milestones/delivered.md#x756--a-staged-destroy-takes-back-a-stage-and-the-context-rests-trimmed) |
| Physical Machines | Always kept; an install or erase in the scope refuses | [B1864](../milestones/backlog.md#b1864) |
| Infrastructure services (mirror content, DNS, registry, hardware claims, enrollment) and the controller | Always kept | none; the whole destroy takes them back |
| OpenShift agent installation | Taken back on kept nodes when no node was handed the image | [B1860](../milestones/m1.md#b1860) |
| OpenShift physical nodes after a boot | Stays open: the installed system would be recorded as residue, the nodes proved off and the next install would consume data loss as it does now | the owner's decision |
| Virtual node Machines after a boot | The install step's inverse blanks the root disks through a substrate port, with every other step on that disk in the scope | [B1861](../milestones/m1.md#b1861) |
| Ceph on kept hosts | First the unwind of a cluster no data client can hold access to: hosts on, `rm-cluster` scoped to the FSID, zap of exactly the volumes tagged with it, removal of the drop-in, the key line, the registry credential and the markers, and an observation that each host is absent; then serving clusters under B491's client-fencing contract | [B1862](../milestones/m2.md#b1862), requiring [B491](../milestones/m2.md#b491) |
| Add-on instances | Retire offline with their cluster's install in the scope | [B769](../milestones/backlog.md#b769) for one instance online |

## Open for the owner

- Which capability follows the restoring apply. The recommendation is Ceph on
  kept hosts first, through B491's definition for a cluster no client can use.
- Physical OpenShift nodes after a boot.
