# State and Reconciliation Contract

This file owns mutation and durable state. Operational lifecycle commands
require the complete CLI, persistence, safety, and qualification boundaries;
[cli.md](cli.md) owns invocation and presentation. A defined but unavailable
command returns `cli.not-implemented` without context resolution, planning,
registration, receipt, or effects.

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
- a `destroy` of everything the apply recorded as owned, which for a paused
  apply is exactly the blocks it completed.

There is no reconciliation, partial planning, adoption, reclaim, or force path.
A completed apply must be destroyed before an apply of *changed* desired state
can start, and an edited input refuses by naming that removal.

A verb whose work durable state already proves performs none of it and
succeeds: an `apply` repeated over the unchanged input its completed apply
froze, and a `destroy` of a context that owns nothing, each report the
completed operation and `done` without registering an operation, opening a
transaction, binding a Secret, claiming a reservation or reaching a host. Such
an invocation requires no authorization and no confirmation, because it has no
consequence to acknowledge, and a token it is given authorizes nothing and
refuses nothing. Repeating a verb is therefore always safe, which is what lets
an operator or a script ask whether anything is left to do.

An implementation may make `apply` operational before public `destroy` only
for one complete selected Environment whose lifecycle obligations are fully
supported and only when all of these staged-availability conditions hold:

- every possible owned effect has an implemented and qualified inverse whose
  immutable implementation and dependency identities are frozen with the
  apply;
- the operation preserves all secret bindings, ownership, completion, and
  removal evidence needed for a later compatible executable to destroy the
  snapshot without rediscovery;
- the available command supports exact apply continuation and unknown-outcome
  resolution;
- once an apply operation is registered, context update, a fresh apply, and
  final context purge refuse, preserving the selectable context and every
  required record until compatible destruction releases its obligations; and
- the operator receives the two-phase
  `lifecycle.destroy-unavailable` warning defined by
  [the CLI contract](cli.md#staged-apply-without-destroy), including in every
  trustworthy post-registration result.

Public `destroy` remains unavailable in that staged build. Plans remain
complete and immutable; later artifact generation cannot append blocks or
expand the operation. Expansion requires destroy followed by fresh apply.

## Context mutation evidence

Reconciliation owns a closed version-1 mutation record initialized with
`operation: none` and `ownership: none`. Recognized operation states are `none`,
`pending`, `failed`, `unknown` and `applied`; ownership is `none` or `retained`.
A paused operation records `pending` and `retained`, exactly as a running one
does, because it owns every effect it completed.
This record establishes only local disposal/update restrictions, not native
execution, readiness, ownership release or permission to run lifecycle work.
Future lifecycle publication must participate in the same context lease and
update its evidence before any remote mutation. Missing or unknown evidence
fails closed. A live lease refuses every context mutation.

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
local deletion under this guard. The staged apply restriction above adds a
stronger update prohibition when destroy is unavailable.

## Durable identities and private paths

[Contexts](contexts.md) owns the versioned registry, immutable input revisions,
selection transactions and permanent deletion.

Workspace owns selection and verification of `<state-root>` and allocation of
`<context-id>`. `<state-root>` is a canonical absolute Bootwright-owned runtime
state directory outside both the desired-state input root and selected
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
| `<context-id>` | Stable and unique within `<state-root>` for one lifecycle unit. |
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

`index.json` names at most one current operation and the completed operations
retained for audit. `operation.json` binds the operation to its verb, context
identity, input revision and digest, plan digest, selected implementation and
automation identities, executable identity, secret bindings, durable state and
log-fault flag. `plan.json` is the immutable frozen plan: every block with its
description, dependencies, impacts, presentation groups, resolved
implementation identity, content digest and canonical secret-free request.
Block records carry the block state and its next attempt number; attempt and
resolution records carry their request, phase, outcome and bounded evidence. A
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
overwriting durable evidence.

## State machine

| Durable state | Allowed lifecycle transition |
| --- | --- |
| no operation, or completed destroy | start a fresh apply; a `destroy` settles without effect |
| apply running | continue that exact apply |
| apply failed | continue that exact apply, or start a fresh destroy of the blocks it started |
| apply paused | continue that exact apply under any stage selection, or start a fresh destroy of the blocks it completed |
| apply unknown | resolve the exact unknown block; start no effect or retry |
| apply done | start a fresh destroy; an `apply` of the unchanged input settles without effect, and of a changed input refuses |
| destroy running | continue that exact destroy |
| destroy failed | continue that exact destroy, or start a fresh destroy of what it has not removed |
| destroy unknown | resolve the exact unknown block; start no effect or retry |

Changed desired state never turns continuation into reconciliation. A
continuation verifies the operation kind, context identity, frozen-input
digest, plan digest, every selected implementation and execution-dependency
digest, and required ownership evidence before doing work.

## Plan and execution

[Secrets](secrets.md#immutable-binding-and-contexts) owns confidential immutable
binding; this contract is its lifecycle consumer, through the
[lifecycle ports](architecture.md#lifecycle-ports).

Planning is pure and read-only. It performs no downloads, remote mutations,
cache or registry writes, cleanup, lock takeover, or secret materialization.
A fresh mutation validates the complete graph, freezes input and non-secret
external content, binds each consumed secret to confidential immutable material
or an immutable external version, and durably publishes the immutable plan and
pending registry before its first platform side effect. Lifecycle commands
remain unavailable until that secret-continuity boundary exists.

A plan is a dependency DAG of stable blocks. Each block has an ID, description,
stage, dependencies, impacts, and execution kind. Operations are `running`,
`paused`, `failed`, `unknown`, or `done`; blocks are `pending`, `running`,
`failed`, `unknown`, or `done`.

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
frozen with the plan and covered by its digest. The tokens an `apply` or
`destroy` receives must equal the union its plan consumes: a missing token
refuses `lifecycle.authorization` before registration, naming the blocks that
consume it, and a surplus token is inapplicable under the
[authorization rules](#confirmation-and-authorization).

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
selected. Execution runs startable blocks in frozen plan order, re-evaluating
after each one.

An operation is `paused` when execution stops because no block is startable,
no block is failed or unknown, and pending blocks remain. A pause is a
successful, resumable stop, not an interruption: it needs no recovery, it
allocates no new identity, and the next `apply` continues the same operation
under whatever selection it is given. Cancellation is never a pause.

Selection never weakens a safety rule. An unproved effect is resolved before
anything else whatever stages are selected, because resolution is a read-only
observation. A failed block remains the only retry candidate and halts
progress; when its stage is not selected the operation refuses `lifecycle.stage`
before any effect. A selection that admits no startable block also refuses
`lifecycle.stage` before registration, naming a stage that would unblock work.
Objects whose kind no capability in the executable can realize refuse before
registration regardless of the selection, because a frozen plan requires a
resolved implementation for every block.

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

Add-on blocks also freeze the package, catalog snapshot, payload manifest,
driver, and compatibility decision under [add-ons.md](add-ons.md). They obey
the same state machine and never re-resolve on continuation or destroy.
Retain the selected non-secret package bytes in the operation snapshot through
completed destroy, independent of source catalog availability.

The executor allocates the next attempt number and durably records `running`
before a block's first side effect. Each attempt durably records its effect
state as `no-effect`, `completed`, or `unknown`; only positive evidence permits
the first two. A Bootwright-controlled block reaches `done` only after its
effect plus required ownership and completion evidence are durable. It reaches
`failed` only with a typed failure whose evidence and capability contract
define a safe continuation or retry boundary. A direct apply or destroy result
with positive `no-effect` is a typed failure and never success. An idempotent
already-complete apply reports `completed` with completion evidence, and an
already-absent destroy reports `completed` with positive absence evidence.

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

A lost response, dead executor, cancellation, required-log failure, or
contradictory observation moves the attempt effect state, block, and operation
to durable `unknown` unless positive evidence already proves completion or no
effect. Resolving `unknown` is a read-only, capability-owned observation
against the frozen request and exact target identity. Before any resolution
observation—including local process, network, or remote probing—Bootwright must
restore the required operation logging boundary, durably allocate the next
resolution number for the exact unknown effect attempt, and securely create its
separate required resolution-attempt log. The resolution identity and log path
are durable before observation begins. Failure at any of those steps performs
no observation and leaves the effect state, block, and operation unchanged. A
logging-boundary restoration or resolution-log creation failure sets or
preserves the durable log fault; a resolution-number allocation failure leaves
its prior log-fault value unchanged.

Resolution permits only these evidence-backed transitions:

| Durable evidence | Effect state | Block | Operation |
| --- | --- | --- | --- |
| Positive completion | `completed` | `done` | `running` or `done`, as the frozen plan requires |
| Positive no effect | `no-effect` | `failed` | `failed`; a new attempt converges it |
| Positive partial realization this context owns | `partial` | `failed` | `failed`; a new attempt converges it |
| Failed, forbidden, empty, malformed, contradictory, or foreign observation | `unknown` | `unknown` | `unknown` |

A partial realization is the ordinary outcome of an interrupted effect, and
resolving it to `failed` is what lets the context move: an `unknown` block
starts no retry, no dependent block, no removal and no deletion, so a target
that is provably this context's own and provably incomplete must not be left
there. What a capability accepts as partial is its own, under the converge
rule below, and it is never a target it cannot prove is ours: a foreign or
unreadable observation stays `unknown`.

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

A required-log failure is also a durable operation fault. Restoration permits
new logging but never repairs, appends to, or replaces the failed attempt log.
Even when positive evidence resolves the affected block to `done` or `failed`,
no later effect or retry starts until the required private logging boundary is
safely restored. Missing or truncated log detail is never reconstructed,
treated as operation evidence, or used to weaken the resolution rules above.

### Continuation and removal

Continuation skips done blocks and may retry only the first failed block under
its frozen capability contract. Execution starts sequentially; concurrency
requires its own contract and proof. Go owns ordering and lifecycle state and
invokes capability ports under the
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary).

Destroy is planned from the plan its apply froze, not from desired state and
not from what current code would derive from it. Every block a removal carries
keeps the identity, implementation, content digest and canonical request its
apply wrote, so a removal describes exactly the effects that exist rather than
the effects this executable would create today. An operation owns every block
it started: a paused apply owns the blocks it completed, and a failed apply
owns those plus the block that failed, because an effect permitted to begin is
proved absent only by its own inverse. A removal covers exactly that set and
nothing the operation never started.

A frozen block records what creating it did; removing it is the other half of
the same request. Each capability therefore reads its own frozen request and
states what removing that block does: the words it is planned and reported in,
the impacts it lists, and the authorization it consumes. Nothing else of the
block may change, so planning a removal can never alter what is removed, and a
removal acknowledges the consequences of removing rather than the consequences
its apply acknowledged.

Reading a frozen request is what makes a context removable by a later build. A
capability reads the request version it writes and the version before it,
upgrading the older one into the shape its adapter is given; the frozen digest
continues to identify the bytes that were frozen. Any change to what a request
encodes is a new version, including adding or renaming one field, because the
bytes a version froze are proved canonical against the shape that wrote them:
a shape that changed without its version refuses its own frozen bytes. A request older than that,
or one whose implementation this executable no longer provides, refuses before
anything is registered and names the block, the version it holds and the
executable identity its operation recorded, so the remedy is the command to
run rather than the obstacle that stopped it.

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

A fresh removal may supersede an incomplete operation that holds no unproved
effect: a paused apply, a failed apply, or a failed destroy, which is removed
over what it has not yet proved gone. This is the only road out of a repaired
adapter, because a continuation is frozen to the automation its operation
registered under while a fresh operation runs under the current one. An
unknown block admits no removal; only its resolution may follow it.

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

Two limits remain and are stated rather than closed. The interval between the
controller's last proof and the installer's first write is a residual race,
narrowed by the in-installer check but not eliminated; the agent installer,
which admits no equivalent check, is recorded in
`.agents/knowledge/openshift-agent-disk-safety.md`. And physical destroy and
offline disk erase remain unsupported until a separate spec defines and tests
a safe path, so a removal retains the machine and the system installed on it.

### Controller-host protection

The controller is an OS-ready provided Machine and remains outside selected
cluster node membership under
[the API relationship](api/environment.md#controller-machine). That declaration
alone does not prove live host identity or ownership.
[Controller setup](controller.md) defines prerequisite preparation and uses
[Workspace host binding](contexts.md#controller-relationship-and-host-binding).
The following rules constrain later controller-hosted service effects; their
availability is separate from prerequisite setup.

Before enabling local service execution, the owning capabilities must define
verified host binding and stable conflict identities for shared ports, paths,
service instances and other exclusive host resources. Context-local leases
remain necessary but do not coordinate two contexts targeting the same host.
The capability and Reconciliation contracts must establish cross-context
ownership, conflict refusal and recovery evidence before those effects run.

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
required token that is not supplied refuses before registration as well. An
enabled `CustomPlaybook` refuses before planning and has no authorization
bypass.

Before operational exposure, every supported substrate/component/version must
pass the shared port suite, safety tests, and real-system qualification with
immutable plans, durable continuation, leases, exact ownership, and private
attempt logs. [The CLI contract](cli.md) must cover the complete journey,
including confirmation/authorization, results, diagnostics, cancellation, and
recovery.
