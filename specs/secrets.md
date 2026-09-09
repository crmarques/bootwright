# Context secret management

Secrets owns runtime custody, materialization, generation, immutable binding and
disclosure. Workspace owns context identity, private paths and atomic publication.
The [Secret API](api/secrets.md) remains declarative. M1c implements local
management without platform, entitlement or lifecycle effects.

## Implementation selection

`secret encryption init --type <type>` requires an explicit safe identifier.
An immutable catalog injected at composition resolves exactly one implementation.
M1c registers `local-keyring`: store `local-v1`, custody `local-keyfile-v1`,
interface/state/config versions `1`, empty closed configurations. Initialization
atomically publishes selection and state. Repeating the same selection is
idempotent; changing type refuses. Subsequent commands resolve the exact persisted
references. Missing, ambiguous or incompatible implementations never fall back.
Rotation does not migrate implementations.

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
automatically import/generate/delete. Final context deletion retains bytes but
makes them unreachable. Same-ID reinitialization permits exact matching
declarations only; human-name reuse under another ID cannot expose prior bytes.
Recovery-only permits check/list/show/encryption status/rotate and exact
continue/destroy binding access, but forbids init/set/generate/delete.

## Local keyring v1

Optional secrets/ extends context v1; secret format v1 is independent. Empty or
absent means uninitialized. Nonempty state without a selector requires recovery.
Unknown/legacy formats refuse; no migration or automatic Workspace repair.
Explicit initialization alone may invoke the selected implementation to recover
an exact, authenticated, never-published initialization transaction. A missing
selector does not authorize accepting arbitrary nonempty state or switching its
implementation. Inspection never performs this recovery.

Canonical selector.json records selector version, context ID, public type,
component references and generation. Immutable encrypted indexes, version/part
blobs, exact 32-byte key files and durable per-key seal ledgers reside beneath
held verified handles. Dirs 0700/files 0600; refuse unsafe ownership/modes,
links/hardlinks/special files, traversal, mount crossings and substitutions.

AES-256-GCM uses random 12-byte nonces and 16-byte tags, capped at 2^20 seals/key.
Durably reserve the full index/parts budget before sealing; abandoned work counts.
Separate canonical index/part AAD domains bind format/algorithm, context,
implementation, generation/key/blob IDs, declaration fingerprint, name/type/
source, logical version and part. Authenticate clear selector fields inside the
index; reconstruct part AAD from stored immutable metadata, never live input.

Identity tombstones use crash-atomic, no-replace publication so partial writes
cannot invalidate readers of an earlier generation. Selector-hidden artifacts
may reserve their final exclusive names first; initialization recovery requires
an attributable intent and never accepts arbitrary partial state by filename.
Sync exclusive immutable writes first. Revalidate expected old selector/context
under the lease; atomic selector replacement is the visibility commit, followed
by parent sync. Outcomes distinguish not committed/committed/uncertain.
Post-rename failure requires inspection before retry, never rollback/fallback.
Rotation reencrypts current/bound versions without changing logical IDs.

Logical collection removes references only. Published ciphertext/indexes/retired
keys remain until a future reader-quiescence protocol proves safe removal.
Deterministic retry may remove only verified never-published temporaries.
Count all artifacts including crash orphans against 256 MiB total physical,
8 MiB index, 64 KiB selector/config and logical ceilings. IDs use 128 random bits
with 16 exclusive collision attempts. Limits refuse, never erase evidence.

Threat exclusions: root, same OS identity, process memory and theft of the full
root with keys. Lost keys need an external complete backup. No recovery slots,
secure erase, automatic expiry, remote KMS, import/export or FIPS claim.

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
