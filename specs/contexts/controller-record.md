# Controller record

Workspace owns the durable shared-host state on this page: the Controller
descriptor, the Controller record with its host identity, bindings, tool
sources and reservations, the tool areas with their bound and retirement, and the setup
subtree beside them.
[Controller](../controller.md) owns what setup and the controller stage record
in it, and [Contexts](../contexts.md#controller-relationship-and-host-binding)
owns the relationship between a context and its host.

## Location and file modes

Shared host state belongs under `/var/lib/bootwright/controller/`, with each
tool area in `controller/tools/<digest>/`. Host
identity, every context binding and every reservation share one atomic
`state.json` publication; there is no independently published per-context
binding record, so a binding and the host it names cannot disagree after an
interruption. Records contain no Secret values, and opaque private host
evidence is never emitted by inspection. Tool files that must execute are a
narrow root-owned `0700` exception to the regular-file `0600` rule; metadata
and other files remain `0600`. Only a tool area's published executables use
this exception.

## Descriptor

The registry carries the independently versioned Controller descriptor once a
controller record exists; the member is absent until then, and a controller
subtree without it refuses. The first [binding](../contexts.md#controller-relationship-and-host-binding)
on a host whose [setup completed](#the-setup-subtree) creates it: an apply that
binds a context and finds no record publishes the descriptor, attributes the
directory and publishes the record holding this host's identity before it
records the binding. Setup itself writes no controller record. Ordinary
context commands never infer ownership or remove shared host state. A read
never introduces Controller state, and ordinary context publications preserve
an existing descriptor.

The descriptor fixes version `1`, mode `initializing` or `ready`, and the
Controller directory's device/inode. Its reservation precedes directory
creation; a second publication records the actual directory identity, and the
mode becomes `ready` once the first record is durable. An unattributable
directory after a crash is preserved and refuses adoption with
`context.state`, "existing controller directory cannot be adopted"; an
attributable initialization that stopped before `ready` is finished by the
next binding, which publishes the record only when none is there.

## Record

The private Controller record version is `3`. Its fields are `version`,
`host`, `bindings`, `toolSources`, `toolAreas` and optional `reservations`,
encoded as compact JSON in schema order followed by LF. It holds nothing of a
setup: the setup receipt, the pending intent and the runtimes live in the
[setup subtree](#the-setup-subtree). Unknown fields, duplicate keys,
noncanonical records and every other version refuse, among them a version 2
record an earlier build wrote; a store that holds one also holds registry
version 5, which [refuses first](../contexts.md#storage-locking-and-publication)
with the fresh-install remedy. A record is proved canonical by re-encoding
what was read and comparing it byte for byte, so a field added to any value
this record contains must encode to nothing when it is unset. The host
contains the confidential
[`linux-installed-v1` tuple](../controller.md#host-identity-and-shared-prerequisites).

Bindings contain the context name, Machine name and the private host digest,
ordered by context name, and `tools`, present once that context's
[controller stage](../controller.md#the-controller-stage) proved a tool area:
the digest of the area its last successful stage proved, which the record must
hold sealed; a stage that selects no tool clears it.
Tool sources bind a stable ID to its original credential-free URL, SHA-256 and
exact byte count, ordered by ID; reusing an ID with different bytes or origin
refuses. A tool area entry is `reserved` while its area is being published,
`attributed` once the store owns the directory, with its device and inode,
`sealed` once its content is immutable, and `retiring` once its
[retirement](#tool-areas) began; a `reserved` entry records no directory
identity, and a `retiring` one keeps the identity its directory had.

Reservations record the host resources that locally hosted services claim,
under the
[Infrastructure services conflict contract](../infrastructure-services.md#host-reservations).
Each entry contains the context name, capability kind, service name and a sorted
unique key list, plus `objectKind`, the API kind of the object the claim
reserves for, present on a claim whose capability kind does not name it (a
`cluster-media` claim records its cluster family kind), which a
[conflict diagnostic](../infrastructure-services.md#host-reservations) names
the claim's object by. It is added under version 3 without a version move, as
the releasing marker was (B839), so no controller needs a fresh install: a
claim an earlier build published records none, still conflicts and is named
by its capability kind and service, and its next apply records it
(`TestAClaimIsNamedByTheAPIKindItRecords`,
`TestAClaimRecordsItsAPIKindOverOneAnEarlierBuildHeld`); a value that is not
an API kind name, an upper-case letter then at most 62 letters and digits,
refuses the record, and an earlier build, whose decoder refuses a member it
does not declare, refuses a record holding one. The member persists for the
claim's life, so once an apply records a `cluster-media` claim no earlier
build reads that host's controller record, for any context, until that claim
is released; a claim whose capability kind already names its object (a
`proxy` claim, say) holding the member refuses the record
(`TestAVersion3RecordHoldsTheAPIKindAClaimReservesFor`). Each entry also holds
a shared marker present only on a shared claim and a
releasing marker present only on a claim whose removal re-proved its release
before an effect (B839): it stays until the claim is released, by the
completed removal or the holder's deletion, and while it stands a claim another context makes on a
key that release guards refuses
([host reservations](../infrastructure-services.md#host-reservations)); an
earlier build refuses a record holding one as an unknown field. Entries are
ordered by context name then kind then service. A claim a block places on
an SSH host that is not the controller prefixes every key with
`ssh/<address>:<port>/`, the SSH address as declared (an IP in canonical form,
unbracketed, a DNS name without case or a final dot) and port its placement
connects to; the same claim's keys are published again prefixed by
`sshkey/SHA256:<digest>/`, the host key its placement Machine pins, in
unpadded URL-safe base64 ([host reservations](../infrastructure-services.md#host-reservations));
keys with two qualifiers, or one and none, never conflict, and
keys with one qualifier conflict by their class's relation. The host-key form
was added under version 3 (D188) as the address form was. Version 3 holds
keys with or without the qualifier, side by side in one record: the qualifier
was added under version 3 (Q96) without a version move, because every earlier
entry is a valid unqualified claim, so a record an earlier build wrote reads
unchanged and each context publishes its SSH-qualified claims on its next
apply. A record of any other version is not read, and no build converts one.
A controller read and every context transaction refuse with `context.state`,
"controller evidence version N is unsupported: this build reads version 3 only
and converts none", and the same fresh-install remedy a registry version 5
names ([contexts](../contexts.md#storage-locking-and-publication)): with the
build that wrote the record, destroy each environment and delete each context,
then, as root, remove `/var/lib/bootwright` and `/var/lib/bootwright-staging`
and run `bootwright setup`. No command rebuilds the record alone: it is
published by the first binding of an apply on a root that holds none, and a
root holding one never gets a second. The fresh install also drops the host's
contexts with their Secrets and its stored media, which `bootwright context init`
and `bootwright media add` recreate from the input directories and images; the
refusal says so and leaves the record unchanged
(`TestAControllerRecordOfAnotherVersionNamesTheFreshInstall`).
Workspace stores them and, before it publishes a context's entries, applies
the conflict relation each key class has under that contract (equal keys,
a wildcard socket and a socket on its port, overlapping prefixes), which the
controller prerequisites evaluate.
Publishing an exclusive key another context holds refuses; a shared key is held
by any number of contexts and a reader asks only whether any context holds it;
a context replaces only its own entries, and only the release paths that
contract lists drop them. The record is absent when empty, so a store that
never hosted a service is unchanged.

## Bounds

The state record is bounded to 4 MiB, with at most 4096 tool source
identities, 4096 bindings and 256 reservations of at most 128 keys of 256 bytes
each: a claim holds at most 64 keys, and an SSH-placed claim publishes each in
both its address and its host-key form (D188); an earlier build, whose
bound was 64, refuses a record holding more. The 4 MiB state bound is reached
before 256 reservations of 128 maximal keys, and a publication past it
refuses as an encoding-limit state error, writing nothing. Controller records allow nesting depth 16 and 32 fields per object;
other durable records keep their own bounds. An apply whose reservations would
take the record past 256 entries refuses with `controller.conflict` before it
publishes them, naming that bound, the entries it needs and those other
contexts hold, with the remedy to take back a context this host no longer
needs with `bootwright destroy --context <name>` or
`bootwright context delete --name <name> --purge`
(`TestReservationsPastTheRecordBoundRefuseBeforePublication`).

There are at most 16 tool areas, a bound of their own that nothing else
counts toward, each named by the 64-character content digest of the closure
it holds. Their entries record their mode and physical directory identity. A
controller stage whose new tool area finds all 16 held first retires every
area no binding names; one that still finds all 16 held, each named by a binding or still being published, refuses
with `controller.conflict`, naming that bound, with the remedy to delete a
context this host no longer needs with
`bootwright context delete --name <name> --purge`, which retires the tool area
only it names, or to run this context's controller stage on another controller
host (`TestAClientAreaAtTheBoundNamesItsRemedy`,
`TestAClientAreaAtTheBoundRetiresUnnamedAreasFirst`). One whose tool sources
would pass 4096 refuses with `controller.conflict`, saying that no command of
this build frees a retained tool source, and names the two exits: run this
context's controller stage on another controller host, or start this host
afresh by destroying every environment and deleting every context on it, then,
as root, removing `/var/lib/bootwright` and running `bootwright setup`
(`TestToolSourcesPastTheBoundNameTheFreshStoreExit`).

A tool area is bounded to 8 GiB total,
1 GiB per file, 32768 entries and depth 32. Symlinks, hard links, nested
mounts, unsafe modes, unexpected entries and replacement refuse. Sealing
verifies and syncs the complete tree; sealed contents cannot be rewritten.
Each published file is written to a private stage named
`pending-<32 lowercase hexadecimal digits>` in its final directory, made
durable, and renamed to its final name without replacing anything, so a final
name only ever holds complete, durable bytes; no area path may use a stage's
name. A write that fails or is cancelled before its rename removes its stage.
A stage a killed write or a crash leaves in an unsealed area is not area
content: reads skip it and leave it, and the area's next writer, such as the
exact replay, removes it before it writes, as sealing does; a sealed area
holding one refuses.

## Tool areas

A **tool area** is named by the digest of one exact target client closure and
writable only inside a lifecycle mutation, which holds the context lease and
an exclusive lease on the tool directory; the `reserved` entry is that operation's
durable intent, published before the directory exists. It is content-addressed,
so every context selecting the same clients proves the same files and a
different closure never disturbs them. A second publication records the actual
directory identity, and an unattributed directory beside a reservation this
store published is its own interrupted attempt, adoptable only while empty.
Sealing verifies and syncs the complete tree, makes the closure immutable and
records its digest on the binding of the context that sealed it; a sealed area
reopens read-only for every later operation and context. An unsealed
attributed area is completed by an exact replay of the same closure, whose
publication verifies existing bytes rather than overwriting them.

The closure is shared host state that outlives the context that published it
for as long as a binding names it. Every successful controller stage ends by
naming on its context's binding the area it proved, whether it published that
area or found it published, sealing first an area an earlier attempt
published whole but never sealed, or by naming none when it selects no tool.
In the same lifecycle mutation, under a short exclusive root transaction, it then
retires every sealed area no binding names, except one the operation itself
holds open; a context deletion does the same once it drops the context's
binding. An area still `reserved` or `attributed` is an unfinished stage's
intent and is never retired, and no other command retires one. Retirement
publishes the areas' entries as `retiring`, removes each directory after
proving it is still the attributed one and taking its exclusive directory
lease, syncs the tools directory, then
publishes the record without those entries. A `retiring` area is no longer
held: no read opens it and the view omits it, and a read accepts its directory
present, partly removed or gone. The next retirement, or a stage whose closure
names it, finishes it before anything else
(`TestAStageRetiresTheAreaNoBindingNames`,
`TestAnAreaAnotherBindingNamesStaysUntilItsContextIsDeleted`,
`TestAStageWithoutToolsReleasesItsAreaAndKeepsUnfinishedOnes`,
`TestAnAreaTheOperationHoldsOpenStays`,
`TestAnInterruptedRetirementIsFinishedByTheNext`).

A controller stage also extends the retained tool sources it acquires under.
Before acquisition it publishes the exact source identities it will fetch into
the same atomic record, append-only: reusing an ID with different bytes or
origin refuses.

## The setup subtree

`/var/lib/bootwright/setup/` is setup's own, independently versioned and
outside the registry: the Controller descriptor never names it, no registry
verification decodes it, and it stays root `0700` like the rest of the store.
`setup/lock` is the exclusive lock of setup and of runtime publication, which
the controller stage's host-package install takes too. Setup and the stage
never wait for it and refuse a held lock as busy; a launch's
[cache-only rebuild](../controller.md#the-controller-runtime) waits for it,
polling so that cancellation ends the wait.

`state.json` is the canonical `bootwright.setup-state-v1` record, at most
256 KiB. Its `pending` member, `{id, planDigest, actions, startedAt}`, is
written and synchronized before setup's first effect and cleared with the
receipt it writes last. The `receipt` holds the platform (`id`, `versionId`,
`like`, `architecture`, `glibc`, `tested`), the lock digest, the runtime
(`digest`, `interpreter`, `python`, `abi`, `ansibleCore`), the closure digest
and the packages, each with its requirement, what requires it, the provider's
NEVRA, its repository and `installedBy`, a setup run ID or
`present before bootwright`. `runtimes` names the current and the previous
runtime digest. A record that cannot be read, or that carries another format,
is another build's: a launch refuses `controller.state`, "the controller setup
record was written by another Bootwright build", and one that finds `pending`
refuses `controller.state`, "a controller setup (`<id>`) was interrupted
before it finished", each with the remedy `run bootwright setup`. Setup
discards another build's subtree after confirmation and re-inspects an
interrupted one ([setup flow](../controller.md#the-setup-flow)).

A runtime is built in `runtimes/.stage-<32 hex>`, a directory setup creates
exclusively, and published at `runtimes/<runtime-digest>/` only with its
`runtime.json` and its `.lock`: one `syncfs` of the store filesystem, a
`renameat2(RENAME_NOREPLACE)` and a synchronized parent, so a directory under
a runtime digest always holds a whole runtime. A publisher that finds the
runtime already published discards its stage, and setup removes the stages a
killed run left under `setup/lock`, however many entries `runtimes/` holds. A
launch holds the runtime's `.lock` shared for its whole Ansible block, and only
while the runtime's `runtime.json` names its own digest; retirement takes the
lock exclusively without waiting, so a runtime in use is skipped, and retires a
runtime whose `.lock` is gone, which no launch can hold. Retirement renames
the runtime to a stage name, synchronizes the parent and then removes it.
Its contents hold the ordinary modes a Python virtual environment creates,
and the `requirements.lock` its build installed from, which names each wheel's
SHA-256. Locked wheels are cached at `cache/wheels/<sha256>/<filename>`, each
verified for size and digest before it is renamed into place.

Which runtimes and cached wheels a setup keeps and retires follows its
[retention](../controller.md#publication-and-interrupted-setup).

`runs/` keeps what setup's dnf printed, as
[setup run output](../cli/output.md#setup-run-output) describes. A run is a
directory named `setup-` and six digits, from `setup-000001` upward, holding at
most one file, `run.output`, which is bounded at 4 MiB. That is the
[bounded run](../cli/output.md#bounded-run-output)'s bound, so every run's
output shares one ([D92](../milestones/backlog.md#d92)). Directories are
`0700` and the file `0600`. There are at most 8 runs:
the next is numbered one past the newest, the oldest are removed before it is
created, and a setup whose next number would need a seventh digit keeps no
run. Its directory and file are created exclusively beneath the held setup
directory handle without following a link, and made durable before the run is
named. A runs directory holding anything else, more than 8 runs, a run holding
anything but its private `run.output` or an output past its bound refuses.
Nothing reads a run back.

## Publication and recovery

Publication writes and syncs an exclusive private staging file, revalidates
the held directory and previous record identity, then renames and syncs its
parent. A pre-rename failure is uncommitted and removes its staging file; a
staging file a killed publication leaves is removed by the next command that
opens a registry transaction
([storage and publication](../contexts.md#storage-locking-and-publication)).
An uncertain rename or durability closes the transaction capability and
permits no further mutation, and its refusal names the command that met it
([controller stage](../controller.md#the-controller-stage)). The root lock
precedes acquisition of the selected context lease. Lifecycle effects retain
that context lease and their opened bundle leases, taking short root locks
for shared-state reads and publications. Read-only area capabilities expire
with their callback, and all
mutation capabilities expire with their transaction. Reads never bootstrap,
upgrade, repair or write these records.
