# Durable contexts

Workspace owns named contexts, immutable input revisions, current selection and
their publication. [State reconciliation](state-reconciliation.md#durable-identities-and-private-paths)
owns the state-root and durable identity rules. This format is version `1`, for
Linux/amd64. Unknown versions, malformed or contradictory records, missing
referenced data and unsafe filesystem objects fail with `context.state`.
There is no automatic repair, migration, orphan collection or identity reset.

## Identity and selection

The canonical original directory containing the admitted Environment is the
identity key. Names are lowercase DNS labels, independent of the allocated
`ctx-` identity. Only one active name may refer to an identity. Updating or
recreating a context must retain that Environment directory; moving an input
tree to another identity requires a separate context. Relocation of an existing
identity requires a future explicit migration contract.

A permanent identity mapping survives deletion. Reinitializing its original
directory uses that identity again after positive disposal proof. An interrupted
unpublished reservation is never reused. Names become available after final
deletion, but recovery-only names remain reserved. Successful init selects the
context. Update preserves selection. Use changes only selection. Deleting the
current context clears selection without choosing another name. Recovery-only
archival retains selection. An absent current context fails with `context.state`;
list on an absent store succeeds with an empty list and does not create it.

## Frozen input and provenance

Init and update require one opened directory and compile its entire acquired
input before creating or changing runtime state. Before discovery, resolve the
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

The private layout is:

```text
<state-root>/
  registry.json
  contexts/<context-id>/
    reservation.json
    mutation.json
    revisions/<revision-id>/
      manifest.json
      file-0000
      ...
    archives/<archive-id>.json
```

Revision and archive IDs are respectively `rev-` and `arc-` followed by 32
lowercase hexadecimal digits from 128 secure random bits, with the same
exclusive reservation and 16-attempt collision rule as context IDs. File blobs
use four-digit manifest indices, never authored path segments. Registry
publication uses an exclusively created `pending-<32-hex>.json` temporary.
Unreferenced temporaries and reserved IDs are never adopted or reused.

Workspace records use UTF-8, compact canonical JSON and exactly one final LF.
Object fields follow the owning typed record order below; registry identities
sort by Environment directory, contexts by name, and manifest files by relative path.
Private JSON strings follow the pinned Go `encoding/json` spelling: escape
quotes, backslashes and control characters; also escape `<`, `>` and `&` as
`\u003c`, `\u003e` and `\u0026`, and U+2028/U+2029 as `\u2028`/`\u2029`. Other
valid Unicode is UTF-8. This private format is separate from CLI JSON output.
Workspace record readers refuse noncanonical encodings as well as unsupported
semantics. Reconciliation mutation evidence is closed JSON with insignificant
whitespace and field order; it rejects duplicate, unknown, missing or incorrectly
typed fields.

| Record | Required fields, in order |
| --- | --- |
| Registry | `version`, `current`, `identities`, `contexts` |
| Identity mapping | `environmentDirectory`, `id` |
| Active context | `name`, `id`, `environmentDirectory`, `revision`, `mode` |
| Reservation | `version`, `id`, `environmentDirectory` |
| Input manifest | `version`, `id`, `revision`, `inputDirectory`, `environmentDirectory`, `files` |
| Manifest file | `path`, `category` (`yaml` or `marker`), `size`, `sha256` (64 lowercase hexadecimal digits) |
| Archive | `version`, `outcome` (`deleted` or `recoveryOnly`), `record` (complete context record), `mutation` (guard evidence) |

All collections are present arrays, including empty ones. Every reservation,
manifest and archive is versioned independently. Mutation evidence has its
Reconciliation-owned version and is interpreted under the guard rules below.

The owner-only state root contains a canonical JSON registry and exclusively
reserved context directories. The registry contains format version, current
name, permanent identity mappings and active records (name, ID, Environment
directory, revision and active/recovery-only mode). Names and identities are
unique, arrays have canonical ordering, and every selected record must agree
with its reservation and manifest. JSON is closed and rejects duplicates,
nulls, coercion and unknown fields. All persisted records are untrusted.

Mutators exclusively lock the verified, never-replaced root directory inode.
The lock is nonblocking and contention fails safely. Revalidate registry,
identity, target and mutation evidence while holding the lock; retain it through
confirmation and publication. A separate nonblocking context lease covers the
Reconciliation guard and publication, preventing a lifecycle mutator from
appearing after a stale safety check. Readers acquire no lock or lease and
perform no writes, repairs, migrations or lifecycle-state reads.

Write new immutable revision files and manifests exclusively with mode `0600`
inside `0700` directories, flush files and containing directories, then publish
one complete registry by atomic replacement and parent-directory sync. That
registry replacement is the visibility commit point. Init's name, identity,
revision and current selection publish together; an update exposes either the
complete old or complete new revision. A post-rename sync failure reports
uncertain durability and never claims rollback. Repeating the command must
first inspect the published registry.

Root selection follows the existing precedence. The fallback account home is
read from the local OS account database (`/etc/passwd`, at most 1 MiB) using the
invoking UID, without consulting `HOME`, NSS, a process or the network. If the
account has no unambiguous absolute local home, fail and request an explicit
absolute `XDG_STATE_HOME`. Never fall back after selecting an unsafe root.
All path traversal uses held no-follow directory/file handles. Inside the
selected root, reject mount crossings, links, hardlinked files, special files,
wrong owners, permissive modes and substitution. Every publication revalidates
its held location. Runtime state remains outside every admitted input root and
the selected Environment directory.

Bounds are checked before allocation or traversal: registry 8 MiB and 4096
identities/active names and 4096 retained context reservations, including
unpublished/orphan directories; manifest 4 MiB, with 32 MiB total referenced
manifest bytes per registry; path 4096 bytes; mutation or archive record 64 KiB; at most
4096 revisions and 4096 archive records per identity. Input limits additionally
bound manifest file counts and retained bytes. Exceeding a limit refuses the
operation; it never deletes old evidence to make room.

Before registry publication, interruption leaves only unreferenced owned
reservations/revisions; existing selection is unchanged. After publication,
the complete selected revision remains readable. Missing initial registry in
a nonempty root is corruption, not a new store. Supported local filesystem
types are ext4, XFS, Btrfs, tmpfs and overlayfs; network and other filesystem
types refuse. Linux must provide `openat2` (Linux 5.6 or later). Unsupported filesystem/kernel durability or containment
primitives fail closed. Cleanup never recursively removes an unverified path
or guesses which interrupted evidence is disposable.

## Mutation evidence and archival

The [Reconciliation mutation guard](state-reconciliation.md#context-mutation-evidence)
owns operation/ownership evidence and permitted dispositions. Workspace holds
its lease through the guard, confirmation and publication. Unknown entries in
the context directory refuse mutation; its version-1 layout permits only the
reservation, mutation record, revisions and archives. Input inspection ignores
lifecycle entries and never opens them.

Final deletion first durably archives the context record and its immutable
revision references, then removes only its active registry entry. Published
revisions, mutation evidence and permanent reservation remain available in the
durable archive. Recovery-only archival likewise records the complete retained
identity and revision before publishing the mode change. Neither form performs
remote effects, deletes Secret bytes or discards recovery material. Retention
and physical archive removal require a separately defined recovery workflow.

## Command results and confirmation

Context commands are text-only. Details contain name, durable identity, mode
and current marker; list is sorted by name. `current --short` emits only the
name and LF. Init/update print compilation counts and copied file count (YAML
candidates plus markers) on stdout. Admission warnings print once on stderr.
No private store paths, payloads or digests appear in presentation.

Fresh init and use need no confirmation. Recreation requires explicit `--yes`.
Update and deletion require ordinary confirmation unless `--yes` was supplied.
The prompt follows admission, target resolution and independent safeguards,
while mutation locks remain held. A noninteractive input, declined answer,
cancellation, read/write failure or unsafe answer refuses publication. Only
`y` or `yes` (case-insensitive, surrounding whitespace ignored) accepts; one
answer of at most 64 bytes including LF is read, without read-ahead. Confirmation
grants no additional safety authority.

Context-backed validate keeps the explicit-input validation report contract.
Effective text is canonical YAML with a final LF and empty stderr. Effective
JSON uses `result: {counts, effectiveState}`, where `effectiveState` is the
canonical ordered array of complete objects; successful diagnostics and logs
are empty. Failures have null result and retain typed diagnostics.
