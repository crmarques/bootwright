# Context secret management

Secrets owns runtime custody, materialization, generation, immutable binding and
disclosure. Workspace owns context identity, private paths and atomic publication.
The [Secret API](api/secrets.md) remains declarative. This contract covers local
management without platform, entitlement or lifecycle effects.

## Implementation selection

The standalone [Context configuration](contexts.md#context-configuration)
selects `spec.secretStore.type`, defaulting to `local-keyring`. An immutable
catalog injected at composition resolves exactly one implementation. Local
keyring persists the immutable backend-format identity `local-keyring-v4`.
The catalog maps that identity to the exact implementation and its public
component status (`local-v4`, custody `local-keyfile-v1`). Interface/configuration
metadata belongs to the catalog and is not repeated in persisted records. Context creation initializes
that implementation through a transaction-scoped Workspace area before ready
publication, without requiring Environment input or generating Secret values.

`secret encryption init` consumes that configuration and is idempotent; it has
no `--type` override. It also completes interrupted cleanup. Changing the
initialized type refuses. Subsequent commands
resolve exact persisted references; absent, ambiguous or incompatible
implementations never fall back. Rotation does not migrate implementations.
Encryption initialization/status/rotation require context identity and
configuration, but no desired-state revision. Declaration-dependent commands
require an imported revision and provide `context update --input-dir` guidance
when it is absent.

Store access consumes a resolver interface, so the catalog itself is replaceable.
Every resolver preserves exact selection, returns a non-nil implementation on
success, and refuses missing or incompatible identities before acquisition or
effects. Type listings are independent snapshots. Custody and encryption each
own the narrow access interface their commands need; neither caller depends on
the other's operations or a concrete access service. Material acquisition owns
its input-reading port; the CLI supplies invocation input at composition.

Core services use semantic implementation/session interfaces for inspection,
publication, reading, binding and rotation, with no concrete imports or identity
branches. Implementations declare session requirements, acquired through an
injected confidential capability after target authorization. Capabilities are
invocation-owned, non-serializable and closed with best-effort memory clearing.
Local keyring requires no input. A second test implementation qualifies this
extension seam. No dynamic plugins, executable paths, ambient discovery,
mutable global registry or generic option maps are permitted.

## Acquisition and commands

Commands resolve one coherent context identity/revision/mode, frozen input and
compiled declaration snapshot. Mutators revalidate this token under the root
lock and context lease. `set` is the only command that opens an operator path:
bind, `check`, `show` and the bounded machine commands read only keyring
versions.

`set --name` accepts only a declared contextStore Secret and these exact flags:

| Type | Input |
| --- | --- |
| opaque, token, dockerConfigJson | One of `--value-file` or `--value-stdin` |
| usernamePassword | `--username` and one of `--password-file` or `--password-stdin` |
| caBundle | `--certificate-file` |
| tlsCertificate | `--certificate-file` and `--private-key-file` |
| sshKeyPair | `--private-key-file`, optional `--public-key-file` |

Reject inapplicable flags even when explicitly empty or false; the former source
flags have no aliases. Fresh set does not confirm. Replacement confirms under
the lease unless `--yes`; stdin
replacement requires yes before any read. Equal material creates no new version.
Multipart versions and generation batches publish atomically.

`generate [--name] [--renew]` selects every effective generated declaration or
one named generated declaration. Unknown/non-generated names fail before writes.
Without renew, create missing/stale values; renew replaces the selected set with
no extra prompt. `check` validates all declarations and their current keyring
material. `list` reads metadata only; an uninitialized store is empty.
`delete --name` removes an active mapping, including orphans, while retaining
bound versions; absence is unchanged, existing deletion confirms unless yes.

`show --name --part` requires a part and uses only the current declared source.
Parts are value for opaque/token/Docker JSON; username/password; certificate for
CA; certificate/private-key for TLS; private-key/public-key for SSH. Generated
CA also retains explicitly revealable private-key; ordinary CA consumers receive
only certificates. Show never reveals superseded/bound versions or falls back.
Validate/authenticate the complete part before exact raw stdout, no added LF or
JSON. A partial writer failure emits no secondary diagnostic.

Bounds: 1 MiB/part, 2 MiB/version, 64 MiB referenced material, 4096 versions and
4096 bindings. Opaque preserves exact bytes. Tokens/passwords are nonempty UTF-8
lines, strip one optional final LF and reject embedded CR/LF/NUL. Docker
JSON rejects duplicates/trailing data and requires a nonempty auths object.
The limits apply to decoded, normalized parts. A token/password transport may
carry one extra final LF; an extra non-LF byte still exceeds the part limit.
Preflight transport bounds before reads and normalized bounds before material
copies; this does not enlarge logical material limits.
CA PEM contains CA certificates; TLS validates key agreement and server-auth
suitability. SSH accepts one unencrypted Ed25519, RSA >=3072 or API-supported
ECDSA private key, derives/verifies public material, and rejects DSA, trailing
keys, unsupported curves and mismatches. Short of its final line feeds, the
whole public key line, its edges and field separators included, holds no
character a [generated comment](api/secrets.md#generated-source) refuses: its
fields are separated by spaces, any line ending it carries is a bare line
feed, and spaces at its edges pass. A tab between or around its fields, or a
CRLF line ending, therefore refuses at import, naming the line, for every
`sshKeyPair` Secret whatever consumes it: the fleet key, Machine host keys and
cluster node keys alike. A key already stored that way must be set again;
`secret check` names it as invalid.
Whole-version file reads hold verified no-follow handles and revalidate
stability. Each file, and every directory
above it, is opened under the invoking account's credentials, and root
re-proves type, owner, link count, permissions and stability on each
descriptor it receives. A permission denial names the authored path and who
was denied: the invoking account, or root itself when the invoking account is
root. Root cannot read a file in a root-squashed network home, so that refusal
says to copy the file to a local directory and name the copy.

Generation uses OS entropy. Tokens use API entropy length; passwords 32 bytes;
both use unpadded base64url. TLS/CA uses P-256, PKCS#8 PEM, self-signed X.509,
random nonzero 128-bit serial, injected UTC-second clock and declared validity.
CA has signing constraints; TLS server-auth usage. SSH follows API type with
RSA-3072 and OpenSSH public serialization. Entropy, time and input are injectable
where the platform API accepts them. A typed adapter-local crypto-operation port
qualifies RSA/ECDSA key-generation and certificate-creation failures without
changing core material services. Production uses standard-library OS entropy;
do not rely on Go's ignored key-generation Reader arguments or a process-global
compatibility setting for this seam.

SSH parsing and public-key codecs use the selected `golang.org/x/crypto` SSH
package at the version locked in `go.mod`; its declared module graph is reviewed
with the selected build graph. The standard library owns AES-GCM, X.509, key
generation and entropy. Do not import unrelated x/crypto packages.

## Immutable binding and contexts

Bind/Reopen/Release operate on whole versions with opaque IDs. Bind pins the
current keyring version of each declaration, and a store refuses a binding
input that names no version it holds. Reopen reads only the versions a binding
holds, including one an earlier build froze from the retired
[file source](api/secrets.md#file-source), and never reads a source path.
Replacement/deletion/rotation preserve bound versions; release drops only its
references.
[State reconciliation](state-reconciliation.md#plan-and-execution) is the
lifecycle consumer: it binds every consumed declaration before operation
registration, reopens bound material for each attempt, and releases the binding
only after a completed destroy has removed the effects that needed it. Each
adapter run is lent, of that material, only the parts its material files name,
as a view that copies nothing, and no Secret those files do not name. The
binding holds whole versions, so an artifact server's serving certificate
Secret is bound with the key the server's own block writes, and a consumer that
verifies the server against it, under the
[private consumer publication contract](infrastructure-services.md#private-consumer-publication),
is lent its certificate alone. A
registration that provably did not happen releases the binding it created, and
one that may have happened keeps it, because the operation it may have
registered reopens it for every later attempt and its removal. A binding no
operation names, which an interrupted registration leaves, is released under
the [collection rule](state-reconciliation.md#context-mutation-evidence): after
the context's next fresh registration, a removal's finalization, or a destroy's
release of what the interrupted registration left, and only when read before
that invocation began and never kept by it. Beside a context at rest, with
pristine evidence and no reservation, the destroy that
[settles](state-reconciliation.md#lifecycle-unit) there releases it. A
consumer may list a context's binding identities for this; the listing unlocks
nothing, reveals no material or version, and a store never initialized lists
none. A binding names no consumer, so a bounded consumer's transient binding
read in such a listing before it was reopened may be released with them. A
bounded consumer whose reopen fails while its binding is no longer listed binds
again, at most three bindings in all, and releases each it made; one whose
binding is still listed, or whose listing fails, reports the failure without
reading any material. Binding and release occur outside the lifecycle
operation's own store transaction, because acquisition holds the same store
lock. [Produced material](#produced-material) is the reverse: it is published
and withdrawn only inside that transaction, through the secret area it lends.
A binding an operation froze that can no longer be reopened, because the
keyring no longer lists it or cannot read its material, is never re-bound from
current declarations or replaced by other material; the lifecycle consumer
[refuses](state-reconciliation.md#continuation-and-removal) the operation that
names it.

Canonical non-secret declaration fingerprints cover type/source/parameters and
provenance. Changed declarations make retained values stale/orphaned, never
automatically import/generate/delete. Context deletion permanently removes the
verified keyring under [permanent deletion](contexts.md#permanent-deletion),
including an orphan-acknowledged deletion, which abandons the context's realized
objects but still removes its keyring. Recreating a name starts with a new
keyring and never exposes prior material.

## Produced material

Produced material is confidential output a lifecycle block's proved effect
leaves, such as the administrator kubeconfig a
[completed installation](container-clusters.md#installation) writes.
It is keyed by the block that captured it and the output's name, never by a
Secret declaration. Only the lifecycle holds and withdraws it: the engine
publishes a block's outputs in one publication, and withdraws every entry of
the context in one publication, through the secret area the
[Workspace lends its transaction](contexts.md#storage-locking-and-publication)
([capture and withdrawal](state-reconciliation.md#produced-material-custody)).
A recapture of equal bytes publishes nothing; different bytes replace the
entry's version, and the block's entries of other names stay. A store never
initialized refuses a capture with `secret.store.uninitialized` and holds
nothing to withdraw. No secret command lists, checks, reveals, sets, generates,
deletes or matches it to a declaration of the same name; `cluster kubeconfig`
reveals an installation's entry under the [explicit sensitive-output
boundary](cli.md#administrator-access-export). Its parts count in `materialParts`,
rotation re-encrypts it with every other version, and it is removed with the
keyring.

## Local keyring v4

The `secrets/` subtree is initialized during context creation, independently
of the enclosing registry format. Empty or absent means uninitialized.
Nonempty state without `store.json` resumes, through `secret encryption init`,
only when everything in it is attributable initialization evidence or an
interrupted temporary described below; inspection never repairs it.

The normal layout has one atomically replaced `store.json`, immutable
`parts/<blob-id>.enc`, immutable 32-byte `keys/<key-id>.key`, independently
updated `keys/<key-id>.usage.json`, and `identities/<id>.json` reservations.
`init.json` exists only while initialization or its cleanup is incomplete. No
permanent initialization-key dependency exists. Dirs are 0700/files 0600 beneath held
verified handles; refuse unsafe ownership/modes, links, hardlinks, special
files, traversal, mount crossings and substitutions.

| Path | Why it remains separate |
| --- | --- |
| `store.json` | Atomic authenticated metadata and exact backend selection; replaces the old selector and index files. |
| `parts/*.enc` | Immutable encrypted material, allowing metadata changes without rewriting every secret. |
| `keys/*.key` | Required local decryption keys. Remove a retired key once retained material no longer references it. |
| `keys/*.usage.json` | Durable seal reservations, including failed attempts; committing usage separately prevents retries from forgetting encryption already performed. |
| `identities/*.json` | Historical version/binding ID reservations. Retain them after logical deletion to prevent ID reuse. |
| `init.json` | Temporary authenticated recovery evidence; remove after successful publication and cleanup. |

The shared canonical `store.json` envelope contains `version` (3), `context`,
`backend`, `generation`, then backend-owned `payload`. The context member is
the owning [context's name, which is its identity](contexts.md#identity-and-selection). Local payload is closed
JSON containing `keyId`, `nonce` and `ciphertext`. Its authenticated header
selects the exact backend and binds metadata to the expected context. Resolve
only catalog implementations; backend selection cannot authorize acquisition
or effects beyond the current command. Unsupported IDs refuse before session
acquisition. The encrypted metadata contains `activeKey`, `keys`, `versions`,
`current`, `bindings` and `produced`.

Each key record stores `id` and committed `seals`; presentation derives its
active/retired state. Each immutable version stores `id`, a `sequence` of at
least one, a declaration summary (`name`, `type`, `source`, `fingerprint`), and
parts (`part`, `blobId`, `keyId`, `generation`, `size`). A produced version's
summary is `name` (its entry's), `type` `opaque`, `source` `produced` and a
`fingerprint` that is the hexadecimal SHA-256 of the canonical JSON
`{"domain":"bootwright.secret.produced.v4","block":…,"name":…}`; its
`sequence` is 1 and it holds one `value` part of one byte up to the part bound.
`produced` is always present, `[]` when empty, and holds `block`, `name` and
`version` entries sorted by block and then name, unique on that pair. A
produced version is reached through exactly the one entry whose block and name
its fingerprint covers, never through a current mapping or a binding.

`sequence` is the version's ordinal within its own secret, counting from one.
A new version takes one more than the highest ordinal that secret has ever
reached, so deleting a version never renumbers the ones that remain and an
ordinal keeps naming the same material for as long as it exists. The ordinal is
how a person names a version: human output shows `v<sequence>` and never the
identifier. The identifier remains the only durable reference, so current
mappings, bindings, and every JSON result continue to carry it. `sequence` is
required and at least 1; a version without it is corrupt and refuses with
`secret.store.corrupt`. A produced version takes no ordinal from a Secret of
the same name, whose series counts its own versions alone. Compute the
declaration fingerprint from full canonical
parameters and provenance at acquisition, then authenticate the summary.
Original paths, source fields and generation options are not copied into each
version. Current mappings contain `name` and `version`; bindings contain `id`
and a sorted `versions` array. They retain whole versions for exact reopening.

AES-256-GCM uses random 12-byte nonces and 16-byte tags, capped at 2^20 seals/key.
Durably reserve the full metadata/parts budget before sealing; abandoned work
counts. Usage records contain `formatVersion`, `keyId`, `seals`, and `mac`.
Their update is independent of metadata publication, and their authenticated
count must meet the metadata's committed floor. Separate canonical metadata/part
AAD domains bind format/algorithm, context, backend, generation/key/blob IDs,
declaration fingerprint, name/type/source, logical version and part. Reconstruct
part AAD from its original immutable metadata, never current input or the newest
metadata generation. The metadata header is authenticated without storing a
second full selector inside its ciphertext.

Identity reservations use crash-atomic no-replace publication and retain
`formatVersion`, `context`, and `id`. They preserve issued version/binding IDs
after logical removal. Initialization uses an attributable,
authenticated intent recording its context, backend, attempted key/generation
identities and MAC; never adopt arbitrary partial state by filename. A
publication stage (`pending-` and 32 lowercase hexadecimal digits) in the
subtree's root or in `keys/` whose bytes are not one complete JSON value is a
temporary a killed write left: initialization recovery skips it, and cleanup
after the next durable publication removes it. A stage that holds a complete
JSON value is attributed as initialization evidence or refused.

Sync new immutable material first, then revalidate the expected metadata inode,
bytes and context under the lease. Atomic `store.json` replacement is the
visibility commit, followed by parent sync. Readers observe a complete old or
new metadata snapshot. Outcomes distinguish not committed, committed and
uncertain; post-rename failure requires inspection before retry and never
falls back. Rotation reencrypts current, bound and produced material under a
fresh key without changing logical IDs. Current metadata references only
required encryption keys, so retired keys cannot prevent access once no retained material needs them.

The root shared lock protects the complete reader callback; its exclusive
writer lock supplies cleanup quiescence. Before a new valid publication and
after a durable publication, collect verified unreferenced material, retired
keys/ledgers, completed initialization intent and known interrupted temporaries.
First establish the exact selected metadata's file and parent durability, then
remove only previously observed objects after revalidating their identities,
parents and context. Sync every affected directory. Preserve current/bound
versions, required recovery evidence and identity reservations. A failed or
uncertain metadata publication never authorizes postpublication cleanup.
Cleanup failure after commit reports the committed outcome and directs an
explicit `secret encryption init` retry; inspection reports retained artifacts
without writing. Cleanup can run at the physical ceiling without first creating
another file. Publication still reserves room for new material and metadata.

Count all artifacts including crash orphans against 256 MiB physical storage
and 32768 entries. Bound both the encoded metadata file and decrypted metadata
at 8 MiB, and initialization/usage records at 64 KiB. Encoding overhead counts
toward the physical limit. Logical material/version/binding limits also apply.
Secret IDs use 128 random bits with 16 exclusive collision attempts. Limit
failure never authorizes removal of referenced material or identity evidence.

There is no conversion from an earlier keyring format. A `secrets/` subtree
that is nonempty, holds no `store.json` this build can authenticate, and holds
anything besides attributable initialization evidence and interrupted
temporaries refuses with `secret.store.corrupt` before any session is acquired,
and the remedy is a new context. A `store.json` that names a backend this
build's catalog lacks, `local-keyring-v3` included, refuses every access,
`secret encryption init` included, with `secret.store.implementation` before
any session, naming that backend and the remedy: destroy the context's effects
with the Bootwright build that created it, then `bootwright context delete
--name <context> --purge` and create the context again.

Threat exclusions remain root, the same OS identity, process memory and theft
of the full store with its keys. Keys share the local filesystem custody
boundary with ciphertext. Lost keys need a complete external backup; ordinary
copy restoration follows the [restore boundary](contexts.md#format-and-restore-boundary).
There are no recovery slots, secure erase, automatic expiry, remote KMS,
import/export or FIPS claims.

## Results and failures

Check/list JSON has context (name/mode) and name-sorted secrets. Check rows:
name/type/source/parts/status/nullable version; statuses available/missing/stale/
invalid. A complete negative check keeps its result and returns 1
with safe diagnostics. List rows: name/type/source/parts/state/nullable
currentVersion/boundVersions; states current/stale/orphaned. A version an
earlier build froze from a file source is never listed.

Encryption status fields: initialized/nullable implementation/nullable activeKey/
keys/items. Implementation exposes type/component references/state, no paths or
config. Keys expose id/state/seals. Items reports currentVersions/boundVersions/
materialParts/retainedArtifacts/cleanupRequired. Uninitialized has null IDs,
empty keys and zero counts. Status never prompts/unlocks/initializes/writes.

Failures use the `secret.*` codes of the
[diagnostic taxonomy](cli/output.md#diagnostic-taxonomy-and-order). Existing CLI
envelope/exit/cancellation rules apply. Normal results, logs, diagnostics and
evidence never contain material or material digests.
