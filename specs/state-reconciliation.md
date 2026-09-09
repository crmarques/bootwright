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
cannot narrow it to kinds, clusters, machines, stages, or ranges.

Supported operation modes are:

- a fresh full-context `apply`;
- exact continuation of an interrupted or failed `apply` or `destroy`; and
- a full-context `destroy` of everything recorded as owned by the completed
  apply.

There is no reconciliation, partial apply/destroy, adoption, reclaim, or force
path. A completed apply must be destroyed before another apply can start.

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
This record establishes only local disposal/update restrictions, not native
execution, readiness, ownership release or permission to run lifecycle work.
Future lifecycle publication must participate in the same context lease and
update its evidence before any remote mutation. Missing or unknown evidence
fails closed. A live lease refuses every context mutation.

The guard allows update only without pending, failed or unknown operations;
recreation/final deletion requires `none` operation and `none` ownership.
Protected contexts remain named and selectable for status, exact continuation
and destroy. Deletion never bypasses unknown or protected evidence. There is
no recovery-only archival or abandonment flag.

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

The production root is exactly `/var/lib/bootwright`, owned by root with mode
`0700`. Its directories and files are root:root with modes `0700` and `0600`.
No XDG, home or other ambient value selects runtime storage. Workspace uses
verified no-follow traversal and refuses unsafe existing objects without
ownership or permission repair. The [CLI privilege boundary](cli.md#local-privilege-and-user-identity)
provides root execution only for valid available commands that require it.

Context-free read-only commands do not access the root. Context-backed reads
acquire the existing store's shared lock and never create, repair or migrate
state. Workspace defines the narrow explicit retry of pending creation/deletion.

State reconciliation owns `<operation-id>`, `<block-id>`, and effect- and
resolution-attempt numbers. The identity contract is:

| Identity | Grammar and scope |
| --- | --- |
| `<context-id>` | Stable and unique within `<state-root>` for one lifecycle unit. |
| `<operation-id>` | Immutable and unique within one context; allocated before its operation record. |
| `<block-id>` | Immutable and unique within one operation's frozen plan. |
| `<attempt-number>` | Monotonic within one block, from `1` through `999999`, never reused, and rendered as six decimal digits. |
| `<resolution-number>` | Monotonic within one effect attempt, from `1` through `999999`, never reused, and rendered as six decimal digits. |

Attempt- or resolution-number exhaustion refuses before observation or effects
and never wraps or reuses an earlier path.

Context, operation, and block IDs are canonical lowercase ASCII segments
matching `[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?`. They contain no separator,
dot segment, whitespace, encoding escape, or user-facing description. An ID is
validated before lookup or path construction and never comes from an Ansible
role, play, task, host alias, or vendor response.

Workspace allocates the context ID independently at creation, before input
or operations exist, as `ctx-` followed by 32 lowercase hexadecimal characters
from 128 OS cryptographically secure random bits. The registry maps each name
to that ID. First input import binds its canonical original Environment
directory; active contexts cannot share that source, and update preserves it.
Name reuse after permanent deletion allocates a fresh ID. State reconciliation
allocates operation IDs with the same entropy and the `op-` prefix.

Reserve candidates exclusively, checking existing identities; a collision
retries with fresh entropy at most 16 times. Random failure has no clock,
process-ID, hash or weaker fallback. Pending operations and initializations
remain attributable to their original identity. Corruption or contradictory
mapping refuses rather than allocating a replacement. Context data resides
beneath `contexts/<name>`; runtime records reside beneath its `state/`.

## State machine

| Durable state | Allowed lifecycle transition |
| --- | --- |
| no operation, or completed destroy | start a fresh apply |
| apply running or failed | continue that exact apply |
| apply unknown | resolve the exact unknown block; start no effect or retry |
| apply done | start a fresh destroy |
| destroy running or failed | continue that exact destroy |
| destroy unknown | resolve the exact unknown block; start no effect or retry |

Changed desired state never turns continuation into reconciliation. A
continuation verifies the operation kind, context identity, frozen-input
digest, plan digest, every selected implementation and execution-dependency
digest, and required ownership evidence before doing work.

## Plan and execution

[Secrets](secrets.md#immutable-binding-and-contexts) owns confidential immutable
binding; the lifecycle consumer interface is introduced with M1d.

Planning is pure and read-only. It performs no downloads, remote mutations,
cache or registry writes, cleanup, lock takeover, or secret materialization.
A fresh mutation validates the complete graph, freezes input and non-secret
external content, binds each consumed secret to confidential immutable material
or an immutable external version, and durably publishes the immutable plan and
pending registry before its first platform side effect. Lifecycle commands
remain unavailable until that secret-continuity boundary exists.

A plan is a dependency DAG of stable blocks. Each block has an ID, description,
dependencies, impacts, and execution kind. Operations are `running`, `failed`,
`unknown`, or `done`; blocks are `pending`, `running`, `failed`, `unknown`, or
`done`.

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
| Positive no effect | `no-effect` | `failed` | `failed`; a new attempt requires the capability's safe retry contract |
| Failed, forbidden, empty, malformed, contradictory, or incomplete observation | `unknown` | `unknown` | `unknown` |

Resolution-log failure requests cancellation, preserves its identity without
reuse, and sets or preserves the durable log fault. It permits a transition
only when independent positive completion or no-effect evidence is durable;
otherwise the unknown states remain unchanged. The log fault blocks later work
even after an evidence-backed transition.

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

Destroy is planned from the completed apply snapshot and ownership evidence,
not newly edited input. It removes dependents before dependencies and retains
evidence until positive removal or positive absence is durable.

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
  authorization, power/readiness, dependency integrity, and required positive
  absence.
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

Existing operator-owned bare metal is the explicit external-substrate
exception: Bootwright never claims ownership of or destroys the physical
machine. Before changing it, an implementation must durably hold an exclusive
context claim, prove the exact management controller/System identity, prove
the complete live MAC set matches the immutable `Machine`, select one whole
root disk from the authored hint, prove the machine is off, and consume
`data-loss`. The claim serializes use but cannot replace any live proof. The
agent installer's remaining disk-safety limitation is recorded in
`.agents/knowledge/openshift-agent-disk-safety.md`. Physical destroy and
offline disk erase remain unsupported until a separate spec defines and tests
a safe path.

## Confirmation and authorization

Ordinary confirmation and any non-interactive suppression belong to the public
CLI contract that exposes an operation; neither is defined here. Suppression
grants no permission. Irreversible authorization acknowledges already-planned
loss but does not select a target or relax validation, identity, ownership,
power, probe, or digest checks.

`data-loss` is the only authorization token. `all`, unknown, duplicate, empty,
and inapplicable values are errors. An enabled `CustomPlaybook` refuses before
planning and has no authorization bypass.

Before operational exposure, every supported substrate/component/version must
pass the shared port suite, safety tests, and real-system qualification with
immutable plans, durable continuation, leases, exact ownership, and private
attempt logs. [The CLI contract](cli.md) must cover the complete journey,
including confirmation/authorization, results, diagnostics, cancellation, and
recovery.
