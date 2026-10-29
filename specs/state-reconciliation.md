# State and Reconciliation Contract

This file owns mutation and durable state; [cli.md](cli.md) owns invocation
and presentation.

## Lifecycle unit

One complete validated Environment selection rooted in one environment
directory is one lifecycle unit. [API selection](api.md#environment-directory-and-selected-state)
owns graph membership. Operations cover the entire unit; a public request
cannot narrow it to kinds, clusters, machines, or ranges. A
[stage selection](#stages-and-the-pause-boundary), with its
[stop after an add-on instance](#stopping-after-an-add-on-instance), is the one
exception, and it narrows nothing: the plan stays complete and frozen, and the
selection only gates which of its blocks this invocation may start.

Supported operation modes are:

- a fresh full-context `apply`, optionally stopping at a stage boundary or
  after a named add-on instance;
- continuation of a paused, interrupted or failed `apply`, or of an
  interrupted `destroy`, against its exact frozen plan; and
- a `destroy` of everything the apply [owns](#continuation-and-removal),
  whether that apply completed or stopped at a boundary, at a failure, or at an
  interruption.

There is no reconciliation of changed desired state or of drift, and no partial
planning, adoption, reclaim of ownership, or force path. The one reclaim this
contract performs is the [housekeeping](#context-mutation-evidence) of
operation directories that hold nothing. A completed apply must be destroyed
before an apply of *changed* desired state can start, and an edited input
refuses by naming that removal.
The refusal also names what changed: up to five objects the input adds,
changes, with up to three field paths each, or removes. It says when the bytes
changed but declare the same objects, or repeat the bytes that apply froze, and
points to `bootwright diff`. When the frozen revision cannot be read or
compiled it keeps the general message. Once an update has changed the input,
one that restores the bytes apply froze still leaves the apply refusing, so
`destroy` stays the way on ([B598](milestones/backlog.md#b598) proposes
settling over restored bytes).
Unchanged input is the context's selected revision and its frozen bytes, which
a [context update](contexts.md#frozen-input-and-provenance) of identical input
keeps, so that update leaves a completed apply settled. A settled apply attests
only that its own records are complete for the unchanged input; drift that
`bootwright diff --observe` [shows](#comparing-desired-and-real-state) is
reported, not repaired.

A verb whose work durable state already proves performs none of it and
succeeds: an `apply` repeated over the unchanged input its completed apply
froze, while every block of that apply's plan is `done`, and a `destroy` of a
context that owns nothing, which is one holding a completed destroy every block
of whose plan is `done` or holding no operation while nothing claims it, below,
each report the completed
operation and `done` without registering an operation, opening a transaction,
binding a Secret, claiming a reservation or reaching a host; the one
transaction a settling `apply` opens is the one that
[restores](#converging-an-effect) a latched log fault. Such
an invocation requires no authorization and no confirmation, because it has no
consequence to acknowledge, and a token it is given authorizes nothing and
refuses nothing. Repeating a verb is therefore always safe, which is what lets
an operator or a script ask whether anything is left to do. A verb settles
only once its operation's finalization, below, is complete. A completed apply
whose records hold a block that is not `done` proves no such thing, so an
`apply` of its unchanged input refuses `lifecycle.state` rather than settling,
naming each such block with the state its record reads and two exits: the
destroy that [takes back its whole frozen plan](#continuation-and-removal),
resolving each such block first, and the deletion below. A
completed destroy whose records hold one proves nothing removed that block's
effect, so both verbs refuse `lifecycle.state` over it, naming each such block
with the state its record reads, rather than settle or start a fresh apply
beside that effect. Over that completed destroy neither verb has a way on, so
deleting the context is its only exit. Each refusal names the deletion the
[guard](#context-mutation-evidence) admits, decided from the guard's own
reading of the evidence however its record is spelled:
`bootwright context delete --name <name> --purge`, adding `--allow-orphans`
unless the guard reads the context's evidence as pristine, because only
pristine evidence admits a [deletion](contexts.md#permanent-deletion) that
acknowledges no orphan. Over evidence the guard cannot read only the
acknowledgement deletes the context, so the refusal names it, saying that it
cannot list what the context may still own.

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
incomplete destroy. One rule covers every such record: an operation whose
blocks are all `done` is finalized by its own verb whatever state it records,
because its record lags its blocks, as any invocation stopped between a
block's last durable write and its operation's write leaves it. A failed
apply's `apply` finalizes it, recording it `done` and publishing its
projection, and its `destroy` replaces it with a removal of its whole frozen
plan, which is every block it started. A completed
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
as pristine, which over evidence the guard cannot read cannot list what it
abandons.

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

Deletion refuses protected evidence by default, naming
`bootwright status --context <name>` and `bootwright diff --context <name>`,
which also lists what the selected input no longer declares, as the inventory
of what it owns and `destroy` as the way to release it. An explicit orphan
acknowledgement waives that one verdict, and nothing else: the objects the
evidence still attributes to the context are abandoned in place, so they
survive unmanaged and unreferenced, and no further command can discover,
continue or remove them through Bootwright. Over missing, corrupt or
unsupported evidence nothing proves what the context owns, so an update and a
default deletion refuse naming status and the acknowledged deletion, and the acknowledgement abandons
the context saying that its objects cannot be listed. A live lease still
refuses under the acknowledgement. The acknowledgement replaces no other
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
[Controller inspection](controller.md#the-setup-flow), which
may read shared host metadata without selecting a context. Context-backed reads
acquire the existing store's shared lock and never create, repair or migrate
state. A bounded run takes the same shared lock and creates at most one thing:
the file its own adapter output is
[retained](cli/output.md#bounded-run-output) in, once its private runtime is
admitted and only for a run whose result names it, so a reading creates
nothing. That file is troubleshooting material, never state, so a run still
repairs and migrates nothing. A bounded run or a bounded consumer binds no
Secret: it [reads](secrets.md#immutable-binding-and-contexts) the current
version of each Secret it names in one keyring session under the store's
shared lock, publishes no binding or identity reservation, and clears that
material when its call returns, a call ended by cancellation included.
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
description.

A block ID is a fixed prefix of at most 46 bytes, such as `os-install-` or
`ceph-service-`, followed by the tail its objects contribute, such as a Machine
name or `<cluster>-crash`. When prefix and tail fit 63 bytes the ID is exactly
their concatenation, so an ID of at most 63 bytes never moves. A longer one
keeps the first 46 bytes of that concatenation, which hold the whole prefix,
without a trailing hyphen, then a hyphen and the first 16 hex digits of the
SHA-256 of the whole concatenation. The ID depends only on the concatenated
bytes, so a planner that builds `ceph-` plus `cluster-<name>` and a builder
that builds `ceph-cluster-` plus `<name>` name the same block at every length;
two names that share a long head name different blocks, and
no [object name limit](api.md#object-names) depends on a prefix or on the
services a cluster declares. A short name can spell a long name's hashed ID;
the plan then refuses with `lifecycle.state`, naming both objects and the
block, and the remedy renames one of them. A native name keeps its own limit on its own
field; no block ID becomes a native host, service or Kubernetes object name. Each is validated before lookup or path construction and never
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

`index.json` names at most one current operation. An index of another version,
or one carrying `qualification`, is unreadable evidence: it refuses every
lifecycle command with the remedy naming the build that prepared it, then
`context delete --purge`, or `--purge --allow-orphans` as the abandoning
fallback.
Every other
operation directory is kept for audit and never named by the index: a
completed operation, one a removal superseded, and the unreferenced directory
an interrupted registration leaves once its plan landed. A later command reads
one again only as the current operation's `source` or, for `status`, as the
removal the current one replaces, and that one's own, and each counts toward the
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
secret bindings, durable state and log-fault flag. The `operation.json` of a
removal that replaces a failed removal also names that removal (`replaces`),
an optional member, below. `plan.json` is the
immutable frozen plan: every block with its
description, dependencies, impacts, presentation groups, resolved
implementation identity, content digest and canonical secret-free request.
A block's dependencies, impacts, consumed authorizations and groups are always
lists, `[]` when empty and never `null`; a plan an earlier build froze with
`null` there is not a plan this executable could have produced, and it refuses
with the remedy to destroy the context with the build that registered the
operation.
Block records carry the block state and the number of attempts started, one
less than the next attempt's number; attempt and
resolution records carry their block, attempt and resolution numbers, phase,
outcome, effect, bounded evidence and timestamps, and a resolution whose
observation could not run also carries `failure`, the first diagnostic that
observation reported, its code, message and remediation, each bounded, an
optional member no other record carries. A resolution a fresh removal records
against an apply's block carries `removal`, so `status` explains it for the
removal's reading first. Every attempt and
resolution also names the build it ran under, `build`: the automation digest
the executable embeds, which the run's staged extraction holds, the block's content
digest, the executable's version and commit, and `modified` when its own stamp
says so. It is an optional member, so a record an earlier build wrote has
none, and a start that adopts an interrupted record names the build that
adopts it. These records never
restate the request: the request an attempt or resolution acted on is its
block's in `plan.json`, which the operation's plan digest names and nothing
replaces. A running attempt may additionally publish one bounded `preparation`
object: the before-state its capability observed, recorded durably while the attempt is
still running and before it is permitted to change the host. It is written once
and never replaced, so a later recovery can tell an attempt that was authorized
to install from one that never reached that point. A lifecycle attempt publishes
it through the acknowledged `prepared` record of
[the adapter result protocol](architecture.md#the-adapter-result-protocol): its
capability validates it, the runner records it, and only then is the adapter
acknowledged, and only an apply's own attempt may. A resolution observation
publishes none, because an observation authorizes no effect. An attempt's
record may also carry `warnings`, the bounded advisories its adapter's
completed result reported, each a `code`, `message` and optional `object` of
`kind` and `name`; they are written in the same write as its outcome, left out
when they would not hold or fit the record's bound, and never on a
resolution's record. They are presentation:
the run reports them once and `status` reports the latest attempt's, and
neither moves the attempt's outcome or its block's state.
[The output contract](cli/output.md#private-operation-logs) owns the `logs/`
tree.

`replaces`, `failure`, `warnings`, `build` and `removal` are optional members: each was added without
moving its record's version, because a record that omits one means what a
record written before it meant. A member whose omission would mean anything
else moves its record's version. An optional member is compatible one way only:
a build from before it decodes records closed and refuses one that carries it
as malformed or unsupported, so once a build that writes `build` has worked an
operation, no earlier build can continue or remove it. Such a refusal of an
attempt or resolution record names the build its own `build` member names,
when exactly one member is spelled `build` in any case and it holds a valid
build in its canonical encoding, with the remedy to continue or remove the
operation with that build; when that build is the unmodified running
executable, the refusal says the record names it and offers no such remedy.

Records are closed, canonical, bounded JSON published atomically beneath held
verified handles, with the directory and file modes of
[the context store](contexts.md#storage-locking-and-publication). An operation
record is at most 1 MiB, a plan record at most 6 MiB, the stored-plan bound of
the [operator-visible bounds](contexts.md#storage-locking-and-publication), an
attempt or resolution record at most 64 KiB, and an index at most 256 KiB.
Unknown fields, duplicate keys, noncanonical encodings and unsupported versions
refuse; there is no repair, migration or scan-based adoption of an unpublished
record. Every record and
frozen request is encoded, read closed and proved canonical by one
canonical-JSON implementation; each format keeps its own byte bound,
pre-decode bounds, trailing line feed and failure words. An operation record
is version 2, and a registration writes no other; any other version, the
version 1 record a build before the execution closure was frozen wrote among
them, refuses `lifecycle.state` as unsupported. A store that holds one is a
version 5 store, which [refuses first](contexts.md#storage-locking-and-publication)
with the fresh-install remedy. Every read of a frozen plan holds it to its
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
| no operation beside evidence or records no index accounts for | none: `apply` and `destroy` refuse, naming `context delete --purge`, with `--allow-orphans` unless the evidence is pristine, which cannot list what it abandons over evidence the guard cannot read |
| completed destroy holding a block that is not `done` | none: `apply` and `destroy` refuse, naming `context delete --purge`, with `--allow-orphans` unless the evidence is pristine, which cannot list what it abandons over evidence the guard cannot read |
| apply running | continue that exact apply, or start a fresh destroy of the blocks it started |
| apply failed | continue that exact apply, or start a fresh destroy of the blocks it started |
| apply failed, every block `done` | an `apply` finalizes it, recording it `done` and publishing its projection, or start a fresh destroy of its whole frozen plan |
| apply paused | continue that exact apply under any stage selection, or start a fresh destroy of the blocks it started |
| apply unknown | continue it, resolving every unproved block first and then retrying what resolution leaves `failed` (D146), or start a fresh destroy of the blocks it started, which resolves them first |
| apply done | start a fresh destroy of its whole frozen plan, which first resolves every unproved block; an `apply` of the unchanged input settles without effect, and of a changed input refuses; that apply refuses while a block of the plan is not `done`, naming that destroy and `context delete --purge` |
| destroy running | continue that exact destroy |
| destroy failed | start a fresh destroy of what it has not removed |
| destroy unknown | continue it, resolving every unproved block first and then retrying what resolution leaves `failed` (D146) |

Before any row, an operation whose blocks are all `done` is
[finalized](#lifecycle-unit) first while its record, evidence, reservations or
Secret bindings do not yet say it completed; a running, unknown or failed one
only by its own verb. A continuation refuses records that
contradict what its operation started before it restores, raises or marks
anything ([continuation and removal](#continuation-and-removal)). Beside any
row, a continuation or removal that reopens a Secret binding the context's
keyring no longer lists, or can no longer read, refuses until the keyring is
restored from a [complete backup](contexts.md#format-and-restore-boundary),
and otherwise the context's only exit is an
orphan-acknowledged deletion
([continuation and removal](#continuation-and-removal)).

Changed desired state never turns continuation into reconciliation. A
continuation verifies, before doing work, the operation's verb, its context
identity and input revision, the frozen-input digest, the plan digest its
record names, the block states and attempt counts it read (unchanged since the
command read them), that this executable is the build the operation registered
under or [freezes the same blocks](#dependency-safety-during-recovery), and the
host binding with completed setup. Only a fresh
apply binds, so a continuation whose context the controller state records no
binding for names the `destroy` that supersedes its operation where one may,
and otherwise the binding's restoration or the context's deletion, never an
`apply`. Each block then runs the capability this executable offers under the
implementation identity the block froze. A block whose implementation this
executable does not offer refuses before its attempt or resolution starts. A
block's content digest travels with the block: a removal keeps the one its apply
froze, and a continuation compares it only as part of proving that another build
freezes the same blocks.

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
records `no-effect` or `partial`; only a resolution does. A canceled attempt
stays `unknown` even when its adapter proves it performed no effect, because
the next verb's resolution reads it read-only.

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
keeps by design proves nothing against it. A read that succeeds yet cannot
tell the removal's postcondition from a target it could not read proves
neither completion nor no effect: its capability reports it `inconclusive`, a
state distinct from `no-effect` (Q65, D183). It admits one retry only when the
capability declares its removal replay-safe (the removal's `ReplaySafe`), the
block's owning apply durably recorded an attempt of it, so its ownership comes
from durable records, and no earlier resolution of the block in this
operation was inconclusive; that retry reads the target again before any
effect, completing over a postcondition the read proves, removing over a
target the read shows, and performing nothing otherwise. Any other
inconclusive observation stays `unknown`, as a failed, forbidden,
disconnected or malformed observation always does, which never reads as
`no-effect` and never admits a retry or a replacement operation.

| Observation proves | Effect state | Block |
| --- | --- | --- |
| Positive completion | `completed` | `done` |
| Positive no effect | `no-effect` | `failed` |
| Positive partial realization this context owns | `partial` | `failed` |
| Inconclusive removal observation of a replay-safe removal, before its one retry | `inconclusive` | `failed` |
| Failed, forbidden, empty, malformed, contradictory, or foreign observation, or any other inconclusive one | `unknown` | `unknown` |

The operation then takes the state its blocks give it under
[precedence](#operation-state-precedence), and a block resolved `failed` is
retried by a new attempt, which converges it. The continuation that resolved it
makes that attempt itself once every unproved effect is resolved, under the
rules of any retry, so one invocation recovers what took two before (D146); a
fresh `destroy`'s resolutions retry nothing of the apply they read. The
diagnostic of a block a
resolution leaves `unknown`, and the `unresolved` remedy `status` names for a
block no resolution observed, end with the exact command that observes it
again: the operation's continuation, or the fresh `destroy` that resolves an
apply's blocks, with `--context` and every `--authorize` token its plan
consumes.

A fresh `destroy` resolves each unproved block of the incomplete apply it
supersedes first with that block's apply observation, the capability's
read-only `Observe` (D123). One that proves the operation completed records
the block `done` and captures the material it produced into
[custody](#produced-material-custody) exactly as a completed apply's
finalization does, such as an agent installation's administrator kubeconfig,
before any inverse runs. Any other block it resolves with the removal's own
check, through the removal block that takes it back and keeps its identity,
implementation, content digest and frozen request, rather than with the
apply's exactness check (D119). That resolution proves that the object is this
context's own and can be taken back, or that it is absent, and never that the
apply's frozen request is realized, so a target that drifted from that request
yet reads as this context's own is removed rather than refused. It is recorded
against the apply's block, as a positive absence of effect for an absent
target and a positive partial realization for an owned one, both `failed` for
the removal to take back, and it never proves the block `done`, so it captures
nothing. A target neither observation resolves stays `unknown`, and the
removal refuses, registering nothing. A continuation of the apply still
resolves by the apply's check alone.

| Removal's check proves, after an apply observation that did not prove completion | Apply effect state | Block |
| --- | --- | --- |
| `completed`: the target is absent | `no-effect` | `failed` |
| `no-effect`: the target shows everything the removal takes back, as this context's own | `partial` | `failed` |
| `partial`: part of it remains, as this context's own | `partial` | `failed` |
| `unknown` | `unknown` | `unknown` |

A resolution records an outcome beside that effect. A completed effect records
the outcome its capability proves the effect had, `changed` or `unchanged`, as
an observation that repeats its apply's own proof does, and `changed` when the
capability proves neither, because the attempt it completed may have changed
its target. Positive no effect and a positive partial realization record
`failed`, and an unproved observation `unknown`: an outcome the capability
states counts only beside a completion.

### Produced material custody

Material a block's proved effect leaves, such as an installation's
administrator kubeconfig or a Ceph cluster's dashboard credential (the entry
`ceph-cluster-<name>`, `dashboard`, recaptured without rotation), enters the
context's [custody](secrets.md#produced-material) before its apply block is
recorded
`done`, whether an attempt or a resolution proves it, including a fresh
`destroy`'s resolution of an incomplete apply's block by its apply observation
([resolution outcomes](#resolution-outcomes)); one resolved by the removal's
own check proves no completion and captures nothing. The engine publishes
one block's outputs in one publication, inside the operation's transaction
through the secret area the Workspace lends it, and a capability offers them
only from a proved completion, except the copy a capability's read-only
observation hands over before a removal, which is published unproved (D124,
below). A publication that fails records the attempt
or the resolution `unknown`, never `failed`, because the effect it follows was
proved: it never leaves a block `done` whose material custody does not hold,
and the next invocation of either verb, including a `destroy` before it
registers, observes the block again by its apply observation and captures what
that proves before any effect or inverse follows. A `failed` block would be
retried by an `apply` but never observed by a `destroy`, whose inverses could
then delete the only copy.
A removal's attempt, and its resolution of its own block, capture nothing
proved. A completed removal
withdraws every entry inside the transaction that records or finalizes its
completion, before it releases its reservations, and a removal that stops part
way keeps them, so access custody already holds is never the first thing a
partly removed context loses. A destroy that resolved an installation by its
removal check, its completion unproved, captures nothing then, yet the
installer's own copy of the access is never deleted while custody holds none
for that cluster (D124). Before an inverse that deletes the copy a block's work
area keeps, such as the media block's inverse that deletes the installer's
work area, runs, the engine asks custody for the entry a proved capture of that
copy fills, keyed by the installation block and `kubeconfig`. When custody
holds none, proved or unproved, the capability's read-only observation hands
the copy over under the same output bounds, the engine publishes it in one
publication under that entry marked unproved, its completion never proved, and
only then does the inverse run. A crash between the publication and the
inverse leaves the copy in custody and the work area both, and the next
removal finds custody holding it and keeps nothing more; a crash before the
publication leaves it in the work area alone. An observation or publication
that fails records the removal attempt `failed` before its inverse ran, so the
copy is never in neither place. This holds whatever the nodes' power state, so
a destroy that waited on its `Stop first` Machines loses nothing. An unproved
copy never replaces an entry custody holds, a proved capture of the same entry
clears the mark, and the entry is withdrawn as every entry is. No record, log,
evidence or adapter output carries
the material.

A block may name, as a *produced input*, an output another block of its plan
produces, by that block's kind and object and the output's name: today a
cluster's `kubeconfig`. Planning resolves it to the producing block, depends on
that block, and freezes the reference, never the bytes. Each apply attempt and
observation of the consumer receives the custody entry as an operation-scoped
material file under the bounds, clearing and logging rules of a declared
Secret's, read from custody at that attempt; an entry marked unproved is lent
only to a removal, and a missing one refuses with `lifecycle.state` naming the
producer. A removal lends the entry while custody holds it, which is until the
removal completes. A consumer reaches the cluster only at the server the entry
names, verifies it against the entry's authority, and reads a foreign or
silent answer as `unknown`. Because a consumer reads the entry again at each
attempt, a producer's recapture keeps the identity of the effect it proved,
such as a cluster's authority or a principal, as the dashboard credential's
recapture without rotation does, and a recapture that presents another
identity refuses `lifecycle.state` rather than replacing the entry. An
installation's `kubeconfig` presents its cluster's server, the digest of its
decoded authority and its client certificate's subject as principal
(`TestARecaptureOfAnotherIdentityRefusesBetweenAttempts`,
`TestAKubeconfigsIdentityIsItsServerAuthorityAndPrincipal`). A Ceph output
(`client`, `object-user`, and the `dashboard` of a `ceph-cluster-<name>`
block) presents as authority its cluster's fsid, which its evidence holds to
the frozen request's, and the creation time of the cluster's monitor map, which
its evidence reports in one UTC form, and as principal the one
its frozen request names, which its validation holds every capture to; custody
records that identity with the entry, so a recapture of another cluster's
material under the same principal refuses, and the detection of a cluster
bootstrapped again under the same fsid relies on that creation time
(`TestEveryCephOutputPresentsItsClusterFSIDAndPrincipal`,
`TestARecordedRecaptureIdentityRefusesAnotherBetweenAttempts`). An entry
recorded with no identity is replaced and records the one presented; a
recapture that presents none keeps the recorded one. An entry
custody marks unproved is replaced by a proved capture whatever it presents.

A capability whose runs must not hold a produced entry itself borrows it for
a *lend*: a run of operation `lend` that is handed the entry alone, mints from
it a narrower access for one later run of the same execution, and leaves that
access as its one output, which is lent to that run and cleared when it
returns, never published into custody. A lend creates no object, so a
read-only or unrecorded execution may run one and take its output, which the
runner refuses for every other operation; a quiescence probe borrows produced
entries as the removal it gates does. The KubeVirt arm on a host this context
builds is the one such consumer
([KubeVirt machine realization](substrates.md#kubevirt-machine-realization)),
and a [machine power command](cli.md#machine-power-operations), which
registers no operation, lends for its own run the same way from the proved
entry custody holds.

Not yet met: no live run has confirmed that the monitor map's creation time
stays the same across a monitor added or removed and a Ceph upgrade, and
changes on a new bootstrap under the same fsid; tracked as
[B867](milestones/backlog.md#b867).

### Block transitions

| Block state | Step | Leads to |
| --- | --- | --- |
| `pending` | `attempt` | `running`, then `failed`, `unknown` or `done` |
| `failed` | `retry` | `running`, then `failed`, `unknown` or `done` |
| `running` | `observe` | `failed`, `unknown` or `done` |
| `unknown` | `observe` | `failed`, `unknown` or `done` |
| `done` | `none` | `done` |

An `attempt` starts once every dependency is `done` and the block's stage is
selected; a `retry` is a new attempt of a failed block the selection admits,
started under the same rules, so independent failed blocks are retried
together (D144). Either records `running` before its first side effect and
then its [attempt outcome](#attempt-outcomes). An `observe` step is a
resolution and records its [resolution outcome](#resolution-outcomes);
unproved blocks are observed before any other step starts. A resolution is not
an attempt: an invocation observes a block at most once and never observes one
its own attempt left unproved, and a block its resolution leaves `failed` is
retried by the same invocation as any failed block is, so its dependents can
start there too (D146). Its progress row then stays running into that retry,
and is settled `failed` if the invocation stops before the retry starts.

An attempt that cannot start, because this executable lacks its implementation
or the store refuses to allocate its number and record `running`, performs no
effect and records no outcome. The log-fault flag is unchanged. The invocation
re-reads the block's record and holds what it reads: the state it had,
`pending` or the `failed` it was retrying, or `running` when the start's final
publication landed and then reported a failure, which the next invocation
observes as it does any `running` block. The invocation that meets it admits
nothing further, never admits that block again, and waits for what is already
running. Its operation then takes the state its block records give it under
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

Every write of an operation's state records the first row its durable block
records satisfy, and an invocation that holds a block in memory holds what that
block's record reads: after a start or a resolution whose publication reports a
failure, it re-reads that record before it writes the operation.

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
numbered plan an operator confirms is the order the work is started in. A
wave is never a barrier: a block starts once its own dependencies are done. Equal definitions in any input order still
produce an identical plan and an identical digest.

A plan holds at most 256 blocks, a block at most 32 groups of at most 512
Machines each, 128 impacts and 8 resources it will not share, every identifier
at most 63 characters, each canonical request at most 256 KiB, and all of a
plan's requests together at most 4 MiB, so a fresh apply's plan and its
removal's fit the bytes it keeps free; the
[operator-visible bounds](contexts.md#storage-locking-and-publication) list
them. A plan past any bound refuses before registration, naming the bound.

A definition may name host resources its block *will not share* while it runs.
Two blocks that name one resource never run at the same time, however
independent the graph says they are, because the graph orders what one block
needs from another and not what two of them would write to at once. The set is
bounded, unique, ordered and frozen with the plan, and a removal keeps exactly
the set its apply froze, because what must not run together to create
something is what must not run together to remove it.

A block's description and impacts state what the planned verb does, never what
the opposite verb would do. A destroy block lists only the effects it
performs: a resource an apply creates, publishes or opens is one the destroy
removes or closes, and one a removal deliberately retains, such as shared host
software, appears in no impact; its description says that it retains.

A capability names what its blocks depend on in domain terms, not by block
identity: a definition may carry *requirements*, each the kind and name of an
API object whose realization must be done first, optionally narrowed to one
capability family, such as the substrate family that claims a Machine.
Reconciliation resolves every requirement to the blocks that realize that
object, or only that family's blocks, before it freezes the plan, and refuses
before registration when no block realizes it, so a capability never learns
another's block-identity grammar. A reference from a block's object to an
object another block of the plan realizes is a requirement unless the domain
page that owns the referencing field states why not; a reference to an
external service, which no block realizes, is the standing exemption. The
[reference edges](#reference-edges) catalog lists every requirement a
capability emits, and domain pages link it rather than restating their sets.

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

### Reference edges

Each row is one requirement class a capability family emits: the block (its
family and block kind), the API object it requires (narrowed to one family
where stated), and the reference or rule that names that object. A
requirement resolves only when some block of the plan realizes its target, so
an external service never orders anything. A reference no row names orders
nothing for that block; it is still resolved and validated. Destroy inverts
every edge. Rows within one family that the family orders by its own block
dependencies, such as a storage child after its cluster, are not requirements
and are not listed. `TestEveryRequirementMatchesTheEdgeCatalog` plans every
example and fails when a capability emits a requirement no row names, or omits
one a row's field path names on its object while a block realizes the target.

| Block | Requires | Named by |
| --- | --- | --- |
| `substrate-machine` `Machine` | `InfraProvider` | `spec.substrate.providerRef` |
| `os-install` `Machine` | `Machine` | the Machine itself, realized by its substrate |
| `os-install` `Machine` | `MachineInstallProfile` | `spec.os.installProfileRef`, realized when its package tree is hosted |
| `os-install` `Machine` | `ArtifactServer` | the install profile's and the Machine's artifact endpoints |
| `os-install` `Machine` | `DNSServer` | the `dns` selections of the Machine's network configuration |
| `os-install` `Machine` | `NTPServer` | `spec.os.install.ntp[].serverRef` |
| `os-install` `Machine` | `CephCluster` from `storage-physical-retirement` | a storage cluster whose retirement must release this physical Machine first |
| `physical-erase` `Machine` | `Machine` from `substrate-machine` | the Machine itself |
| `physical-erase` `Machine` | `ArtifactServer` | the erase image's artifact endpoint |
| `physical-erase` `Machine` | `DNSServer` | the `dns` selections of the Machine's network configuration |
| `physical-erase` `Machine` | `NTPServer` | `spec.os.install.ntp[].serverRef` |
| `physical-erase` `Machine` | `CephCluster` from `storage-physical-retirement` | a storage cluster whose retirement must release this physical Machine first |
| `os-package-tree` `MachineInstallProfile` | `ArtifactServer` | `spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint.serverRef`, `spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint.serverRef` |
| `os-package-tree` `MachineInstallProfile` | `Environment` | the selected Environment, whose controller stage prepares the host |
| `managed-service` `ArtifactServer` | `InfraProvider` | the provider whose libvirt bridge carries the placement Machine's service address |
| `managed-service` `DNSServer` | `InfraProvider` | the provider whose libvirt bridge carries the placement Machine's service address |
| `managed-service` `NTPServer` | `InfraProvider` | the provider whose libvirt bridge carries the placement Machine's service address |
| `managed-service` `Proxy` | `InfraProvider` | the provider whose libvirt bridge carries the placement Machine's service address |
| `substrate-host` `InfraProvider` | `OpenShiftCluster` | `spec.kubevirt.hostClusterRef` |
| `substrate-host` `InfraProvider` | `ClusterAddonBinding` | non-last instances on that host |
| `substrate-host` `InfraProvider` | `OpenShiftClusterNetwork` | that host's network block |
| `cluster-media` `OpenShiftCluster` | `ArtifactServer` | `spec.install.agent.redfishVirtualMedia.artifactServerEndpoint.serverRef` |
| `cluster-media` `OpenShiftCluster` | `Proxy` | `spec.install.proxy.proxyRef` |
| `cluster-install` `OpenShiftCluster` | `Machine` | `spec.nodes[].machineRef` |
| `cluster-install` `OpenShiftCluster` | `DNSServer` | the `dns` selections of each node's network configuration |
| `cluster-install` `OpenShiftCluster` | `NTPServer` | `spec.install.ntp[].serverRef` |
| `cluster-install` `OpenShiftCluster` | `Proxy` | `spec.install.proxy.proxyRef` |
| `cluster-network` `OpenShiftClusterNetwork` | `OpenShiftCluster` | the cluster whose secondary networks it configures |
| `cluster-network` `OpenShiftClusterNetwork` | `ClusterAddonBinding` | the add-on instance on that cluster that provides `nmstate` |
| `addon-instance` `ClusterAddonBinding` | `OpenShiftCluster` | the binding's `spec.clusterRef` |
| `addon-instance` `ClusterAddonBinding` | `ClusterAddonBinding` | each instance on the same cluster providing a capability the package requires |
| `addon-instance` `ClusterAddonBinding` | `CephConsumer` | the package's `cephConsumerRef` |
| `storage-core` `CephCluster` | `Machine` | `spec.topology.nodes[].machineRef` |
| `storage-core` `CephPool` | `CephPlacementPolicy` | `spec.placementPolicyRef` |
| `storage-core` `CephRBDImage` | `CephPool` | `spec.poolRef`, `spec.dataPoolRef` |
| `storage-core` `CephFilesystem` | `CephPool` | `spec.metadataPoolRef`, `spec.dataPoolRefs[]` |
| `storage-core` `CephSubvolume` | `CephFilesystem` | `spec.filesystemRef` |
| `storage-core` `CephClient` | `CephPool` | `spec.poolRef` |
| `storage-core` `CephClient` | `CephSubvolume` | `spec.subvolumeRef` |
| `storage-core` `CephNFSExport` | `CephSubvolume` | `spec.subvolumeRef` |
| `storage-core` `CephNFSExport` | `CephObjectUser` | `spec.objectUserRef` |
| `storage-core` `CephNVMeoFSubsystem` | `CephRBDImage` | `spec.namespaces[].imageRef` |
| `storage-core` `CephObjectUser` | `CephObjectGateway` | `spec.objectGatewayRef` |
| `storage-core` `CephObjectBucket` | `CephObjectUser` | `spec.ownerRef` |
| `storage-consumer` `CephConsumer` | `CephCluster` | `spec.clusterRef` |
| `storage-consumer` `CephConsumer` | `CephPool` | `spec.dataFoundation.rbdPoolRef` |
| `storage-consumer` `CephConsumer` | `CephFilesystem` | `spec.dataFoundation.filesystemRef` |
| `storage-consumer` `CephConsumer` | `CephObjectGateway` | `spec.dataFoundation.objectGatewayRef` |
| `storage-consumer` `CephConsumer` | `DNSServer` | every DNSServer of the selection, when the export names an object gateway |
| `storage-host` `CephCluster` | `Machine` | `spec.topology.nodes[].machineRef` |
| `storage-host` `CephCluster` | `MachineInstallProfile` | each node Machine's `spec.os.installProfileRef` |
| `storage-physical-retirement` `CephCluster` | `Machine` from `substrate-machine` | `spec.topology.nodes[].machineRef` |
| `storage-replication` `CephReplication` | `CephPool` | `spec.poolRef`, `spec.peerPoolRef` |
| `storage-replication` `CephReplication` | `CephRBDImage` | `spec.imageRefs[]` |
| `storage-smb` `CephSMBResource` | `CephSMBResource` | `spec.smbClusterRef`, `spec.resourceRefs[]` |
| `storage-smb` `CephSMBResource` | `CephSubvolume` | `spec.subvolumeRef` |

### Stages and the pause boundary

Every block carries exactly one stage, frozen with the plan and covered by its
digest. A stage names the kind of platform work its block performs:

| Stage | Blocks |
| --- | --- |
| `controller` | The [context prerequisites](controller.md) this Environment adds to its controller host: the target clients its graph selects; the libvirt client a libvirt provider hosted on the controller or any Machine a libvirt provider hosts selects, [and nothing else](controller.md#the-controller-stage); the hypervisor closure a hosted provider selects; the installer-media tooling an artifact server on the controller selects; the `nmstate` package an agent-installed OpenShiftCluster node that references a declared Machine selects; and the `iputils` package an agent-installed multi-node baremetal OpenShiftCluster selects for an OpenShift-sourced IPv4 virtual IP in a managed libvirt network of a provider hosted on the controller. |
| `infrastructure-services` | Managed shared services: proxying, name resolution, time, artifact serving, registries and load balancing. |
| `substrates` | [Provider host realization](substrates.md#provider-host-realization) for a declared `InfraProvider`: its virtualization runtime, managed networks and virtual-media pool. |
| `machines` | [Machine realization](substrates.md#machine-realization) with its management controller, the [claim and proof](substrates.md#physical-machine-realization) of a physical machine, [shared package-tree publication](managed-os.md#shared-package-tree-publication), and [managed operating-system installation](managed-os.md#installation). |
| `clusters` | Container and storage cluster installation. |
| `add-ons` | Add-on instances bound to a cluster, and the cluster configuration they enable. |

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
selects no client or native closure beyond the host baseline contributes no
controller block at all.

A controller block is the only block whose effects are shared host state rather
than this context's own. Its closure therefore outlives the context that
selected it: the block's removal retains what it installed, and the shared area
it published into is proved, not deleted, by a destroy. Because it extends what
[controller setup](controller.md) prepared, the engine hands that block the
retained setup evidence, the shared publication area and its sealing, the
durable dependency record that precedes acquisition and the before-state
publication that precedes a host transaction. No other block receives them.
No block holds or hands over a native package lock: a launch holds only its
runtime's shared launch lock, which never blocks the host's package manager.

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

Production uses the engine's bound of eight running blocks. Independent
services and machines are admitted together; a fast dependency chain advances
while unrelated work remains running. A hosted package tree is its own durable
block: installations require its completion and hold no exclusive tree claim
while they build images, boot or wait for guests. Replay and inverse dependency
ordering use that same ownership boundary.

The lifecycle opens a private invocation scope at its execution boundary and
closes it after every admitted adapter has joined. Runner copies share its
synchronized live-job registry. An adapter's held job is exempt from the
running-adapter refusal only while that exact job's original lock and holder
files belong to this live scope. Matching context, operation or attribution
bytes alone grants no exemption. Another invocation, an unattributed held job,
or a descendant holding a job after its owner released it still refuses.
Sweep and claim publication, and unregister and release, are synchronized;
adapter processes run concurrently outside that critical section.

Each adapter receives the runner's private scratch path as execution material,
outside its frozen request and digest. A role proves its own root-only child
before writing, and cleans only that child, including on a remote placement.
Durable machine work and published artifacts remain beneath the disjoint paths
their capabilities freeze. Cancellation and cleanup never remove a sibling's
scratch or retained effects. [B303](milestones/delivered.md#x48--bounded-concurrent-execution),
[B304](milestones/delivered.md#x48--bounded-concurrent-execution) and [B466](milestones/delivered.md#b466) qualify these
boundaries in production composition and the closing operator run.

An operation is `paused` when execution stops because no block is startable,
nothing is still running, no block is failed or unknown, and pending blocks
remain. A pause is a
successful, resumable stop, not an interruption: it needs no recovery, it
allocates no new identity, and the next `apply` continues the same operation
under whatever selection it is given. Cancellation is never a pause.

#### Stopping after an add-on instance

`apply --until <cluster>.<package>` adds one more gate to the selection: a
block is startable only when it is also the named add-on instance or a block
that instance depends on, directly or through other blocks. The name is the
object of exactly one block of the `add-ons` stage in the plan the apply works
on, fresh or frozen, whose object has the `<cluster>.<package>` form; another
`add-ons` block, such as a cluster's network block named by the cluster alone,
is no candidate; it is resolved for each invocation and never recorded, so
the operation holds no selection. When that instance and its prerequisites are
done the operation pauses as above, and a later `apply`, with or without
another `--until`, continues the same operation. `--until` composes with
`--stage`, and `plan --until` previews the same selection.

Before any effect, an apply refuses with `lifecycle.stage`:

| Fault | Message | Remedy |
| --- | --- | --- |
| The plan has no add-on instance | `the plan has no add-on instance for --until to name` | `repeat bootwright apply --context <context> without --until` |
| No add-on instance carries the name | `the plan has no add-on instance named by --until` | `name one of: <objects>` (at most eight, then `and <n> more`) |
| The selected stages admit none of the ready blocks the instance requires | `the selected stages have nothing to start` | the apply with `--stage` widened by the first such block's stage, keeping `--until` and the plan's authorizations |
| A failed block the instance requires lies outside the selected stages | `the block this operation must retry is outside the selected stages` | the apply with `--stage` widened by the first such block's stage, keeping `--until` and the plan's authorizations |
| The instance and every block it requires are done, or every block to retry lies outside them | `the add-on instance --until names and the blocks it requires have nothing to start under this selection` | `repeat bootwright apply --context <context> [--stage <list>] [--authorize <tokens>] without --until` |

A destroy accepts neither selection: `plan --until` over a due destroy refuses
as `plan --stage` does.

Selection never weakens a safety rule. An unproved effect is resolved before
anything else whatever stages are selected, because resolution is a read-only
observation; several unproved effects may be observed together, and nothing
starts, retries or is removed beside them. Failed blocks are then retried
under the rules a first attempt follows: each whose stage is selected starts
once the bound and its exclusive resources admit it, beside the other retries
and beside every startable block, and what depends on a failed block waits
for its retry to complete, as it would for a first attempt. An invocation
retries a block at most once, and once a block it worked fails, it starts no
further first attempt, although the failed blocks still waiting are retried.
A failed block outside the selection is never retried, and when no failed
block's stage is selected the operation refuses `lifecycle.stage` before any
effect, naming the stage of the first. That refusal is decided once, before the
invocation works any block; once one is worked, admission follows only these
rules, so what starts never depends on which running block completes first. A selection that admits no startable block also refuses
`lifecycle.stage` before registration, naming a stage that would unblock work.

A fresh `plan` and a fresh `apply` refuse, whatever stages are selected and
before registration, every selected object this executable cannot realize: an
effect-bearing object no implementation
[claims](architecture.md#implementation-selection), such as a managed
`Registry` or an enabled `CustomPlaybook`, and every shape a capability reports
unsupported. Each refused object is its own `lifecycle.unsupported`
diagnostic, with the reason its capability gives, or that nothing claims it,
and the remedy, so `plan` says why as `apply` does; one refused for two
reasons is two diagnostics. An object two implementations of one family both
plan blocks for is refused the same way after planning, naming both, because
binding order never decides which one realizes it. A frozen plan
requires a resolved implementation for every block; a removal planned from a
frozen plan is never refused this way.

Which add-on kinds select an effect is owned by
[add-ons](add-ons.md#built-in-packages-and-selection): the add-on capability
claims a `ClusterAddonBinding` and reports what it cannot realize as
[add-on refusals](add-ons.md#refusal-table).

The engine's own refusals, of objects nothing claims or refuses, are the rows
below, and `TestUnclaimedObjectTableMatchesTheEngine` holds
`Unrealizable` to them. An external service and a Machine whose operating
system is provided are inputs, never refused. Whether an
object bears an effect is its domain's predicate, which composition hands the
engine, so it names no domain kind or field. Each
reason and remedy is the diagnostic's message and remediation, in which
`<kind>` is the refused object's kind and `<object>` its identity;
`examples/machines/lab-rhel` is the supported shape the remedy names.

<!-- unclaimed objects -->
| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| An unclaimed effect-bearing kind | an InfraProvider, OpenShiftCluster, storage or add-on object of a kind no capability claims | `no capability of this executable realizes the <kind> kind` | `remove <object> from the selected Environment, or use an example within the supported shape such as examples/machines/lab-rhel` |
| An unclaimed managed service | a `management: managed` Proxy, DNSServer, NTPServer or ArtifactServer of a kind no capability claims | `no capability of this executable manages the <kind> kind` | `remove <object> from the selected Environment, or use an example within the supported shape such as examples/machines/lab-rhel` |
| An unclaimed managed Registry or LoadBalancer | a `management: managed` Registry or LoadBalancer, which no capability claims | `no capability of this executable manages the <kind> kind` | `declare spec.management: external on <object> to use a <kind> the operator runs, or remove it from the selected Environment` |
| A Machine to install with no capability | a Machine with `spec.os.provided: false`, its kind unbound | `no capability of this executable realizes a Machine whose operating system is not provided` | `remove <object> from the selected Environment, or use an example within the supported shape such as examples/machines/lab-rhel` |
| An enabled CustomPlaybook | a CustomPlaybook with `spec.enabled: true`, its kind unbound | `no capability of this executable runs an enabled CustomPlaybook` | `remove <object> from the selected Environment, or use an example within the supported shape such as examples/machines/lab-rhel` |
| An unclaimed object of a claimed kind | an effect-bearing object of a bound kind none claims | `no implementation of this executable claims <object>` | `remove <object> from the selected Environment, or use an example within the supported shape such as examples/machines/lab-rhel` |

A fresh `apply` also refuses, once it has bound the Secrets
its plan names and before registration, any bound material a capability proves
unusable, naming the Secret, and it releases the binding it created
(`TestAFreshApplyRefusesMaterialItsCapabilityRefusesBeforeRegistration`).

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
planned from a frozen plan is never refused this way. A bounded run, which a
`machine start`, `stop` or `restart` and the power reading of
`machine list --power-status` each carry and which is never planned, refuses
the same way before its confirmation and before any runtime is lent or Secret
bound. It names the Machine whose value holds the delimiter or key and the
field, read from that Machine's request, and its remedy ends with that same
command, with `--context`.

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
registry, and the operation record freezes the Python and Ansible
[execution closure](#dependency-safety-during-recovery) every implementation
runs in and names the automation and build it registered under. That digest
covers the canonical
secret-free request and only non-sensitive immutable secret-binding
identifiers or versions; it never hashes secret material. Secret bytes and any
sensitive binding metadata remain solely in the confidential snapshot or
external-version mechanism above. A missing, ambiguous, unsupported, or
drifted implementation refuses before the affected side effect; continuation
never re-resolves to a different implementation.

### Dependency safety during recovery

A continuation of either verb uses the exact frozen execution closure its
operation registered with, and the automation of the build in hand only where
that build freezes the same blocks, as below; a fresh removal, which registers
an
operation of its own, runs under the build and runtime in hand, as below. If
required content is unavailable, changed, or its safe use for the requested
operation cannot be established, refuse before the affected observation or
effect and report the recovery requirement. Preserve the plan and ownership
evidence; never silently upgrade, substitute a dependency, or discard recovery
state.

Every effect of an operation runs inside the controller runtime of the build
in hand: the venv its embedded runtime lock builds on the interpreter the setup
receipt names ([controller runtime](controller.md#host-identity-and-shared-prerequisites)).
An operation locates that runtime once, before it registers, and refuses
`controller.state` with `run bootwright setup` when the setup record holds no
receipt, records an interrupted setup or another build's format, or this host
has published no runtime for the build's lock. A registration freezes the
runtime's Python and Ansible *execution closure* in the operation record:
`bootwright.controller.execution-closure-v2`, a SHA-256 over the runtime lock
digest and the interpreter's ABI, with that ABI (`python`) and the lock's
`ansible-core` release (`ansible`). The automation, the interpreter's path and
full release and the host's native packages are no part of it, so any rebuild
that keeps the lock, an OS Python update within the ABI and an OS upgrade that
keeps it all continue a live operation, and an automation-only fix needs no
setup.

Before a continuation of either verb restores its operation's log, raises
evidence or marks its operation `running`, it refuses `lifecycle.state`, with no
effect and no record changed, when this build's runtime carries another closure (another
runtime lock or another interpreter ABI), naming the build that registered it,
whose `setup` restores its runtime, fetching any wheel the cache no longer
holds over the network or with `--wheel-mirror`, then continues it, and for an
apply the removal that takes back what it owns, or when
another build registered the operation and this executable does not freeze the
same blocks. Another build is one whose automation differs from the one the
operation registered under or whose version or commit differs from the ones it
records, and any build whose own stamp says its source was modified, since no
recorded identity tells two such builds apart. For an apply, a build freezes the
same blocks when planning the operation's frozen input again gives exactly the
operation's blocks, each with every member its plan froze except the words it is
presented in: its description, its impacts and its groups' descriptions. For a
destroy, the proof derives the removal again from the operation's own frozen
plan, which copies each block's request, implementation, content digest, stage
and dependencies from that plan, so it compares what the removal decides, the
authorizations it consumes and its groups, and whether every block's capability
resolves and reads its frozen request, as a fresh removal does. That proof
plans, and planning reads the media store under the store's own shared lock, so
the decision proves it under the shared lock and the exclusive re-proof keeps it
once it finds every record it read unchanged. The proof compares blocks only: a
continuation keeps the host reservations and the Secret binding its registration
took. A refusal names the first block that differs, or why the operation could
not be planned again. A continuation that passes runs this executable's
automation, which is how an automation fix reaches a failed or unknown block,
and its record keeps naming the build that registered it. Every content digest
is computed from the implementation's declared contract (its identity, its
request and evidence versions and its effect pins) and never from automation
bytes. An *effect pin* is a value Go owns outside the frozen request that
fixes what an effect is, such as an artifact digest, a package set, a host
path, a name prefix or an ownership mark; the digest hashes each one, and a
value the request carries needs no pin
([when a digest moves](#continuation-and-removal)). A fix confined to
automation, or to Go code that moves no version or pin, therefore continues
with no `setup` between, because automation is staged per invocation and a
continuation needs only the same runtime lock and Python ABI. Such a
continuation resolves an
unknown block first and, when the observation leaves it failed, retries it in
the same invocation (D146). Each attempt names the build that ran it, while the operation
keeps naming the one that registered it. Each refusal names the executable
version and commit the operation registered under, and the closure refusal
also names the Python and
`ansible-core` releases the operation needs and the ones this build's runtime holds. Each
remedy restores what the operation registered under, and where a fresh removal
may [supersede](#continuation-and-removal) the operation, it also offers that
removal. Once a build that writes `build` has worked the operation, the build
it registered under cannot read its records if it predates that member, so the
way out is then the superseding removal under the build in hand, or the
[deletion exit](#lifecycle-unit) where none may take its place. A fresh
verb always runs under the build and runtime in hand.

A removal keeps the content digest its apply froze and compares none, so every
value that fixes a removal's target identity, place, ownership mark or host
claim travels in the request that apply froze, and the removal reads it from
there, never from the build in hand. The automation values that are exceptions,
a host path, name prefix, mark or proof identifier the automation fixes outside
the request, are an exact list of the literals the
[contract guard](#continuation-and-removal) sees, which it holds to each
implementation's contract, and a changed fixed place also moves the
implementation's request version, so a removal under the new build refuses the
old block's request instead of looking for its effects in the new place.

That re-proof, with the controller setup and host binding a continuation also
re-proves ([controller-host protection](#controller-host-protection)), runs
under the shared lock before the plan is presented, authorized or confirmed,
and again under the exclusive lock before registration, so no plan is
presented for a continuation or resolution that refuses. A fresh removal
proves its host the same way. An `apply` refused over an incomplete removal
names that removal's continuation, or the fresh removal that replaces a failed
one, only when its re-proof, or that replacement's host proof, admits it;
otherwise its remedy is the exit that proof's refusal names, and that refusal
follows it. A host whose setup has not completed, or whose runtime for the
running build is not prepared, refuses every verb that runs a block or
registers, naming `run bootwright setup`, and `status` offers
`bootwright setup` in their place.

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

An invocation that names no attempt of its own, a resolution, or a keep is
read-only: the runner refuses any adapter operation but `observe` there, before
it creates a job. An invocation no operation record names, a quiescence probe
or a comparison, may take over no produced material: the runner refuses one
whose request declares an output.

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

While a block is `unknown`, Bootwright starts no first attempt, retry,
dependent block, destroy effect, or replacement operation. Confirmation,
authorization, manual state editing, elapsed time, and a dead prior executor
cannot resolve it. The resolution evidence and resulting transition must be
durable before execution continues.

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
repeating the verb. Evidence it reads as this request's, which none of the
verb's checks accept, names the check that decided: for an apply the realized
target's first difference from the frozen request, for a removal what keeps the
target from reading as this context's own, and either way the remedy is to
restore the target. A removal that supersedes an apply reads the apply's
blocks by the apply's observation and then by its own check
([resolution outcomes](#resolution-outcomes)), so it takes back a target its
check reads as this context's own, however that target drifted from the
apply's request, and refuses only a block neither reading proves, naming what
keeps the target from reading as this context's own and the removal's remedy.
Evidence the capability cannot explain gets the general
reason that the observation proved neither the effect nor its absence, and an
unproved block no resolution has observed yet says that no observation has
read it. A resolution that leaves its block unknown records that reason in its
resolution log as `unresolved` and reports it with its remedy in a
`lifecycle.unknown` diagnostic naming the block, unless its
observation could not run at all, which reports why it did not, so the refusal
of a removal names every block it could not prove with why. A capability
returns an observation it could not make as its failure; the engine logs each
diagnostic of it as `observation-failed` and records the first on the
resolution record. `status` reports the same reason and remedy for each
`unknown` or `running` block of the current operation, read from the last
resolution of that block's last attempt: one whose observation could not run
reports its recorded failure, with the reason
`its observation could not run: <message>` and its remediation as the remedy,
before any capability reads its evidence, so a refusal and a later `status` agree. An undiagnosed failure
records none and keeps the capability's or the general reason. A resolution
that proves completion but cannot place what the block produced in custody
records no failure, since its observation ran: it refuses with custody's
failure and its block stays `unknown`. The reason changes no state: an explained
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

Restoration is proved, never assumed. While the faulted operation is the
context's current one, exactly these invocations restore it: any later
invocation that would observe, probe, register or start anything, and an
`apply` that [settles](#lifecycle-unit) over the unchanged input of that
operation once it is a completed apply, after re-proving the apply it decided
from. Each first reopens that operation's operation log under the exclusive
lock and durably writes its opening record, and only then durably clears the
flag; the settling `apply` reports only after that. Nothing else clears it:
not elapsed time, a read, `status`, a `destroy` that settles, or a write to any
other log. A failure at either step preserves the flag and starts nothing.
A continuation performs the same reopening before any work whether or not the
flag is set, and a removal performs it for the operation it replaces before
resolving any of its effects; that reopening is the restoration a resolution
requires. Restoration permits new logging but never repairs, appends to, or
replaces the failed attempt log. Missing or truncated log detail is never
reconstructed, treated as operation evidence, or used to weaken the resolution
rules above.

### Continuation and removal

Continuation skips done blocks and retries failed blocks under their frozen
capability contracts, concurrently and each once per invocation: an invocation
never retries a block it failed itself, because the condition that failed it
has not been shown to have changed. A block that its own resolution, not an
attempt, left `failed` is one the invocation has not yet retried (D146). Go
owns ordering and lifecycle state and invokes capability ports under the
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary).

A failed attempt admits no further first attempt, although the selected failed
blocks the invocation has not yet retried are still retried, and the blocks
already running are waited for rather than abandoned: each records the outcome it proved, and stopping one
mid-effect would turn a provable outcome into an unproved one. The result names
every block that did not complete, in frozen plan order, because blocks run
together and which of them failed first is a race rather than a fact about the
environment. An attempt that ends `unknown` admits nothing further, first
attempts and retries alike, and waits for what is in flight; the invocation
never observes it, because its own attempt left it unproved. Cancellation is
the same: it admits nothing further and waits for what is in flight, and it is
never a pause. These three are every cause that stops admission short of a
stage or operator-stop boundary.

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

A completed apply owns its whole frozen plan, because it completed only once
every block of it was `done`, so its removal covers that whole plan even while
a block of it reads anything else, rather than removing only the `done` rest. A
lost block record reads back as `pending`, and a removal that skipped that
block would leave its effect in place and then release the binding it needs.
The removal resolves each unproved block of the plan by observation before it
registers, as it does an incomplete apply's, and takes back every other block
not `done` by its inverse, as it does a failed block of an incomplete apply. The apply's own record stays `done` whatever those resolutions
prove, so a removal that stops before it registers leaves the next one the
same whole plan. Every view of what that apply owns, the ownership and
realization `status` reports and the objects a lost frozen binding names, reads
the same set. The context's deletion stays available as the other exit,
which the repeated `apply`'s refusal and `status` name beside this removal. A
completed removal holding a block that is not `done` is refused by both
verbs, as the [lifecycle unit](#lifecycle-unit) states. An incomplete apply
and a failed removal are not held to this, because blocks that are not `done`
are legitimate in both.

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
  `failed` or `unknown` block, because an apply records `failed` only once a
  block's record reads `failed`, a `failed` block changes only through a retry, which first records
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
adopted or written. A retry whose start published the block's `running`
record and then reported a failure leaves the apply `running` beside that
`running` block, because the invocation re-reads the record before it records
the apply ([precedence](#operation-state-precedence)). A `running` or
`unknown` block started and its outcome is unproved, so the removal resolves it
before it registers and counting it skips no effect. An `unknown` apply need
not hold an `unknown` block: a removal interrupted after resolving that block
and before recording the apply's new state leaves it `done` or `failed`, and
still owned. A lost record that leaves none of the three cannot be told from a
block that never started.

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
`bootwright status`, except those that name deleting the context, beside no
operation and over a completed destroy holding a block that is not `done`,
whose only exit it is, and the repeated `apply` over a completed apply holding
one, which names that deletion beside the destroy of its whole frozen plan
([lifecycle unit](#lifecycle-unit)). The
[`contradictions`](cli/output.md#lifecycle-and-status-results) of `status`
name what each of those refusals names over the context's records: each block
a completed operation does not show `done`, each contradiction of an incomplete apply
above and an incomplete apply that records a state only a started block
explains beside no block that started, each lost record of an incomplete
removal, beside no operation, each state no index accounts for, and an
operation whose frozen Secret binding the keyring no longer lists, or whose
keyring listing fails as its reopen would, as below.

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
exists. A decoder reads the version a request holds before its shape, so a
request whose version removed or renamed a field still refuses by naming that
version, with the remedy to destroy it with the build that applied it. A request of any other version, or one whose implementation this
executable no longer provides, refuses before anything is registered and names
the block, the version it holds and the executable identity its operation
recorded, so the remedy is the command to run rather than the obstacle that
stopped it.

A capability's content digest moves when its automation changes what a block's
effect is rather than how the effect is reached: the objects it creates, their
names and places, the mark that proves them this context's own, the host
resources it claims, or what its observation proves. An apply continuation under
another build compares content digests and never automation bytes, so automation
that moves no content digest reaches the blocks a continuation still runs, and a
removal still reads the request its apply froze. A mechanical guard,
`TestEveryImplementationMovesItsContentDigestWithItsContract`, keeps one
contract golden per implementation and fails the build when any of its
sections changes while its content digest does not: the Go request and
evidence schemas, the persisted format versions quoted in the collection's
embedded plugins, roles and playbooks, the marks, proof identifiers and places
its automation fixes outside the frozen request, the effect pins its digest
hashes (the golden's recipe must reproduce the digest) and the request members
that carry its effects' identity. A changed fixed place fails even under a
moved digest until the request version moves, since only a request the new
build cannot read keeps a removal from looking in the new place.
`TestEveryAutomationVersionLiteralIsClassified` requires every quoted literal
ending in a version, every token naming a `bootwright.io` URI or key or a
Bootwright host path, name prefix or mark, and every capitalized `Bootwright`
token with up to three words after it, in every automation file of the
collection, to be classified with its reason: persisted, fixed, transient,
validated (it only bounds what a request may carry), mirrored, controller or
documentation; an automation file of an extension it does not read fails it.
`TestEveryCephMarkerNameIsDeclared` holds the declared Ceph marker record
names, each a proof identifier of the Ceph implementations whose sources carry
it, to the quoted record names and name fragments those sources carry. The
guard fingerprints a
raw-JSON member, such as the cluster services the Ceph core and host requests
carry, as an opaque leaf, and sees no dynamically built value, no value naming
no Bootwright token, no marker name built only from values and no unversioned
format; the schema nested in such a
member, and what a record or mark means beyond its shape and declared version,
stay the author's to move. A mark changed without moving the
digest fails closed: a removal under the new build reads the objects the old
mark proves as foreign and refuses, leaving them in place, so its cost is a
stuck or partial destroy. A change that loosens what an observation accepts as
this context's own has no such backstop, so it always moves the digest.

A fresh removal uses the secret material its apply bound, by reopening that
operation's own binding rather than binding what the current declarations name.
The material that created an effect is the material that proves it gone, and a
declaration edited or rotated afterwards never silently changes which version a
removal presents; only the recorded
[access-credential rebind](#access-credential-rebind) changes it, for an
access credential alone.

An attempt that cannot read a bound Secret or the part it needs, in the Ansible
runner, a managed-OS installation or an agent installation, refuses naming the
exact command that repeats its operation and so reopens its bindings: the
continuation the lifecycle handed the attempt, with `--context` and every
`--authorize` token its frozen plan consumes, or, when the attempt carries
none, `bootwright status --context <name>` and the command it names
(`TestABindingRefusalNamesTheCommandThatReopensIt`). A fresh removal that
cannot prove what its effects left behind names, after each effect's own
diagnostic, `bootwright destroy --context <name>` with the removal plan's
`--authorize` tokens.

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
from a [complete backup](contexts.md#format-and-restore-boundary) and repeat
the verb, or run `bootwright context delete
--name <name> --purge --allow-orphans` and then remove those objects by hand.
Only a listing that answers proves a binding gone; to a verb one that fails
proves nothing, and the reopen decides. A finalization and a verb that settles
reopen no binding, so neither refuses on its account: a removal whose blocks
are all `done` is acted on only by the `destroy` that finalizes it, so `status`
names no lost binding over it and offers that `destroy`. A binding an
operation names is released only by an invocation that moved the context on,
so a refusal whose binding was released after its decision was read re-reads
the context and reports that change instead. No re-binding of the current declarations,
substitution of other material, removal without the binding or release of
ownership inside the context exists, because none of them proves that the
material a verb presents is the material that created the effects; the
[access-credential rebind](#access-credential-rebind) reopens the binding it
replaces, so it is no exit from a lost one. `status`
names such an operation among its `contradictions`, in the refusal's words. It
reads a keyring listing that fails reporting the material corrupt or
undecryptable (`secret.store.corrupt`, `secret.store.crypto`) as the reopen
will, since every keyring session reads the same metadata and artifacts, and
any other failing listing as proving nothing. Over an apply whose blocks are
all `done` it offers `bootwright apply` first, which finalizes that apply
without reopening the binding, and then the deletion; over any other such
operation it offers that deletion as its only next step.

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
An unproved effect is not an exception: the removal resolves it first, by its
apply observation and otherwise by the removal's own check
([resolution outcomes](#resolution-outcomes)), and refuses,
registering nothing, when it cannot. A continuation of an apply still
resolves by the apply's check. A running or unknown removal is
continued rather than replaced, because a removal that lost an outcome is
resolved by repeating itself. A continuation runs a repaired adapter when the
repair leaves every frozen block the same; replacement is the road out of any
other repair, because a continuation stays frozen to its blocks and execution
closure while a fresh operation runs under the current ones.

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

### Access-credential rebind

An access credential rotated at its endpoint after an operation bound it, such
as a management controller's password, a provider's kubeconfig, a vCenter
login or a registry login, would strand that operation's continuation and
removal, because both present the bound version, which the endpoint now
refuses. The access-credential rebind is the one recorded exit beside the two
a [lost binding](#continuation-and-removal) names (D37), and it is not the
bounded run's rebind, which binds nothing and names no operation. An `apply`
or `destroy` that continues an operation or removes one names each such Secret
with `--authorize rebind=<secret>` (D183, Q70). Before anything is confirmed,
bound or registered, and after the plan's own tokens are authorized, each named
Secret must meet every condition below, or the invocation refuses
`lifecycle.authorization` naming the condition and the command without that
token:

- the transition reopens a frozen binding, so a fresh apply and a verb that
  settles or finalizes take no rebind;
- that binding pins a version of the Secret, and the Secret's current version,
  read and validated as a binding reads one, is another version;
- every block of the plan whose frozen request names the Secret reads it, through
  its capability, as an access credential it presents only to authenticate. A
  block that names it for any other reason, or whose capability reads no
  access credential, rests on the bound version as effect material and refuses
  the rebind, naming the step.

Today the bare-metal Machine, the managed-OS installation and its physical
erase of a bare-metal target, and the agent installation's bare-metal nodes
present their management controller's `credentialsRef`, the KubeVirt blocks
their `kubeconfigRef`, and the Ceph cluster, host and image blocks the
registry login. An emulated BMC's
credentials are effect material its libvirt host gave the emulator, and a
trust anchor (`trustBundleRef`, `knownHostsRef`) is never presented, so neither
is rebound. A vSphere arm will name its vCenter login likewise.

The rebind issues a new binding version: a binding equal to the reopened one
except that each named Secret is pinned to its current version. A fresh removal
registers with it; a continuation writes it into its operation's record as it
resumes, so every later attempt and the removal that follows reopen it. The
operation log records the binding it replaced, the one it issued and the
Secrets rebound. The replaced binding stays until the operation that names it
is released, and a rebind whose fresh removal provably did not register is
released at once. Every ownership proof the plan runs is unchanged: the
rebound credential only reaches the endpoint, and what the block proves it
owns is proved as before.

## Comparing desired and real state

A comparison sets a context's desired state beside what its operations
realized, object by object. It is a read: it registers no operation, writes no
record, binds no Secret, reserves nothing and runs no effect. It runs under one
shared-lock read of the context, so it refuses `lifecycle.lease` while an apply
or destroy holds the context, refuses a retired operation index and unreadable
or contradictory records as every lifecycle read does, and refuses with the
compiler's own diagnostics when the selected input revision does not compile.
Nothing else refuses it except as
[reading the real side live](#reading-the-real-side-live) adds: what it cannot
read is reported as unknown.

It compares three sides:

| Side | Source |
| --- | --- |
| desired | The effective catalog of the context's selected input revision, compiled under the same lock, which is exactly what `apply` reads. |
| applied | The effective catalog of the input revision the owning apply froze, compiled by this build, and used only while that revision's bytes still match the input digest the apply recorded. |
| real | What a read-only observation of each realized block proves, [read live](#reading-the-real-side-live) and projected by its capability. It is not read from records; without an observation the applied side stands in for it. |

The owning apply is the current operation when it is an apply, the apply a
current removal takes back, and none when the context holds no operation or
its removal names no apply. The comparison reads, in order: the operation
index, the current operation, its frozen plan and block records, what those
records prove about each object (the realization `status` reports), the owning
apply's records, the revision it froze, both compilations, a fresh plan of the
desired side with `reconciliation.Compare` against the owning apply's plan,
and, when another build registered the owning apply, the blocks this build
plans differently from that frozen revision.

Each object reads present, absent or unknown from records alone, contacting no
host. A block's state, not the absence of a record, decides absence:

| Condition | Real side | Reason |
| --- | --- | --- |
| No owning apply | absent | `no operation of this context realized it` |
| Its blocks are done | present: the applied object | — |
| Its blocks are pending under an apply | absent | `apply <op> has not realized it` |
| Every block of it the current destroy names is taken back | absent | `destroy <op> took it back` |
| Some blocks of it the current destroy names are taken back and others not yet | unknown | `destroy <op> has removed <blocks> and not yet <blocks>` |
| Its blocks read failed or unknown | unknown | `its blocks read <state>: <blocks>`, with the remedy of reviewing `bootwright status` |
| No block realizes it, under a completed destroy | absent | `destroy <op> took it back` |
| No block realizes it otherwise | present while the applied catalog declares it, else absent | `apply <op> did not declare it` |
| The applied revision is collected, its bytes moved, or it no longer compiles, where the applied object would be shown | unknown | `the input revision <rev> that apply <op> froze cannot be read: <message>` |
| Its blocks are done but the applied catalog does not declare it | unknown | `the input revision <rev> that apply <op> froze does not declare it`, with the remedy of reviewing `bootwright status` |

An object both undeclared and absent is not listed. Objects are ordered by API
kind order, then name. The input reads `none` with no owning apply or after a
completed destroy, `unchanged` while the context holds exactly the revision and
bytes the owning apply froze, and `changed` otherwise, a bytes-only edit
included. The plan reads `none` with no owning apply, `same` or `differs` by
`reconciliation.Compare`, with the count of changed, added and removed blocks,
and `unplanned`, with the planner's message, when this executable no longer
plans the desired side; each object carries its blocks' change. A collected
revision reads as unknown under the [retention rule of
contexts](contexts.md#storage-locking-and-publication), never as a refusal.

Both sides are compared in canonical form: a field whose value is absent is
dropped, and a list the schema names by key is ordered by that key, so the
authored order of a named list is never shown as a difference. A `Secret`
object is shown as its declaration, never its bytes.

*Drift* is an owned object whose observed real side differs from the applied
side its records state. A comparison reports it and nothing repairs it; inside
an attempt, each capability's own refusal or readiness rule decides what a
drifted target means for its block.

### Reading the real side live

A comparison asked to observe (`bootwright diff --observe`) first reads
everything above, then reads the real side live. Nothing else about it
changes: it registers no operation, writes no record, binds, reserves or
releases nothing and runs no lifecycle effect; the one store-internal effect
it may make is the [cache-only rebuild](controller.md#the-controller-runtime)
of this build's runtime.

**Observed set.** It observes what a removal of the owning apply would take
back: a completed apply's whole frozen plan, so a lost block record drops no
block, and an incomplete apply's blocks its records show started. Under an
apply that is the whole set; under a removal only those the current removal
names and has not recorded done. A block this executable plans differently from the build that
froze it is not observed: it reads unknown with
`this executable plans block <id> differently from the build that froze it, so its observation is not read`
and the remedy `install <build>, which froze it, to observe it`. An object with
no observed block keeps the reading its records give. With nothing to observe
it contacts no host and reopens nothing.

**Boundary.** Each block is read through its capability's `Observe` alone,
never `ObserveRemoval`, `Apply`, `Destroy`, `Quiescent` or `Keep`, under an
execution that names no operation, attempt or resolution, so `RunFor` marks its
run read-only and unrecorded and the runner runs only the observation and
takes over no material (see [Attempts and unknown
outcomes](#attempts-and-unknown-outcomes)). The execution carries the owning
apply as `ApplyOperation`, its dependencies' apply records as `ApplyProved`
and the tool locator of that apply's controller stage, and no log, adapter
output, progress or continuation. A controller-stage block receives a stage
whose every mutator refuses `lifecycle.state`
`an observation may not authorize a host effect`. No run directory or run
output is opened, and material an observation produces is cleared unread. An
observation that hands over material, today the Ceph cluster and client blocks,
is refused by the runner and reads unknown with that refusal as its reason;
the agent install block's observation declares its administrator-access
output only inside an operation, and the agent media block's declares none.

**Material.** The Secret material an observation reads is the binding the
owning apply froze, reopened read-only before the lock is taken, binding
nothing, and cleared when the comparison returns. A binding that cannot be
reopened refuses `lifecycle.state`
`the Secret binding apply <op> froze cannot be reopened, so diff cannot observe its effects: <message>`,
with the remedy of restoring the context's keyring from a
[complete backup](contexts.md#format-and-restore-boundary) or running
`bootwright diff` without `--observe`.

**Host proof and re-proof.** It then holds the context's shared lock as a
bounded run does. It re-reads the operation records and refuses
`lifecycle.state` `the context changed while diff read it`, with the remedy of
repeating the command, when they, the selected input revision or its bytes
moved since the first read. It proves this context is bound to this host as a
continuation does, refusing `controller.identity` or `controller.state` with
the remedy `run bootwright diff --context <name> without --observe` for an
unbound context, and locates this build's controller runtime, refusing
`controller.state` with `run bootwright setup` when it is not prepared. It observes at most
as many blocks at once as an operation runs, each inside its own execution
guard, reports one check row, `observation`, and holds the lock until every
observation returned, so an apply or destroy started meanwhile refuses
`lifecycle.lease`. Canceling it stops further observations, and a block not
yet observed reads unknown with `the observation was canceled`; a read-only
Ceph remote worker an observation already started may run on until its own
deadline.

**Readings.** One block reads:

| Observation | Reading |
| --- | --- |
| an error, a runner refusal or a cancellation | unknown |
| `unknown`, or an effect outside the four | unknown |
| `no-effect` | absent |
| `partial` | partial, with the note `<block>: part of this effect is realized as its frozen request states` |
| `completed` | present |

An unknown reading's reason is the capability's explanation of what it read
when it gives one, with its remedy; otherwise the error's first message, or
the general reason of an observation that proved nothing, with the remedy
`restore what the reason names, then repeat bootwright diff --context <name> --observe`.
The header counts the blocks read (`observed`), the blocks left unknown, and
the present or partial blocks no projection rendered (`not projected`).

**Fold.** An object folds its own observed blocks: any unknown reads unknown;
otherwise all absent read absent; otherwise any partial, or present beside
absent, reads partial; otherwise present. An object whose only observed
readings are other blocks' projections onto it folds those blocks. Unknown is
never absent: an observation that proved nothing never reads as an object
taken back. An absent object reads `its observation proves none of it exists`.

**Projection.** A capability that implements `Projector` turns the evidence
of a present or partial reading, with the catalog of the revision the apply
froze, into spec fragments of API objects, its own or another's. It is not
asked while that revision cannot be read; such objects read unknown with the
applied reason. A fragment is admitted only when it names a known kind other
than `Secret` and an object, its value is a spec mapping, every field it
claims is a path its `ProjectedPaths` and the [table
below](#observable-fields-by-implementation) list, and it carries at most 8
notes of at most 200 bytes without control characters; otherwise its block
reads unknown, naming why. Before anything is admitted, any fragment, note or
name holding a part of 8 bytes or more of the material lent to the run refuses
the whole comparison with `lifecycle.state`
`a projection of block <id> carries Secret material, so diff shows nothing`.
Fragments merge by field and named lists align by their key; a field two
fragments observe differently is dropped from both sides and named in the note
`<path>: <block> observes <value>, <block> observes <value>`. The declared
object is then reduced to the fields the merged fragments carry: a named item
keeps the fields its observed siblings carry, an item with no observed
counterpart keeps only its key, so it shows as declared and not realized, and
every dropped declared path, shallowest first, is listed as not compared.
While a block whose implementation's `ProjectedPaths` name the object's kind
reads unknown, an item with no observed counterpart is instead dropped and
listed as not compared, because that block may hold it. A
present or partial object no fragment renders keeps the applied side, with
the note `observed <state>; <implementation> renders no field, so the fields shown are what apply <op> applied`.

An object an abandoned context of the same name left on a host reads as this
context's, because no native identity yet distinguishes the two, the known
exception [mutation safety](#mutation-safety) names.

### Observable fields by implementation

Each row names the API fields one implementation's projection observes; a
projection claiming any other field is refused as above.

| Implementation | Object | Observed fields | Notes |
| --- | --- | --- | --- |
| `machine-libvirt-v1` | `Machine` | `spec.substrate.providerRef` | domain state; emulated BMC power |
| `machine-libvirt-v1` | `InfraProvider` | `spec.libvirt.machineProfiles[].name`, `[].diskGiB`, `[].dataDisks[].name`, `[].dataDisks[].sizeGiB`, `[].dataDisks[].media` | missing disks |
| `cluster-install-agent-v1` | `OpenShiftCluster` | `spec.nodes[].name`, `[].labels`, `[].taints` | declared labels and taints a node lacks |

## Bootstrap completion and GitOps readiness

State reconciliation owns the complete-environment GitOps-readiness predicate.
An apply is ready for handoff exactly when:

- the context's latest operation is an apply, that apply is `done`, and every
  block in its immutable plan is `done`;
- every completion, readiness, access, and ownership requirement frozen in
  that plan has positive durable evidence;
- no effect attempt remains unresolved `unknown`;
- no latched required-log fault (`logFault`) still blocks work; and
- the context's selected input is the revision, with the bytes, that apply
  froze, so ready never describes an input the apply did not realize.

Resolved earlier failures, retries, and `no-effect` attempts remain in the
audit history but do not independently make a completed apply unready. The
predicate is derived from authoritative operation state and evidence; it does
not perform another platform effect, transfer ownership, or authorize GitOps
to mutate Bootwright-owned resources, which stay as the
[project's handoff rule](project.md#lifecycle-and-handoff) states and the
[add-on handoff evidence](add-ons.md#gitops-handoff-evidence) lists. Add-on
drivers and other capabilities contribute typed readiness evidence, but cannot
declare the complete environment ready themselves.

A [GitOps bootstrap object](add-ons.md#the-gitops-bootstrap-object) is an
owned object of the apply, created after the blocks the
[last-package rule](add-ons.md#built-in-packages-and-selection) names. It hands that cluster to GitOps without waiting for the predicate
over the whole environment, as the
[project's handoff paragraph](project.md#lifecycle-and-handoff) allows
(pending the owner's Q60 under [D173](milestones/backlog.md#d173)), and
transfers no ownership.

Not yet met: no apply creates a bootstrap object; tracked as
[B765](milestones/m4.md#b765).

`status` reports the predicate as its handoff section: ready, or each reason it
is not, in this order: no apply recorded or a destroy as the latest operation,
the apply not `done`, each block not `done` in plan order, each `unknown` block,
a latched log fault, and a selected input that is not the revision the apply
froze, which names that revision and `bootwright diff --context <name>`, whose
comparison names what changed. A block recorded `done` discharges the completion,
readiness, access and ownership requirements frozen for it, because the engine
records `done` only after its capability validated positive evidence and
captured the material the block produces, so the second condition holds
whenever the first does. A latched log fault clears only through
[restoration](#converging-an-effect), which a settled `apply` over
unchanged input performs before it reports; `status` never clears it.

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
  identity. A remote report proves only the reporting principal's own resources
  ([remote principals](security.md#remote-principals)).
- Failed, forbidden, unsupported, empty, malformed, or contradictory probes
  are unknown, never absence.
- Foreign, unowned, replacement, and unknown targets refuse without bypass.
- Every refusal identifies the target, evidence, safety reason, and safe next
  action. It never invents a force command.
- Human plan, confirmation, progress, status, and refusal views derive from the
  same immutable plan and registry.

A name still proves ownership in one known case: a libvirt domain, an emulated
BMC unit, a managed-service unit and an artifact-server path derive their
identity from the context and Machine names, so an object an abandoned context
of the same name left reads as this context's own.
[B568](milestones/backlog.md#b568) gives each context a persistent incarnation
that tells the two apart; until it lands, the objects an orphan-acknowledged
deletion abandons are removed by hand before the same name is initialized
again.

Not yet met: name-derived identity proves ownership in that case; tracked as
[B568](milestones/backlog.md#b568).

Lost-reply adoption is the one case where a record and a shape together prove
an object: a Ceph placement rule, profile, pool, image, filesystem, subvolume
or export whose create lost its reply is adopted only beside the attempt record
written before the create and only at exactly the frozen shape
([storage resources](ceph.md#resources-readiness-and-access)). That
proof holds only because Bootwright is the one writer of such objects under the
cluster's exclusive reservation; a writer outside Bootwright on a managed
cluster is outside the guarantee, and adoption never authorizes a removal.

### Quiescence before removal

The engine supports the online ordering below. Candidate Ceph capabilities
refuse removal while any host survives it
([B491](milestones/m2.md#b491)); their
[target contract](ceph.md) owns the consumer-quiescence and cleanup proofs.

A storage effect whose every host Machine is a Bootwright-owned virtual
Machine destroyed by the same removal is retired with them, provided no
consumer of its produced output, or of an effect removed before it, survives
the removal: each consumer's cluster must leave with owned virtual node
Machines the same removal deletes, not only erases, or the removal refuses
before registration, before any quiescence probe and whatever a capability
would answer, naming the consumer and what it consumes. The apply's `plan`
warns of each such consumer with `lifecycle.surviving-consumer`, from the same
decision taken over the apply's inverse. It is not an
online effect, its Machines are `Stop first`, its quiescence derives from
their stop proof, and its inverse and removal observation record retirement
without contacting the hosts. A retired block's removal observation is answered
from its frozen request with no host contact, and the controller-local destroy
runs of the image and host blocks are not host-contacting calls.

Bootwright removes and replaces only what is out of use. Machine capabilities
prove their targets stopped from native state; a silent or unreadable target
is unproved and refuses. Most removals prove every affected Machine stopped
before registration, report `lifecycle.live` with the stop commands (or the
remedy a capability names when no stop settles its block), and make no effect
when any target is live. Each Machine inverse revalidates its target
immediately before removing it.

An `OnlineRemoval` capability requires its effects removed while their Machines
remain reachable. Only Machine blocks whose inverse dependency closure contains
such an effect defer the initial stop proof. The plan lists them under
`Stop after online cleanup`; unrelated Machines retain `Stop first`. The
engine durably completes every required online effect before probing all
remaining deferred Machines. A live or unproved target pauses the registered
removal before any deferred Machine inverse. After the operator stops those
Machines, repeating destroy continues the same removal. It never repeats an
online effect already durably completed. Unknown or failed online cleanup
cannot admit Machine or substrate deletion.

A `PreStopRemoval` capability opts a block into the pre-stop phase of a
removal. It does so only for an inverse that touches its own Machine's guest
and its own published content and that no other block's inverse or retirement
reads, such as an installation's unregistration from its subscription service.
Machine blocks whose inverse dependency closure contains such a block, and
that no online effect already defers, skip the initial stop proof and are
listed under `Stop after online cleanup`. Once the removal registers and every
unproved effect is resolved, the engine runs every pre-stop inverse first,
ahead of dependency order, and starts nothing else until each has durably
completed. It then proves those Machines stopped before any other inverse: a
live or unproved target pauses the registered removal with the stop commands,
and repeating destroy after the operator stops them continues it without
repeating a completed pre-stop inverse. A failed pre-stop inverse fails the
removal before any other inverse runs. Pre-stop deferral never defers a host
for retirement: a storage effect retired with its hosts stays retired and its
retirement runs after the stop proof. A destroy may name, with
`--unregistered`, Machines whose guest-side pre-stop removal the operator
performed outside Bootwright; for that invocation only, the pre-stop inverse of
those Machines' blocks removes nothing from their guest
([managed OS](managed-os.md#registration)).

Every execution and quiescence probe carries its immutable owning apply
identity. `ApplyProved` supplies detached frozen blocks and durable completion
evidence only from the transitive dependency closure of that original apply.
Every invocation also receives `ApplyPreparations`: the preparations the
owning apply's own attempts of the same block recorded, in attempt order and
unread, or none when that apply never started the block. Removal reads them
through the active operation store, without reopening the workspace lock. Context renames and a new removal identity do not create a new
Ceph incarnation or substitute a currently observed host key. Missing original
evidence refuses before dependent effects.

Stopping is never a side effect of removal. The operator uses
[Machine power commands](cli/commands.md#command-and-flag-catalog) or their own
means. `data-loss` acknowledges destruction; quiescence independently proves
nothing is using the data. A native daemon list alone proves neither consumer
quiescence nor the removal of OSD data, credentials and SSH authorization.
The [storage contract](ceph.md) owns those additional proofs.

### Physical disk safety

Existing operator-owned bare metal is the explicit external-substrate
exception: Bootwright never claims ownership of or destroys the physical
machine. Before changing it, an implementation must durably hold an exclusive
context claim, prove the exact management controller/System identity, prove
the complete live MAC set matches the immutable `Machine`, select one whole
root disk from the authored hint by a durable disk identity such as its WWN,
never by a kernel device name another boot may give another disk, prove the
machine is off, and consume `data-loss`. The live MAC set matches when the MACs
the system reports for its own network ports, a management controller's host
interface excepted, are exactly those the `Machine` declares, no more and no
fewer; every proof of a physical machine, the installer's included, uses this
one predicate. The claim serializes use but cannot replace any live proof.
[Physical machine realization](substrates.md#physical-machine-realization)
holds the claim and the controller-side proof, and
[physical installation](managed-os.md#physical-installation) repeats that proof
immediately before it boots and again inside the installer itself. A declared
[firmware write](substrates.md#physical-firmware) is such a change: it runs
only after those proofs, inside the installation apply that consumes
`data-loss`, shows as that block's `configure-firmware <machine>` impact, and
is set-only: no removal reverts it.

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

Omitting the opt-in physical erase policy retains the machine and its installed
system. Offline erase follows its separate safety contract below
([B70](milestones/m1.md#b70)).

### Opt-in physical logical overwrite

`Machine.spec.hardware.eraseOnRemoval` is an explicit removal policy. Its only
method is `logicalOverwrite`; omission retains disks. Each target supplies
`deviceName`, `wwn`, `serialNumber` and exact `sizeBytes`. The approved
`hardware.rootDevice.deviceName` must be listed; data disks are individually listed.
The initial execution bound is 16 whole disks, each at most 64 TiB with a
4096-byte aligned logical size. These are implementation bounds, not hardware
sanitize capabilities. All addressable bytes are overwritten with zero and
independently read back after flush through direct I/O; hidden, remapped and
overprovisioned storage is outside this claim.

The erase block depends on the physical claim and publication services; the
installation depends on it. Reverse order unregisters and removes installation
content before erase, and releases the hardware claim only after erase. Removal
consumes `data-loss`. It never chooses disks from authorization or discovery.
An original installation block proved never started is retained without booting
or writing disks; the policy's removal records retention, never an erase receipt.
Missing original records refuse rather than imply that no installation started.
The original system UUID, exact complete physical MAC set, WWN, serial and size
must agree immediately before media insertion and in the RAM worker. Missing
identity is unknown. All targets are opened exclusively before the first write;
held descriptors, Linux disk sequence and live sysfs identity are rechecked
throughout write and readback. A path replacement is not a new target.
Linux SCSI `naa.<hex>` and a declared canonical `0x<hex>` WWN compare by the
same complete 8- or 16-byte NAA identifier; other native forms remain exact.
This follows the
[kernel NAA formatter](https://github.com/torvalds/linux/blob/v6.12/drivers/scsi/scsi_lib.c)
and [sysfs attributes](https://github.com/torvalds/linux/blob/v6.12/drivers/scsi/scsi_sysfs.c).
Serial identity comes from the native serial attribute or a complete bounded
SCSI VPD page 0x80, never from a guessed path or stale udev text.

The private image contains only network configuration and a nonreturning
Anaconda `%pre` which launches a dedicated RAM service. The service masks and
stops installer frontends and refuses any remaining installer process before
starting the writer. It requires RAM-backed runtime/root filesystems; no
ordinary installation directives are present. The image selects `rd.live.ram=1`,
`rd.live.overlay.overlayfs=1` and `inst.zram=0`; an overlay's upper and work
directories must both resolve to tmpfs. The
[dracut 057 live-root implementation](https://github.com/dracutdevs/dracut/blob/057/modules.d/90dmsquash-live/dmsquash-live-root.sh)
defines the RAM copy and overlay selection, and
[RHEL 9's boot reference](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html-single/interactively_installing_rhel_from_installation_media/index)
documents the stage2 RAM copy. Installer isolation follows the
[RHEL 9 Anaconda startup order](https://github.com/rhinstaller/anaconda/blob/rhel-9/anaconda.py)
and [synchronous pre-script execution](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/kickstart.py),
also present in the
[RHEL 10 implementation](https://github.com/rhinstaller/anaconda/blob/rhel-10/pyanaconda/kickstart.py).
The boot media's frozen size and SHA-256 remain mandatory. Source review does
not qualify an image build or physical machine.

Original-attempt custody and a fresh erase-only SSH host key remain private on
the execution host, beneath the immutable original apply operation identity so
reuse of a context name cannot adopt an old receipt. Initial private custody is
atomically committed before boot; its first erase attempt remains frozen across
retries. The installed OS's existing host key is never the erase
receipt identity. The RAM service accepts only receipt reads and receipt-hash
acknowledgement through pinned SSH. A complete receipt binds request, original
attempt, system UUID, MAC set and exact written/verified coverage of every disk.
It is durably recorded on the execution host and in the lifecycle attempt
before shutdown acknowledgement. Completion additionally proves the same
machine powered off, media ejected and private image withdrawn. The durable
receipt remains; boot-private material is removed. A durable finalization
checkpoint follows powered-off/eject proof and publication withdrawal. Cleanup
removes the private key and boot files and fsyncs their directory before the
completion record is published. Interrupted cleanup resumes without recreating
keys, booting or acknowledging another worker.

The worker has four hours for full overwrite and readback. Erase-owned native
controller phases cap their entire discovery/retry/poll sequence; together with
the 900-second build and 900-second RAM startup allowance they fit within the
two-hour controller margin, keeping the invocation within six hours. Exhaustion
leaves a partial or unknown result and retains the claim.

Cancellation never means erase finished. An authenticated partial status resumes
only while the independent RAM worker service is active. A stopped or failed
worker refuses promptly; the operator powers off the Machine before continuing
the same destroy, whose fresh boot repeats the entire frozen overwrite and
readback after rechecking every identity. A completed durable receipt is replayed.
Without a receipt, another boot must
reprove all identities and repeat the whole overwrite/readback. A powered-off
machine, missing OS or zero prefix never proves completion. Identity drift,
short writes, failed readback and unavailable probes refuse without selecting
another disk. B70 remains subject to failure-injection and real-hardware
qualification; firmware identity, hotplug and cache semantics are not inferred
from passing unit tests.

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
host. A managed network keeps off the host's own links and routes under the
probe of [provider host realization](substrates.md#provider-host-realization).

A fresh removal proves, before its plan is presented and again under the
exclusive lock before it resolves an effect, probes quiescence or registers,
that controller setup completed on this host, that this host is the host the
controller state records, and that a binding the controller state records for
the context names this host. A fresh removal over a context the controller
state records no binding for proceeds once that host is proved: no apply can
bind a context that holds an incomplete operation, the removal is that
context's exit, and the apply after it binds the context again.

An apply, a destroy and a bounded run must preserve the controller OS, power,
Bootwright runtime, Workspace state, keyring and evidence needed to continue or
remove owned resources. A bounded
[power request](substrates.md#identity-and-power-operations) therefore refuses
the environment's controller Machine and any target whose reported system
identity is the controller host's own, and it acts on a physical Machine only
under a current pin or a fresh proof of its complete MAC set. An owned service
may be removed only through its frozen inverse when remaining operations no
longer depend on it. Selecting a Machine as the controller never grants ownership of
the host, its runtime or another context's services.

Controller setup, immutable execution dependencies and locally hosted services
have separate owners. Plans must model their actual readiness relationships
without converting every Machine address or service reference into a readiness
edge. Missing, changed or unprovable controller bindings refuse the affected
operation, except a missing binding beneath a fresh removal, which proceeds on
a proved host as above; continuation never silently substitutes the invoking host or moves
context state. Migration and complete-store restore require their own defined
recovery journey.

## Confirmation and authorization

Ordinary confirmation and any non-interactive suppression belong to the public
CLI contract that exposes an operation; neither is defined here. Suppression
grants no permission. Irreversible authorization acknowledges already-planned
loss but does not select a target or relax validation, identity, ownership,
power, probe, or digest checks.

`data-loss` is the only authorization token a block consumes; `rebind=<secret>`
is the [access-credential rebind](#access-credential-rebind), which no block
consumes and which refuses when it does not apply. `all`, unknown, duplicate,
empty, and inapplicable values are errors. A plan requires exactly the tokens its
blocks [consume](#plan-and-execution). A token the frozen plan does not require
is inapplicable and refuses with `lifecycle.authorization` before registration,
so a habitual authorization cannot pre-authorize a future destructive plan; a
required token that is not supplied refuses the same way, naming the blocks
that consume it. A `data-loss` token may carry the digest of the steps that
consume it, `data-loss=<digest>`: the first 16 lowercase hexadecimal digits of
a SHA-256 over those steps' identity, kind, object, implementation, content
digest and request digest in frozen order, which every presentation of the
plan prints beside the token. A digest the frozen plan does not give refuses
`lifecycle.authorization` before registration, naming the steps the plan holds
now and `bootwright plan` to review them. An invocation no reviewed prompt
precedes, under `--yes` or with input that cannot answer one, must carry it, and
a bare token there refuses the same way, so an unattended authorization is
always bound to the steps someone reviewed (D183, Q117). The
[finalization](#lifecycle-unit) of an operation whose
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
