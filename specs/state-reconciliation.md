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
  interrupted or failed `destroy`, against its exact frozen plan; and
- a `destroy` of everything the apply [owns](#continuation-and-removal),
  whether that apply completed or stopped at a boundary, at a failure, or at an
  interruption.

There is no reconciliation, partial planning, adoption, reclaim, or force path.
A completed apply must be destroyed before an apply of *changed* desired state
can start, and an edited input refuses by naming that removal.

A verb whose work durable state already proves performs none of it and
succeeds: an `apply` repeated over the unchanged input its completed apply
froze, while every block of that apply's plan is `done`, and a `destroy` of a
context that owns nothing, which is one holding no operation or a completed
destroy, each report the completed operation and `done` without registering an
operation, opening a transaction, binding a Secret, claiming a reservation or
reaching a host. Such
an invocation requires no authorization and no confirmation, because it has no
consequence to acknowledge, and a token it is given authorizes nothing and
refuses nothing. Repeating a verb is therefore always safe, which is what lets
an operator or a script ask whether anything is left to do. A completed apply
whose records hold a block that is not `done` proves no such thing, so an
`apply` of its unchanged input refuses, as
[its removal does](#continuation-and-removal), rather than settling.

Plans remain complete and immutable; later artifact generation cannot append
blocks or expand the operation. Expansion requires destroy followed by fresh
apply.

## Context mutation evidence

Reconciliation owns a closed version-1 mutation record initialized with
`operation: none` and `ownership: none`. Recognized operation states are `none`,
`pending`, `failed`, `unknown` and `applied`; ownership is `none` or `retained`.
A paused operation records `pending` and `retained`, exactly as a running one
does, because a pause leaves its [ownership](#continuation-and-removal) in
place.
This record establishes only local disposal/update restrictions, not native
execution, readiness, ownership release or permission to run lifecycle work.
Lifecycle publication participates in the same context lease and updates this
evidence before any remote mutation. Missing or unknown evidence fails closed.
A live lease refuses every context mutation.

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
archival mode.

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
state. A bounded run takes the same shared lock and creates exactly one thing:
the file its own adapter output is
[retained](cli/output.md#bounded-run-output) in. That file is troubleshooting
material, never state, so a run still repairs and migrates nothing.
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
an interrupted registration leaves. A later command reads one again only as the
current operation's `source`, and each counts toward the
[retained-operation bound](contexts.md#storage-locking-and-publication).
`operation.json` binds the operation to its verb, context
identity, input revision and digest, plan digest, selected implementation and
automation identities, executable identity, secret bindings, durable state and
log-fault flag. `plan.json` is the immutable frozen plan: every block with its
description, dependencies, impacts, presentation groups, resolved
implementation identity, content digest and canonical secret-free request.
Block records carry the block state and its next attempt number; attempt and
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
migration or scan-based adoption of an unpublished record. An attempt record
is created exclusively, so a reused attempt number refuses rather than
overwriting durable evidence. An exclusively created record is staged and then
renamed without replacement, so its name appears only with complete,
synchronized bytes.

## State machine

| Durable state | Allowed lifecycle transition |
| --- | --- |
| no operation, or completed destroy | start a fresh apply; a `destroy` settles without effect |
| apply running | continue that exact apply, or start a fresh destroy of the blocks it started |
| apply failed | continue that exact apply, or start a fresh destroy of the blocks it started |
| apply paused | continue that exact apply under any stage selection, or start a fresh destroy of the blocks it started |
| apply unknown | resolve the exact unknown block, or start a fresh destroy of the blocks it started, which resolves that block first; start no other effect or retry |
| apply done | start a fresh destroy; an `apply` of the unchanged input settles without effect, and of a changed input refuses; both the destroy and that apply refuse while a block of the plan is not `done` |
| destroy running | continue that exact destroy |
| destroy failed | continue that exact destroy, or start a fresh destroy of what it has not removed |
| destroy unknown | resolve the exact unknown block; start no effect or retry |

Changed desired state never turns continuation into reconciliation. A
continuation verifies the operation kind, context identity, frozen-input
digest, plan digest, every selected implementation and execution-dependency
digest, and required ownership evidence before doing work.

The four tables below state the transition code exactly:
`TestTransitionTablesMatchSpec` drives that code with every row, so a row
changes only together with the code.

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
effect and records no outcome. Its block record and the log-fault flag are
unchanged, so the block keeps the state it had: `pending`, or the `failed` it
was retrying. The invocation that meets it admits nothing further, never admits
that block again, and waits for what is already running. Its operation then
takes the state its blocks give it under
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
tracked as [backlog S4b](milestones/backlog.md#audit-follow-ups-2026-09).

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
shape a capability reports unsupported. A frozen plan requires a resolved
implementation for every block; a removal planned from a frozen plan is never
refused this way.

A fresh `plan` previews exactly the decision a fresh `apply` registers, and one
path takes it: it compiles the frozen input, refuses what this executable
cannot realize, plans, refuses `lifecycle.state` for a plan with no block, and
refuses a selection that admits no startable block. Both verbs report a refusal
of that decision with the same code, message and remedy, and a preview that
succeeds shows the plan `apply` then registers. The preview stops at the
decision: it binds no secret, claims neither the controller host nor a host
reservation, takes no exclusive lock and registers nothing, so a refusal from
those steps, like the authorization and confirmation gates, stays `apply`'s.

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
identity, version, content digest, pinned execution dependencies, and request
digest in the plan and operation registry. That digest covers the canonical
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
proved survives whether or not the removal then goes on to register. A removal
that still cannot prove an effect registers nothing and refuses, naming each
block it could not prove, because an effect no observation resolves says
nothing about what it owns.

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
blocks are all `done`, a `failed` or `unknown` apply holds the block that made
it so, and a failed removal holds the block that failed. Records that say
otherwise contradict themselves, so a removal over them refuses before it
registers, reaches a host, or releases a binding the effects still on the host
need.

A completed apply owns its whole frozen plan, so the removal of one refuses
`lifecycle.state` while any block of that plan is not `done`, rather than
removing only the `done` rest. A lost block record reads back as `pending`, and
a removal that skipped that block would leave its effect in place and then
release the binding it needs. The refusal names the apply and every such block
with the state its record reads, points at `bootwright status`, and comes
before the removal binds, probes, registers or releases anything. An incomplete
apply and a failed removal are not held to this, because blocks that are not
`done` are legitimate in both.

A removal of an incomplete apply refuses instead where the apply's records
contradict what it started, with the completed-apply refusal's code, remedy
and timing. The refusal names the apply and each contradiction, per block in
frozen plan order and then the apply's own:

- a block that reads `pending` while a block that depends on it directly
  reads anything else, because a block starts only once every dependency is
  `done`;
- a block with no block record beside an attempt or resolution record of its
  own, because a start publishes a block's record before any attempt record of
  it, so only a lost record leaves one;
- an apply recorded `failed` that holds no `failed` block, because an apply
  records `failed` only while a block is `failed`, and a `failed` block
  changes only through a retry, which first records the apply `running`.

Each shows the records no longer say what the apply started, and a started
block whose record was lost reads back as `pending`, which a removal planned
from them would skip. Records that leave nothing to remove although
the apply's own state says it started a block answer first, with the refusal
of records that contradict themselves above. Only a file named exactly as an
attempt or resolution record is published counts; a staged file and the
operation logs are never evidence. A block's records are listed only for a
block without a block record and only to refuse: nothing listed is read,
adopted or written. An `unknown` apply need not hold an `unknown` block: a
removal interrupted after resolving that block and before recording the
apply's new state leaves it `done` or `failed`, and still owned. A lost record
that leaves none of the three cannot be told from a block that never started,
and the rule holds whatever executable wrote the records.

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

Destroy removes dependents before dependencies, so a removal inverts the
apply's dependency graph and not merely its order: every edge turns around and
a block waits on its own dependents, because what it provided stays in use
until they are gone. Narrowing a removal to the owned set drops the edges to
blocks outside that set. Both results are ordered by the one canonical rule
every plan obeys, so a frozen removal is rebuilt from its own record exactly as
it was written.

A fresh removal may supersede any apply that has not completed, and a failed
destroy, which is removed over what it has not yet proved gone. An apply
qualifies however it stopped, because the set it owns is the same at a
boundary, at a failure, and at an interruption.
An unproved effect is not an exception: the removal resolves it first and
refuses, registering nothing, when it cannot. An incomplete removal is
continued rather than replaced, because a removal that lost an outcome is
resolved by repeating itself. Replacement is the only road out of a repaired
adapter, because a continuation is frozen to the automation its operation
registered under while a fresh operation runs under the current one.

Every transition is decided from durable state read under the shared lock and
performs its effects under the exclusive one. Under the exclusive lock, before
it does anything, it re-proves that the context still holds exactly the state
it was planned from: the same current operation, or still none, in the same
state and with the same block states. Anything else means another invocation
advanced the context in between, so the plan that was presented and confirmed
may no longer describe what the context owns, and the transition refuses
`lifecycle.state` rather than applying it. A removal re-proves it before it
resolves, proves quiescence or registers; a continuation and a fresh apply
re-prove it before they register, claim a controller host or reserve anything,
together with the input revision and input digest the context holds: a
continuation against those its operation froze, and a fresh apply against those
its plan was compiled from, so it never registers a plan under an input it was
not compiled from.

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
separate safety contract ([backlog C9](milestones/backlog.md#candidates)).

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
that consume it. An
enabled `CustomPlaybook` has no authorization bypass; it refuses under the
[unrealizable-kind rule](#stages-and-the-pause-boundary).

Before operational exposure, every supported substrate/component/version must
pass the shared port suite, safety tests, and real-system qualification with
immutable plans, durable continuation, leases, exact ownership, and private
attempt logs. [The CLI contract](cli.md) must cover the complete journey,
including confirmation/authorization, results, diagnostics, cancellation, and
recovery.
