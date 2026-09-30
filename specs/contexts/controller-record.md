# Controller record

Workspace owns the durable shared-host state on this page: the Controller
descriptor, the Controller record with its setup receipt, bindings and
reservations, and the bundle and client-area namespaces with their bounds.
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
`failed`/`canceled` require no unresolved intent or unknown effect. The plan
digest is SHA-256 of the domain-separated host digest, catalog, resolution, egress,
sources and immutable action requests; progress and receipt ID are excluded.
Receipt IDs are `setup-` plus 128 bits of a domain-separated SHA-256 over the
plan digest and previous receipt ID. They are private coordination evidence.

The ordered actions prepare the execution bundle and run the Ansible native
dependency transaction (`container-runtime`). The catalog digest binds the base
runtime and automation assets plus the exact resolved tool definitions and
native artifacts. Each full definition also has a resolution digest binding
version intent, platform, Python/wheel metadata and the native transaction's
exact before/after inventory and actions. Retained definitions are append-only
in publication order and identified by resolution digest. The receipt's
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

A lifecycle operation may also extend the retained dependency evidence it
acquires under. Before acquisition it publishes the exact source identities it
will fetch, and the resolved native definition a selected client closure needs,
into the same atomic record. Sources remain immutable: reusing an ID with
different bytes or origin refuses, and retained resolutions stay append-only
and identified by resolution digest, exactly as an explicit setup publishes
them.

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
resolves it through Controller postconditions. Setup effects retain their
receipt; a new context cannot bypass that pending host work. Reads never bootstrap, upgrade, repair or write these records.
