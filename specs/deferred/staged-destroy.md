# Staged destroy

This page holds the design of `bootwright destroy --stage` and of the apply
that restores what it takes back. It authorizes nothing: only an item in an
active slice does, and the owner's decision
[D288](../milestones/backlog.md#d288) names the program. Each slice promotes
its part into the owning spec in its own commit and deletes that part from this
page. A rule that the code already holds is not stated here: slice 0, the
frozen inverse of every destroy step, is implemented and is owned by
[B1855](../milestones/delivered.md#x753--every-destroy-step-states-its-inverse) and
[state reconciliation](../state-reconciliation.md#frozen-requests-and-contract-guards).

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
| 1 | [B1856](../milestones/m1.md#b1856) | The remaining inverse branches decided at run time are frozen |
| 2 | [B1857](../milestones/m1.md#b1857) | One ownership fold answers what a context owns |
| 3 | [B166](../milestones/m1.md#b166) | `destroy --stage` and the trimmed resting state |
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

## Floor and scope

- `bootwright destroy --stage <stage>` takes exactly one value. The value is a
  floor: that stage and every later stage.
- A list, a repeated flag, `controller` and `infrastructure-services` are usage
  errors. A staged destroy never takes back the controller or an
  infrastructure service.
- Without `--stage`, `destroy` is the whole destroy.
- The seeds are the Live blocks whose frozen stage is at or above the floor.
- The scope is the seeds and every Live block that depends on a seed, over the
  frozen dependencies with prerequisites folded in.
- Nothing Live at or above the floor settles with exit 0 and registers
  nothing. Over a trimmed context the same or a higher floor settles the same
  way.
- Every proof below runs before the operation registers, and each refusal
  changes nothing.

### The scope rule

A block that stays kept must not depend on an effect that goes.

- Every step the removal plan marks as leaving with another step names that step
  inside the scope.
- Every removal on a kept Machine is declared reachable in the scope and states
  the power state it needs.
- A block that retires offline names the install it leaves with, and that
  install is in the scope.
- A physical install in the scope refuses, because the restore over a physical
  host needs the disk discovery that a preparation apply owns
  ([B1864](../milestones/backlog.md#b1864)).
- A physical Machine is always a kept Machine: its own step only releases a
  claim.

### Refusals of the scope

| Stop | When | Remedy it names |
| --- | --- | --- |
| `destroy.stage-below-floor` | The scope reaches a Live block below the floor, such as a KubeVirt provider host that hosts a cluster of this context or an artifact server on an installed mirror host | The exact lower floor, or the whole destroy when the scope reaches the controller or an infrastructure service |
| `destroy.stage-keeps-unfinished` | A Live block outside the scope does not read `done` | `apply` to finish it, a lower floor, or `destroy` |
| `destroy.stage-keeps-host` | A scoped step needs a kept Machine in a way no capability declares reachable | The block, the Machines, the reason and the floor or item that unlocks it |
| `destroy.stage-changed` | An unfinished staged destroy meets a higher floor, or a `destroy` without `--stage` | The command with the frozen floor, and the whole destroy |

The survivors of a removal are computed over Live, not only over the removal
plan, so a consumer outside the plan blocks the removal as it does for a whole
destroy.

## The staged destroy operation

- A staged destroy is a destroy operation. Its `source` is the lineage head
  apply. Its plan is the inverse of the scope.
- A new optional member `stage` freezes the floor, the scoped block ids and the
  kept power targets with the power state each needs. Every invocation probes
  those targets, not only the entry check.
- The frozen `stage` also holds the **cumulative taken-back set**: what the
  staged destroy it replaces froze, what that destroy proved done, and its own
  scope. A staged destroy names its predecessor staged destroy in `replaces`,
  so the fold never walks the chain again.
- `done` stays terminal for every block, no record of an apply is rewritten and
  the index only moves forward through `Register`.
- A destroy step runs its capability even when its inverse does nothing. The
  engine never records a step `done` without calling it, abandonment aside.
- A staged destroy completes in the one transaction that records the removal
  `done`:
  - discards the produced-custody entries whose producer is in the scope;
  - re-reserves the context's unchanged exclusive claim set, which clears the
    `Releasing` marks the removal set, and drops the shared in-use claims of
    the scoped blocks proved gone;
  - releases no binding and withdraws nothing else;
  - publishes the evidence `{operation: none, ownership: retained}`.
- A staged destroy is finalized when that evidence is published, no custody
  entry of a scoped producer remains and no `Releasing` mark remains. Its
  finalization repeats these steps idempotently and branches on `stage`, so it
  never runs the releases of a whole removal.
- The evidence of a done destroy is a function of its record: a done destroy
  that carries `stage` projects `{none, retained}`. Settle, finalization,
  completion and `reclaim` all read that function. "Pristine" is a literal.
- A staged destroy collects no binding on any path. The collect keep-list is the
  binding of every lineage member, and every binding stays until the whole
  removal.
- A staged destroy runs no registration release check with an empty kind. It
  runs the per-block checks of the objects it removes.
- A destroy recorded by an earlier build whose frozen inverse the current
  build contradicts refuses and names the build that registered it. That build
  finishes it.

## The trimmed resting state

The index names a done staged destroy. The rows below enter the
[state machine](../state-reconciliation.md#state-machine) with the slice that
makes each reachable, together with their `stateMachineRows` cases.

| Durable state | Allowed transition |
| --- | --- |
| staged destroy done (trimmed) | `apply` starts a restoring apply or refuses; `destroy` starts a whole removal of Live, with `source` unchanged and `replaces` naming the staged destroy, which ends pristine as today; `destroy --stage` at the same or a higher floor settles, and at a lower floor starts a staged destroy over Live |
| staged destroy running, paused, unknown or failed | the existing destroy rows with the floor frozen: the same floor continues it or replaces a failed one over what it has not proved gone; a lower floor supersedes it with a staged destroy over the remainder and the wider selection; a higher floor refuses `destroy.stage-changed`; no `--stage` refuses `destroy.stage-changed` and names both commands; `apply` refuses as over any incomplete destroy |
| staged destroy unfinished, plain `destroy` | a whole removal of Live supersedes it behind a confirmation that states the widening and, unattended, the digest of the whole plan |

- `context update` over a trimmed context is allowed. It warns
  `input.changes-kept` when the new input changes a kept block.
- `context delete` over a trimmed context refuses without `--allow-orphans`.
- `machine discover`, `machine enroll` and automatic preparation over a
  trimmed context refuse with a named stop.
- A staged destroy takes any apply as its source, completed or not, and takes an
  additive lineage as one source.
- After every registration the log area still fits one whole removal of Live.
  A staged destroy and its restore cost two operation directories, and nothing
  is pruned. At the bound the command refuses and names the whole destroy.

## Step life

One pure function, `Live`, in `internal/reconciliation` answers what a context
owns. Every reader goes through it: ownership, `status`, `diff`, the Machine
list, Secret holds, the keep-list of binding collection, survivors, lineage
removal, the build comparison, planning evidence, pins and host keys, store
admission and the attempt reserve, block routing, completeness and contradiction
checks, the machine power realization fallback, the storage and container
preflights and host-key access. An architecture test forbids reading ownership
from the index's plan and forbids `ownerOf` and `Lineage` folds outside it.

- The fold input is the apply chain by `predecessor` and the staged-destroy
  chain by `replaces`, in registration order.
- An apply adds the blocks it started, its whole plan when it completed. A done
  staged destroy subtracts its cumulative taken-back set.
- `Live` returns, per block, the member that holds it and that member's state,
  and the merged plan, where a repeated id takes the newest holder's
  definition.
- `reconciliation.Lineage` allows a repeated id only where a done staged
  destroy took it back in between.
- An ownership stamp reads `apply` with the holder's state for a Live block, and
  `destroy` with `done` only for the scope of a done staged destroy.

The table below is the specified life of one step across operations. A test
drives the fold over every row and over sequences of four operations.

| State | Meaning | Next |
| --- | --- | --- |
| absent | No member started the step, or a done staged destroy took it back | An apply that starts it: realizing |
| realizing | An attempt of an apply runs the step | `done`: realized; failed or lost: unproved |
| realized | The step is `done` in a member | A removal that starts it: removing |
| unproved | The outcome failed or was lost | Resolution proves it realized or absent; a removal takes it back by its inverse |
| removing | A removal attempt runs the inverse | `done` with every witness done: absent; `done` while a witness is not: leaving; failed or lost: unproved |
| leaving | The inverse is done, and the step leaves with a witness step that is not done, so the step stays owned | The witness reads `done`: absent; the witness fails: still leaving, and its replacement inherits it |
| retained | The inverse leaves something on purpose, with its residue class | A whole destroy that proves it gone; an apply never adopts it |
| abandoned | `abandon=` left the effect in place | A whole destroy without `abandon` returns it to removing; an apply refuses and never adopts |
| partial, inconclusive, no-effect | The outcome of an inverse that finished part of its effect, could not tell, or made none | Removing again, or resolution, as the outcome table of [resolution outcomes](../state-reconciliation.md#resolution-outcomes) states |

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
- **Authorization.** The tokens a staged plan consumes are the tokens of its
  scope. The authorization digest carries a salt of the floor and every scoped
  block id, so a digest reviewed at one width never authorizes another,
  including a whole destroy. Unattended rules are unchanged.
- **Flags.** `abandon=` with `--stage` is a usage error. `erase=` and
  `--unregistered` must name a Machine in the scope, or the command refuses with
  `authorize.unneeded`. `--keep-going` keeps its meaning inside the scope, and
  only a complete removal reaches the staged completion.
- **Controller binding.** The controller host binding is required: a staged
  destroy is not the context's exit.
- **Retry.** A retry with no `--stage` never deletes the kept Machines by
  accident, because it refuses and names both commands.

## CLI

- `--dry-run` shows the steps taken back under a header per stage, what each
  stage keeps, the kept power targets, the residue lines, the `--authorize`
  tokens it requires, and the sentence "the next apply restores N steps over M
  kept; values frozen in kept steps (DNS names, VIPs, release, storage ports)
  cannot change without taking those steps back".
- The confirmation names the floor and both counts.
- The receipt names the verb `destroy`, the state `done` and the next command
  `apply`. This is the one case where a done operation reports a next command.
  The `destroy` and `continue-destroy` commands in a receipt or remedy carry
  `--stage <frozen floor>` when the operation froze one. The floor never
  travels as an apply selection.
- `destroy.stage-selected` retires for `--stage`. The stops of this page enter
  the [stop catalog](../cli/stops.md) with their slices.
- The JSON plan and operation shapes gain a `scope` object with the floor, the
  removed steps, the kept steps, the residue and the kept power targets.
  `takesBack` keeps its meaning of lineage operations. `status` gains the
  trimmed standing.
- The ask rules that cover `destroy` cover `destroy --stage`.

## Scenarios

| Scenario | Result |
| --- | --- |
| Cluster provisioning failed and the nodes are virtual, installed and proved | `--stage clusters` is admitted once the node Machines are kept and the root-disk reset exists ([B1861](../milestones/m1.md#b1861)); before that it refuses with `destroy.stage-keeps-host` |
| A cluster install failed before any node booted the image | `--stage clusters` takes back the agent installation on kept nodes ([B1860](../milestones/m1.md#b1860)) |
| `--stage machines` over libvirt or KubeVirt Machines | The scope deletes the Machines it names, so the scope rule holds and the context rests trimmed |
| `--stage substrates` | Takes back provider hosts and everything above them; the controller and the infrastructure services stay |
| The scope reaches a block below the floor | Refuses with `destroy.stage-below-floor` and the lower floor |
| A kept block is unfinished | Refuses with `destroy.stage-keeps-unfinished` |
| Nothing is Live at or above the floor | Settles with exit 0 |
| A staged destroy fails halfway | The same floor continues or replaces it; a lower floor widens it; no `--stage` refuses |
| Plain `destroy` over a staged destroy that is unfinished | A whole removal of Live supersedes it behind the widening confirmation |
| Plain `destroy` over a trimmed context | A whole removal of Live; the context ends pristine |
| `apply` over a trimmed context with unchanged input | A restoring apply creates the blocks that are absent and keeps the rest |
| `apply` over a trimmed context with input that changes a kept block | Refuses with `apply.kept-block-changed` and the floor that would take the block back |
| `apply` over a trimmed context with input that changes a taken-back block | A restoring apply that carries the change ([B1859](../milestones/m1.md#b1859)) |
| A kept host is in the wrong power state | Stops with the `machine start` or `machine stop` command |
| A physical install is in the scope | Refuses; the physical restore is parked ([B1864](../milestones/backlog.md#b1864)) |
| `--clusters` narrows a staged destroy | Parked ([B1865](../milestones/backlog.md#b1865)) |
| A Ceph cluster no data client can reach is in the scope | Taken back on kept hosts through the unwind below ([B1862](../milestones/m2.md#b1862)) |
| A destroy recorded by an earlier build contradicts the frozen inverse | Refuses and names the build that registered it |

## Capabilities

Each row names what a staged destroy does with the capability and the item that
unlocks the part that refuses.

| Capability | In a staged scope | Unlocked by |
| --- | --- | --- |
| libvirt and KubeVirt Machines | Deleted by the scope, or kept with their steps below the floor | [B166](../milestones/m1.md#b166) |
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
