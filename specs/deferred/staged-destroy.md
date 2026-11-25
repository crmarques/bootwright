# Staged destroy

This page holds the design of `bootwright destroy --stage` and of the apply
that restores what it takes back. It authorizes nothing: only an item in an
active slice does, and the owner's decision
[D288](../milestones/backlog.md#d288) names the program. Each slice promotes
its part into the owning spec in its own commit and deletes that part from this
page. A rule that the code already holds is not stated here: slice 0, the
frozen inverse of every destroy step, slice 1, its branches once decided at
run time, slice 2, the ownership fold, and slice 3, `destroy --stage` and the
trimmed resting state, are implemented and are owned by
[state reconciliation](../state-reconciliation.md#staged-destroy) and the
[delivered records](../milestones/delivered.md#x756--a-staged-destroy-takes-back-a-stage-and-the-context-rests-trimmed).

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
| 3b | [B1867](../milestones/m1.md#b1867) | A `destroy` without `--stage`, or a lower floor, supersedes an unfinished staged destroy |
| 4 | [B1858](../milestones/m1.md#b1858) | The restoring apply over unchanged input |
| 5 | [B1859](../milestones/m1.md#b1859) | A restore admits changed input inside the taken-back part |
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

- `context update` over a trimmed context warns `input.changes-kept` when the
  new input changes a kept block ([B1859](../milestones/m1.md#b1859)).
- After every registration the log area still fits one whole removal of what
  the context owns. A staged destroy and its restore cost two operation
  directories, and nothing is pruned. At the bound the command refuses and
  names the whole destroy.
- A done staged destroy's receipt names next `apply` once the restoring apply
  exists ([B1858](../milestones/m1.md#b1858)).

## Step life

The fold and its step life table are owned by
[state reconciliation](../state-reconciliation.md#step-life). The restore adds
to it:

- A restoring apply is the first member a fold admits over a take-back, so a
  re-created step routes to it ([B1858](../milestones/m1.md#b1858)).

## Identity follows the creation of an effect

- An effect carries the identity of the operation that created it, never the
  identity of the newest record of its step.
- A kept effect keeps its mark. A re-created effect takes the identity of the
  restoring apply, with fresh attempt records, so nothing the earlier
  incarnation left reads as this context's own.
- Ownership marks, a Ceph FSID and the KubeVirt annotation are identities of
  this kind. One accessor answers them for every reader.
- The restoring apply makes the additive apply's choice for the same reason.
  The alternative settles one identity per context first, through
  [B568](../milestones/backlog.md#b568) and [B79](../milestones/m2.md#b79),
  and stays open for the owner.
- Pins and host keys reach an added block as prerequisite evidence from the
  member that holds the Machine's block.

## The restoring apply

A restoring apply is a new apply operation over a trimmed context, the fifth
apply scenario of the
[reconciliation scenarios](reconciliation-scenarios.md#the-admitted-scenarios).
It adds one row to the state machine, one `stateMachineRows` case and one guard
placement in its own slice.

- Its `predecessor` is the lineage head apply, and a new optional member
  `restores` names the last staged destroy.
- It plans the whole selected input with the current build, hands planning the
  evidence of Live, and registers only the blocks that are not Live. The edges
  from an added block into Live are frozen as `prerequisites`.
- The admission classifies the fresh plan against Live: a block that is the same
  keeps, a block that is absent creates, and anything else refuses. The refusal
  is the default branch that a later scenario extends.
- `apply.kept-block-changed` refuses a Live block that is missing from the
  fresh plan or does not compare the same. It names the block and the floor that
  would take it back.
- Every added block is one a staged destroy took back or one no member ever
  started. Otherwise the apply refuses with `apply.restore-outside-trim`.
- An added block whose frozen execution contract conflicts with a Live
  member's refuses at once, not at the later whole destroy.
- The contracts of the Live members are re-proved under the current build. A
  build that moved a kept block's answer refuses with
  `apply.restore-other-build`.
- Custody is proved over Live and the producers of the added plan.
- `Reserve` runs over the whole planned set, as in a fresh apply.
- The first restoring slice takes unchanged input and reuses the Secret
  material the lineage bound. A Secret hold stays over every Secret a lineage
  binding pins while the context rests trimmed.
- The next slice takes changed input and a newer build for the added blocks
  under its own binding, and a whole removal then reopens each member's binding
  for that member's blocks
  ([B1859](../milestones/m1.md#b1859)).
- The restore accepts `rebind=` for the access credentials it presents,
  including those of the kept power targets.

## Safety gates

- **Power.** The entry check reads the Machine blocks of the scope and every
  kept Machine a scoped block names as its host or node, in the state its
  declared removal needs: off for an agent installation, on and reachable for
  an online teardown. A kept host in the wrong state stops with the exact
  `machine start` or `machine stop` command.
- **Restore power.** The restoring apply checks power only for the Machines its
  own blocks realize or act on. A kept Machine that nothing of the restore names
  is read in either state, as a continuation reads it, and an attempt may start
  a kept host it needs through the identified power path. This narrows the rule
  that an additive apply checks its managed fleet for this one scenario, so it
  lands only in its own named slice.
- **Kept power targets.** The first scoped step that acts on a kept Machine
  ([B1860](../milestones/m1.md#b1860)) freezes each such Machine with the power
  state it needs in `stage`, and every invocation probes them.

## CLI

- `--dry-run` shows the steps taken back under a header per stage and the
  sentence "the next apply restores N steps over M kept; values frozen in kept
  steps (DNS names, VIPs, release, storage ports) cannot change without taking
  those steps back" once the restore exists.
- The ask rules that cover `destroy` cover `destroy --stage`.

## Scenarios

| Scenario | Result |
| --- | --- |
| Cluster provisioning failed and the nodes are virtual, installed and proved | `--stage clusters` is admitted once the node Machines are kept and the root-disk reset exists ([B1861](../milestones/m1.md#b1861)); before that it refuses with `destroy.stage-keeps-host` |
| A cluster install failed before any node booted the image | `--stage clusters` takes back the agent installation on kept nodes ([B1860](../milestones/m1.md#b1860)) |
| A staged destroy fails halfway and the operator widens it | A lower floor or no `--stage` supersedes it behind the widening confirmation ([B1867](../milestones/m1.md#b1867)) |
| `apply` over a trimmed context with unchanged input | A restoring apply creates the blocks that are absent and keeps the rest |
| `apply` over a trimmed context with input that changes a kept block | Refuses with `apply.kept-block-changed` and the floor that would take the block back |
| `apply` over a trimmed context with input that changes a taken-back block | A restoring apply that carries the change ([B1859](../milestones/m1.md#b1859)) |
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
- Whether re-created effects take the creating operation's identity or wait for
  one identity per context through B568 and B79.
