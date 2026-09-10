# Context secret management

Secrets owns runtime custody, materialization, generation, immutable binding and
disclosure. Workspace owns context identity, private paths and atomic publication.
The [Secret API](api/secrets.md) remains declarative. M1c implements local
management without platform, entitlement or lifecycle effects.

## Implementation selection

The standalone [Context configuration](contexts.md#context-configuration)
selects `spec.secretStore.type`, defaulting to `local-keyring`. An immutable
catalog injected at composition resolves exactly one implementation. Local
keyring persists the immutable backend-format identity `local-keyring-v2`.
The catalog maps that identity to the exact implementation and its public
component status (`local-v2`, custody `local-keyfile-v1`). Interface/configuration
metadata belongs to the catalog and is not repeated in persisted records. Context creation initializes
that implementation through a transaction-scoped Workspace area before ready
publication, without requiring Environment input or generating Secret values.

`secret encryption init` consumes that configuration and is idempotent; it has
no `--type` override. It also completes interrupted cleanup and explicitly
upgrades supported local-keyring v1 state. Changing the initialized type refuses. Subsequent commands
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
extension seam. Production passphrase stores, brokers and KDFs are deferred.
No dynamic plugins, executable paths, ambient discovery, mutable global registry
or generic option maps are permitted.

## Acquisition and commands

Commands resolve one coherent context identity/revision/mode, frozen input and
compiled declaration snapshot. Relative file paths retain compiler provenance.
Mutators revalidate this token under the root lock and context lease.

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
no extra prompt. `check` validates all declarations and material, including live
bounded file reads. `list` reads metadata only; an uninitialized store is empty.
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
lines, strip one optional final LF and reject embedded CR/LF/NUL. File
usernamePassword is closed JSON with username/password string fields. Docker
JSON rejects duplicates/trailing data and requires a nonempty auths object.
The limits apply to decoded, normalized parts. A token/password transport may
carry one extra final LF; an extra non-LF byte still exceeds the part limit.
Closed usernamePassword JSON has a transport ceiling of 12 MiB + 116 bytes,
covering maximal JSON escaping of both bounded parts and member names, the
optional password LF, syntax and one trailing LF. All whitespace counts toward
this ceiling. Preflight transport bounds before reads and normalized bounds
before material copies; this does not enlarge logical material limits.
CA PEM contains CA certificates; TLS validates key agreement and server-auth
suitability. SSH accepts one unencrypted Ed25519, RSA >=3072 or API-supported
ECDSA private key, derives/verifies public material, and rejects DSA, trailing
keys, unsupported curves and mismatches. Whole-version file reads hold verified
no-follow handles and revalidate stability.

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

SSH parsing and public-key codecs use `golang.org/x/crypto v0.57.0`; its
[declared module graph](https://proxy.golang.org/golang.org/x/crypto/@v/v0.57.0.mod)
is reviewed with the selected build graph. The standard library owns AES-GCM,
X.509, key generation and entropy. Do not import unrelated x/crypto packages.

## Immutable binding and contexts

Bind/Reopen/Release operate on whole versions with opaque IDs. File binding
freezes one validated read without changing source or importing a named entry.
Reopen never rereads the source. Replacement/deletion/rotation preserve bound
versions; release drops only its references. M1d owns the future lifecycle port.

Canonical non-secret declaration fingerprints cover type/source/parameters and
provenance. Changed declarations make retained values stale/orphaned, never
automatically import/generate/delete. Final context deletion permanently removes
the verified keyring only after positive disposal proof. Human-name reuse
allocates a fresh identity and never exposes prior material. Protected contexts cannot be deleted or abandoned.

## Local keyring v2

The `secrets/` subtree is initialized during context creation, independently
of the enclosing registry format. Empty or absent means uninitialized.
Nonempty state without `store.json` requires explicit initialization recovery
or the supported upgrade; inspection never repairs it. Unknown formats refuse.

The normal layout has one atomically replaced `store.json`, immutable
`parts/<blob-id>.enc`, immutable 32-byte `keys/<key-id>.key`, independently
updated `keys/<key-id>.usage.json`, and `identities/<id>.json` reservations.
`init.json` exists only while initialization or its cleanup is incomplete;
`upgrade.json` serves the same role for version-1 conversion. No permanent
initialization-key dependency exists. Dirs are 0700/files 0600 beneath held
verified handles; refuse unsafe ownership/modes, links, hardlinks, special
files, traversal, mount crossings and substitutions.

| Path | Why it remains separate |
| --- | --- |
| `store.json` | Atomic authenticated metadata and exact backend selection; replaces the old selector and index files. |
| `parts/*.enc` | Immutable encrypted material, allowing metadata changes without rewriting every secret. |
| `keys/*.key` | Required local decryption keys. Remove a retired key once retained material no longer references it. |
| `keys/*.usage.json` | Durable seal reservations, including failed attempts; committing usage separately prevents retries from forgetting encryption already performed. |
| `identities/*.json` | Historical version/binding ID reservations. Retain them after logical deletion to prevent ID reuse. |
| `init.json`, `upgrade.json` | Temporary authenticated recovery evidence; remove after successful publication and cleanup. |

The shared canonical `store.json` envelope contains `version` (2), `contextId`,
`backend`, `generation`, then backend-owned `payload`. Local payload is closed
JSON containing `keyId`, `nonce` and `ciphertext`. Its authenticated header
selects the exact backend and binds metadata to the expected context. Resolve
only catalog implementations; backend selection cannot authorize acquisition
or effects beyond the current command. Unsupported IDs refuse before session
acquisition. The encrypted metadata contains `activeKey`, `keys`, `versions`,
`current` and `bindings`; a true `legacy` flag is present only when the
published upgrade authorizes removal of remaining version-1 artifacts.

Each key record stores `id` and committed `seals`; presentation derives its
active/retired state. Each immutable version stores `id`, a declaration summary
(`name`, `type`, `source`, `fingerprint`), and parts (`part`, `blobId`, `keyId`,
`generation`, `size`). Compute the declaration fingerprint from full canonical
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
`formatVersion`, `contextId`, and `id`. They preserve issued version/binding IDs
after logical removal. Their allocation policy remains separate from the
registry's namespace/counter scheme. Initialization uses an attributable,
authenticated intent recording its context, backend, attempted key/generation
identities and MAC; never adopt arbitrary partial state by filename.

Sync new immutable material first, then revalidate the expected metadata inode,
bytes and context under the lease. Atomic `store.json` replacement is the
visibility commit, followed by parent sync. Readers observe a complete old or
new metadata snapshot. Outcomes distinguish not committed, committed and
uncertain; post-rename failure requires inspection before retry and never
falls back. Rotation reencrypts current/bound material under a fresh key without
changing logical IDs. Current metadata references only required encryption
keys, so retired keys cannot prevent access once no retained material needs them.

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

Only `secret encryption init` upgrades a complete authenticated local-keyring
v1 store. Authenticate its selector, encrypted index, declaration fingerprints,
parts, key usage and initialization evidence before conversion. Preserve
current/bound logical IDs, mappings, bindings and historical identity
reservations. Write an attributable upgrade intent before new artifacts;
reencrypt retained material with a fresh key and durably reserve its seals.
The single new `store.json` commit selects v2. Before that commit, leave all
version-1 files unchanged and permit only exact attributable retry. After it,
remove verified obsolete version-1 artifacts using the normal cleanup boundary.
An interrupted cleanup must not depend on a retired version-1 key. Never import
changed source files or regenerate logical material during upgrade.

Before a retry allocates another key, authenticate the original selection and
upgrade intent, establish their durability, and collect only attributed
unpublished conversion artifacts. Revalidate both guards and context identity
at every removal. A new attempt replaces the cleaned attempt's intent rather
than accumulating a lifetime history. Preflight the conversion's peak encoded
bytes and entries before new publication effects; a source without enough
headroom refuses while retaining its complete version-1 state.

Threat exclusions remain root, the same OS identity, process memory and theft
of the full store with its keys. Keys share the local filesystem custody
boundary with ciphertext. Lost keys need a complete external backup; ordinary
copy restoration follows the [restore boundary](contexts.md#upgrade-and-restore-boundary).
There are no recovery slots, secure erase, automatic expiry, remote KMS,
import/export or FIPS claims.

## Results and failures

Check/list JSON has context (name/id/mode) and name-sorted secrets. Check rows:
name/type/source/parts/status/nullable version; statuses available/missing/stale/
invalid/unreadable. A complete negative check keeps its result and returns 1
with safe diagnostics. List rows: name/type/source/parts/state/nullable
currentVersion/boundVersions; states current/stale/orphaned. Hidden file bindings
are never listed as imports.

Encryption status fields: initialized/nullable implementation/nullable activeKey/
keys/items. Implementation exposes type/component references/state, no paths or
config. Keys expose id/state/seals. Items reports currentVersions/boundVersions/
materialParts/retainedArtifacts/cleanupRequired. Uninitialized has null IDs,
empty keys and zero counts. Status never prompts/unlocks/initializes/writes.

Failures: secret.declaration/source/part/input and secret.store.uninitialized/
implementation/key-unavailable/conflict/corrupt/crypto/limit. Existing CLI
envelope/exit/cancellation rules apply. Normal results, logs, diagnostics and
evidence never contain material or material digests.
