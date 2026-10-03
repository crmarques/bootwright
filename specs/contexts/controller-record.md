# Controller record

Workspace owns the durable shared-host state on this page: the Controller
descriptor, the Controller record with its setup receipt, bindings and
reservations, the bundle and client-area namespaces with their bounds, and the
setup runs beside them.
[Controller](../controller.md) owns what setup and the controller stage record
in it, and [Contexts](../contexts.md#controller-relationship-and-host-binding)
owns the relationship between a context and its host.

## Location and file modes

Shared host state belongs under `/var/lib/bootwright/controller/`. The current
receipt and all context bindings share one atomic `state.json` publication;
there is no independently published per-context binding record. This prevents
completion and binding evidence from disagreeing after interruption. Records contain no Secret values, and
opaque private host evidence is never emitted by inspection. Dependency files
that must execute are a narrow root-owned `0700` exception to the regular-file
`0600` rule; metadata and other files remain `0600`. Only catalogued immutable
controller bundles may use this exception.

## Descriptor

Explicit setup may initialize the fixed root and publish a durable
empty registry without creating a context or keyring. That registry commit
must precede any controller subtree. Confirmed setup adds the independently
versioned Controller descriptor to the registry, preserving its active context
records; the member is absent until then, and a controller subtree without it
refuses. The admitted root layout must include that subtree even with zero
contexts; unknown layouts or versions refuse. Ordinary context commands never
infer ownership, repair interrupted setup or remove shared host state. A read
never introduces Controller state, and ordinary context publications preserve
an existing descriptor.

The descriptor fixes version `1`, mode `initializing` or `ready`, and the
Controller directory's device/inode. Its reservation precedes directory
creation; a second publication records the actual directory identity. An
unattributable directory after a crash is preserved and refuses adoption.
Only explicit setup can finish an attributable initialization.

## Record and receipt

The private Controller record version is `2`. Its fields are `version`, `host`,
`receipt`, `bindings`, `retainedSources`, optional `retainedDefinitions`,
`bundles` and optional `reservations`, encoded as compact JSON
in schema order followed by LF. A bundle reservation is `reserved` while its
area is being published, `attributed` once the store owns the directory,
`sealed` once its content is immutable, and `retiring` once its removal is
intended, which is the one mode an area may never be read or published through.
Unknown fields, duplicate keys, noncanonical
records and versions refuse. A record is proved canonical by re-encoding what
was read and comparing it byte for byte, so a field added to any value this
record contains must encode to nothing when it is unset: otherwise every host
that already holds a record refuses the next setup, and the version this
record carries cannot be moved to admit the field without refusing that host
outright. The host contains the confidential
[`linux-installed-v1` tuple](../controller.md#host-identity-and-shared-prerequisites).
The receipt fixes its ID, catalog digest, plan digest, explicit egress, source
closure, full resolved dependency definition, ordered actions and status; it
carries no context, because setup selects none. Sources bind a stable ID to its
original credential-free URL, SHA-256 and exact byte count. Bindings contain the context
name, Machine name and the private host digest, ordered by context name;
sources are ordered by source ID. Reusing a source ID with different bytes or
origin refuses, including after replacing a terminal receipt.

Not yet met: the receipt still encodes, and its plan digest still covers, an always-empty `context` member; tracked as [B94](../milestones/backlog.md#b94).

Reservations record the host resources that locally hosted services claim,
under the
[Infrastructure services conflict contract](../infrastructure-services.md#host-reservations).
Each entry contains the context name, capability kind, service name and a sorted
unique key list, plus a shared marker present only on a shared claim; entries
are ordered by context name then kind then service.
Workspace stores and compares them without interpreting a key's meaning.
Publishing an exclusive key another context holds refuses; a shared key is held
by any number of contexts and a reader asks only whether any context holds it;
a context replaces only its own entries, and its completed removal drops them. The record is absent when empty,
so a store that never hosted a service is unchanged.

Action requests and evidence are canonical compact JSON objects with sorted
keys. Each action records `planned`, `intent` or `observed`; an observed action
requires bounded evidence and an explicit outcome. Successful observations
cannot regress. `pending` and `unknown` retain recovery protection. `complete`
requires every action's verified `changed` or `unchanged` postcondition;
`failed`/`canceled` require no unresolved intent or unknown effect. A receipt
setup [abandons at the bound](../controller.md#supported-host-and-dependency-selection)
is recorded `canceled` under its own ID and plan: its first action,
`execution-bundle`, when it held its intent, is observed `canceled` with the
evidence `{"bundleArea":"absent"}`, because the record holds no area for the
bundle the receipt names, and every later action stays `planned`. The plan
digest is SHA-256 of the domain-separated host digest, catalog, resolution, egress,
sources and immutable action requests; progress and receipt ID are excluded.
Receipt IDs are `setup-` plus 128 bits of a domain-separated SHA-256 over the
plan digest and previous receipt ID. They are private coordination evidence.

The ordered actions prepare the execution bundle and run the Ansible native
dependency transaction (`container-runtime`). The catalog digest binds the base
runtime and automation assets plus the exact resolved tool definitions and
native artifacts. Each full definition also has a resolution digest binding
version intent, platform, Python/wheel metadata and the native transaction's
exact before/after inventory and actions. Retained definitions keep their
publication order and are identified by resolution digest; only a
[retirement](#bundles-and-client-areas) removes one. The receipt's
definition must match one retained entry. Versions resolved from latest become
ordinary immutable retained sources; neither exact recovery nor a later setup
with the same intent refreshes them. A new solve with unchanged selected
releases and no native action reuses the existing verified bundle and source
closure.

An action may also carry a canonical `preparation` object, omitted until its
before-state has been observed. First publication requires an existing durable
intent; the object is immutable for that receipt and excluded from the plan
digest. Native preparation contains `inventorySHA256`, `afterInventorySHA256`,
`planDigest`, `transitionsSHA256` and sorted unique `addedSources`. These bind
the complete before and expected after inventories, exact native plan and its
ordered transitions, with payload IDs retained for acquisition evidence.
It must commit before the installer receives permission
to enter its native transaction. Its absence proves installation was not
authorized; its presence requires exact transaction recovery, even if ordinary
readiness checks pass. Recovery may retry an unchanged exact before-state, or
verify the complete expected after-state without repeating native effects.
An intermediate or contradictory inventory remains unknown.

## Bounds

The state record is bounded to 4 MiB, with at most 128 actions, 512 current
sources, 4096 retained source identities, 4096 bindings and 256 reservations of
at most 64 keys of 256 bytes each. Each request,
preparation or evidence object is at most 64 KiB. A full resolved definition is
at most 512 KiB, with at most 16 retained definitions subject to the aggregate
state bound. Controller records allow nesting depth 16 and 32 fields per object;
other durable records keep their own bounds. There are at most 16 retained bundle
namespaces, named by the 64-character content digest of what they hold. Their
reservations record their mode and physical directory identity. A new receipt
naming a namespace the record does not hold is refused while all 16 are held,
so none is left pending on a bundle that can never be reserved;
[setup](../controller.md#supported-host-and-dependency-selection) makes room
first. A bundle is bounded to 8 GiB total, 1 GiB per file, 32768 entries and
depth 32. Symlinks, hard links, nested
mounts, unsafe modes, unexpected entries and replacement refuse. Completion
verifies and syncs the complete tree before sealing; sealed contents cannot be
rewritten. Each published file's own contents are made durable as it is written,
so an interrupted publication always resumes from bytes it can attribute, while
the directory entries naming them are synced with that tree at completion: a
name lost to a crash leaves the file absent for the replay to publish again,
never present with content the replay cannot attribute.

## Bundles and client areas

Two kinds of namespace share that list and those rules, and differ only in what
grants their write capability. The setup bundle is named by the approved
catalog digest and writable only under a durable setup intent. A **client
area** is named by the digest of one exact target client closure and writable
only inside a lifecycle mutation, which holds the root lock and the context
lease for its whole callback; the reservation itself is that operation's
durable intent, published before the directory exists. Neither identity may be
opened as the other.

A client area is content-addressed, so every context selecting the same clients
proves the same files and a different closure never disturbs them. Attribution
follows the setup bundle exactly: the `reserved` entry precedes directory
creation, a second publication records the actual directory identity, and an
unattributed directory beside a reservation this store published is its own
interrupted attempt, adoptable only while empty. Sealing verifies and syncs the
complete tree, then makes the closure immutable; a sealed area reopens
read-only for every later operation and context. An unsealed attributed area is
completed by an exact replay of the same closure, whose publication verifies
existing bytes rather than overwriting them. Removal never applies: the
closure is shared host state that outlives the context that published it, and
no command deletes or garbage-collects one.

The store upholds that itself rather than trusting the IDs a
[retirement](../controller.md#supported-host-and-dependency-selection) names.
The record keeps no kind per area, so the store retires only an area it can
identify as an execution bundle: one a retained resolution's catalog digest
names, or one already `retiring` because an earlier retirement dropped that
resolution. A client area is named by its closure, which no resolution names.
Naming any other area it holds, a client area included, refuses the whole
retirement before anything changes, as naming the bundle the receipt names
does; naming an area it does not hold removes nothing, not even a resolution
naming it. A retirement needs a
settled receipt, except that a pending one whose bundle holds no area while
all 16 are held, which an earlier build could publish, admits a retirement of
areas, so [setup](../controller.md#supported-host-and-dependency-selection)
can make room and resume it; no pending receipt admits a retirement of
resolutions alone. A `retiring` entry
keeps the directory identity its removal is verified against, and a
`reserved` one records none, so retiring a reserved area first attributes the
empty directory its interrupted publication left, as a resumed publication
adopts it, or drops the reservation when no directory exists. A directory
beside that reservation holding anything refuses the retirement before
anything changes.

A retirement may instead name retained resolutions alone, by resolution
digest, which [setup at its bound](../controller.md#supported-host-and-dependency-selection)
does for the superseded resolutions of a bundle it keeps, and its retirement
after completion does for a superseded resolution whose bundle holds no area,
as a canceled receipt's does once a later receipt replaced it. It removes no
area. Because an execution bundle is known by the resolutions naming it, the
store refuses to drop the last resolution naming a bundle it holds, as it
refuses the one the receipt carries, and either refusal changes nothing; one
naming a bundle it does not hold identifies no area, and a resolution it does
not hold is already gone.

A lifecycle operation may also extend the retained dependency evidence it
acquires under. Before acquisition it publishes the exact source identities it
will fetch, and the resolved native definition a selected client closure needs,
into the same atomic record. Sources remain immutable: reusing an ID with
different bytes or origin refuses, and a retained resolution is identified by
resolution digest and never replaced under it, exactly as an explicit setup
publishes them. That same publication retires the retained resolutions the
[controller stage](../controller.md#the-controller-stage) names, by resolution
digest, as superseded by the one it retains. Which are superseded is the
stage's judgement; the store refuses to retire the receipt's own, the one it
retains, or the last resolution naming an area it holds, retires none beside
no resolution, and removes no area. A resolution it does not hold is already
gone. The [bound](#bounds) on retained definitions is judged after that
retirement, so a record already at the bound accepts a new resolution that
retires one it holds.

## Setup runs

`runs/` beside the record keeps what setup's controller Ansible printed, as
[setup run output](../cli/output.md#setup-run-output) describes. A run is a
directory named `setup-` and six digits, from `setup-000001` upward, holding at
most one file, `run.output`, which is bounded at 8 MiB. Directories are `0700`
and the file `0600`, owned like every other entry. There are at most 8 runs:
the next is numbered one past the newest, the oldest are removed before it is
created, and a setup whose next number would need a seventh digit keeps no
run. A run is opened only while an action of the receipt holds its durable
intent. Its directory and file are created exclusively beneath the held
controller directory handle without following a link, and made durable before
the run is named, so an existing name is never reused and nothing but the
oldest runs' own entries is removed. Neither the record nor its receipt names a
run, and nothing reads one back.

Admission accepts `runs/` only as this store leaves it, a killed run's shape
included: a private directory holding at most 8 run directories, each empty or
holding only its private, singly linked `run.output` within its bound. Anything
else refuses every controller read and mutation, as any other unexpected entry
does, and a controller that never published its first record admits no runs.
A build that predates setup runs refuses `runs/` the same way, and no setup
removes the directory once it exists, so after one setup kept a run no earlier
build reads the host again. Every controller view reports whether the
directory keeps runs, so a lifecycle refusal never names such a build as its
remedy ([execution closure](../state-reconciliation.md#dependency-safety-during-recovery)).

## Publication and recovery

Publication writes and syncs an exclusive private staging file, revalidates
the held directory and previous record identity, then renames and syncs its
parent. A pre-rename failure is uncommitted and removes its staging file; a
staging file a killed publication leaves is removed by the next command that
opens a registry transaction
([storage and publication](../contexts.md#storage-locking-and-publication)).
An uncertain rename or durability closes the transaction capability and
permits no further mutation. The root lock
precedes the selected context lease and remains held through each callback.
Read-only bundle capabilities expire with their shared-lock callback, and all
mutation capabilities expire with their transaction.

Inspection never converts a pending receipt to completion. Only explicit setup retry
resolves it through Controller postconditions, or, for one stranded at the
bound that the running executable cannot resume, the explicit setup that
abandons it. Setup effects retain their
receipt; a new context cannot bypass that pending host work. Reads never bootstrap, upgrade, repair or write these records.
