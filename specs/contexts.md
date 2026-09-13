# Durable contexts

Workspace owns named contexts, Context configuration, immutable input revisions,
per-user selection and local publication. The private Linux/amd64 store reads registry versions 2 and 3.
Malformed records, unsupported formats, contradictory identity and unsafe
filesystem objects fail with `context.state`. There is no archive retention
or staging directory.

## Identity and selection

Names are lowercase DNS labels of at most 63 characters. Each creation allocates
an independent immutable `ctx-` identity before importing input. The registry
reserves a name and identity through `initializing`, `ready` and `deleting`.
Only ready contexts are usable; lack of an input revision is a valid ready state.
A successfully deleted name may be reused only with a fresh identity.

First import binds the canonical original Environment directory. It must not
belong to another context, and subsequent imports preserve it. Runtime state
remains separate from authored input. Context identity is not derived from an
Environment directory or a human-readable name.

Current selection belongs to the invoking user in `~/.bootwright/context`, a
bounded canonical JSON record containing `version: 1`, `name` and `id` (at most
4096 bytes). Its parent
is user-owned `0700`; the regular file is user-owned `0600`. Resolve the account
through the invocation identity and local account database, never `HOME`.
Perform its filesystem effects with that user's credentials. Use verified
no-follow handles, exclusive temporary files and atomic replacement.

There is no global current selection. Explicit `--context` bypasses the user
file; an implicit selection must still match the registry's immutable ID.
Missing or stale selection fails with `context use` guidance. List remains
usable without a current marker. Init selects only after store publication;
use changes only selection; update preserves it; delete clears only the
invoking user's matching name and ID. Never enumerate other users' homes.

Store publication and user-file publication are separate durable effects. If
selection fails after creation, retain the context and report that creation
succeeded but selection failed, with `context use --name <name>` guidance.
Never claim rollback. A post-rename sync failure reports uncertain durability.

### Controller relationship and host binding

An admitted Environment names its required controller Machine under
[the API contract](api/environment.md#controller-machine). Empty context
initialization remains valid without an Environment. Importing that
relationship does not bind the registry to a newly verified host, move local
state, change its root or infer a Machine from the invocation.

[Controller](controller.md) owns verified local-host evidence and setup
semantics. Workspace owns its durable shared-host receipt and the binding
between immutable context ID, admitted controller Machine identity and verified
host. First binding is an explicit confirmed setup publication under the root
lock and context lease; context import and inspection never publish it.
Missing, contradictory or different host evidence refuses without replacement.
A changed Machine name cannot silently transfer an existing binding. Ordinary
same-host reboot must remain distinguishable from relocation in the qualified
identity implementation.

Shared host state belongs under `/var/lib/bootwright/controller/`. The current
receipt and all context bindings share one atomic `state.json` publication;
there is no independently published per-context binding record. This prevents
completion and binding evidence from disagreeing after interruption. Records contain no Secret values, and
opaque private host evidence is never emitted by inspection. Dependency files
that must execute are a narrow root-owned `0700` exception to the regular-file
`0600` rule; metadata and other files remain `0600`. Only catalogued immutable
controller bundles may use this exception.

Explicit baseline setup may initialize the fixed root and publish a durable
empty registry without creating a context or keyring. That registry commit
must precede any controller subtree. Confirmed setup introduces registry version
4, preserving the version-3 allocator and active context records and adding a
versioned Controller descriptor. An older reader refuses version 4. The admitted
root layout must include the independently versioned controller subtree even
with zero contexts; unknown layouts or versions refuse. Ordinary context
commands never infer ownership, repair interrupted setup or remove shared host
state. Reading versions 2 or 3 does not introduce Controller state. Ordinary
context publications preserve an existing version-4 descriptor.

The descriptor fixes version `1`, mode `initializing` or `ready`, and the
Controller directory's device/inode. Its reservation precedes directory
creation; a second publication records the actual directory identity. An
unattributable directory after a crash is preserved and refuses adoption.
Only explicit setup can finish an attributable initialization.

The private Controller record version is `1`. Its fields are `version`, `host`,
`receipt`, `bindings`, `retainedSources`, optional `retainedDefinitions`,
`bundles` and optional `reservations`, encoded as compact JSON
in schema order followed by LF. Unknown fields, duplicate keys, noncanonical
records and versions refuse. The host contains the confidential
[`linux-installed-v1` tuple](controller.md#host-identity-and-shared-prerequisites).
The receipt fixes its ID, catalog digest, plan digest, context name/ID/revision/
Machine, explicit egress, source closure, full resolved dependency definition,
ordered actions and status. A baseline
receipt has empty context fields. Sources bind a stable ID to its original
credential-free URL, SHA-256 and exact byte count. Bindings contain immutable
context ID, Machine name and the private host digest, ordered by context ID;
sources are ordered by source ID. Reusing a source ID with different bytes or
origin refuses, including after replacing a terminal receipt.

Reservations record the exclusive host resources that locally hosted services
claim, under the
[Infrastructure services conflict contract](infrastructure-services.md#host-reservations).
Each entry contains immutable context ID, capability kind, service name and a
sorted unique key list; entries are ordered by context ID then kind then name.
Workspace stores and compares them without interpreting a key's meaning.
Publishing a key another context holds refuses; a context replaces only its own
entries, and its completed removal drops them. The record is absent when empty,
so a store that never hosted a service is unchanged.

Action requests and evidence are canonical compact JSON objects with sorted
keys. Each action records `planned`, `intent` or `observed`; an observed action
requires bounded evidence and an explicit outcome. Successful observations
cannot regress. `pending` and `unknown` retain recovery protection. `complete`
requires every action's verified `changed` or `unchanged` postcondition;
`failed`/`canceled` require no unresolved intent or unknown effect. The plan
digest is SHA-256 of the domain-separated host digest, catalog, resolution, context, egress,
sources and immutable action requests; progress and receipt ID are excluded.
Receipt IDs are `setup-` plus 128 bits of a domain-separated SHA-256 over the
plan digest and previous receipt ID. They are private coordination evidence.

The ordered actions prepare the execution bundle, run the Ansible native
dependency transaction (`container-runtime`, including selected target clients),
and publish an explicit context binding. The catalog digest binds the base
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

The state record is bounded to 4 MiB, with at most 128 actions, 512 current
sources, 4096 retained source identities, 4096 bindings and 256 reservations of
at most 64 keys of 256 bytes each. Each request,
preparation or evidence object is at most 64 KiB. A full resolved definition is
at most 512 KiB, with at most 16 retained definitions subject to the aggregate
state bound. Controller records allow nesting depth 16 and 32 fields per object;
other durable record bounds remain unchanged. There are at most 16 retained bundle
namespaces, named by the 64-character catalog digest. Their reservations record
`reserved`, `attributed` or `sealed` and physical directory identity. A bundle
is bounded to 8 GiB total, 1 GiB per file, 32768 entries and depth 32. Symlinks, hard links, nested
mounts, unsafe modes, unexpected entries and replacement refuse. Only a durable
setup intent grants its scoped write capability. Completion verifies and syncs
the complete tree before sealing; sealed contents cannot be rewritten.

Publication writes and syncs an exclusive private staging file, revalidates
the held directory and previous record identity, then renames and syncs its
parent. A pre-rename failure is uncommitted; uncertain rename/durability closes
the transaction capability and permits no further mutation. The root lock
precedes the selected context lease and remains held through each callback.
Read-only bundle capabilities expire with their shared-lock callback, and all
mutation capabilities expire with their transaction.

Before a context-bound setup effect, persist its pending receipt with the exact
input revision and binding references. Under the same root/context coordination,
context update, purge and revision collection refuse while setup recovery needs
those references, independently of lifecycle mutation evidence. Inspection
never converts a pending receipt to completion. Only explicit setup retry
resolves it through Controller postconditions. Host-wide setup effects retain
their receipt even without a context; a new context cannot bypass that pending
host work. Reads never bootstrap, upgrade, repair or write these records.

After definitive setup completion, preserve existing lifecycle guards. A safe
input update retains binding identity; context deletion may drop disposable
context-specific references but never uninstalls shared packages or deletes
shared bundles/host evidence. Package failure or process death is not disposal
proof. Complete-store restore and controller relocation must preserve these
relationships and remain separate recovery work.
[Architecture](architecture.md#controller-host-and-local-services) owns consumer
boundaries, and [milestones](milestones.md) owns delivery status.

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
and malformed values refuse before publication. Configuration contains no
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
selected input revision. Before discovery, resolve the
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
provenance through recompilation. Relative Secret file references, including
inherited defaults, remain anchored at the original recipient descriptor's
directory. Import does not rebase references or copy, inspect or expand their
payload paths. Authored `~` spelling remains unchanged during admission.

## Storage, locking and publication

The registry writes private format version 3, or version 4 once confirmed setup
has introduced the shared Controller descriptor. Active records hold the name,
immutable ID, initialization/deletion mode, selected revision, Environment
directory, configured secret-store type and reserved directory device/inode.
A fixed-size allocation namespace and counter prevent ID reuse within the store
without retaining a lifetime history of deleted contexts. The 4096-name bound
applies to active or reserved contexts, not past creations.

Version-2 registries remain readable without writes. The next authorized
registry publication upgrades them atomically, preserving every active name,
ID and record. Its fresh namespace must differ from the prefix of every used
version-2 ID before the old identity ledger can be discarded. Admission,
confirmation and mutation safeguards still precede an authorized publication.
There is no fallback from corrupt version-3 or version-4 state to older metadata.

The production root is `/var/lib/bootwright`. Its layout follows the
[host-binding contract](#controller-relationship-and-host-binding); the
Controller subtree exists only after confirmed setup:

```text
/var/lib/bootwright/
  registry.json
  controller/
    state.json
    bundles/<catalog-digest>/
      sources/
      python/
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
    secrets/
      store.json
      identities/
      keys/
      parts/
```

| Path | Purpose and retention |
| --- | --- |
| `registry.json` | One atomic map of names to identities, selected inputs and context status, plus the ID allocator. |
| `contexts/<name>/context.yaml` | Canonical immutable Context configuration; keeps the authored configuration contract separate from runtime metadata. |
| `desired-state/revisions/<revision-id>/` | Immutable input snapshot, so publication and protected recovery can retain a complete selected revision. Collect unselected revisions only with disposal proof. |
| `manifest.json` | Original input provenance, blob mapping, sizes and hashes needed to verify and replay the snapshot. |
| `file-NNNN` | Exact acquired descriptor or marker bytes; authored filenames never become storage paths. |
| `state/reservation.json` | Durable name/ID ownership evidence for interrupted creation and guarded deletion. |
| `state/mutation.json` | Lifecycle ownership and operation evidence; missing or unknown evidence prevents destructive cleanup. |
| `state/operations/` | [Reconciliation-owned operation records and logs](state-reconciliation.md#operation-records). Workspace supplies the held area and its publication primitives; it never interprets their content. |
| `secrets/` | Context-bound encrypted custody with its own independently versioned [storage contract](secrets.md#local-keyring-v2). |

Every directory is owned by `root:root` with mode `0700`; every file is owned
by `root:root` with mode `0600`, except the explicitly catalogued controller
executables described in the host-binding contract. All store access runs as
root. No environment variable selects another production root. Isolated test storage is injected at
composition. Reject unsafe existing objects without chmod/chown repair.

Context IDs retain `ctx-` plus 32 lowercase hexadecimal digits: a 64-bit
random allocation namespace followed by a positive 64-bit sequence. Reserve
and increment the counter durably before creating a directory; deletion and
interruption never decrease it, and exhaustion refuses without wrapping.
Namespace selection has at most 16 collision attempts. Existing version-2 IDs
remain unchanged. Revision IDs use `rev-` plus 128 secure random bits and at
most 16 exclusive collision attempts. File blobs use four-digit manifest indices, never authored
path segments. Records use bounded closed canonical JSON, UTF-8, compact typed
field order, sorted collections and one final LF. Private JSON uses Go's
`encoding/json` escaping. Readers reject noncanonical or contradictory records.
Manifest integrity failures never fall back to external input.

| Record | Required fields in canonical order |
| --- | --- |
| Registry | `version` (3 or 4), `idNamespace`, `nextIdentity`, `contexts`, then `controller` (version 4 only) |
| Context record | `name`, `id`, `environmentDirectory`, `revision`, `mode`, `secretStoreType`, `directoryDevice`, `directoryInode` |
| Reservation | `version` (2), `id`, `name` |
| Input manifest | `version` (2), `id`, `revision`, `inputDirectory`, `environmentDirectory`, `files` |
| Manifest file | `path`, `category` (`yaml` or `marker`), `size`, `sha256` |
| User selection | `version` (1), `name`, `id` |

Collections are present arrays, including empty ones. Contexts sort by name
and manifest entries by relative path. Empty input is encoded
with empty revision and Environment-directory strings; no input is invented.
Reservation, manifest and keyring formats are versioned independently. Mutation
evidence follows the Reconciliation-owned closed record contract.

Readers hold a shared nonblocking lock on the verified, never-replaced root
inode until all stored input or secret-session files have been consumed.
Mutators hold its exclusive lock plus the context lease where applicable;
contention fails safely. Revalidate target, identity and evidence under those
locks. Read-only operations perform no repair, initialization or publication.

A lifecycle operation is one such mutator: it holds the exclusive root lock and
the selected context's lease for its entire execution, because its host
reservations, controller evidence and operation records must stay coherent
while its effects run. Concurrent context reads therefore wait for it, and a
second mutator refuses rather than waiting indefinitely. Narrowing that
boundary is [deferred work](milestones.md#candidates). Within it, Workspace
supplies the operation area, the mutation-evidence replacement primitive and
the reservation publication; Reconciliation owns what they contain and when
they advance.

Init validates supplied configuration/input before effects, records its
initializing name/ID, and creates the final named directory directly. It
creates default state, empty revision storage and the configured local keyring
through a transaction-scoped Secrets capability. Only after all required data
is durable may it publish ready. Secret initialization never reacquires the
store lock or needs an already-published input revision. No Secret values are
automatically generated.

Interrupted initialization reserves its name and ID. Explicit init retry may
resume only the exact attributable pending identity and configuration, using
the keyring's authenticated initialization recovery. Unverifiable partial
state refuses; never adopt an unrelated directory or silently allocate a new
identity. A filesystem create and recording its identity are not one atomic
operation, so not every interruption is automatically resumable.

Input update writes new immutable blobs/manifests exclusively, flushes files
and containing directories, then atomically replaces and syncs the registry.
The selected revision changes at that registry commit point. Readers observe
a complete old or new input; interrupted unpublished revisions are never
adopted by scanning. Small temporary files for atomic record replacement stay
inside existing directories; they do not introduce a staging tree.

After durable registry publication, a pristine context may collect verified
unselected revisions while holding the root lock and context lease. Pristine
means exact `operation: none` and `ownership: none` evidence; unknown or
protected evidence retains all revisions. Revalidate registry selection,
context identity and mutation evidence before removal, and sync the containing
directories. Cleanup reads filesystem metadata, not frozen payload bytes.
Failure after publication reports committed input with incomplete cleanup;
an uncertain publication performs no cleanup. An authorized update can also
collect old unselected revisions before allocation after establishing the
current selected registry's durability, so an existing full revision tree can
recover capacity. Never collect the selected revision or infer disposal proof
from age. Future lifecycle consumers must define retention before their inputs
become collectible.

All traversal uses held no-follow handles. Inside the root reject mount
crossings, links, hardlinks, special files, wrong ownership/modes and path
substitution. Every publication revalidates location. Supported local
filesystems are ext4, XFS, Btrfs, tmpfs and overlayfs; Linux must provide
`openat2`. Unsupported containment or durability primitives fail closed.

Bounds apply before allocation/traversal: registry 8 MiB and 4096 active or reserved
names (version-2 decoding also bounds its historical identity ledger); manifest 4 MiB and 32 MiB aggregate referenced manifests; paths 4096
bytes; mutation records 64 KiB; 4096 revisions per context. Input and Secrets
limits additionally bound their trees. Missing registry in a nonempty root is
corruption, except that explicit init may finish publication when the root's
only entry is one private `pending-<32 lowercase hexadecimal digits>.json` file
whose bytes are exactly the canonical empty version-2 registry. Recovery holds
the exclusive root lock, revalidates the file and sole-entry layout, publishes
with a no-replace rename, verifies the result, and syncs the root before
proceeding. Inspection never performs this recovery; it directs the user to
repeat context init with the original options. Every other missing-registry
shape is left unchanged and reports that the complete store must be restored
from a matching backup or moved aside only after it is verified disposable.
For the currently implemented root formats, a published empty registry permits
only its own file and verified private, bounded
`pending-<32 lowercase hexadecimal digits>.json` files from interrupted registry
replacement; those files are ignored, never adopted. Any other entry
refuses with the same complete-store guidance. Bounds never authorize evidence
deletion to make room.

## Upgrade and restore boundary

Registry upgrade and the [explicit Secrets upgrade](secrets.md#local-keyring-v2)
preserve logical context and secret identities. Unsupported future formats
refuse before effects. Format changes must state their supported source
versions, exact commit point, retry behavior and retained recovery evidence.

Device/inode bindings deliberately reject ordinary directory-copy restoration.
Read-only commands never rebind them. There is no general copy-restore or
rollback command. A future restore must validate one coherent complete store,
preserve live logical identities, explicitly rebind verified filesystem
objects, and establish a fresh allocation epoch and encryption key before new
writes when restoring older allocation/seal counters. An old snapshot cannot
silently become current writable state. See the bounded restore outcome in
[milestones](milestones.md).

## Permanent deletion

The [Reconciliation guard](state-reconciliation.md#context-mutation-evidence)
owns positive disposal proof. Under the root lock and context lease, refuse
live resources, incomplete operations, retained ownership, unknown/corrupt
evidence, or any required recovery material. There is no abandonment bypass,
recovery-only mode, archival or remote resource effect.

After proof and ordinary confirmation, durably mark the exact context deleting
before removing any file. Remove only verified objects through bounded held
handles; preserve identifying state until the remaining children are removed.
Permanently remove imported revisions, keyring and all other disposable local
content, then the directory. Sync its parent before removing the active
registry entry. The name stays reserved until completion.

An explicit delete retry resumes the recorded identity, accepting verified
missing children as completed removal and refusing replacement or unknown
objects. A partially deleted context is never reactivated or reported rolled
back. Recovery guidance names `context delete --name <name> --purge`.

## Command results and confirmation

Context commands are text-only. Details include name, ID, initialization and
input readiness, and the invoking user's current marker. List sorts by name;
`current --short` emits only the name and LF. Input admission reports counts
and copied files; default creation does not invent compilation counts.
Warnings appear once on stderr. No private paths, payloads or digests appear.

Fresh init and use need no confirmation. Init refuses an already-ready name
and directs the user to update. Input update and deletion require ordinary
confirmation unless `--yes`; equivalent configuration-only update does not.
The prompt follows admission and safeguards while locks remain held. Only
`y` or `yes`, case-insensitive with surrounding whitespace ignored, accepts.
Read one answer of at most 64 bytes including LF without read-ahead. Decline,
noninteractive input, cancellation or I/O failure refuses publication.

Context-backed validate/render and declaration-dependent secret commands
reject a missing input revision with `context update --name <name>
--input-dir <dir>` guidance. Encryption init/status/rotate do not require input.
Effective output and compilation diagnostics retain their existing contracts.
