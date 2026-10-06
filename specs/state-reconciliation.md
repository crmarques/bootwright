# State and Reconciliation Contract

This file owns mutation and durable state; [cli.md](cli.md) owns invocation
and presentation.

## Lifecycle unit

One complete validated Environment selection rooted in one environment
directory is one lifecycle unit. [API selection](api.md#environment-directory-and-selected-state)
owns graph membership. Operations cover the entire unit; a public request
cannot narrow it to kinds, clusters, machines, or ranges. A
[stage selection](#stages-and-the-pause-boundary) is the one exception, and it
narrows nothing: the plan stays complete and frozen, and the selection only
gates which of its blocks this invocation may start.

Supported operation modes are:

- a fresh full-context `apply`, optionally stopping at a stage boundary;
- continuation of a paused, interrupted or failed `apply`, or of an
  interrupted `destroy`, against its exact frozen plan; and
- a `destroy` of everything the apply [owns](#continuation-and-removal),
  whether that apply completed or stopped at a boundary, at a failure, or at an
  interruption.

There is no reconciliation, partial planning, adoption, reclaim of ownership,
or force path. The one reclaim this contract performs is the
[housekeeping](#context-mutation-evidence) of operation directories that hold
nothing. A completed apply must be destroyed before an apply of *changed*
desired state can start, and an edited input refuses by naming that removal.

A verb whose work durable state already proves performs none of it and
succeeds: an `apply` repeated over the unchanged input its completed apply
froze, while every block of that apply's plan is `done`, and a `destroy` of a
context that owns nothing, which is one holding a completed destroy every block
of whose plan is `done` or holding no operation while nothing claims it, below,
each report the completed
operation and `done` without registering an operation, opening a transaction,
binding a Secret, claiming a reservation or reaching a host. Such
an invocation requires no authorization and no confirmation, because it has no
consequence to acknowledge, and a token it is given authorizes nothing and
refuses nothing. Repeating a verb is therefore always safe, which is what lets
an operator or a script ask whether anything is left to do. A verb settles
only once its operation's finalization, below, is complete. A completed apply
whose records hold a block that is not `done` proves no such thing, so an
`apply` of its unchanged input refuses, as
[its removal does](#continuation-and-removal), rather than settling. A
completed destroy whose records hold one proves nothing removed that block's
effect, so both verbs refuse `lifecycle.state` over it, naming each such block
with the state its record reads, rather than settle or start a fresh apply
beside that effect. Neither verb has a way on from there, so deleting the
context is its only exit, and the refusal names the deletion the
[guard](#context-mutation-evidence) admits, decided from the guard's own
reading of the evidence however its record is spelled:
`bootwright context delete --name <name> --purge`, adding `--allow-orphans`
unless the guard reads the context's evidence as pristine, because only
pristine evidence admits a [deletion](contexts.md#permanent-deletion) that
acknowledges no orphan. Evidence the guard cannot read admits no deletion,
with the acknowledgement or without, so over it the refusal names none and
names restoring the whole store from a matching backup instead.

Before any verb, an operation whose blocks are all `done` but whose record,
[evidence](#context-mutation-evidence), reservations, Secret bindings or
[produced material](#produced-material-custody) do not yet say so is finalized
first; an invocation interrupted between its last outcome and its last write
leaves exactly that. The finalization runs under
the exclusive lock after re-proving the state it was decided from, without
authorization, presentation or confirmation, because it performs only the
record, the releases and the projection its records prove: it records the
operation `done`, then publishes an apply's projection, or withdraws a
removal's produced material, releases its reservations and then its Secret
bindings, its own and its apply's, and only then publishes pristine evidence.
A running, unknown or failed operation is finalized only by its own verb,
because the other verb decides for itself: a `destroy` supersedes an
incomplete apply, removing every block it started, and an `apply` refuses an
incomplete destroy. An unknown one whose blocks are all `done` has a
record that lags behind them, as a removal stopped after its resolution proved
an unknown apply's block and before it recorded the apply leaves it. A failed
removal whose blocks are all `done` lags the same way: a removal superseding it
records nothing of it before it resolves the `running` block a failed retry
start left, so one stopped after that resolution leaves it `failed` beside
blocks that are all `done`. A failed apply whose blocks are all `done` lags as
well: an executable before `4513e123` resolved the `running` block a failed
retry start left without first recording the apply in the state its blocks
gave it, so a removal it ran, stopped after that resolution, leaves the apply
`failed` beside blocks that are all `done`. Its `apply` finalizes it, recording
it `done` and publishing its projection, and its `destroy` replaces it with a
removal of its whole frozen plan, which is every block it started. A completed
operation is finalized under either verb. The verb then decides
again from what the finalization left and goes on as it would over it, and a
finalization that leaves another one due refuses `lifecycle.state` rather than
repeating. An operation holding a block that is not `done` proves no
completion and is never finalized. Once a removal's record reads `done`, the
releases and the pristine publication that follow it are made even when the
invocation is interrupted, whether it ran that removal or finalizes it. When
the Secret-binding releases or the pristine publication that follow a
removal's completion fail, the invocation fails `lifecycle.state`, beside any
log fault it reports, naming the repeated `destroy` that finishes it, and an
invocation that completed the removal itself still reports it `done`. A
withdrawal that fails fails the invocation as a reservation release that fails
does, and leaves the evidence short of pristine, so the next verb's
finalization withdraws again.

A lost index reads as no operation, so what a context holding no operation
holds is proved before anything else about it. Evidence beside no operation
other than pristine evidence and the running evidence an interrupted
registration leaves, and an operation directory whose `blocks/` lists anything
while no index names it, is state no index accounts for: a `destroy` and a
fresh `apply` over it each refuse `lifecycle.state`, naming each such evidence
and directory, and write nothing, whatever else the context holds. Evidence
spelled otherwise than the record this build publishes, even with the same
members, is such evidence. Deleting the context is its only exit, so the
refusal names it as the refusal over a completed destroy holding a block that
is not `done` does: with `--allow-orphans` unless the guard reads the evidence
as pristine, and over evidence the guard cannot read no deletion at all.

Otherwise nothing claims a context holding no operation only while its
evidence is pristine and it holds no reservation. Running evidence, or a
reservation beside pristine evidence, is what a
[registration](#context-mutation-evidence) interrupted before its index named
its operation leaves, and the destroy releases it first, under the exclusive
lock and without authorization, presentation or confirmation, because it
performs no effect: once it re-proves that the context still holds no
operation, the evidence it decided from and no state no index accounts for,
it releases the context's reservations and publishes pristine
evidence, and only then releases every Secret binding the context held before
it began; it then settles, reporting no operation and `done`. A destroy that
settles over pristine evidence and no reservation, beside no operation or a
completed destroy, opens no transaction unless the context holds a claim that
holds nothing, which it then [reclaims](#context-mutation-evidence) in a
transaction that does nothing else. No operation names a Secret binding there,
so it releases every binding it listed before it read that evidence and
reports only that it settled.

Plans remain complete and immutable; later artifact generation cannot append
blocks or expand the operation. Expansion requires destroy followed by fresh
apply.

## Context mutation evidence

Reconciliation owns a closed version-1 mutation record initialized with
`operation: none` and `ownership: none`. Recognized operation states are `none`,
`pending`, `failed`, `unknown` and `applied`; ownership is `none` or `retained`.
A paused operation records `pending` and `retained`, exactly as a running one
does, because a pause leaves its [ownership](#continuation-and-removal) in
place. A completed removal's evidence becomes `none` and `none` only once its
[produced material](#produced-material-custody) is withdrawn and its
reservations and Secret bindings are released, so evidence that is not its
completed operation's projection marks a [finalization](#lifecycle-unit) that
did not complete, or a fresh apply over it that has not registered, which the
next verb's finalization gives back the same way.
This record establishes only local disposal/update restrictions, not native
execution, readiness, ownership release or permission to run lifecycle work.
Lifecycle publication participates in the same context lease and updates this
evidence before any remote mutation. Missing or unknown evidence fails closed.
A live lease refuses every context mutation.

A fresh apply claims its [operation directory](#operation-records) and then
raises its running evidence, in an exclusive transaction of its own, before it
binds a Secret, claims the controller host, reserves or registers, so nothing
it holds is ever covered by evidence that permits update or deletion. Before it
registers, it re-proves that this evidence still holds and that the operation
directories are exactly those it listed right after its own claim, because
evidence bytes are no token: a release that lowered them and a later claim that
raised them again leave the same bytes, and only that claim's directory tells
the two apart. A directory is reclaimed, below, only while the evidence reads
pristine or once a registration has moved the index, which refuses the apply
on its own, and every raise over no operation or a completed destroy claims
first, so the claim that raised the evidence again stays listed while that
evidence stands. The directories themselves are proved rather than their
number, because a reclaim of an older claim beside a newer one leaves the same
number. The claim precedes the evidence so that no running evidence, even from
an invocation interrupted between the two, lands without one. A fresh removal
raises its running evidence immediately before it registers, and a
continuation before it marks its operation running; each publication is
skipped when the evidence already reads so.

A registration that provably did not happen, because it stopped before its
index write or its index does not name the operation it wrote, releases the
binding it created and restores the evidence the index then implies: the
projection of the operation it names, read again, or pristine for none. The
evidence is its to restore once it may have published it, or once it claimed a
directory under running evidence it found, because every apply that claimed
before it then refuses. A claim that failed, interrupted or not, counts as one
unless a listing of the operation directories read after it proves its
directory absent, because a claim can fail after creating it; an older apply
that such a failure provably left alone keeps its evidence and registers. The
running evidence stays instead when a newer claim
raised it since, which makes it that claim's, and when the index names no
operation or a completed destroy while the context holds a reservation no
operation owns, which keeps the context protected until a destroy or its next
apply releases it. A restoration that fails is reported beside the failure that
caused it. A registration that may have happened, because its index write
failed where it may have landed, keeps its running evidence and its binding,
which the operation it may have registered needs. A Secret binding no operation
names, which an interrupted registration or a release that failed leaves, is
released once the context's next fresh operation registers, after its pristine
evidence when that operation is a removal that completes, once a removal's
[finalization](#lifecycle-unit) publishes pristine evidence, by a destroy's
release of what an interrupted registration left, or by a destroy that
[settles](#lifecycle-unit) over pristine evidence and no reservation; a
continuation collects nothing, and a release that fails leaves the binding for
the next of them. Each reads the context's bindings before its first
transaction, or a settling destroy before its decision, and releases only what
it read and does not itself keep, so a binding issued since is never touched,
and each fresh apply still in flight that bound one of them refuses at its
re-proof rather than register it. The releases are made even once the
invocation is interrupted. A listing that fails collects nothing, because
collection is housekeeping no transition may be refused for.

A claim that holds nothing, only the empty `blocks/` and `logs/` a claim
creates, is what a fresh apply that never registered leaves, as does a
registration stopped before its plan landed. Reclaiming such a claim removes
its directory and nothing else: it changes no evidence, reservation, Secret
binding or ownership, so it is not the reclaim of ownership the
[lifecycle unit](#lifecycle-unit) rules out. A transaction that gives back
pristine evidence, which is a restoration, a destroy's release of what an
interrupted registration left and a completed removal's pristine publication,
then reclaims each such directory other than the one its index names, once the
evidence reads pristine and before it releases the lock, children before their
directory, so one reclaimed part way still holds nothing and goes with the
next. An apply stopped after its claim and before its running evidence landed
leaves such a claim under pristine evidence that no restoration follows, so a
destroy that [settles](#lifecycle-unit) beside one reclaims it the same way, in
a transaction of its own that reclaims nothing unless the evidence still reads
pristine under its lock. A restoration beside a reservation no operation owns
keeps its running evidence, and with it its claim, so the transaction that
registers a fresh
operation reclaims the same way once its index names that operation: every
fresh apply planned before it then refuses at its re-proof whether or not its
claim is still listed, and none claims again before it decides from the new
operation. A refused retry's claim therefore lasts only until the next
pristine publication or registration, and a stopped one's until those or the
next settling destroy, so none is left to count toward the
[retained-operation bound](contexts.md#storage-locking-and-publication) or the
operation area's entry bound when a removal registers, and a destroy frees a
context whose claims that hold nothing fill that bound, where every fresh
apply refuses before it raises anything. A directory holding
anything else is kept, and a reclaim that fails is housekeeping: it refuses
nothing and leaves the rest for the next pristine publication, settling
destroy or registration.

The guard allows update only without pending, failed or unknown operations;
recreation/final deletion requires `none` operation and `none` ownership.
Protected contexts remain named and selectable for status, exact continuation
and destroy. Recreation never bypasses protected evidence.

Deletion refuses protected evidence by default and names `destroy` as the way
to release it. An explicit orphan acknowledgement waives that one verdict over
recognized evidence, and nothing else: the objects the evidence still
attributes to the context are abandoned in place, so they survive unmanaged and
unreferenced, and no further command can discover, continue or remove them
through Bootwright. Missing, corrupt or unsupported evidence and a live lease
still refuse under the acknowledgement, because an unreadable record names
nothing an operator can acknowledge. The acknowledgement replaces no other
deletion safeguard, performs no remote effect, and there is no recovery-only
archival mode. The deleted context's host reservations go with it, as
[permanent deletion](contexts.md#permanent-deletion) states, so the keys they
name no longer refuse another context, while the abandoned objects themselves
stay where they are.

The [Workspace context contract](contexts.md) defines publication and permanent
local deletion under this guard.

## Durable identities and private paths

[Contexts](contexts.md) owns the versioned registry, immutable input revisions,
selection transactions and permanent deletion.

Workspace owns selection and verification of `<state-root>` and reservation
of each context name. `<state-root>` is a canonical absolute Bootwright-owned
runtime state directory outside both the desired-state input root and selected
environment directory. It is never either directory, a descendant of either,
or a path derived by appending to either. Desired-state discovery never enters
it.

[Contexts](contexts.md#storage-locking-and-publication) owns the production
root path, its ownership and modes, the executable-file exception for immutable
controller bundles, and the rule that no ambient value selects runtime storage.
The [CLI privilege boundary](cli.md#local-privilege-and-user-identity)
provides root execution only for valid available commands that require it.

Context-free read-only commands do not access the root except available
[Controller inspection](controller.md#selection-and-command-journeys), which
may read shared host metadata without selecting a context. Context-backed reads
acquire the existing store's shared lock and never create, repair or migrate
state. A bounded run takes the same shared lock and creates at most one thing:
the file its own adapter output is
[retained](cli/output.md#bounded-run-output) in, once its private runtime is
admitted and only for a run whose result names it, so a reading creates
nothing. That file is troubleshooting material, never state, so a run still
repairs and migrates nothing. The transient Secret binding a bounded run or a
bounded consumer makes is released when its call returns, a call ended by
cancellation included: the release outlives the cancellation as every
recording does.
Workspace defines the narrow explicit retry of pending creation/deletion.

State reconciliation owns `<operation-id>`, `<block-id>`, and effect- and
resolution-attempt numbers. The identity contract is:

| Identity | Grammar and scope |
| --- | --- |
| `<name>` | The context name: unique within `<state-root>` for one lifecycle unit under the [context identity contract](contexts.md#identity-and-selection). |
| `<operation-id>` | Immutable and unique within one context; allocated before its operation record. |
| `<run-id>` | Immutable and unique within one context; allocated before the bounded run it names, and never an operation identity. |
| `<block-id>` | Immutable and unique within one operation's frozen plan. |
| `<attempt-number>` | Monotonic within one block, from `1` through `999999`, never reused, and rendered as six decimal digits. |
| `<resolution-number>` | Monotonic within one effect attempt, from `1` through `999999`, never reused, and rendered as six decimal digits. |

Attempt- or resolution-number exhaustion refuses before observation or effects
and never wraps or reuses an earlier path.

Context names and operation and block IDs are canonical lowercase ASCII
segments matching `[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?`. They contain no
separator, dot segment, whitespace, encoding escape, or user-facing
description. Each is validated before lookup or path construction and never
comes from an Ansible role, play, task, host alias, or vendor response.

A context is identified by its name alone, under the
[context identity contract](contexts.md#identity-and-selection); Workspace
reserves it at creation, before input or operations exist.
State reconciliation allocates operation IDs as `op-` followed by 32 lowercase
hexadecimal characters from 128 OS cryptographically secure random bits, and
run IDs the same way under `run-`, so no identity can be read as the other's.

Reserve operation candidates exclusively, checking existing identities; a
collision retries with fresh entropy at most 16 times. Random failure has no
clock, process-ID, hash or weaker fallback. Pending operations remain
attributable to their original identity. Corruption or contradictory mapping
refuses rather than allocating a replacement. Context data resides beneath
`contexts/<name>`; runtime records reside beneath its `state/`.

### Operation records

Reconciliation owns one operation subtree per context, beside the mutation
evidence it maintains:

```text
contexts/<name>/state/operations/
  index.json
  <operation-id>/
    operation.json
    plan.json
    blocks/<block-id>/state.json
    blocks/<block-id>/attempt-NNNNNN.json
    blocks/<block-id>/attempt-NNNNNN-resolution-NNNNNN.json
    logs/
```

`index.json` names at most one current operation and nothing else. Every other
operation directory is kept for audit and never named by the index: a
completed operation, one a removal superseded, and the unreferenced directory
an interrupted registration leaves once its plan landed. A later command reads
one again only as the current operation's `source`, and each counts toward the
[retained-operation bound](contexts.md#storage-locking-and-publication). A
fresh apply's `<operation-id>/` exists, holding only its empty `blocks/` and
`logs/`, from the transaction that raises its
[evidence](#context-mutation-evidence); its identity is allocated against every
operation directory, and its registration fills that directory without
counting it against the bound a second time. A claim that never registered
holds nothing to audit, so the next pristine publication, registration or
destroy that settles beside it [reclaims](#context-mutation-evidence) it.
`operation.json` binds the operation to its verb, context
identity, input revision and digest, plan digest, selected implementation and
automation identities, executable identity, the Python and Ansible
[execution closure](#dependency-safety-during-recovery) it registered with,
secret bindings, durable state and log-fault flag. `plan.json` is the
immutable frozen plan: every block with its
description, dependencies, impacts, presentation groups, resolved
implementation identity, content digest and canonical secret-free request.
Block records carry the block state and the number of attempts started, one
less than the next attempt's number; attempt and
resolution records carry their block, attempt and resolution numbers, phase,
outcome, effect, bounded evidence and timestamps, and never restate the
request: the request an attempt or resolution acted on is its block's in
`plan.json`, which the operation's plan digest names and nothing replaces. A
running attempt may additionally publish one bounded `preparation` object: the
before-state its capability observed, recorded durably while the attempt is
still running and before it is permitted to change the host. It is written once
and never replaced, so a later recovery can tell an attempt that was authorized
to install from one that never reached that point. A resolution observation
publishes none, because an observation authorizes no effect.
[The output contract](cli/output.md#private-operation-logs) owns the `logs/`
tree.

Records are closed, canonical, bounded JSON published atomically beneath held
verified handles, with the directory and file modes of
[the context store](contexts.md#storage-locking-and-publication). An operation
or plan record is at most 1 MiB, an attempt or resolution record at most
64 KiB, and an index at most 256 KiB. Unknown fields, duplicate keys,
noncanonical encodings and unsupported versions refuse; there is no repair,
migration or scan-based adoption of an unpublished record. An operation record
is version 2, and a registration writes no other. A version 1 record, which a
build before the execution closure was frozen wrote, names no closure; it
still reads, updates as itself and serves a fresh verb and `status`, and only
its continuation refuses. Every read of a frozen plan holds it to its
operation's record, as the registration did: a
`plan.json` whose digest or verb is not the one that record carries refuses
`lifecycle.state`, however valid a plan it is, as does one beside no operation
record, so a replaced plan is never continued, removed, previewed or reported.
An attempt record is created exclusively, so a reused attempt number refuses
rather than overwriting durable evidence. An exclusively created record is
staged and then renamed without replacement, so its name appears only with
complete, synchronized bytes.

## State machine

| Durable state | Allowed lifecycle transition |
| --- | --- |
| no operation, or completed destroy | start a fresh apply; a `destroy` settles without effect, over no operation first releasing what an interrupted registration left |
| no operation beside evidence or records no index accounts for | none: `apply` and `destroy` refuse, naming `context delete --purge`, with `--allow-orphans` unless the evidence is pristine, and no deletion over evidence the guard cannot read |
| completed destroy holding a block that is not `done` | none: `apply` and `destroy` refuse, naming `context delete --purge`, with `--allow-orphans` unless the evidence is pristine, and no deletion over evidence the guard cannot read |
| apply running | continue that exact apply, or start a fresh destroy of the blocks it started |
| apply failed | continue that exact apply, or start a fresh destroy of the blocks it started |
| apply failed, every block `done` | an `apply` finalizes it, recording it `done` and publishing its projection, or start a fresh destroy of its whole frozen plan |
| apply paused | continue that exact apply under any stage selection, or start a fresh destroy of the blocks it started |
| apply unknown | resolve the exact unknown block, or start a fresh destroy of the blocks it started, which resolves that block first; start no other effect or retry |
| apply done | start a fresh destroy; an `apply` of the unchanged input settles without effect, and of a changed input refuses; both the destroy and that apply refuse while a block of the plan is not `done` |
| destroy running | continue that exact destroy |
| destroy failed | start a fresh destroy of what it has not removed |
| destroy unknown | resolve the exact unknown block; start no effect or retry |

Before any row, an operation whose blocks are all `done` is
[finalized](#lifecycle-unit) first while its record, evidence, reservations or
Secret bindings do not yet say it completed; a running, unknown or failed one
only by its own verb. A continuation refuses records that
contradict what its operation started before it restores, raises or marks
anything ([continuation and removal](#continuation-and-removal)). Beside any
row, a continuation or removal that reopens a Secret binding the context's
keyring no longer lists, or can no longer read, refuses until the keyring is
restored from a complete backup, and otherwise the context's only exit is an
orphan-acknowledged deletion
([continuation and removal](#continuation-and-removal)).

Changed desired state never turns continuation into reconciliation. A
continuation verifies, before doing work, the operation's verb, its context
identity and input revision, the frozen-input digest, the plan digest its
record names, the block states and attempt counts it read (unchanged since the
command read them), that this executable embeds the automation the operation
registered under, and the host binding with completed setup. Each block then
runs the capability this executable offers under the implementation identity
the block froze. A block whose implementation this executable does not offer
refuses before its attempt or resolution starts. A block's content digest
travels with the block and is not compared, because a removal keeps the one
its apply froze.

This table and the four below state the transition code exactly:
`TestTransitionTablesMatchSpec` drives that code with every row of each, so a
row changes only together with the code.

### Attempt outcomes

An attempt records the effect and block state its capability's outcome
justifies. An outcome outside this set is recorded as `unknown`, and an attempt
its invocation's cancellation interrupted as `canceled`, whatever it reported.
An attempt whose required log failed before its outcome was logged records a
reported `failed` as `unknown`, because a typed failure proves less than
completion, and one whose log could not be created records `unknown` without
running its effect.

| Adapter outcome | Effect state | Block |
| --- | --- | --- |
| `changed` | `completed` | `done` |
| `unchanged` | `completed` | `done` |
| `failed` | `unknown` | `failed` |
| `canceled` | `unknown` | `unknown` |
| `unknown` | `unknown` | `unknown` |

A typed failure proves nothing about the target, so its effect stays `unknown`,
yet its block is `failed` rather than `unknown`: the retry that follows
[converges](#converging-an-effect) whatever the attempt left. An attempt never
records `no-effect` or `partial`; only a resolution does.

### Resolution outcomes

A resolution is the read-only observation that
[converging an effect](#converging-an-effect) defines, and it is the only way
an unproved block moves. An operator starts one only by repeating the
operation's verb: a continuation observes every unproved block before it starts
anything else, and a fresh `destroy` over an incomplete apply observes them
before it registers. No command edits a block state. An observation that fails
or reports an effect outside this set is recorded as `unknown`.

A block is observed for the verb its operation froze, so each row below proves
that verb's effect. A destroy block's completion is its removal's own
postcondition, as its capability states it, so a target that still shows what
the removal takes back, and is this context's own, is positive no effect or a
positive partial realization, as its capability states which. What a removal
keeps by design proves nothing against it. An observation that cannot tell the
removal's postcondition from a target it could not read never proves
completion: its capability reads it as no effect, and the retry that follows
proves its own absence, since a removal over an absent target completes (see
[attempts and unknown outcomes](#attempts-and-unknown-outcomes)). A fresh
`destroy` observes the incomplete apply's blocks as that apply's.

| Observation proves | Effect state | Block |
| --- | --- | --- |
| Positive completion | `completed` | `done` |
| Positive no effect | `no-effect` | `failed` |
| Positive partial realization this context owns | `partial` | `failed` |
| Failed, forbidden, empty, malformed, contradictory, or foreign observation | `unknown` | `unknown` |

The operation then takes the state its blocks give it under
[precedence](#operation-state-precedence), and a block resolved `failed` is
retried by a new attempt, which converges it.

A resolution records an outcome beside that effect. A completed effect records
the outcome its capability proves the effect had, `changed` or `unchanged`, as
an observation that repeats its apply's own proof does, and `changed` when the
capability proves neither, because the attempt it completed may have changed
its target. Positive no effect and a positive partial realization record
`failed`, and an unproved observation `unknown`: an outcome the capability
states counts only beside a completion.

### Produced material custody

Material a block's proved effect leaves, such as an installation's
administrator kubeconfig, enters the context's
[custody](secrets.md#produced-material) before its apply block is recorded
`done`, whether an attempt or a resolution proves it, including a fresh
`destroy`'s resolution of an incomplete apply's block. The engine publishes
one block's outputs in one publication, inside the operation's transaction
through the secret area the Workspace lends it, and a capability offers them
only from a proved completion. A publication that fails records the attempt
or the resolution `unknown`, never `failed`, because the effect it follows was
proved: it never leaves a block `done` whose material custody does not hold,
and the next invocation of either verb, including a `destroy` before it
registers, observes the block again and captures what that proves before any
effect or inverse follows. A `failed` block would be retried by an `apply` but
never observed by a `destroy`, whose inverses could then delete the only copy.
A removal's attempt and resolution capture nothing. A completed removal
withdraws every entry inside the transaction that records or finalizes its
completion, before it releases its reservations, and a removal that stops part
way keeps them, so the only access a partly removed context still has is never
the first thing it loses. No record, log, evidence or adapter output carries
the material.

### Block transitions

| Block state | Step | Leads to |
| --- | --- | --- |
| `pending` | `attempt` | `running`, then `failed`, `unknown` or `done` |
| `failed` | `retry` | `running`, then `failed`, `unknown` or `done` |
| `running` | `observe` | `failed`, `unknown` or `done` |
| `unknown` | `observe` | `failed`, `unknown` or `done` |
| `done` | `none` | `done` |

An `attempt` starts once every dependency is `done` and the block's stage is
selected; a `retry` is a new attempt of the first failed block the selection
admits, run alone. Either records `running` before its first side effect and
then its [attempt outcome](#attempt-outcomes). An `observe` step is a
resolution and records its [resolution outcome](#resolution-outcomes);
unproved blocks are observed before any other step starts.

An attempt that cannot start, because this executable lacks its implementation
or the store refuses to allocate its number and record `running`, performs no
effect and records no outcome. The log-fault flag is unchanged, and so is its
block record, so the block keeps the state it had: `pending`, or the `failed`
it was retrying. A start whose final publication lands and then reports a
failure is the one exception: its block record reads `running` and counts the
attempt, although the invocation still holds the block in the state it had, so
a retry stopped this way leaves its operation `failed` beside that `running`
block, which the next invocation observes as it does any. The invocation that
meets it admits nothing further, never admits
that block again, and waits for what is already running. Its operation then
takes the state its blocks give it, as the invocation holds them, under
[precedence](#operation-state-precedence), and the stop is never a pause. The
result names that failure as its block's cause, as `runtime.internal` naming
the block when the failure carries no diagnostic of its own, and never in the
failure's own words.

A block that an invocation finds `running` lost its executor mid-attempt.
Nothing recorded its outcome, so it is unproved exactly as an `unknown` block
is: the next continuation or removal observes it under the exclusive lock, and
it becomes `unknown` only when that observation proves nothing. A resolution
that cannot start observes nothing, so the block stays `running` and its
operation keeps its state; an execution stopped with a block still `running` is
never a pause.

### Operation state precedence

When an invocation ends, the operation records the state of the first row its
blocks satisfy:

| Operation state | Holds when |
| --- | --- |
| `unknown` | any block is `unknown` |
| `failed` | any block is `failed` |
| `done` | every block is `done` |
| `paused` | its execution paused: it stopped uncancelled with work left, no log fault and every start recorded |
| `running` | otherwise |

## Plan and execution

[Secrets](secrets.md#immutable-binding-and-contexts) owns confidential immutable
binding; this contract is its lifecycle consumer, through the
[lifecycle ports](architecture.md#lifecycle-ports).

Planning is pure and read-only. It performs no downloads, remote mutations,
cache or registry writes, cleanup, lock takeover, or secret materialization.
A fresh mutation validates the complete graph, freezes input and non-secret
external content, binds each consumed secret to confidential immutable material
or an immutable external version, and durably publishes the immutable plan and
pending registry before its first platform side effect.

A plan is a dependency DAG of stable blocks. Each block has an ID, description,
stage, dependencies, the host resources it will not share, impacts, and
execution kind. Operations are `running`, `paused`, `failed`, `unknown`, or
`done`; blocks are `pending`, `running`, `failed`, `unknown`, or `done`; the
effect an attempt or resolution records is `no-effect`, `partial`,
`completed`, or `unknown`.

A plan is written wave by wave. A block's *wave* is one past the deepest block
it waits for, and a block that waits for nothing is in the first wave. Blocks
are frozen in wave order and, within one wave, in identity order, so the
numbered plan an operator confirms is the order the work is started in rather
than one arbitrary sequential walk of the same graph. A wave is never a
barrier: a block starts as soon as its own dependencies are done, so a later
wave overlaps an earlier one. Equal definitions in any input order still
produce an identical plan and an identical digest.

A definition may name host resources its block *will not share* while it runs.
Two blocks that name one resource never run at the same time, however
independent the graph says they are, because the graph orders what one block
needs from another and not what two of them would write to at once. The set is
bounded, unique, ordered and frozen with the plan, and a removal keeps exactly
the set its apply froze, because what must not run together to create
something is what must not run together to remove it.

A block's description and impacts state what the planned verb does, never what
the opposite verb would do. A destroy block describes the removal it performs
and lists only the effects it performs: a resource an apply creates, publishes
or opens is one the destroy removes or closes, and one a removal deliberately
retains, such as shared host software another context may still need, appears
in no impact at all. The description of such a block says that it retains.

A capability names what its blocks depend on in domain terms, not by block
identity: a definition may carry *requirements*, each the kind and name of an
API object whose realization must be done first. Reconciliation resolves every
requirement to the blocks that realize that object before it freezes the plan,
and refuses before registration when no block realizes it, so a capability
never learns another capability's block-identity grammar and a frozen plan
carries only resolved dependencies.

An apply attempt receives what each block it depends on durably proved in this
operation: that block's kind, object, implementation, verb and state, and the
evidence of the record that last settled it, handed over unread in frozen plan
order. That record is the block's last attempt, or, when a resolution observed
that attempt, the highest-numbered resolution allocated against it, because
only an unproved attempt is resolved and what the resolution observed is then
what the block proved. Its evidence is handed over once that record completed;
a block that never started, or whose settling record is still running, hands
over none rather than an earlier record's. It is read before the attempt
starts, so a record that cannot be read leaves the block as it was. A
removal's attempts, every resolution and every quiescence probe receive none.

A definition may also name the authorization tokens its effects *consume*,
frozen with the plan and covered by its digest; the
[authorization rules](#confirmation-and-authorization) require exactly the
union a plan consumes.

### Stages and the pause boundary

Every block carries exactly one stage, frozen with the plan and covered by its
digest. A stage names the kind of platform work its block performs:

| Stage | Blocks |
| --- | --- |
| `controller` | The [context prerequisites](controller.md) this Environment adds to its controller host: the target clients its graph selects and the libvirt client it declares. |
| `infra-components` | Managed shared services: proxying, name resolution, time, artifact serving, registries and load balancing. |
| `substrates` | [Provider host realization](substrates.md#provider-host-realization) for a declared `InfraProvider`: its virtualization runtime, managed networks and virtual-media pool. |
| `machines` | [Machine realization](substrates.md#machine-realization) with its management controller, the [claim and proof](substrates.md#physical-machine-realization) of a physical machine, and [managed operating-system installation](managed-os.md#installation). |
| `clusters` | Container and storage cluster installation. |
| `add-ons` | Add-on instances bound to a cluster. |

The capability that plans a block owns its stage. Stages are not strata: a
block depends on other blocks, never on a stage, so a `substrates` block may
legitimately wait on an `add-ons` block when a provider is hosted by a cluster
that an add-on enables. Ordering always follows the dependency DAG.

The `controller` stage is the one exception, and the engine owns it rather than
any capability. When a plan contains a controller block, every other block in
that plan depends on it, because the clients it installs are what the other
blocks' adapters run. The edge is a real block dependency frozen with the plan
and covered by its digest, not a rule about stages; a plan with no controller
block has no such edge and is ordered by its capabilities alone. A context that
selects no client beyond the host baseline contributes no controller block at
all.

A controller block is the only block whose effects are shared host state rather
than this context's own. Its closure therefore outlives the context that
selected it: the block's removal retains what it installed, and the shared area
it published into is proved, not deleted, by a destroy. Because it extends what
[controller setup](controller.md) prepared, the engine hands that block the
retained setup evidence, the shared publication area and its sealing, the
durable dependency record that precedes acquisition, the before-state
publication that precedes a host transaction, and the native package read lock
its own transaction has to take over. No other block receives them.

A stage selection is the set of stages an invocation may start; an omitted
selection admits every stage. A block is *ready* when it is `pending` and every
dependency is `done`, and *startable* when it is ready and its stage is
selected. Execution starts every startable block, in frozen plan order, up to a
bound, and re-evaluates as each one settles. A block is admitted only while no
block already running holds a resource it will not share.

The bound is how much one host is asked to do at the same time, not what the
plan permits, so it belongs to the executable and is never frozen with the
plan, never named by desired state, and never a public flag: what may run
together is the graph's answer and does not change between invocations. A
continuation of an operation frozen by another build therefore runs under this
build's bound, which changes no effect, no order and no evidence.

Not yet met: roles still share host scratch paths, so the bound is one block;
tracked as [B303](milestones/m3.md#b303).

An operation is `paused` when execution stops because no block is startable,
nothing is still running, no block is failed or unknown, and pending blocks
remain. A pause is a
successful, resumable stop, not an interruption: it needs no recovery, it
allocates no new identity, and the next `apply` continues the same operation
under whatever selection it is given. Cancellation is never a pause.

Selection never weakens a safety rule. An unproved effect is resolved before
anything else whatever stages are selected, because resolution is a read-only
observation; several unproved effects may be observed together, and nothing
starts, retries or is removed beside them. A failed block remains the only
retry candidate and runs alone, because what follows it depends on it
succeeding: a retry starts only while no other block is running and only when
its stage is selected, so failed blocks are retried one at a time in frozen
plan order and one outside the selection is never retried. When no failed
block's stage is selected the operation refuses `lifecycle.stage` before any
effect, naming the stage of the first. A selection that admits no startable block also refuses
`lifecycle.stage` before registration, naming a stage that would unblock work.

A fresh `plan` and a fresh `apply` refuse, whatever stages are selected and
before registration, every selected object this executable cannot realize: an
effect-bearing object of a kind no capability claims, such as a storage or
add-on object or a managed `Registry`, an enabled `CustomPlaybook`, and every
shape a capability reports unsupported. Each object is its own
`lifecycle.unsupported` diagnostic, whose object is the refused one and which
carries the reason its capability gives, or that no capability claims its kind,
and the remedy, so `plan` says why as `apply` does; an object refused for two
reasons is two diagnostics. A frozen plan requires a resolved
implementation for every block; a removal planned from a frozen plan is never
refused this way.

A fresh `plan` and a fresh `apply` also refuse, before registration, every
block whose request holds a template delimiter, `{{`, `{%` or `{#`, in a string
or a mapping key, or a mapping key named exactly `__ansible_unsafe`,
`__ansible_vault` or `__ansible_type`, whatever the block's stage, the
controller prerequisites block included. No Bootwright request carries template
syntax or such a key of its own, so one there came from an authored or remote
value: a delimiter a runner hands Ansible as [data](security.md#process-boundary),
and a key no runner can hand it as data, so a request holding one would refuse
at every run of its block, its removal included. Each such block is one
`api.value` diagnostic whose object is the block's, naming the first field
holding a delimiter or such a key, how many more of its fields hold one, and
the remedy: remove the delimiters and keys from the values the object is
planned from, import them with `bootwright context update`, then run
`bootwright plan --context` with the context. A removal or a continuation
planned from a frozen plan is never refused this way.

Every `plan` previews exactly the decision the verb it previews takes, and one
path takes both. That verb is a fresh `apply` over no operation or a completed
destroy, the `destroy` of a completed apply, and an incomplete operation's own
verb. A fresh apply's decision refuses, over no operation, state no index
accounts for, and over a completed destroy one holding a block that is not
`done` ([lifecycle unit](#lifecycle-unit)); it then compiles the frozen input,
refuses what this executable cannot realize, plans, refusing a block whose
request holds a template delimiter or a key ansible-core reserves and then two
of its own blocks' conflicting
[socket claims](infrastructure-services.md#host-reservations),
refuses `lifecycle.state` for a plan with no block, and refuses a selection
that admits no startable block; a continuation's refuses records that
contradict what its operation started, and a continued apply's then refuses a
selection as the rules above do; and a removal's refuses, as
[continuation and removal](#continuation-and-removal) requires, a completed
apply holding a block that is not `done`, records that leave nothing to remove,
and a frozen request this executable cannot read. A failed removal is previewed
as the fresh removal `destroy` starts over what it has not yet proved gone,
unless its blocks are all `done`, when its finalization is due.
`plan` over an incomplete apply previews its continuation, never the removal a
`destroy` would start over it, so the refusal of records that contradict what
an incomplete apply started stays that `destroy`'s. A `destroy` accepts no
stage selection, so its preview refuses `lifecycle.stage` for any. Where a
[finalization](#lifecycle-unit) is due, the preview decides as the verb does
once that finalization is done, without performing it, and where the verb then
settles, the preview says the verb only completes that finalization. The
preview and its verb report a refusal of that decision with the same code,
message and remedy, and a preview that succeeds shows the plan the verb then
presents. The preview stops at the decision: it finalizes nothing, binds no
secret, claims neither the controller host nor a host reservation, takes no
exclusive lock and registers nothing, so a refusal from those steps, like the
authorization and confirmation gates, a continuation's re-proof of its input,
automation, execution closure and host, and a removal's resolution and
quiescence proof, stays the verb's.

A domain capability may define ordered presentation groups within its block for
one operation performed across one or more Machines. The capability owns each
group's stable ID, non-empty safe single-line description, non-empty Machine
target set, and outcome meaning. A group ID is unique within its block and uses
the same safe-segment grammar as a block ID. The plan freezes group definitions
and order before effects. An adapter returns results keyed by those IDs and
cannot invent, rename, or reorder a group; Ansible play and task names remain
private adapter detail.

Each Bootwright-controlled executable block names a logical domain capability,
not an Ansible role or vendor operation. Before the plan is published, Go
deterministically resolves exactly one matching implementation and freezes its
identity, version, content digest and request digest in the plan and operation
registry, and the operation record freezes the automation and the Python and
Ansible [execution closure](#dependency-safety-during-recovery) every
implementation runs in. That digest covers the canonical
secret-free request and only non-sensitive immutable secret-binding
identifiers or versions; it never hashes secret material. Secret bytes and any
sensitive binding metadata remain solely in the confidential snapshot or
external-version mechanism above. A missing, ambiguous, unsupported, or
drifted implementation refuses before the affected side effect; continuation
never re-resolves to a different implementation.

### Dependency safety during recovery

Continuation and destroy use the exact frozen dependency versions and content.
If required content is unavailable, changed, or its safe use for the requested
operation cannot be established, refuse before the affected observation or
effect and report the recovery requirement. Preserve the plan and ownership
evidence; never silently upgrade, substitute a dependency, or discard recovery
state.

Every effect of an operation runs inside the execution bundle the host's
completed setup approves, and a registration freezes that bundle's Python and
Ansible *execution closure* in the operation record: an identity over the
platform, the interpreter release and layout, the approved bytes of every
Python and wheel source and the provided execution foundation the closure was
qualified against, and its Python and `ansible-core` releases. The index
documents the closure was resolved from, the automation projected beside it
and the projection that automation moves are no part of it, so setup carrying
the same closure onto other automation keeps it. The native package closure
setup installs is host-owned: the host's package manager may update it and
setup may solve its transaction again, so it is neither frozen nor compared,
and a bundle republished only for a native re-solve carries the same closure
and continues.

Before a continuation of either verb restores its operation's log, raises
evidence or marks its operation `running`, it refuses `lifecycle.state`, with
no effect and no record changed, when this executable's automation differs
from the one its operation registered under, when its operation record is the
version 1 record that names no closure, or when the approved bundle holds
another closure. Each refusal names the executable version and commit the
operation registered under, and the closure refusal also names the Python and
`ansible-core` releases the operation needs and the ones the bundle holds. Each
remedy restores what the operation registered under, and where a fresh removal
may [supersede](#continuation-and-removal) the operation, it also offers that
removal. The one exception is a version 1 record once the controller directory
keeps [setup runs](contexts/controller-record.md#setup-runs): every build that
wrote such a record predates them and refuses that directory, so the refusal
says so and names no build. It offers the superseding removal where one may
take the operation's place, and otherwise the
[deletion exit](#lifecycle-unit) of the records neither verb acts on. Only a
fresh verb runs under the build and bundle in hand.

### Attempts and unknown outcomes

The executor allocates the next attempt number and durably records `running`
before a block's first side effect. Each attempt durably records the effect
state its [outcome](#attempt-outcomes) justifies, `completed` or `unknown`, and
only positive evidence permits an effect state other than `unknown`. A
Bootwright-controlled block reaches `done` only after its
effect plus required ownership and completion evidence are durable. It reaches
`failed` only with a typed failure whose evidence and capability contract
define a safe continuation or retry boundary. A direct apply or destroy result
with positive `no-effect` is a typed failure and never success. An idempotent
already-complete apply reports `completed` with completion evidence, and an
already-absent destroy reports `completed` with positive absence evidence.

Starting an attempt is one publication although it writes two records. The
store creates the attempt record exclusively and only then publishes the block
record that counts it and says `running`, so a block record never counts an
attempt that has no record. A block with no record first gains one that reads
exactly as its absence did, `pending` with no attempts, so no start leaves an
attempt record beside no block record. A start interrupted between the two
writes has performed nothing, because the executor begins an effect only once
its start returns. It leaves the block record as it was, as an
[attempt that cannot start](#block-transitions) does, beside a `running`
attempt record of the next number, and the next start of that block adopts that
record: it publishes the block record that counts it, and neither rewrites the
record nor allocates another number, so numbering stays monotonic and no path
is reused. The start takes its record only from the one path its block
record's count names, and lists the block's records only to refuse a
resolution allocated against that number, so this is not the scan-based
adoption [operation records](#operation-records) forbid, and it adopts only a
record that can be nothing but an interrupted
start: the block record exists, and the record is a canonical attempt record of
a supported version, is `running`, names that block and number and no
resolution, has published no `preparation`, and has no resolution allocated
against it. Anything else at that number refuses `lifecycle.state` and writes
nothing, including an attempt record beside no block record, which is how a
lost block record reads: starting over an effect that may have begun would skip
the observation an unproved effect requires.

### Converging an effect

Every attempt converges rather than acts: it observes the target, proves what
is this context's own, and performs only the difference between that and its
frozen request. An absent target is created. A target that is ours and already
equals the request reports `unchanged` and changes nothing. A target that is
ours and differs is converged only where its capability declares that
difference convergible, and otherwise fails naming the difference rather than
replacing what it cannot safely replace. A target that is not ours is foreign
and fails without exception. Identities a capability derives are deterministic,
and an identity a target already holds is offered back to the provider rather
than allocated again, so a repeated attempt addresses the same object instead
of colliding with it.

This is what makes a retry safe, and every retry in this contract depends on
it: a failed block is retried by repeating its operation, and a block resolved
from a partial realization is retried the same way. Each capability owns which
of its own differences are convergible and names them beside its replay rules.

A lost response, cancellation, required-log failure, or contradictory
observation moves the attempt effect state, block, and operation to durable
`unknown` unless positive evidence already proves completion or no effect. A
dead executor records nothing and leaves its block `running`, which every later
invocation treats as `unknown` ([block transitions](#block-transitions)). Once
an attempt's outcome is decided, the records that state it are written under a
boundary the cancellation does not reach, because an interruption that recorded
nothing would leave its block durably `running`: unproved, so no later
operation may continue past, remove, or delete it until a new observation
resolves it, although the attempt had already proved its outcome. Cancellation
stops the next attempt from starting; it never suppresses the record of the
attempt that already ran. Resolving `unknown` is a read-only, capability-owned
observation against the frozen request, the verb it was frozen for and the
exact target identity. Before any resolution
observation—including local process, network, or remote probing—Bootwright must
restore the required operation logging boundary as defined below, durably
allocate the next resolution number for the exact unknown effect attempt, and
securely create its separate required resolution-attempt log. The resolution
identity and log path
are durable before observation begins. Failure at any of those steps performs
no observation and leaves the effect state, block, and operation unchanged. A
logging-boundary restoration or resolution-log creation failure sets or
preserves the durable log fault; a resolution-number allocation failure leaves
its prior log-fault value unchanged.

Resolution permits only the evidence-backed transitions of the
[resolution outcomes](#resolution-outcomes).

A partial realization is the ordinary outcome of an interrupted effect, and
resolving it to `failed` is what lets the context move: an `unknown` block
starts no retry, no dependent block, no removal effect and no deletion, so a
target that is provably this context's own and provably incomplete must not be
left there. What a capability accepts as partial is its own, under the rule of
[converging an effect](#converging-an-effect), and it is never a target it
cannot prove is ours: a foreign or unreadable observation stays `unknown`.

Resolution-log failure requests cancellation, preserves its identity without
reuse, and sets or preserves the durable log fault. It permits a transition
only when independent positive completion, no-effect or partial evidence is
durable; otherwise the unknown states remain unchanged. The log fault blocks
later work even after an evidence-backed transition.

While a block is `unknown`, Bootwright starts no retry, dependent block,
destroy effect, or replacement operation. Confirmation, authorization, manual
state editing, elapsed time, and a dead prior executor cannot resolve it. The
resolution evidence and resulting transition must be durable before execution
continues.

A removal may perform that resolution itself, as its first step. Planning a
removal needs no resolution, because resolving a block never changes the
[set the operation owns](#continuation-and-removal), so the plan an operator
confirms is the plan either outcome produces. Performing one does: a removal
resolves every unproved effect it would take back, read-only and against the
frozen request, before it proves quiescence and before it registers anything of
its own. It records each resolution on the operation it replaces, so what was
proved survives whether or not the removal then goes on to register. Before its
first resolution it records a `failed` apply it replaces in the state that
apply's blocks give it, as a retry first records its apply `running`, so a
removal stopped between a resolution and the record of what it proved never
leaves a `failed` apply beside no block that
[accounts for it](#continuation-and-removal). A removal that still cannot prove
an effect registers nothing and refuses, naming each block it could not prove,
because an effect no observation resolves says nothing about what it owns.

An observation that proves nothing names why. The block's capability reads the
evidence that observation recorded, or its absence when it returned none, and
names the foreign object at the target with its identity and host, the target
it could not read with its host or endpoint, or the listener with nothing of
the target behind it, together with the remedy an operator applies before
repeating the verb. Evidence the capability cannot explain gets the general
reason that the observation proved neither the effect nor its absence, and an
unproved block no resolution has observed yet says that no observation has
read it. A resolution that leaves its block unknown reports that reason and
remedy in a `lifecycle.unknown` diagnostic naming the block, unless its
observation could not run at all, which reports why it did not, so the refusal
of a removal names every block it could not prove with why. `status` reports
the same reason and remedy for each `unknown` or `running` block of the current
operation, read from the last resolution of that block's last attempt, so a
refusal and a later `status` agree. The reason changes no state: an explained
block stays `unknown` and admits no effect, no capability proves an outcome
outside the [resolution outcomes](#resolution-outcomes), and a removal is never
admitted over it. Once the operator removes the foreign object or restores the
target, repeating the verb observes the block again and goes on from what that
observation proves. Deleting the context with `--allow-orphans` instead
abandons whatever the block may own, and releases the context's host
reservations with it ([permanent deletion](contexts.md#permanent-deletion)).

A required-log write failure is also a sticky, durable operation fault: it sets
the operation record's `logFault` flag, and no later effect or retry starts
until the required private logging boundary is safely restored, even when
positive evidence resolves the affected block to `done` or `failed`. The
required logs are the operation log and each effect or resolution attempt log,
and a failure to create, append to or finalize one is the fault; retained
adapter output is not a required log. The invocation that meets it admits
nothing further, requests cancellation of what is in flight, appends nothing
more to the log that failed, records the flag under the boundary cancellation
does not reach, and returns `runtime.log`.

Restoration is proved, never assumed. Any later invocation that would observe,
probe, register or start anything while the faulted operation is the context's
current one first reopens that operation's operation log under the exclusive
lock and durably writes its opening record, and only then durably clears the
flag.
Nothing else clears it: not elapsed time, a read, a settled verb, or a write to
any other log. A failure at either step preserves the flag and starts nothing.
A continuation performs the same reopening before any work whether or not the
flag is set, and a removal performs it for the operation it replaces before
resolving any of its effects; that reopening is the restoration a resolution
requires. Restoration permits new logging but never repairs, appends to, or
replaces the failed attempt log. Missing or truncated log detail is never
reconstructed, treated as operation evidence, or used to weaken the resolution
rules above.

### Continuation and removal

Continuation skips done blocks and retries a failed block under its frozen
capability contract, one block at a time and once per invocation: an invocation
never retries a block it failed itself, because the condition that failed it
has not been shown to have changed. Go owns ordering and lifecycle state and
invokes capability ports under the
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary).

A failure admits no further work, and the blocks already running are waited
for rather than abandoned: each records the outcome it proved, and stopping one
mid-effect would turn a provable outcome into an unproved one. The result names
every block that did not complete, in frozen plan order, because blocks run
together and which of them failed first is a race rather than a fact about the
environment. Cancellation is the same: it admits nothing further and waits for
what is in flight, and it is never a pause.

Destroy is planned from the plan its apply froze, not from desired state and
not from what current code would derive from it. Every block a removal carries
keeps the identity, implementation, content digest and canonical request its
apply wrote, so a removal describes exactly the effects that exist rather than
the effects this executable would create today. An operation owns every block
it started, whatever stopped it: an effect permitted to begin is proved absent
only by its own inverse, so a block that completed, one that failed, and one
whose outcome was lost are owned alike, and only a block that never started is
not. A removal covers exactly that set and nothing the operation never started.
Ownership therefore does not move as an unproved block is resolved, because
resolution leaves it `done` or `failed` and the set already held both. An
incomplete apply that started no block, which is one still `running` or
`paused`, owns no effect, yet it is still the context's operation, which only
the exact input it froze could continue; its removal is therefore not a settled
verb but a removal that carries no block: it registers, performs no effect and
completes, which releases what the apply claimed and leaves the context at
rest. No other operation can leave nothing to remove: a completed apply's
blocks are all `done`, and a `failed` or `unknown` apply holds the block that
made it so or, where its record [lags](#lifecycle-unit) behind them, blocks
that are all `done`. A failed removal holds a block it has not proved gone,
whether the one that failed or the `running` one a retry start that failed
left, unless a
removal superseding it resolved that block `done` and stopped before recording
what it proved; its blocks are then all `done`, and the removal is
[finalized](#lifecycle-unit) rather than superseded. Records that say
otherwise contradict themselves, so a removal over them refuses before it
registers, reaches a host, or releases a binding the effects still on the host
need.

A completed apply owns its whole frozen plan, so the removal of one refuses
`lifecycle.state` while any block of that plan is not `done`, rather than
removing only the `done` rest. A lost block record reads back as `pending`, and
a removal that skipped that block would leave its effect in place and then
release the binding it needs. The refusal names the apply and every such block
with the state its record reads, points at `bootwright status`, and comes
before the removal binds, probes, registers or releases anything. A completed
removal is held to the same rule by both verbs, as the
[lifecycle unit](#lifecycle-unit) states. An incomplete apply and a failed
removal are not held to this, because blocks that are not `done` are
legitimate in both.

A removal of an incomplete apply refuses instead where the apply's records
contradict what it started, with the completed-apply refusal's code, remedy
and timing. The refusal names the apply and each contradiction, per block in
frozen plan order and then the apply's own:

- a block that reads anything but `done` while a block that depends on it
  directly reads anything but `pending`, because a block starts only once
  every dependency is `done` and a `done` block never changes;
- a block with no block record beside an attempt or resolution record of its
  own, because a start publishes a block's record before any attempt record of
  it, so only a lost record leaves one;
- an apply recorded `failed` that holds a block that is not `done`, yet no
  `failed` or `unknown` block and no `running` block whose record counts more
  than one attempt, because an apply records `failed` only once a block
  failed, a `failed` block changes only through a retry, which first records
  the apply `running`, and a `running` or `unknown` block changes only through
  a resolution, before which a continuation records the apply `running` and a
  removal records it in the state its blocks give it, which is `failed` only
  while another block is.

Each shows the records no longer say what the apply started, and a started
block whose record was lost reads back as `pending`, which a removal planned
from them would skip. An apply recorded `failed` whose blocks are all `done`
is none of them: its record [lags](#lifecycle-unit) behind its blocks, and its
removal takes back its whole frozen plan. Records that leave nothing to remove
although the apply's own state says it started a block answer first, with the refusal
of records that contradict themselves above. Only a file named exactly as an
attempt or resolution record is published counts; a staged file and the
operation logs are never evidence. A block's records are listed only for a
block without a block record and only to refuse: nothing listed is read,
adopted or written. A `failed` apply need not hold a `failed` block: a retry
whose start published the block's `running` record and then reported a
failure leaves the apply `failed` beside that `running` block, which is not a
contradiction, and whose record counts the attempt it retried as well as its
own. A `running` block with one attempt is never that retry, so it accounts for
no failure, and a `failed` block whose whole directory was lost cannot hide
behind it. A `running` or `unknown` block started and its outcome is
unproved, so the removal resolves it before it registers and counting it skips
no effect. A removal stopped after resolving that block leaves the apply in the
state it recorded before the resolution, `running` or `unknown`, rather than
`failed` beside a block the resolution proved `done`, which only an executable
before `4513e123` left. An `unknown` apply need
not hold an `unknown` block either: a removal interrupted after resolving that
block and before recording the apply's new state leaves it `done` or `failed`,
and still owned. A lost record that leaves none of the three cannot be told
from a block that never started, and the rule holds whatever executable wrote
the records, including the attempt beside no block record that a first start
an executable before `83dcbebe` interrupted leaves.

A continuation refuses, with the same code and remedy and before it restores
its operation's log, raises evidence or marks its operation `running`, where
its operation's records contradict what it started: a block with no block
record beside an attempt or resolution record of its own, whose start would
otherwise refuse only after the operation was marked. It names the operation
and each contradiction in frozen plan order. An apply recorded `failed` whose
blocks are all `done` is [finalized](#lifecycle-unit) rather than continued.
A continuation is not held to the other
contradictions above, because it removes nothing: a started block whose whole
directory was lost is started again, and its attempt
[converges](#converging-an-effect) what the lost one left.

Every refusal of records that contradict themselves points at
`bootwright status`, except the two whose only exit is deleting the context,
beside no operation and over a completed destroy, which name that deletion
instead ([lifecycle unit](#lifecycle-unit)). The
[`contradictions`](cli/output.md#lifecycle-and-status-results) of `status`
name what each of those refusals names over the context's records: each block
a completed operation does not show `done`, each contradiction of an incomplete apply
above and an incomplete apply that records a state only a started block
explains beside no block that started, each lost record of an incomplete
removal, beside no operation, each state no index accounts for, and an
operation whose frozen Secret binding the keyring no longer lists, as below.

A frozen block records what creating it did; removing it is the other half of
the same request. Each capability therefore reads its own frozen request and
states what removing that block does: the words it is planned and reported in,
the impacts it lists, and the authorization it consumes. Nothing else of the
block may change, so planning a removal can never alter what is removed, and a
removal acknowledges the consequences of removing rather than the consequences
its apply acknowledged.

Reading a frozen request is what makes a context removable without re-deriving
it. A capability reads exactly the request version it writes; the frozen digest
identifies the bytes that were frozen. Any change to what a request encodes is
a new version, including adding or renaming one field, because the bytes a
version froze are proved canonical against the shape that wrote them: a shape
that changed without its version refuses its own frozen bytes. No conversion
exists. A request of any other version, or one whose implementation this
executable no longer provides, refuses before anything is registered and names
the block, the version it holds and the executable identity its operation
recorded, so the remedy is the command to run rather than the obstacle that
stopped it.

A fresh removal uses the secret material its apply bound, by reopening that
operation's own binding rather than binding what the current declarations name.
The material that created an effect is the material that proves it gone, and a
declaration edited or rotated afterwards never silently changes which version a
removal presents.

Nothing stands in for a frozen binding. A continuation, or a fresh removal,
whose operation names a binding the context's keyring no longer lists refuses
`lifecycle.state` before it is authorized, presented or confirmed, and before
it registers, probes, or releases any reservation or binding; a `plan`
previewing that verb refuses the same way. A binding whose material the keyring
cannot read, as a missing or damaged part file leaves it, refuses the same way
when the verb reopens it, after confirmation and still before any of those: a
reopen that fails is a lost binding when the keyring reports the material
corrupt or undecryptable (`secret.store.corrupt`, `secret.store.crypto`) or a
listing that answers no longer names the binding, and any other failure is
reported as it failed. The one diagnostic names the operation and the state it
records, the binding, why the binding cannot be reopened and every object that
operation owns, and its remedy names the only two exits: restore the keyring
from a complete backup and repeat the verb, or run `bootwright context delete
--name <name> --purge --allow-orphans` and then remove those objects by hand.
Only a listing that answers proves a binding gone; one that fails proves
nothing, and the reopen decides. A finalization and a verb that settles reopen
no binding, so neither refuses on its account: a removal whose blocks are all
`done` is acted on only by the `destroy` that finalizes it, so `status` names no
lost binding over it and offers that `destroy`. A binding an operation names is
released only by an invocation that moved the context on, so a refusal whose
binding was released after its decision was read re-reads the context and
reports that change instead. No re-binding of the current declarations,
substitution of other material, removal without the binding or release of
ownership inside the context exists, because none of them proves that the
material a verb presents is the material that created the effects. `status`
names such an operation among its `contradictions`, in the refusal's words, and
offers that deletion as its only next step.

Destroy removes dependents before dependencies, so a removal inverts the
apply's dependency graph and not merely its order: every edge turns around and
a block waits on its own dependents, because what it provided stays in use
until they are gone. Narrowing a removal to the owned set drops the edges to
blocks outside that set. Both results are ordered by the one canonical rule
every plan obeys, so a frozen removal is rebuilt from its own record exactly as
it was written.

A fresh removal may supersede any apply that has not completed, and a failed
destroy, which is removed over what it has not yet proved gone and is never
continued. An apply
qualifies however it stopped, because the set it owns is the same at a
boundary, at a failure, and at an interruption.
An unproved effect is not an exception: the removal resolves it first and
refuses, registering nothing, when it cannot. A running or unknown removal is
continued rather than replaced, because a removal that lost an outcome is
resolved by repeating itself. Replacement is the only road out of a repaired
adapter, because a continuation is frozen to the automation and execution
closure its operation registered under while a fresh operation runs under the
current ones.

Every transition is decided from durable state read under the shared lock and
performs its effects under the exclusive one. Under the exclusive lock, before
it does anything, it re-proves that the context still holds exactly the state
it was planned from: the same current operation, or still none; the same
operation record, every field of it; the same frozen plan, whose digest is the
one that record carries; and the same block records, state and attempt count
alike, since a retry that failed again leaves its block's state as it found
it. Anything else means another invocation advanced the context in between, so
the plan that was presented and confirmed may no longer describe what the
context owns, and the transition refuses `lifecycle.state` rather than
applying it. A removal re-proves it before it resolves, proves quiescence or
registers; a continuation before it registers; a
[finalization](#lifecycle-unit) before it records, releases or projects; a
completed removal, against the records its completion left, before it
publishes pristine evidence; a destroy of a context holding no operation
before it releases what an interrupted registration left; and a fresh apply
both before it claims its operation directory, raises its evidence and binds a
Secret, and again before it claims a controller host, reserves anything or
registers. A continuation and a fresh apply each re-prove it together with the
input revision and input digest the context holds: a continuation against
those its operation froze, and a fresh apply against those its plan was
compiled from, so it never registers a plan under an input it was not compiled
from.

Destroy retains evidence until positive removal or positive absence is durable.
It accepts no stage selection.

## Bootstrap completion and GitOps readiness

State reconciliation owns the complete-environment GitOps-readiness predicate.
An apply is ready for handoff exactly when:

- the apply operation is `done` and every block in its immutable plan is
  `done`;
- every completion, readiness, live access, and ownership requirement frozen
  in that plan has positive durable evidence;
- no effect attempt remains unresolved `unknown`; and
- no required-log or other durable operation fault still blocks work.

Resolved earlier failures, retries, and `no-effect` attempts remain in the
audit history but do not independently make a completed apply unready. The
predicate is derived from authoritative operation state and evidence; it does
not perform another platform effect, transfer ownership, or authorize GitOps
to mutate Bootwright-owned resources. Add-on drivers and other capabilities
contribute typed readiness evidence, but cannot declare the complete
environment ready themselves.

## Mutation safety

- Validate and plan the complete context before the first side effect.
- Admit one mutator per context through a durable lease.
- Immediately before every effect, revalidate exact target identity, ownership,
  authorization, [quiescence](#quiescence-before-removal), dependency integrity,
  and required positive absence.
- A name or controller record may locate a target but never proves ownership or
  identity.
- A Bootwright-created resource requires positive absence or exact agreement
  between recorded and live manager, context, resource, and immutable provider
  identity.
- Failed, forbidden, unsupported, empty, malformed, or contradictory probes
  are unknown, never absence.
- Foreign, unowned, replacement, and unknown targets refuse without bypass.
- Every refusal identifies the target, evidence, safety reason, and safe next
  action. It never invents a force command.
- Human plan, confirmation, progress, status, and refusal views derive from the
  same immutable plan and registry.

### Quiescence before removal

Bootwright removes and replaces only what is out of use, and the one thing it
proves is that the **Machines** are down. A Machine is what runs an operating
system and an operator's work; everything else a context owns serves those
Machines, so a context whose Machines are all stopped has nothing left running
to interrupt. A capability that realizes Machines defines their *quiescent*
predicate and observes it on the host itself: never a value echoed back from
its own request, never a record of what Bootwright intended, and never a state
a live system also reports for something that is gone. Every other capability
derives its answer from the Machines the same removal already probes, and
observes nothing of its own. Bootwright does not ask whether a managed service,
a published artifact or a provider network is being used, by this context or by
anything else.

A fresh removal probes every block it would take back **before it registers**,
and refuses `lifecycle.live` with no operation, no reservation and no effect
when any Machine is still running, naming each one and the command that stops
it. The check cannot be per-effect alone, because a removal takes dependents
before dependencies: it would delete the quiescent leaves and then stop at the
running machine, leaving a context that can only continue a removal it should
never have started. Every block is asked rather than the first live one alone,
so an operator learns every Machine to stop at once. The Machine inverse then
revalidates its own target immediately before its effect and fails its block
rather than forcing, which covers a Machine started while the removal ran. A
continuation is not gated again: its operation is already registered and each
of its effects still revalidates.

The gate is one [check](cli/output.md#long-running-progress), not a preview of
the removal: it settles as a single row whose sub-step is the block being
probed, ahead of the log directory and every effect row, so an operator reads
each step of the removal once and reads it as the effect it is.

A Machine whose state cannot be read is live. An environment that cannot prove
its Machines are idle is never assumed to be.

Stopping is never a side effect of removal. An inverse never powers a Machine
off, and the operator stops it explicitly — through
[the Machine power commands](cli/commands.md#command-and-flag-catalog) or their
own means — so a removal never destroys work that was still running. A Machine
that exists before Bootwright touches it, such as operator-owned bare metal,
must be down before it is changed for the same reason.

This composes with authorization rather than replacing it: `data-loss`
acknowledges that removal destroys data, and quiescence proves that nothing is
using that data now. Neither substitutes for the other.

### Physical disk safety

Existing operator-owned bare metal is the explicit external-substrate
exception: Bootwright never claims ownership of or destroys the physical
machine. Before changing it, an implementation must durably hold an exclusive
context claim, prove the exact management controller/System identity, prove
the complete live MAC set matches the immutable `Machine`, select one whole
root disk from the authored hint, prove the machine is off, and consume
`data-loss`. The claim serializes use but cannot replace any live proof.
[Physical machine realization](substrates.md#physical-machine-realization)
holds the claim and the controller-side proof, and
[physical installation](managed-os.md#physical-installation) repeats that proof
immediately before it boots and again inside the installer itself.

The interval between the controller's last proof and the installer's first
write is a residual race, not an ownership proof. No MAC set, root-device hint,
power-off state, context claim or `data-loss` authorization closes it, and none
is described as closing it. The managed-OS installer narrows it with its
in-installer check without eliminating it; the agent installer binds a host by
declared MACs and root-device hints and offers no supported hook before disk
erasure, so it admits no equivalent check. Every bare-metal apply therefore
retains all controller-side proofs, fails closed on any mismatch or unknown
probe, and passes explicit real-hardware qualification before it is called
supported.

Physical destroy and offline disk erase are outside this contract: a removal
retains the machine and the system installed on it, and erasing one requires a
separate safety contract ([B70](milestones/m3.md#b70)).

### Controller-host protection

The controller is an OS-ready provided Machine and remains outside selected
cluster node membership under
[the API relationship](api/environment.md#controller-machine). That declaration
alone does not prove live host identity or ownership.
[Controller setup](controller.md) defines prerequisite preparation and uses
[Workspace host binding](contexts.md#controller-relationship-and-host-binding).

A controller-hosted service effect runs only under verified host binding and
the stable conflict identities of the
[host reservations](infrastructure-services.md#host-reservations), which give
shared ports, paths, service instances and other exclusive host resources
cross-context ownership, conflict refusal and recovery evidence. Context-local
leases remain necessary but do not coordinate two contexts targeting the same
host.

An apply or destroy must preserve the controller OS, power, Bootwright runtime,
Workspace state, keyring and evidence needed to continue or remove owned
resources. An owned service may be removed only through its frozen inverse
when remaining operations no longer depend on it. Selecting a Machine as the
controller never grants ownership of the host, its runtime or another context's
services.

Controller setup, immutable execution dependencies and locally hosted services
have separate owners. Plans must model their actual readiness relationships
without converting every Machine address or service reference into a readiness
edge. Missing, changed or unprovable controller bindings refuse the affected
operation; continuation never silently substitutes the invoking host or moves
context state. Migration and complete-store restore require their own defined
recovery journey.

## Confirmation and authorization

Ordinary confirmation and any non-interactive suppression belong to the public
CLI contract that exposes an operation; neither is defined here. Suppression
grants no permission. Irreversible authorization acknowledges already-planned
loss but does not select a target or relax validation, identity, ownership,
power, probe, or digest checks.

`data-loss` is the only authorization token. `all`, unknown, duplicate, empty,
and inapplicable values are errors. A plan requires exactly the tokens its
blocks [consume](#plan-and-execution). A token the frozen plan does not require
is inapplicable and refuses with `lifecycle.authorization` before registration,
so a habitual authorization cannot pre-authorize a future destructive plan; a
required token that is not supplied refuses the same way, naming the blocks
that consume it. The [finalization](#lifecycle-unit) of an operation whose
blocks are all `done` precedes planning, consumes no token and asks no
confirmation, because it performs only the record, releases and projection its
records prove. An
enabled `CustomPlaybook` has no authorization bypass; it refuses under the
[unrealizable-kind rule](#stages-and-the-pause-boundary).

Before operational exposure, every supported substrate/component/version must
pass the shared port suite, safety tests, and real-system qualification with
immutable plans, durable continuation, leases, exact ownership, and private
attempt logs. [The CLI contract](cli.md) must cover the complete journey,
including confirmation/authorization, results, diagnostics, cancellation, and
recovery.
