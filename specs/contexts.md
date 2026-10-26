# Durable contexts

Workspace owns named contexts, Context configuration, immutable input revisions,
per-user selection and local publication. The private Linux/amd64 store reads
one registry format. Malformed root-store records, unsupported formats,
contradictory identity and unsafe filesystem objects fail with `context.state`.
There is no archive retention or staging directory.

## Identity and selection

A context's name is its identity. Names are lowercase DNS labels of at most 63
characters, and no separate identifier, allocation namespace or counter exists.
The registry reserves a name through `initializing`, `ready` and `deleting`.
Only ready contexts are usable; lack of an input revision is a valid ready state.

A name is free again once its deletion completes, and recreating it produces an
independent context: deletion removes that name's input revisions, keyring,
controller binding and other per-context state before the registry entry, so the
replacement starts with none of it. While a deletion remains incomplete the name
stays reserved and only `context delete --name <name> --purge` may finish it.
The consequence of name identity is that a reference another user or record
still holds resolves to whatever context now carries that name; nothing detects
that the name was recreated.

Import copies the admitted input into the store under the product's
[copied-input rule](project.md#design-priorities-and-non-goals). The recorded
original input and Environment directories are provenance for logical compiler
paths and diagnostics: they are never context identity, are not unique across
contexts and are never read again. Any number of contexts may import one
directory, a later import may come from any directory, and the recorded
directories follow the selected revision. Runtime state remains separate from
authored input. Context identity is not derived from a source directory.

Current selection belongs to the invoking user in `~/.bootwright/context`, a
bounded canonical JSON record containing `version: 2` and `name` (at most
4096 bytes). Its parent
is user-owned `0700`; the regular file is user-owned `0600`. Resolve the account
through the invocation identity, never `HOME`: an operator whose account comes
from a directory service resolves through NSS with a pinned, root-owned
`getent` under the same single-entry and clean-home checks, `/etc/passwd`
serving only where `getent` is absent, and a `sudo -i` root shell resolves as
direct root, as [local privilege and user identity](cli.md#local-privilege-and-user-identity)
states. Perform its filesystem effects with that user's credentials. Use verified
no-follow handles, exclusive temporary files and atomic replacement. A refusal
raised under that account reports that account's own bounded diagnosis and
its repair, so an elevated command names the same repair as an unelevated one
(`TestAnElevatedSelectionRefusalKeepsItsRepair`).

Unsafe ownership, permissions, object type (a symbolic link included) or link
count fail with `context.state`, naming the object and its repair: an unsafe
marker names `~/.bootwright/context` and its removal followed by
`bootwright context use --name <context>`, and an unsafe directory names
`~/.bootwright` and that it must be a directory the user owns with mode 0700.
Content the store cannot read as a supported record, including
a superseded format, an unknown or retired field, a non-canonical encoding and
an over-bound file, is the account's own superseded marker rather than an unsafe
object: report no current selection, hold its observed identity, and let the
next selecting command replace it. A superseded marker never requires manual
repair.

There is no global current selection. Explicit `--context` bypasses the user
file; an implicit selection must still name a ready registry entry. A selection
naming an absent context fails naming that context, with
`bootwright context use --name <context>` and
`bootwright context init --name <name>` guidance. List remains
usable without a current marker. Init selects only after store publication;
use changes only selection; update preserves it; delete clears only the
invoking user's matching name. Never enumerate other users' homes.

Store publication and user-file publication are separate durable effects. If
selection fails after creation, retain the context and report that creation
succeeded but selection failed, with the selection store's own diagnosis and
`bootwright context use --name <name>` guidance.
Never claim rollback. A post-rename sync failure reports uncertain durability.

### Controller relationship and host binding

An admitted Environment names its required controller Machine under
[the API contract](api/environment.md#controller-machine). Empty context
initialization remains valid without an Environment. Importing that
relationship does not bind the registry to a newly verified host, move local
state, change its root or infer a Machine from the invocation.

[Controller](controller.md) owns verified local-host evidence and setup
semantics. Workspace owns the setup subtree's receipt and the binding between context
name, admitted controller Machine identity and verified host. First binding is
published by the first `apply` that uses the host, under the root lock and
context lease it already holds, before that operation reserves anything or
performs any effect, and only once setup completed; on a host no context
bound yet it also creates the controller record. Setup claims no context, and context
import, preflight and inspection never publish it. Missing, contradictory or
different host evidence refuses without replacement. A changed Machine name
cannot silently transfer an existing binding. Ordinary same-host reboot must
remain distinguishable from relocation in the qualified identity
implementation.

A host whose [identity](controller.md#host-identity-and-shared-prerequisites)
changed, because its machine-id, DMI product UUID or root filesystem UUID
changed, strands every context bound to it. The refusal names its exits in
order: run on the host the binding names, or restore those identity values on
this one; otherwise `bootwright context delete --name <name> --purge
--allow-orphans`, naming what it abandons. Destroying each context from the
original host is the only exit that abandons nothing, so it precedes a planned
change of any of those values.

Not yet met: the refusal names only the host the state belongs to; tracked as
[B832](milestones/backlog.md#b832).

Workspace keeps that shared-host state, the Controller descriptor and record,
the tool areas and the setup subtree with its receipt and runtimes, in the
[controller record](contexts/controller-record.md).

After definitive setup completion, preserve existing lifecycle guards. A safe
input update retains binding identity; context deletion may drop disposable
context-specific references but never uninstalls shared packages or deletes
runtimes or host evidence; it retires only a
[tool area](contexts/controller-record.md#tool-areas) no remaining binding
names. Package failure or process death is not disposal
proof. Complete-store restore must preserve these relationships under the
[restore boundary](#format-and-restore-boundary); controller relocation waits
on [B103](milestones/backlog.md#b103).
[Architecture](architecture.md#controller-host-and-local-services) owns consumer
boundaries.

## Context configuration

A Context is a standalone setup document, separate from the Environment graph:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Context
metadata:
  name: example

spec:
  secretStore:
    type: local-keyring
```

The envelope and all records are closed. Exactly one document is accepted;
`metadata.name` must equal the command's required `--name`. The secret-store
type defaults to `local-keyring`; its current configuration has no additional
parameters. Unknown types/fields, duplicate keys, aliases, conflicting names,
and malformed values refuse before publication, under the file's path. An
unknown or duplicate field is named by its field path and YAML line; a
document that is not a Context is refused naming the apiVersion and kind it
declares, with the remedy that desired state is passed with
`--input-dir <dir>` and `-f` takes one standalone Context file, a remedy a
refused file read without its own also carries. Configuration contains no
material, runtime identity, path override or user selection.

Store the canonical defaulted document as `context.yaml`. Context commands
admit it independently of desired-state compilation; it does not participate
in Environment resources, defaults, or effective rendering. Init without a
file synthesizes the default configuration without input discovery. Current
configuration fields are immutable: an equivalent configuration-only update
succeeds without writes or confirmation; backend changes refuse.

## Frozen input and provenance

An explicitly supplied `--input-dir` opens one directory and compiles its
entire acquired input before creating or changing runtime state. Omission at
init creates a context without desired state; omission at update preserves its
selected input revision. An input update preserves the context's name, secrets
and runtime state, and the
[mutation guard](state-reconciliation.md#context-mutation-evidence) refuses one
that would invalidate exact continuation. Once the guard admits it, an update
whose admitted manifest equals the selected revision's, its revision aside,
over frozen bytes that still verify, keeps that revision: it publishes
nothing, asks nothing and reports the context unchanged, so a completed apply
over that revision still
[settles](state-reconciliation.md#lifecycle-unit). Changed input over a
completed apply warns, before the prompt, with `lifecycle.state` that apply
refuses it until `bootwright destroy --context <name>` takes back what that
apply owns, and that importing the original files again does not undo the
update: they become a new revision, which apply refuses the same way
([B598](milestones/backlog.md#b598)).

Not yet met: the warning does not say that importing the original files again
leaves apply refusing; tracked as [B807](milestones/backlog.md#b807).

Before discovery, resolve the
state-root location read-only and reject a root within the input directory;
revalidate containment through held handles at publication. Freeze every
acquired YAML candidate and permitted marker, including streams excluded by
Environment selection. Copy exact bytes, never payloads or an effective-state serialization.
All ordinary acquisition, parser and semantic limits still apply.

Each immutable revision records the canonical original input directory,
Environment directory, canonical relative file names, file category, byte
length and SHA-256 digest. A manifest lists every frozen file exactly once.
Replay opens only those listed files through held revision handles. Extra
unlisted files never enter compilation. Paths must remain beneath the original
input root, satisfy acquisition path/exclusion rules, and use their correct
YAML or marker category. Lengths and digests are verified before compilation.
Manifest integrity failures never fall back to the original source directory.

Logical compiler paths are original input directory plus manifest-relative
path. This preserves resource selection, counts, diagnostics and field-level
provenance through recompilation. Import does not rebase typed payload
references or copy, inspect or expand the paths they name.

## Storage, locking and publication

The registry writes private format version 6. Active records hold the name,
initialization/deletion mode, selected revision and its Environment directory,
configured secret-store type and reserved directory device/inode. The
Controller [descriptor](contexts/controller-record.md#descriptor) is an
optional trailing member that a host's first binding adds; setup never adds it.
The 4096-name bound applies to active or reserved contexts, not past creations.

A `registry.json` of version 5, which every build before the setup v2
cut-over wrote, refuses every access with `context.state`, "/var/lib/bootwright
holds a store an earlier Bootwright build wrote (registry version 5), which
this build does not read or convert", and the fresh-install remedy: "with the
build that wrote it, destroy each environment (bootwright destroy --context
<name>) and delete each context (bootwright context delete --name <name>
--purge); then, as root, remove /var/lib/bootwright and
/var/lib/bootwright-staging and run bootwright setup with this build. Never remove /var/lib/bootwright while
services it manages run." ([D152](milestones/backlog.md#decisions),
`TestARegistryVersion5StoreNamesTheFreshInstallRemedy`). Nothing in that store
is rewritten. A `registry.json` of any other format version, such as the
versions 2, 3 and 4 earlier builds wrote, is refused rather than converted, as
state this build cannot read, with the earlier-build guidance below. There is
no fallback from corrupt state to older metadata.

The production root is `/var/lib/bootwright`. Its Controller subtree follows
the [controller record](contexts/controller-record.md) and exists only after a
first binding; its setup subtree exists after a confirmed setup:

```text
/var/lib/bootwright/
  registry.json
  controller/
    state.json
    tools/<tool-closure-digest>/
      sources/
      tools/
  media/
    <filename.iso>
    <filename.iso>.json
  setup/
    lock
    state.json
    runtimes/<runtime-digest>/
    cache/wheels/<sha256>/<file>
    runs/setup-NNNNNN/
      run.output
  contexts/<name>/
    context.yaml
    desired-state/revisions/<revision-id>/
      manifest.json
      file-0000
      ...
    state/
      reservation.json
      mutation.json
      operations/
      runs/
      trust/hosts.json
    secrets/
      store.json
      identities/
      keys/
      parts/
```

| Path | Purpose and retention |
| --- | --- |
| `registry.json` | One atomic map of context names to their selected inputs and status, plus the Controller descriptor once a first binding publishes it. |
| `media/` | Host-wide installer media under the [Managed OS media contract](managed-os.md#media-store): each image's exact bytes beside its canonical record, created by the first `media add`, shared read-only by every context and frozen through shared reservations rather than copied. An image is [acquired](#media-acquisition) into a private stage in this directory while no root lock is held, then published exclusively and atomically with its record; a pinned stage whose publication met another command's lock is kept beside its record for the command's repetition; deletion is guarded by the reservation record. |
| `setup/` | Setup's own [subtree](contexts/controller-record.md#the-setup-subtree), independently versioned and outside the registry: its lock, its `bootwright.setup-state-v1` record with the pending intent and receipt, the published runtimes, the locked wheel cache and the newest setup runs. |
| `contexts/<name>/context.yaml` | Canonical immutable Context configuration; keeps the authored configuration contract separate from runtime metadata. |
| `desired-state/revisions/<revision-id>/` | Immutable input snapshot, so publication and protected recovery can retain a complete selected revision. Collect unselected revisions only with disposal proof. |
| `manifest.json` | Original input provenance, blob mapping, sizes and hashes needed to verify and replay the snapshot. |
| `file-NNNN` | Exact acquired descriptor or marker bytes; authored filenames never become storage paths. |
| `state/reservation.json` | Durable name-ownership evidence for interrupted creation and guarded deletion. |
| `state/mutation.json` | Lifecycle ownership and operation evidence; missing or unknown evidence prevents destructive cleanup. |
| `state/operations/` | [Reconciliation-owned operation records and logs](state-reconciliation.md#operation-records). Workspace supplies the held area and its publication primitives; it never interprets their content. |
| `state/runs/` | Retained adapter output of [bounded runs](cli/output.md#bounded-run-output), in an area Workspace supplies and never interprets. The area keeps the newest runs the bounds table below states: before a run creates its directory, the oldest runs no live run holds are removed, each only while it holds nothing but its own `run.output`, and an entry not named as a run is neither counted nor removed. A run that cannot hold the directory it created removes it (`TestARunThatCannotOpenLeavesNoDirectory`). Runs are removed with the context. |
| `state/trust/hosts.json` | The context-managed SSH host-key trust an [SSH session](cli.md#machine-ssh-sessions) proves a Machine against when it declares no `knownHostsRef` and Bootwright did not install it: one public-key record per Machine, and one key per address. Written only by `machine trust` and by an explicitly confirmed first use, published atomically against its exact prior content, and removed with the context. A write that takes over the endpoint of a Machine the context no longer declares, or that no longer uses the context's SSH trust, removes that Machine's record in the same write. It holds no confidential material. |
| `secrets/` | Context-bound encrypted custody with its own independently versioned [storage contract](secrets.md#local-keyring-v4). |

Every directory is owned by `root:root` with mode `0700`; every file is owned
by `root:root` with mode `0600`, except the catalogued controller executables
the [controller record](contexts/controller-record.md#location-and-file-modes)
allows, and the content of a setup runtime. All store access runs as root. No environment variable selects another
production root. Isolated test storage is injected at
composition. Reject unsafe existing objects without chmod/chown repair. A root
of another type, owner or mode refuses naming the type, the `uid:gid` owner and
the octal mode it holds and the `root:root` `0700` directory it must be, and
says that nothing repairs it, with the guidance below for a root another build
created. A root that is itself a symbolic link is such a root, refused the
same way except for its repair: replace that path with a directory, such as
the one the link names or a mount of that storage, then repeat the command
(`TestAStateRootThatIsALinkIsRefusedAsARootOfAnotherType`). An ancestor of the
root that is a symbolic link or not a directory refuses `context.state` naming
that absolute path and its type, that the store reaches its root only through
directories and never follows a link, and the repair: replace that path with a
directory, such as the one a link there names or a mount of that storage, then
repeat the command (`TestAStateRootBehindALinkNamesTheAncestorAndItsExit`).

A host whose `/var/lib/bootwright` holds a store an earlier Bootwright build
created is not a supported controller for this build until that build's
environments are retired and its root archived. This build refuses that root
and never adopts or converts it. A version 5 registry it names by the version
it read, with the fresh-install remedy above; any other root it refuses by
what it lacks rather than by recognizing another build's files. The refusal says that another Bootwright build may
manage live environments there, to run this build on another controller host
or to retire that build's environments before archiving its root, and that the
root is never moved aside while that build's services run.

A context's name is its durable identity, so nothing is allocated for it:
reserve the name durably before creating its directory. Revision IDs use `rev-`
plus 128 secure random bits and at
most 16 exclusive collision attempts. File blobs use four-digit manifest indices, never authored
path segments. Records use bounded closed canonical JSON, UTF-8, compact typed
field order, sorted collections and one final LF. Private JSON uses Go's
`encoding/json` escaping. Readers reject noncanonical or contradictory records.
Manifest integrity failures never fall back to external input.

| Record | Required fields in canonical order |
| --- | --- |
| Registry | `version` (6), `contexts`, then `controller` once a first binding has published it |
| Context record | `name`, `environmentDirectory`, `revision`, `mode`, `secretStoreType`, `directoryDevice`, `directoryInode` |
| Reservation | `version` (3), `name` |
| Input manifest | `version` (3), `context`, `revision`, `inputDirectory`, `environmentDirectory`, `files` |
| Manifest file | `path`, `category` (`yaml` or `marker`), `size`, `sha256` |
| User selection | `version` (2), `name` |
| Media record | `version` (1), `name`, `size`, `sha256`, `source`, `added` |

Collections are present arrays, including empty ones. Contexts sort by name
and manifest entries by relative path. Empty input is encoded
with empty revision and Environment-directory strings; no input is invented.
Reservation, manifest and keyring formats are versioned independently. Mutation
evidence follows the Reconciliation-owned closed record contract.

Two advisory locks guard the store, and neither is ever waited for:

- the host-wide **root lock** on the verified, never-replaced root inode,
  shared for a read and exclusive for any command that may publish; and
- a context's **lease**, exclusive for mutation and shared for a bounded run,
  acquired on that context's directory while the root lock is held.

A media stage's own lock marks a live [acquisition](#media-acquisition) or the
add adopting a retained stage, and guards only that stage and the record
retained beside it.

| Root lock | Lease | Commands |
| --- | --- | --- |
| shared | none | Every read: `context list` and `current`, `plan`, `status`, `preflight controller`, `secret check`, `list` and `show`, `secret encryption status`, `media list`, including the opening of each image `--checksums` then reads, the admission a `media add` or `media delete` without `--yes` reviews before its confirmation, the store check a `secret set` from standard input makes before it reads, the plan a `context update` or `context delete` without `--yes` presents before its confirmation, SSH-trust and input reads, and the reads that precede `apply` and `destroy`. |
| exclusive | none | Every other command that may publish, such as `context use`, a `context update` that imports no input, `media delete`, the admission and publication of `media add`, the SSH trust that `machine trust` or a confirmed first use records, and the one registry transaction in which `setup` creates a missing setup subtree. |
| none | none | The [acquisition](#media-acquisition) of `media add` between those two holds: its copy or download, or its re-read of a stage it adopts, its digest verification, and the record it retains beside a stage whose publication met another command's lock; the full read of each image by `media list --checksums`, through the descriptor its shared hold opened; the confirmation prompt of `media add`, `media delete`, `context update` and `context delete`; the [read of standard input](secrets.md#acquisition-and-commands) by `secret set --value-stdin` or `--password-stdin`, including its terminal prompt; and every other part of `setup`, which holds only [`setup/lock`](contexts/controller-record.md#the-setup-subtree). |
| exclusive | held | `context init`; a `context update` that imports input; `context delete` of a ready context; `secret set` (from standard input, only to write what it read), `generate` and `delete`, `secret encryption init` and `rotate`, and the Secret binding `apply` and `destroy` take before they execute; and the short shared-state publications of `apply` and `destroy`. |
| none during effects | exclusive | The execution of `apply` and `destroy`, between short root transactions. |
| shared during admission, none during effects | shared | [Bounded runs](cli/output.md#bounded-run-output), including Machine power; their lease excludes lifecycle mutation of the same context. |

A read holds its lock until all stored input or secret-session files have been
consumed, and a mutator holds its locks until it finishes, except that
lifecycle effects and bounded runs release the root lock after admission
while retaining their context lease;
`media add` releases the root lock while it acquires,
`media list --checksums` releases its shared hold before it reads any image
in full, neither `media add`, `media delete`, `context update` nor
`context delete` holds one while it prompts, and a `secret set` from
standard input holds none while it reads, then takes the exclusive root lock
and the lease to write and re-proves its
[replacement decision](secrets.md#acquisition-and-commands) there. A command
that cannot take the lock or lease refuses with `lifecycle.lease` and a retry
remedy; no lock or lease is ever waited for or taken over. A lease is held
only once its stage collection, layout verification and reservation check
succeed; a lease that refuses is released at once. `setup`
[selects no context](controller.md#the-setup-flow), so it
takes no lease and collects no stage in a context's `state/` subtree. Its
reads take no root lock, and its run holds no exclusive root lock: it opens
one exclusive registry transaction only to create the root and its setup
subtree when that subtree is missing, then holds only `setup/lock`
([controller](controller.md#host-identity-and-shared-prerequisites)).
Revalidate target, identity and evidence under those locks. Read-only
operations perform no repair, initialization or publication.

Every read and every registry transaction verifies the mapping of every ready
context: its directory and recorded identity, its reservation, its
configuration and the manifest of its selected revision. A failure refuses
`context.state` naming the context, the entry relative to the state root and
the kernel's answer or the store's own refusal, with that damage's exit: a
context directory that is gone is abandoned with `bootwright context delete
--name <name> --purge --allow-orphans`; a missing or inconsistent
configuration or revision entry in a present directory is purged with
`bootwright context delete --name <name> --purge`; and a replaced directory, a
damaged reservation, a missing `contexts` container or an entry the store
refuses to open, for its type, owner, permissions or link count or a kernel
error, is resolved only by restoring the whole store from a
[matching backup](#format-and-restore-boundary), because the purge refuses
each of them too; no command performs that restore, so these refusals offer no
supported exit until [B103](milestones/backlog.md#b103). The purge of a context
verifies every other ready context and admits exactly that one: its
transaction serves only the registry, that context's mutation state and host
reservations, and the deletion of its record, so another damaged context still
refuses, named. Isolating a damaged context from the rest of the store is
[B325](milestones/backlog.md#b325).

A lifecycle operation snapshots its selected context and controller under the
exclusive root lock, then retains the context's exclusive lease throughout its
effects. Readers and other contexts may proceed. Context identity, inputs and
mutation evidence remain scoped to that lease; short root transactions refresh
the registry and shared controller state before reservation, binding, secret
or tool publication. Another context's bindings and reservations are preserved;
changes to this operation's own binding, reservations, host identity or retained
dependency sources refuse. Bundle reads take short shared root locks and retain
shared directory leases; publication holds an exclusive bundle lease until
sealing. Retirement refuses while any reader holds the bundle, and a later
retry completes it. Bounded runs retain a shared context lease, excluding a
lifecycle mutation of that context while allowing independent contexts to run.
Within this boundary, Workspace
supplies the operation area, the mutation-evidence replacement primitive and
the reservation publication; Reconciliation owns what they contain and when
they advance. It also lends a transaction-scoped secrets area, re-proving the
context's exact record, its layout and its secret directory as a secret
mutation does but under the lock and lease it already holds, fresh for each
caller, closed when that caller returns and serialized between an operation's
blocks. Only Secrets interprets it: the engine keeps
[produced material](secrets.md#produced-material) in custody through it.

Init validates supplied configuration/input before effects, records its
initializing name, and creates the final named directory directly. It
creates default state, empty revision storage and the configured local keyring
through a transaction-scoped Secrets capability. Only after all required data
is durable may it publish ready. Secret initialization never reacquires the
store lock or needs an already-published input revision. No Secret values are
automatically generated.

Interrupted initialization keeps its name reserved. Explicit init retry may
resume only that exact attributable pending name and configuration, using
the keyring's authenticated initialization recovery; its input, if any, is
supplied again and may come from any directory, and the resumed context
records the directory its retry admitted. Unverifiable partial
state refuses; never adopt an unrelated directory. A filesystem create and
recording its identity are not one atomic operation, so not every interruption
is automatically resumable. `bootwright context delete --name <name> --purge`
discards an interrupted initialization, and every refusal over an initializing
name names both exits: finishing it with `bootwright context init --name <name>`
and its original Context file, or discarding it with that delete.

Input update writes new immutable blobs/manifests exclusively, flushes files
and containing directories, then atomically replaces and syncs the registry.
The selected revision changes at that registry commit point. Readers observe
a complete old or new input; interrupted unpublished revisions are never
adopted by scanning. Small temporary files for atomic record replacement stay
inside existing directories; they do not introduce a staging tree. A write
that fails after creating its file removes exactly that file, unless its retry
relies on what it leaves, as below; a controller bundle file is staged and
renamed into place, and a failed or cancelled write removes its stage. A
write, sync or create the kernel
refuses for capacity (`ENOSPC`, `EDQUOT` or `EFBIG`) refuses as capacity,
naming only the kernel's answer, never a path, and the remedy of freeing space
or raising the quota on the filesystem that holds `/var/lib/bootwright`; any
other answer the kernel gives a write or sync is named the same way, with the
remedy of inspecting that filesystem. Only a failed integrity proof of the
store's own state reads as corruption: the secret area reports capacity and
I/O failures as `secret.store` with that cause and remedy, never as
`secret.store.corrupt`. Every record publication except the
initial registry's and the secret store's exclusive writes stages its bytes
beside its target, proves right before one rename that the destination still
holds what it replaces or renames without replacing anything, and removes its
stage on any failure before that rename, even when the command was cancelled.
Removal happens only while the containing directory still verifies; otherwise
the file stays and the original failure is reported. An operation or run area
removes a directory only once it is empty, and a record only while it is the
file that held exactly the bytes its caller expects, and each removal is
durable when it returns; an entry already absent counts as removed.

Registry and controller record writers hold the exclusive root lock during
publication; context `state/` writers hold the context's exclusive lease.
Collection takes the corresponding lock, so no collected stage has a live
writer: only a killed process,
or a failure whose directory no longer verified, leaves one. Every command
that opens a registry transaction (`context init`, `use`, `update` and
`delete`, `setup` when it creates its missing subtree, and the execution of
`apply` and `destroy`) therefore first removes the root's registry stages and
the controller directory's stages and, when it takes the lease of a context whose directory identity the registry
records, the stages in that context's `state/` directory and in its operation,
run and trust areas; every secret mutation first removes the stages in its
context's `state/` directory; each does so before it verifies the layout. Only
a private regular file named as that directory's publication stage, on its
device and within its record bound, is removed; anything else is left to the
verification that already refuses it. An operation, run or trust area refuses
a record or directory named as a stage, so collection never removes one.
Reads never collect.

The [secret store](secrets.md#local-keyring-v4)'s exclusive writes keep what
they leave for its own recovery. Its replacement stages through the same
publication and removes its stage on any failure before the rename, keeping
its exact read expectation, its publication phase and its not-committed,
committed or uncertain outcome; what a killed write leaves follows
[Local keyring v4](secrets.md#local-keyring-v4).
The context reservation write also keeps what it leaves: until the registry
records the context directory's identity, the reservation alone lets an init
retry attribute that directory and record the identity that deletion
requires. The initial registry's stage is the recovery artifact the
missing-registry rule below names; a replacement registry stage is removed on
failure like any other, and after a kill by the next registry transaction.
Media stages follow [Media acquisition](#media-acquisition), and tool area
stages follow the [controller record](contexts/controller-record.md#bounds).

After durable registry publication, a pristine context may collect verified
unselected revisions while holding the root lock and context lease. Pristine
means exact `operation: none` and `ownership: none` evidence; unknown or
protected evidence retains all revisions. Revalidate registry selection,
context identity and mutation evidence before removal, and sync the containing
directories. Cleanup reads filesystem metadata, not frozen payload bytes.
Failure after publication reports committed input with incomplete cleanup;
an uncertain publication performs no cleanup. An authorized update can also
collect old unselected revisions before allocating a new one, after
establishing the current selected registry's durability, so a full tree can
recover capacity. Never collect the selected revision or infer disposal proof
from age. Future lifecycle consumers must define retention before their inputs
become collectible.

A lifecycle read may open a retained unselected revision under its shared
lock, through the same no-follow, bounded and digest-verified reads as the
selected one, with its manifest validated against its own context, revision
and Environment directory; it writes nothing, and a mutation transaction reads
only its selected revision. `bootwright diff` and the changed-input refusal
read the revision the owning apply froze, which non-pristine evidence keeps. A
collected revision reads as unknown there, never as a refusal of the command.

All traversal uses held no-follow handles. Inside the root reject mount
crossings, links, hardlinks, special files, wrong ownership/modes and path
substitution. A listing reads the directory through the handle it holds, never
a fresh resolution of its name, and refuses a directory that was replaced or
moved while it was held. Every publication revalidates location. The
production root `/var/lib/bootwright` must survive a reboot, because it holds
the only ownership evidence of what its contexts realized: it is qualified on
ext4, XFS and Btrfs. A production root on tmpfs or overlayfs, whose upper
layer the kernel does not name, refuses `context.state` before any
publication, naming the filesystem type and the remedy of placing
`/var/lib/bootwright` on a persistent local filesystem, and the
[setup dry run](controller.md#the-setup-flow)'s `state-root` check reports the
same refusal. tmpfs and overlayfs serve only isolated test roots injected at
composition. Linux must provide `openat2`. Unsupported containment or
durability primitives fail closed.

Not yet met: the store admits a production root on tmpfs or overlayfs; tracked
as [B806](milestones/backlog.md#b806).

Bounds apply before allocation/traversal: registry 8 MiB; manifest 4 MiB and
32 MiB aggregate referenced manifests; paths 4096 bytes; mutation records
64 KiB, with at most 16 abandoned publication stages beside a context's state
entries; media records 4 KiB; and the operator-visible bounds below. Input and
Secrets limits additionally bound their trees.

| Operator-visible bound | Value | Go constant |
| --- | --- | --- |
| Blocks in one lifecycle plan | 256 | `MaxBlocks` in `internal/reconciliation/plan.go` |
| Groups in one lifecycle block | 32 | `MaxGroups` in `internal/reconciliation/plan.go` |
| Machines in one lifecycle group | 512 | `MaxMachines` in `internal/reconciliation/plan.go` |
| Impacts in one lifecycle block | 128 | `MaxImpacts` in `internal/reconciliation/plan.go` |
| Exclusive resources in one lifecycle block | 8 | `MaxExclusive` in `internal/reconciliation/plan.go` |
| Characters in a lifecycle identifier | 63 | `maxIdentifier` in `internal/reconciliation/plan.go` |
| Bytes in one canonical lifecycle request | 256 KiB | `MaxRequestBytes` in `internal/reconciliation/plan.go` |
| Canonical request bytes in one lifecycle plan | 4 MiB | `MaxPlanRequestBytes` in `internal/reconciliation/plan.go` |
| Encoded bytes in one stored lifecycle plan, including metadata | 6 MiB | `MaxPlanBytes` in `internal/reconciliation/operationstore/records.go` |
| Active or reserved context names | 4096 | `maxContexts` in `internal/workspace/contextfs/store.go` |
| Revisions per context | 4096 | `maxRevisions` in `internal/workspace/contextfs/store.go` |
| Host [reservation](contexts/controller-record.md#record) entries the controller record holds | 256 | `maxControllerReservations` in `internal/workspace/contextfs/controller_records.go` |
| Retained [controller bundle namespaces](contexts/controller-record.md#bounds), the tool areas | 16 | `maxControllerBundles` in `internal/workspace/contextfs/controller_records.go` |
| Lifecycle operations one context retains | 1024 | `MaxOperations` in `internal/reconciliation/operationstore/records.go` |
| Entries in one context's lifecycle operation area, and in each of its runs and SSH-trust areas | 8192 | `MaxEntries` in `internal/reconciliation/operationstore/records.go` |
| Bytes in one context's lifecycle operation area, and in each of its runs and SSH-trust areas | 64 MiB | `MaxBytes` in `internal/reconciliation/operationstore/records.go` |
| One lifecycle adapter invocation whose request states no deadline | 2 hours | `invocationTimeout` in `internal/reconciliation/ansiblerunner/process_linux_amd64.go` |
| The longest deadline a lifecycle adapter request may state | 6 hours | `MaxDeadline` in `internal/reconciliation/lifecycle/invocation.go` |
| One controller Ansible run: the base of a controller-stage tool publication | 10 minutes | `runTimeout` in `internal/controller/ansiblelocal/runner_linux_amd64.go` |
| The longest deadline a controller-stage client installation may run under | 2 hours | `clientStageCeiling` in `internal/controller/ansiblelocal/runner_linux_amd64.go` |
| Bytes in one installer media image | 32 GiB | `MaxMediaBytes` in `internal/managedos/media.go` |
| Installer media images one host holds | 64 | `MaxMediaEntries` in `internal/managedos/media.go` |
| Bytes in an installer media name | 250 | `MaxMediaName` in `internal/managedos/media.go` |
| Installer media stages at once, live, retained or abandoned | 16 | `maxStagedMedia` in `internal/workspace/contextfs/media_linux_amd64.go` |
| [Setup runs](contexts/controller-record.md#the-setup-subtree) the setup subtree keeps | 8 | `maxSetupRuns` in `internal/workspace/contextfs/setup_runs_linux_amd64.go` |
| [Bounded runs](cli/output.md#bounded-run-output) one context's runs area keeps | 16 | `maxBoundedRuns` in `internal/workspace/contextfs/runs_linux_amd64.go` |

`TestDocumentedBoundsMatchCode` compares each value with its code, and each
deadline with the one its runner passes to `context.WithTimeout`: the lifecycle
runner passes a request's own deadline clamped to the longest one, or the
default for a request that states none. The controller runner passes the base
alone for a run without target clients, and otherwise the base plus each
client source's [acquisition deadline](controller.md#the-controller-stage)
clamped to the client-installation ceiling; a closure past that ceiling is
refused before Ansible starts, so the clamp never shortens a run it admits.

A claim or registration refuses `lifecycle.state` at the retained-operation
bound once the context could not retain the operation directories the new
operation needs, or once the operation area could not hold what it needs. A
fresh apply needs room for its own directory and for that of the removal that
takes it back, and a removal for its own, so a fresh apply refuses one
directory short of the bound the table states, while the removal of the last
apply admitted still registers and completes at that bound
(`TestTheRemovalOfTheLastAdmittedApplyRegistersAtTheDirectoryBound`). A first
pass is what an
[operation directory](state-reconciliation.md#operation-records) holds once
each block of its frozen plan ran one attempt: seven entries of its own and six
for each block. A fresh apply needs its own first pass and the first pass of
the removal that takes it back, which carries at most the blocks the apply
froze, and must still keep 1024 entries free. A removal needs its own first
pass and one entry more, because every record or log write needs one entry
free. Operations that ran their blocks therefore reach the bound while fewer
directories are retained, and the next fresh apply refuses at its claim,
before it raises evidence or binds anything, never at one of its block writes,
while the removal of the last apply admitted still registers and completes
(`TestTheOperationAreaAdmitsEveryRetainedOperation`,
`TestTheRemovalOfTheLastAdmittedApplyRegistersAtTheLine`). The free entries
hold the index and the later attempts and resolutions of the current apply and
of its removal, the only operations that write again. The removal's first
pass, its one entry more, the bytes its registration writes and the opening
record that restores a latched log fault are its reserve, which only that
removal writes. An attempt, retry or resolution of the apply whose writes
would cut into the reserve refuses `lifecycle.state` before its effect, naming
`bootwright destroy --context <name>`, which is then that apply's only way on;
a retry or resolution of the removal that would pass the bound refuses before
its effect and leaves its block as it was.

Not yet met: the apply's later attempts may use that reserve, and its removal
then refuses at its registration, leaving the apply's evidence, binding and
effects as they were (`TestADestroyTheAreaCannotHoldRefusesAtItsRegistration`);
tracked as [B806](milestones/backlog.md#b806).

The area's bytes are admitted the same way, as its byte bound counts them. A
fresh apply needs 16 MiB of them free, which hold its records and logs and
those of the removal that takes it back, and a removal needs the bytes its
registration writes: its plan, its operation record and the index. An
attempt's retained adapter output never takes the area into those 16 MiB: it
keeps what fits beneath them and is then cut short, as its own bound cuts it,
and the outputs of one invocation share what is left. However much an adapter
prints, only records and logs use them, so the removal of the last apply
admitted registers and completes within the bound
(`TestTheRemovalOfTheLastAdmittedApplyRegistersAtTheByteBound`,
`TestTheOperationAreaKeepsTheReservedBytesForTheLastRemoval`). Those bytes
belong to the removal's reserve above, so the apply's later records and logs
never use them. A bounded run's
output lies in an area of its own, which holds no records, and only its own
bound cuts it. A record or log write that would take the area past its entries
or its 64 MiB refuses `context.state`, naming the lifecycle operation, bounded
run or SSH trust storage it would overfill. The operation area, which only its
transaction writes, and the SSH-trust area, which only its trust mutation
writes, measure their subtree at their first write and then count their own
writes; a write that fails may still have landed, so the next one measures
again (`TestTheOperationAreaMeasuresItsSubtreeOncePerTransaction`,
`TestAFailedOperationWriteMeasuresAgain`). Every bounded run of the context
writes the runs area beside the others under the shared lock, so it measures
every write (`TestTheRunsAreaMeasuresEveryWrite`). A run that leaves its name
while that measure walks inside it, as one another run's retention removes
does, is absent from it, as a run gone before the walk reached it is, while
one replaced at its name still refuses
(`TestARunGoneWhileTheRunsAreaIsMeasuredIsAbsent`). The area's retention keeps
the runs it holds within its byte bound and far below its entry bound, so
`machine` power commands keep working however many ran before
(`TestBoundedRunsKeepWorkingPastTheOldEntryWall`).

No command prunes the operations a context retains, so a refusal at the
retained-operation bound names the exits that exist, from what a `destroy`
would still free and from the
[guard's](state-reconciliation.md#context-mutation-evidence) own reading of the
evidence. A fresh apply refused beside evidence the guard does not read as
pristine names `bootwright destroy --context <name>` and then the apply again:
that destroy removes or releases what the evidence protects, which a deletion
would have to abandon. One refused beside pristine evidence and a claim that
holds nothing names the same destroy, which
[reclaims](state-reconciliation.md#context-mutation-evidence) that claim. The
repeated apply fits in what the destroy freed or refuses again. A fresh apply
refused beside pristine evidence and no such claim names
`bootwright context delete --name <name> --purge` and then
`bootwright context init --name <name>` from the context's original
configuration and input. A removal refused at its registration has no destroy
left to name, so it names the deletion the guard admits, with
`--allow-orphans`, which abandons what the context may still own, and then the
same init. Over evidence the guard cannot read, either refusal names the
deletion with `--allow-orphans`, which cannot list what the context may still
own, and then the same init
(`TestTheRetainedOperationRefusalsMatchTheirGolden`,
`TestEachRetainedOperationExitLeadsOutOfTheBound`).

Missing registry in a nonempty root is
corruption, except that explicit init may finish publication when the root's
only entry is one private `pending-<32 lowercase hexadecimal digits>.json` file
whose bytes are exactly the canonical empty registry. Recovery holds
the exclusive root lock, revalidates the file and sole-entry layout, publishes
with a no-replace rename, verifies the result, and syncs the root before
proceeding. Inspection never performs this recovery; it directs the user to
repeat context init with the original options. Every other missing-registry
shape is left unchanged and refuses as state this build did not publish, with
the earlier-build guidance [above](#storage-locking-and-publication): another
Bootwright build may manage live environments from that root, so this build
runs on another controller host or that build's environments are retired
before its root is archived, and the root is never moved aside while that
build's services run; a store this build created is restored whole from a
[matching backup](#format-and-restore-boundary).

The root admits only the store's own published objects: `registry.json`, the
`contexts` container, the `controller` subtree once the registry declares it,
the `media` container once a `media add` has created it, the private `setup`
directory once setup has created it, whose content the root admission never reads,
and verified private, bounded `pending-<32 lowercase hexadecimal digits>.json`
files left by an interrupted registry replacement; reads ignore them, nothing
adopts them, and the next command that opens a registry transaction removes
them. Any other entry refuses with the same earlier-build and whole-store
guidance, whether or not the registry holds contexts. Bounds never authorize evidence
deletion to make room.

### Media acquisition

`media add` takes the exclusive root lock twice and holds no root lock
between, because an image may take hours to arrive and every other store
command on the host would refuse for that long. The first hold admits the
request as the [media store](managed-os.md#media-store) requires and claims the
image's stage: the private file `media/staging-<32 lowercase hexadecimal digits>`,
named by the first 128 bits of the SHA-256 of the image name. The stage shares
the media directory's filesystem, so publication is one rename, and it is
created exclusively, so a second claim of the same name refuses while it
lives. Its owner holds an exclusive advisory lock on the stage from the claim
until it publishes or removes it, and copies or downloads the image into it,
computing the digest as it writes, with no root lock held. The second hold
re-proves the admission against the store as it then stands and proves the
stage unchanged since its bytes were measured: its status (inode, size, link
count, modification and change times) is the one its owner took when it
finished filling the stage or re-reading an adopted one, and the record states
the size and digest measured then. The proof compares that status and does not
read the bytes again. A stage or record that fails it refuses, publishing
nothing, before a replacement removes the image it supersedes. The hold then
renames the stage to the image name without replacing anything, proves the
renamed file is still that stage with that status, apart from the change time
the rename itself sets, and only then publishes the record.

Without `--yes`, a `media add` and a `media delete` first take a shared hold
that admits the request as the media store requires. A `media add` that
replaces an image and a `media delete` then ask for confirmation with no root
lock held, and the exclusive hold that claims the stage or deletes refuses,
changing nothing, when the name's occupancy, its published size and digest, its
freeze or, for a deletion, its retained stage changed meanwhile, whether or not
a prompt ran. With `--yes` the command takes the exclusive hold alone.

A deletion removes whatever occupies the image's name and its record's name
without following it: a file, link or other non-directory is unlinked and an
empty directory is removed. A directory that still holds entries is refused
with `media.store`, naming the entry relative to the state root and the remedy
of removing it and repeating the deletion, and nothing inside it is removed
(`TestAFailedNonRegularMediaEntryIsRemovedByMediaDelete`).

A command that cannot take the lock for the second hold refuses with
`lifecycle.lease` and publishes nothing. When the request pinned the digest with
`--sha256`, it first retains its stage: still holding the stage's lock and no
root lock, it writes exclusively beside the stage the canonical media record it
would have published, as
`media/staging-<32 lowercase hexadecimal digits>.json`, and its refusal says
that repeating the command publishes the image without acquiring it again. It
retains a stage it filled only when the stage passes the second hold's proof
that it is unchanged since its bytes were measured. A filled stage that fails
that proof, and an unpinned stage, are removed, and the refusal is the lock's
alone.

A stage and the record beside it are a retained pair while no process holds
the stage's lock, the record decodes as a canonical media record within its
bound, the stage is the one named by the record's image, and the opened stage
is a private regular file of exactly the size the record states. The next
`media add` of that image whose `--sha256` equals the recorded digest adopts
the pair: its first hold takes the stage's lock, the add re-reads the stage in
full with no root lock held, and its second hold publishes it as a filled stage
with the retained source and its own publication time, then removes the
retained record. An adopted stage whose bytes no longer match the record is
removed and the add refuses; any other failure leaves it retained. An add of
that image with another digest or none removes the pair before it claims, and
`media delete` of that image removes it; either refuses while another add holds
the stage's lock, and a frozen image's pair stays until its freeze ends.

A failed, refused or cancelled acquisition removes its own stage while it still
holds the stage's lock. A stage write or sync the filesystem refuses reads as
the media store's failure, never as the source's: a capacity answer names only
the kernel's answer and the remedy of freeing space or raising the quota on
the filesystem that holds `/var/lib/bootwright/media`, while a source that
fails mid-stream is still reported as the source's. A stage whose lock no process holds and that is not
retained, such as one a killed `media add` left, is abandoned: it is never
listed or adopted, and the next `media add` or `media delete` removes it, after
the record beside it if any, under the exclusive root lock before it claims
anything, as it removes a retained record whose stage is gone and the record
temporary files named `pending-<32 lowercase hexadecimal digits>` that only an
exclusive holder writes. At most 16 stages exist at once, live, retained or
abandoned; a claim beyond them refuses until another `media add` finishes or
`media delete` removes a retained stage.

## Format and restore boundary

Under the [pre-1.0 format policy](project.md#design-priorities-and-non-goals),
no stored format is converted. A registry, [secret store](secrets.md#local-keyring-v4),
[controller record](contexts/controller-record.md) or frozen request written in
any other format is refused, not upgraded, and unsupported past and future
formats refuse before effects. A format change states the refusal its
predecessor now meets.

Device/inode bindings deliberately reject ordinary directory-copy restoration.
Read-only commands never rebind them. There is no general copy-restore or
rollback command. A future restore must validate one coherent complete store,
preserve live context names, explicitly rebind verified filesystem
objects, and establish a fresh encryption key before new
writes when restoring older seal counters. An old snapshot cannot
silently become current writable state. See the bounded restore outcome in
[milestones](milestones.md).

A **matching backup**, which every remedy that names a restore of the whole
store or of a keyring means, is an image of the whole state root, taken while
no command holds its root lock or any context lease, whose restoration keeps
the device and inode identity of every object the registry and records bind.
No command takes or restores one: until [B103](milestones/backlog.md#b103) a
restore is the operator's own act outside every supported command, so a remedy
that names a matching backup is not a supported exit. No new seal may be
written to a keyring whose seal counters a restore rolled back.

Not yet met: nothing establishes the fresh encryption key the rule above
requires or detects rolled-back seal counters, so a keyring restored with older
seal counters keeps sealing under its old key with its usage counted from the
older value; tracked as [B816](milestones/backlog.md#b816) and
[B103](milestones/backlog.md#b103).

## Permanent deletion

The [Reconciliation guard](state-reconciliation.md#context-mutation-evidence)
owns positive disposal proof. Under the root lock and context lease, refuse
live resources, incomplete operations, retained ownership, unknown/corrupt
evidence, or any required recovery material. A default refusal over evidence
that still attributes objects to the context names
`bootwright status --context <name>` and `bootwright diff --context <name>` as
their inventory, `destroy` as the way to remove them before deletion, and the
acknowledged deletion. `status` lists only the objects the selected input
declares ([B578](milestones/backlog.md#b578)), while `diff` also lists what
the owning apply realized that the selected input no longer declares; the
deletion plan and the orphan confirmation list, from readable evidence, every
object the context's frozen operations realized, whether or not the selected
input still declares it.

Only the explicit orphan acknowledgement defined by that guard waives the
disposal verdict. Over missing, corrupt or unsupported evidence in a present
context directory nothing proves what the context owns: an input update and a
default `--purge` refuse, naming `bootwright status --context <name>` and the
exit `bootwright context delete --name <name> --purge --allow-orphans`, and
that acknowledgement abandons the context, its confirmation saying that its
objects cannot be listed because its mutation evidence cannot be read; an
absent evidence file is read so, never as storage that cannot be accessed
(`TestMissingEvidenceIsRefusedAndAbandonedAsCorruptEvidenceIs`). An evidence
file the store refuses to open as its own, for its type, owner, permissions,
link count, size or a kernel error, refuses an input update, `--purge` with or
without `--allow-orphans`, `status` and every lifecycle command with
`context.state`. The refusal names the context, the entry
`contexts/<name>/state/mutation.json` and the store's answer, and gives the
exit: remove that entry beneath the state root, which leaves the evidence
missing, then `bootwright context delete --name <name> --purge
--allow-orphans`, or restore the whole store from a
[matching backup](#format-and-restore-boundary). The deletion never removes an
entry it cannot verify as its own
(`TestUnsafeEvidenceIsRefusedNamingItsContextEntryAndExit`). Any other entry
the deletion's walk cannot verify as its context's own, beneath `secrets/` or
`desired-state/` among others, refuses `--purge`, with or without
`--allow-orphans`, with `context.state` naming the context, the entry relative
to the state root and why: its type, another filesystem, a regular file's link
count, its owner and mode, a name outside the context's layout, or the
kernel's answer. Its exit is to remove that entry beneath the state root, then
repeat `bootwright context delete --name <name> --purge`, adding
`--allow-orphans` when the deletion abandons what the context owns, or to
restore the whole store from a [matching backup](#format-and-restore-boundary)
(`TestTheDeletionWalkNamesItsContextEntryAndExit`). A mount point the walk
never crosses has the same exit except that it is unmounted, never removed,
because its contents are not the context's
(`TestAMountPointInAContextTreeIsUnmountedNeverRemoved`). An entry the walk
reaches without crossing a mount but whose device is not its context's, as a
btrfs subvolume is, has the exit to unmount it when it is a mount point and
otherwise to remove it, never removing the contents of a mount
(`TestAnEntryOnAnotherFilesystemIsUnmountedNeverRemoved`). A live
lease refuses under it exactly as it does without it, before the guard reads
anything. A ready context whose directory is gone from a `contexts`
container that still verifies is lost: what it owned cannot be listed, so its
`--purge` refuses with `context.unsafe-delete`, naming the context, the entry
`contexts/<name>` and the kernel's answer, and the exit `bootwright context
delete --name <name> --purge --allow-orphans`. That acknowledgement abandons
it: its confirmation says that its objects cannot be listed, the deletion
proves the directory still absent, durably marks the context deleting, syncs
the container, drops its binding and host reservations and removes its
registry entry, and the result reports the abandonment. An acknowledged
deletion abandons the objects rather than removing them: it is otherwise the
same permanent local deletion, it needs `--purge` and ordinary confirmation
like any other, its confirmation names the abandonment with
`bootwright status --context <name>` and `bootwright diff --context <name>` as
the inventory of the objects it abandons, or says they cannot be listed, its
result reports it, and it has no
remote resource effect. There is no recovery-only mode or
archival. It still removes the keyring, and with it the
[produced material](secrets.md#produced-material) no completed removal
withdrew, such as the administrator kubeconfig of an installed cluster, which
nothing else keeps: export it first with `bootwright cluster kubeconfig --name
<cluster>`. The orphan confirmation names that custodied access and the
export command, with the context, beside the abandoned objects, whether status
lists them or the context's mutation evidence cannot be read; a lost context's
keyring went with its directory, so its confirmation names none
(`TestTheOrphanAcknowledgementAbandonsAContextWhoseEvidenceCannotBeRead`).

An abandoned object keeps the identity its context's name gave it, so a
context initialized again under that name, or a context of that name on
another controller that shares a provider host, reads it as its own and may
continue, reinstall or remove it, as
[B568](milestones/backlog.md#b568) records, although a name never proves ownership under
[mutation safety](state-reconciliation.md#mutation-safety). The orphan confirmation therefore
says to remove the abandoned objects by hand before initializing the same name
again, and never to reuse a context name across controllers that share a
provider host; every exit that names an acknowledged deletion followed by
`bootwright context init` of the same name carries the same warning. An
acknowledged deletion also leaves in place each
[installer work area](container-clusters.md#boot-media) its clusters'
installations keep on their artifact server's placement Machine, which holds
the material those installations were given, and the rest of the context's
[native-side retention](secrets.md#native-side-retention) on the hosts it lives
on. Its confirmation names those areas and that retention beside the custodied
access.

Deletion also drops what the controller record holds for the context: its
[host binding](#controller-relationship-and-host-binding) and every host
[reservation](infrastructure-services.md#host-reservations) it still holds,
which a completed removal would have released and an abandoning deletion
otherwise strands, because no context is left to release them. The result names the released reservation keys, so another context may
reserve them. It releases only the record's claims: an abandoned object that
still holds such a resource on the host is left in place, as abandonment
states. The orphan confirmation names the exclusive keys the deletion frees
and warns that another context may then claim a resource an abandoned object
still uses, such as the management controller of a powered-off Machine.

Not yet met: the refusal, the orphan confirmation and the deletion plan name
`status` alone as the inventory and list only what the selected input
declares, and the orphan confirmation names neither the same-name hazard nor
the exclusive keys it frees; tracked as [B803](milestones/backlog.md#b803).

Not yet met: the orphan confirmation does not name the installer work areas
or the native-side retention the deletion leaves; tracked as
[B829](milestones/backlog.md#b829) and [B817](milestones/backlog.md#b817).

After proof and ordinary confirmation, durably mark the exact context deleting
before removing any file or claim. Remove only verified objects through bounded
held handles; preserve identifying state until the remaining children are
removed.
Permanently remove imported revisions, keyring and all other disposable local
content, then the directory. Sync its parent before removing the active
registry entry. The name stays reserved until completion.

An explicit delete retry resumes the recorded deletion, accepting verified
missing children as completed removal and refusing replacement or unknown
objects. A partially deleted context is never reactivated or reported rolled
back, and its name cannot be recreated until the deletion completes. Recovery
guidance names `context delete --name <name> --purge`. A deletion runs in a
transaction scoped to its context, which verifies every other ready context as
any transaction does and leaves the named one to the deletion, so a context
whose damage leaves its evidence readable is purged with `--purge` alone when
the guard reads that evidence as pristine.

## Command results and confirmation

Context commands support human, JSON and YAML results through the
[shared output contract](cli/output.md#json-output). Default text details include
name, initialization and
input readiness, and the invoking user's current marker. List sorts by name;
`current --short` emits only the name and LF. Human details show `Current` as
`yes` or `no`, `Input` as `imported` or `not imported`, and retain the context
`Mode`. Their `Next` names validation with `--context <name>` for a ready
context with imported input, input update with `--name <name>` otherwise,
`context init` to resume initialization, or `context delete` to resume
deletion. Input admission reports counts
and copied files; default creation does not invent compilation counts.
Human warnings appear once on stderr; structured warnings appear in envelope
diagnostics with empty stderr. No private paths, payloads or digests appear:
a refusal naming damaged store state names its entry relative to the state
root.

`context list` also reports, for each context whose current operation is not
finished, that operation's verb, its state and the build that registered it,
as `<version> (<commit>)`, so an operator sees what to finish before
installing another build. It never judges whether another build can continue
the operation; only that build's plan preview does.

Not yet met: `context list` reports no operation; tracked as
[B832](milestones/backlog.md#b832).

Fresh init and use need no confirmation. Init refuses an already-ready name
and directs the user to update. Input update and deletion require
[ordinary confirmation](cli.md#ordinary-confirmation) unless `--yes`;
equivalent configuration-only update and an input update that keeps the
selected revision do not. The prompt follows admission and safeguards, which
a shared hold reads, and runs with no lock held; a refused confirmation
publishes nothing. A confirmed update or deletion then takes the exclusive
root lock and the lease and proves again everything its plan presented, and
refuses with no write, naming the repetition of the command, when any of it
changed meanwhile, as `media delete` does. With `--yes` the command takes the
exclusive lock and the lease alone.

Not yet met: `context update` and `context delete` hold the exclusive root
lock and the lease across their prompt; tracked as
[B806](milestones/backlog.md#b806).

An update's prompt follows its presented plan: the name, the admitted input
directory, the files it copies, the files seen and the objects decoded on
standard output, and every warning on standard error, the completed-apply
warning above included, which its result then does not repeat. A deletion's prompt follows a
plan naming the context, its selected input revision, the keyring with every
secret version it holds (or, for a lost context whose directory is gone, that
its keyring already went with it) and the host reservations it removes, and what it
abandons: nothing, the objects the context's frozen operations realized, or
objects that cannot be listed and why. A command that would prompt without a
plan presenter refuses with `context.state` rather than ask.

Every context-backed command that needs desired state (validate, render,
declaration-dependent secret commands, plan, status, apply, destroy, the
machine commands and `preflight controller --context`) rejects a missing input
revision with `context.input`, naming the context, and the remedy
`bootwright context update --name <name> --input-dir <dir>`. An absent store or
an absent named context refuses with `context.state`, naming that context, with
`bootwright context init --name <name>` and
`bootwright context use --name <context>` guidance. Encryption init/status/rotate do not require input.
Effective output and compilation diagnostics retain their existing contracts.
