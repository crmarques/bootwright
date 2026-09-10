# Durable contexts

Workspace owns named contexts, Context configuration, immutable input revisions,
per-user selection and local publication. This is a breaking private format for
Linux/amd64: old formats, malformed records, contradictory identity and unsafe
filesystem objects fail with `context.state`. There is no legacy lookup,
migration, archive retention or staging directory.

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

The registry uses private format version 2; earlier versions are refused.
Its active records hold the name, immutable ID, initialization/deletion mode,
selected revision, Environment directory, configured secret-store type and
reserved directory device/inode. A bounded ledger retains only used IDs after
deletion so recreation can never recycle them. It retains no context content.

The production root is `/var/lib/bootwright`. Its complete context layout is:

```text
/var/lib/bootwright/
  registry.json
  contexts/<name>/
    context.yaml
    desired-state/revisions/<revision-id>/
      manifest.json
      file-0000
      ...
    state/
      reservation.json
      mutation.json
    secrets/
      init.json
      selector.json
      identities/
      indexes/
      keys/
      ledgers/
      parts/
```

Every directory is owned by `root:root` with mode `0700`; every file is owned
by `root:root` with mode `0600`. All store access runs as root. No environment
variable selects another production root. Isolated test storage is injected at
composition. Reject unsafe existing objects without chmod/chown repair.

Context and revision IDs use `ctx-` and `rev-` plus 32 lowercase hexadecimal
digits from 128 secure random bits, exclusive reservation and at most 16
collision attempts. File blobs use four-digit manifest indices, never authored
path segments. Records use bounded closed canonical JSON, UTF-8, compact typed
field order, sorted collections and one final LF. Private JSON uses Go's
`encoding/json` escaping. Readers reject noncanonical or contradictory records.
Manifest integrity failures never fall back to external input.

| Record | Required fields in canonical order |
| --- | --- |
| Registry | `version` (2), `identities`, `contexts` |
| Used identity | `id` |
| Context record | `name`, `id`, `environmentDirectory`, `revision`, `mode`, `secretStoreType`, `directoryDevice`, `directoryInode` |
| Reservation | `version` (2), `id`, `name` |
| Input manifest | `version` (2), `id`, `revision`, `inputDirectory`, `environmentDirectory`, `files` |
| Manifest file | `path`, `category` (`yaml` or `marker`), `size`, `sha256` |
| User selection | `version` (1), `name`, `id` |

Collections are present arrays, including empty ones. Identities sort by ID,
contexts by name and manifest entries by relative path. Empty input is encoded
with empty revision and Environment-directory strings; no input is invented.
Reservation, manifest and keyring formats are versioned independently. Mutation
evidence follows the Reconciliation-owned closed record contract.

Readers hold a shared nonblocking lock on the verified, never-replaced root
inode until all stored input or secret-session files have been consumed.
Mutators hold its exclusive lock plus the context lease where applicable;
contention fails safely. Revalidate target, identity and evidence under those
locks. Read-only operations perform no repair, initialization or publication.

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

All traversal uses held no-follow handles. Inside the root reject mount
crossings, links, hardlinks, special files, wrong ownership/modes and path
substitution. Every publication revalidates location. Supported local
filesystems are ext4, XFS, Btrfs, tmpfs and overlayfs; Linux must provide
`openat2`. Unsupported containment or durability primitives fail closed.

Bounds apply before allocation/traversal: registry 8 MiB and 4096 identities or
names; manifest 4 MiB and 32 MiB aggregate referenced manifests; paths 4096
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
A published empty registry permits only its own file and verified private,
bounded `pending-<32 lowercase hexadecimal digits>.json` files from interrupted
registry replacement; those files are ignored, never adopted. Any other entry
refuses with the same complete-store guidance. Bounds never authorize evidence
deletion to make room.

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
