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
through the invocation identity and local account database, never `HOME`.
Perform its filesystem effects with that user's credentials. Use verified
no-follow handles, exclusive temporary files and atomic replacement. A refusal
raised under that account reports that account's own bounded diagnosis.

Unsafe ownership, permissions, object type or link count fail with
`context.state`. Content the store cannot read as a supported record, including
a superseded format, an unknown or retired field, a non-canonical encoding and
an over-bound file, is the account's own superseded marker rather than an unsafe
object: report no current selection, hold its observed identity, and let the
next selecting command replace it. Selection never requires manual repair.

There is no global current selection. Explicit `--context` bypasses the user
file; an implicit selection must still name a ready registry entry. A selection
naming an absent context fails with `context use` guidance. List remains
usable without a current marker. Init selects only after store publication;
use changes only selection; update preserves it; delete clears only the
invoking user's matching name. Never enumerate other users' homes.

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
between context name, admitted controller Machine identity and verified
host. First binding is published by the first `apply` that uses the host, under
the root lock and context lease it already holds, before that operation
reserves anything or performs any effect. Setup claims no context, and context
import, preflight and inspection never publish it. Missing, contradictory or
different host evidence refuses without replacement. A changed Machine name
cannot silently transfer an existing binding. Ordinary same-host reboot must
remain distinguishable from relocation in the qualified identity
implementation.

Workspace keeps that shared-host state, the Controller descriptor and record,
the setup receipt and the bundle and client-area namespaces, in the
[controller record](contexts/controller-record.md).

After definitive setup completion, preserve existing lifecycle guards. A safe
input update retains binding identity; context deletion may drop disposable
context-specific references but never uninstalls shared packages or deletes
shared bundles/host evidence. Package failure or process death is not disposal
proof. Complete-store restore must preserve these relationships under the
[restore boundary](#format-and-restore-boundary); controller relocation waits
on backlog [C14](milestones/backlog.md#candidates).
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
selected input revision. An input update preserves the context's name, secrets
and runtime state, and the
[mutation guard](state-reconciliation.md#context-mutation-evidence) refuses one
that would invalidate exact continuation. Before discovery, resolve the
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

The registry writes private format version 5. Active records hold the name,
initialization/deletion mode, selected revision and its Environment directory,
configured secret-store type and reserved directory device/inode. The
Controller descriptor is an optional trailing member that confirmed setup adds.
The 4096-name bound applies to active or reserved contexts, not past creations.

A registry written in an earlier format is refused rather than converted, with
the complete-store guidance below. There is no fallback from corrupt state to
older metadata.

The production root is `/var/lib/bootwright`. Its Controller subtree follows
the [controller record](contexts/controller-record.md) and exists only after
confirmed setup:

```text
/var/lib/bootwright/
  registry.json
  controller/
    state.json
    bundles/<catalog-digest>/
      sources/
      python/
  media/
    <filename.iso>
    <filename.iso>.json
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
| `registry.json` | One atomic map of context names to their selected inputs and status, plus the Controller descriptor once setup publishes it. |
| `media/` | Host-wide installer media under the [Managed OS media contract](managed-os.md#media-store): each image's exact bytes beside its canonical record, created by the first `media add`, shared read-only by every context and frozen through shared reservations rather than copied. Images and records are published exclusively and atomically; deletion is guarded by the reservation record. |
| `contexts/<name>/context.yaml` | Canonical immutable Context configuration; keeps the authored configuration contract separate from runtime metadata. |
| `desired-state/revisions/<revision-id>/` | Immutable input snapshot, so publication and protected recovery can retain a complete selected revision. Collect unselected revisions only with disposal proof. |
| `manifest.json` | Original input provenance, blob mapping, sizes and hashes needed to verify and replay the snapshot. |
| `file-NNNN` | Exact acquired descriptor or marker bytes; authored filenames never become storage paths. |
| `state/reservation.json` | Durable name-ownership evidence for interrupted creation and guarded deletion. |
| `state/mutation.json` | Lifecycle ownership and operation evidence; missing or unknown evidence prevents destructive cleanup. |
| `state/operations/` | [Reconciliation-owned operation records and logs](state-reconciliation.md#operation-records). Workspace supplies the held area and its publication primitives; it never interprets their content. |
| `state/runs/` | Retained adapter output of [bounded runs](cli/output.md#bounded-run-output), in an area Workspace supplies and never interprets; removed with the context. |
| `state/trust/hosts.json` | The context-managed SSH host-key trust an [SSH session](cli.md#machine-ssh-sessions) proves a Machine against when it declares no `knownHostsRef` and Bootwright did not install it: one public-key record per Machine, and one key per address. Written only by `machine trust` and by an explicitly confirmed first use, published atomically against its exact prior content, and removed with the context. It holds no confidential material. |
| `secrets/` | Context-bound encrypted custody with its own independently versioned [storage contract](secrets.md#local-keyring-v3). |

Every directory is owned by `root:root` with mode `0700`; every file is owned
by `root:root` with mode `0600`, except the catalogued controller executables
the [controller record](contexts/controller-record.md#location-and-file-modes)
allows. All store access runs as root. No environment variable selects another
production root. Isolated test storage is injected at
composition. Reject unsafe existing objects without chmod/chown repair.

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
| Registry | `version` (5), `contexts`, then `controller` when setup has published it |
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
- a context's **lease**, an exclusive lock on that context's directory, taken
  only while the exclusive root lock is held.

| Root lock | Lease | Commands |
| --- | --- | --- |
| shared | none | Every read: `context list` and `current`, `plan`, `status`, `preflight controller`, `secret check`, `list` and `show`, `secret encryption status`, `media list`, SSH-trust and input reads, [bounded runs](cli/output.md#bounded-run-output), and the reads that precede `setup`, `apply` and `destroy`. |
| exclusive | none | Every other command that may publish, such as `context use`, a `context update` that imports no input, `setup`, `media add` and `delete`, and the SSH trust that `machine trust` or a confirmed first use records. |
| exclusive | held | `context init`; a `context update` that imports input; `context delete` of a ready context; `secret set`, `generate` and `delete`, `secret encryption init` and `rotate`, and the Secret binding `apply` and `destroy` take before they execute; and the execution of `apply` and `destroy`. |

A read holds its lock until all stored input or secret-session files have been
consumed, and a mutator holds its locks until it finishes. A command that
cannot take the lock or lease refuses with `lifecycle.lease` and a retry
remedy; no lock or lease is ever waited for or taken over. Revalidate target,
identity and evidence under those locks. Read-only operations perform no
repair, initialization or publication.

A lifecycle operation holds the exclusive root lock and the selected context's
lease for its entire execution, because its host reservations, controller
evidence and operation records must stay coherent while its effects run. Every
other store command, a read included, therefore refuses while one runs.
Narrowing that boundary to the lease alone is
[deferred work](milestones/backlog.md#candidates). Within it, Workspace
supplies the operation area, the mutation-evidence replacement primitive and
the reservation publication; Reconciliation owns what they contain and when
they advance.

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
supplied again and may come from any directory. Unverifiable partial
state refuses; never adopt an unrelated directory. A filesystem create and
recording its identity are not one atomic operation, so not every interruption
is automatically resumable.

Input update writes new immutable blobs/manifests exclusively, flushes files
and containing directories, then atomically replaces and syncs the registry.
The selected revision changes at that registry commit point. Readers observe
a complete old or new input; interrupted unpublished revisions are never
adopted by scanning. Small temporary files for atomic record replacement stay
inside existing directories; they do not introduce a staging tree. A write
that fails after creating its file removes exactly that file, and a mutation
evidence or operation record publication that fails before its rename removes
its temporary file, even when the command was cancelled: a temporary file left
directly in the context's `state/` directory would refuse every later mutation
of the context. Removal happens only while the containing directory still
verifies; otherwise the file stays and the original failure is reported. The
[secret store](secrets.md#local-keyring-v3) instead retains what an
interrupted write leaves for its own recovery, and so does the context
reservation write: until the registry records the context directory's
identity, the reservation alone lets an init retry attribute that directory
and record the identity that deletion requires.

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

All traversal uses held no-follow handles. Inside the root reject mount
crossings, links, hardlinks, special files, wrong ownership/modes and path
substitution. Every publication revalidates location. Supported local
filesystems are ext4, XFS, Btrfs, tmpfs and overlayfs; Linux must provide
`openat2`. Unsupported containment or durability primitives fail closed.

Bounds apply before allocation/traversal: registry 8 MiB; manifest 4 MiB and
32 MiB aggregate referenced manifests; paths 4096 bytes; mutation records
64 KiB; media images 32 GiB each and 64 entries, with records of at most 4 KiB;
and the operator-visible bounds below. Input and Secrets limits additionally
bound their trees.

| Operator-visible bound | Value | Go constant |
| --- | --- | --- |
| Active or reserved context names | 4096 | `maxContexts` in `internal/workspace/contextfs/store.go` |
| Revisions per context | 4096 | `maxRevisions` in `internal/workspace/contextfs/store.go` |
| Retained [controller bundle namespaces](contexts/controller-record.md#bounds) | 16 | `maxControllerBundles` in `internal/workspace/contextfs/controller_bundles_linux_amd64.go` |
| Lifecycle operations one context retains | 4096 | `MaxOperations` in `internal/reconciliation/operationstore/records.go` |
| One lifecycle adapter invocation | 2 hours | `invocationTimeout` in `internal/reconciliation/ansiblerunner/process_linux_amd64.go` |
| One controller Ansible run: setup, its recovery or a controller-stage client installation | 10 minutes | `runTimeout` in `internal/controller/ansiblelocal/runner_linux_amd64.go` |

`TestDocumentedBoundsMatchCode` compares each value with its code.

Missing registry in a nonempty root is
corruption, except that explicit init may finish publication when the root's
only entry is one private `pending-<32 lowercase hexadecimal digits>.json` file
whose bytes are exactly the canonical empty registry. Recovery holds
the exclusive root lock, revalidates the file and sole-entry layout, publishes
with a no-replace rename, verifies the result, and syncs the root before
proceeding. Inspection never performs this recovery; it directs the user to
repeat context init with the original options. Every other missing-registry
shape is left unchanged and reports that the complete store must be restored
from a matching backup or moved aside only after it is verified disposable.

The root admits only the store's own published objects: `registry.json`, the
`contexts` container, the `controller` subtree once the registry declares it,
the `media` container once a `media add` has created it,
and verified private, bounded `pending-<32 lowercase hexadecimal digits>.json`
files left by an interrupted registry replacement; those files are ignored,
never adopted. Any other entry refuses with the same complete-store guidance,
whether or not the registry holds contexts. Bounds never authorize evidence
deletion to make room.

## Format and restore boundary

Under the [pre-1.0 format policy](project.md#design-priorities-and-non-goals),
no stored format is converted. A registry, [secret store](secrets.md#local-keyring-v3),
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

## Permanent deletion

The [Reconciliation guard](state-reconciliation.md#context-mutation-evidence)
owns positive disposal proof. Under the root lock and context lease, refuse
live resources, incomplete operations, retained ownership, unknown/corrupt
evidence, or any required recovery material. A default refusal reports what the
context still owns and directs the operator to `destroy` before deletion.

Only the explicit orphan acknowledgement defined by that guard waives the
disposal verdict, and only over recognized evidence; unreadable evidence and a
live lease refuse under it exactly as they do without it. An acknowledged
deletion abandons the objects rather than removing them: it is otherwise the
same permanent local deletion, it needs `--purge` and ordinary confirmation
like any other, its confirmation names the abandonment, its result reports it,
and it has no remote resource effect. There is no recovery-only mode or
archival.

After proof and ordinary confirmation, durably mark the exact context deleting
before removing any file. Remove only verified objects through bounded held
handles; preserve identifying state until the remaining children are removed.
Permanently remove imported revisions, keyring and all other disposable local
content, then the directory. Sync its parent before removing the active
registry entry. The name stays reserved until completion.

An explicit delete retry resumes the recorded deletion, accepting verified
missing children as completed removal and refusing replacement or unknown
objects. A partially deleted context is never reactivated or reported rolled
back, and its name cannot be recreated until the deletion completes. Recovery
guidance names `context delete --name <name> --purge`.

## Command results and confirmation

Context commands are text-only. Details include name, initialization and
input readiness, and the invoking user's current marker. List sorts by name;
`current --short` emits only the name and LF. Input admission reports counts
and copied files; default creation does not invent compilation counts.
Warnings appear once on stderr. No private paths, payloads or digests appear.

Fresh init and use need no confirmation. Init refuses an already-ready name
and directs the user to update. Input update and deletion require
[ordinary confirmation](cli.md#ordinary-confirmation) unless `--yes`;
equivalent configuration-only update does not. The prompt follows admission and
safeguards while locks remain held, and a refused confirmation publishes
nothing.

Context-backed validate/render and declaration-dependent secret commands
reject a missing input revision with `context update --name <name>
--input-dir <dir>` guidance. Encryption init/status/rotate do not require input.
Effective output and compilation diagnostics retain their existing contracts.
