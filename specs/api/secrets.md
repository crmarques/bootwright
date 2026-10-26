# Secrets and entitlements

`Secret` and `Entitlement` carry names, types, source/generation declarations,
subscription and license intent, never Secret values.
[The compiler boundary](../api.md#compiler-boundary) applies; materialization,
generation, registration, login and license acceptance need effectful consumers.
[Environment kind defaults](environment.md#kind-defaults) may supply only the
fields permitted by that closed contract; this page owns their type-scoped
validation and built-in defaults.

## Secret

`Secret.spec` emits fields in this order:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `type` | string | yes | — | `opaque`, `token`, `usernamePassword`, `dockerConfigJson`, `caBundle`, `tlsCertificate`, or `sshKeyPair`. |
| `source` | object | no | `contextStore` | Closed source union described below. |

The type is explicit; Bootwright never infers it from a consumer or source.
It fixes the legal source fields and generated parameters. Every consumer
reference resolves by `metadata.name` and must accept the declared type.

### Source union

`spec.source` contains fields in the order `contextStore` then `generated`;
the retired `file` arm is refused, never emitted. At most one arm may be
present. An omitted source first receives any applicable Environment kind
default; if it remains omitted, it selects context storage and stays omitted canonically. An authored `source: {}` blocks
Environment source inheritance and explicitly selects context storage;
normalization emits `source: {contextStore: {}}` so internal effective-state
serialization preserves that choice. An authored `contextStore: {}` likewise
remains explicit.
`contextStore` is an empty object and rejects parameters.

| Arm | Meaning |
| --- | --- |
| `contextStore: {}` | Material lives only in confidential per-context storage. This is the semantic default and is legal for every type. |
| `file` | Retired: compilation refuses it, naming its [replacement](#file-source). |
| `generated` | Material is minted from type-scoped parameters; legal only for `token`, `usernamePassword`, `tlsCertificate`, `caBundle`, and `sshKeyPair`. |

Each Secret owns its source. Supplied material enters Bootwright storage only
through `contextStore` and an explicit `secret set`. Changing a declaration
neither imports nor generates bytes.

For `contextStore`, `metadata.name` identifies the entry within the selected
local context. A material consumer must refuse a missing entry without
falling back to a former file, another context, or ambient credentials. The
compiler validates the declaration without accessing the confidential store
or requiring an entry to exist. Store formats, encryption, key management and
materialization belong to their separately qualified consumers; declaring this
source does not implement them.

### File source

The `file` arm is retired under the
[copied-input rule](../project.md#design-priorities-and-non-goals): desired
state names no path that a lifecycle operation reads after admission.
Compilation refuses `source.file` in every type's shape (`path`, `cert` with
`key`, or `privateKey` with an optional `publicKey`), whether authored or
supplied by an [Environment kind default](environment.md#kind-defaults), with
`api.field`, before it opens any path. The refusal of an authored arm, at
`$.spec.source.file`, names the Secret and its remedy: declare
`source: {contextStore: {}}` and load the files with
`bootwright secret set --name <n> --context <context>` followed by the file
flags of the declared type in the
[set table](../secrets.md#acquisition-and-commands), each as `<flag> <path>`,
an optional one in brackets, after `--username <username>` for a
`usernamePassword` Secret. A name that is not a valid object name prints as
`<name>`. `<context>` is the context the invocation reads: the one `validate`
names, or the selected one when `validate` reads no file; the one a `secret`
command acts on; and the one `context init` or `context update` names. Since
the input those two refuse reaches no context, their remedy first reruns that
command, `bootwright context init` or `context update` `--name <context>
--input-dir <dir>`, after the declaration is fixed in the input, then names
`secret set`; this holds for the kind-default remedy below too. A `secret`
command acting on a context whose stored input still holds the arm names the
same `context update` rerun, since the stored input must be corrected and
re-imported before `secret set` can load files. A rerun of an
invocation also given a Context configuration file keeps it, as `--name
<context> -f <file> --input-dir <dir>`, so following the remedy never drops
that configuration.
`validate -f`, which reads no context, prints the `<context>` placeholder.

A kind default that supplies the arm is refused twice. The Environment is
refused at `$.spec.defaults.Secret.source.file`, with the remedy to replace the
arm in that default with `source: {contextStore: {}}`, then run
`bootwright secret set --name <name> --context <context>`, its `<context>`
named as above, with the file flags
of its type for each
Secret that inherits it; when the default declares a type, the remedy names
that type's file flags as above. Each Secret that inherits the arm is refused
at `$.spec.source.file`, located at the default, with the remedy of every
inherited field: override the field on that Secret or correct its Environment
kind default. A version an earlier build froze from a file source
stays in the binding that holds it
([immutable binding](../secrets.md#immutable-binding-and-contexts)).

### Generated source

`source.generated` contains fields in this order: `issuerRef`, `username`, `commonName`,
`dnsNames`, `ipAddresses`, `validityDays`, `keyType`, `comment`, then `bytes`.
Parameters are flat and type-scoped:

| Field | Type | Secret types | Required | Default | Rule |
| --- | --- | --- | --- | --- | --- |
| `issuerRef` | string | `tlsCertificate` | no | omitted | Reference to a generated `caBundle` Secret; custody signs the leaf with its private CA key. External/contextStore issuers and CA issuer chains are refused. Omission keeps self-signing. Effect material: its CA key signs the leaf. |
| `username` | string | `usernamePassword` | no | `admin` | UTF-8 within the [part byte limit](../secrets.md#acquisition-and-commands); no whitespace, colon, newline or NUL. The password itself is randomly generated material. |
| `commonName` | string | `tlsCertificate`, `caBundle` | yes | — | Common name for the certificate: non-empty UTF-8 within the [part byte limit](../secrets.md#acquisition-and-commands), without NUL. |
| `dnsNames` | array of strings | `tlsCertificate`, `caBundle` | no | omitted | DNS subject-alternative names. |
| `ipAddresses` | array of strings | `tlsCertificate`, `caBundle` | no | omitted | IP subject-alternative names. |
| `validityDays` | integer | `tlsCertificate`, `caBundle` | no | `3650` | Inclusive range `1..36500`, counted from generation; the [generated certificate](../secrets.md#acquisition-and-commands) starts 24 hours before it. |
| `keyType` | string | `sshKeyPair` | no | `ed25519` | `ed25519`, `rsa`, `ecdsa-p256`, `ecdsa-p384`, or `ecdsa-p521`. |
| `comment` | string | `sshKeyPair` | no | omitted | UTF-8 within the [part byte limit](../secrets.md#acquisition-and-commands); no leading/trailing whitespace, no control character (newline, tab and NUL included), no Unicode line or paragraph separator, no double quote and no backslash, so the key stays on its quoted Kickstart line. |
| `bytes` | integer | `token` | no | `32` | Token entropy in bytes; inclusive range `16..1024`. |

`opaque` and `dockerConfigJson` cannot use `generated`. A parameter not
consumed by the selected type is rejected. Normalization materializes the
`username`, `validityDays`, and `keyType` defaults on their applicable arms.
The token-byte default is a generation-time semantic default and may remain
omitted in effective state. Normalization never fabricates secret bytes.

Materialization follows [security.md](../security.md) for randomness, storage
and non-disclosure, and [state reconciliation](../state-reconciliation.md) for
stable retry binding.
The [runtime Secrets contract](../secrets.md) owns implementation selection,
acquisition, encrypted custody and immutable bindings.

## Entitlement

`Entitlement` declares named vendor-controlled access for one product. Its
secret fields are plain references to first-class `Secret` objects; the object
contains no organization ID, activation key, password, certificate, or token
bytes. Each such reference states the
[class of bound Secret](../secrets.md#immutable-binding-and-contexts) it names.

`Entitlement.spec` emits fields in the order `type`, `rhsm`, `registry`, then
`license`:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `type` | string | yes | — | `redhat-rhel`, `redhat-ceph`, or `ibm-storage-ceph`. |
| `rhsm` | object | conditional | — | Required for both Red Hat types; forbidden for `ibm-storage-ceph`. |
| `registry` | object | conditional | product registry | Credentials required for `redhat-ceph` and `ibm-storage-ceph`; forbidden for `redhat-rhel`. |
| `license` | object | conditional | `accept: false` | Only for `ibm-storage-ceph`, where `accept` must be `true`. |

Each type's arm set is closed:

| `spec.type` | Permitted arms | Required shape |
| --- | --- | --- |
| `redhat-rhel` | `rhsm` | `rhsm`; managed RHSM requires `organizationRef` and `activationKeyRef`. |
| `redhat-ceph` | `rhsm`, `registry` | `rhsm` plus `registry.credentialsRef`. |
| `ibm-storage-ceph` | `registry`, `license` | `registry.credentialsRef` plus `license.accept: true`. |

An authored arm a type does not permit refuses with `api.field`, naming the
type and the arms it permits.

Not yet met: admission refuses only `rhsm` on `ibm-storage-ceph`, so `registry`
or `license` on a Red Hat type is admitted and ignored; tracked as
[B826](../milestones/backlog.md#b826).

IBM Storage Ceph does not itself entitle the RHEL operating system. A
`redhat-rhel` Entitlement is selected separately by the owning machine-install
or storage-cluster field.

### RHSM

`rhsm` contains fields in the order `management`, `organizationRef`,
`activationKeyRef`, `connectToInsights`, then `satellite`:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `management` | string | no | `managed` | `managed` or `external`; identifies who runs registration. |
| `organizationRef` | string | conditional | — | Required for managed RHSM; `opaque` Secret containing the organization identifier. Access credential: registration presents it. |
| `activationKeyRef` | string | conditional | — | Required for managed RHSM; `opaque` or `token` Secret containing the activation key. Access credential: registration presents it. |
| `connectToInsights` | boolean | no | `false` | Future managed-registration enrollment intent. |
| `satellite` | object | no | omitted | Optional managed Satellite/Capsule redirect. |

An external RHSM arm may contain only `management: external`.
`organizationRef`, `activationKeyRef`, `connectToInsights`, and `satellite`
are rejected there.

`satellite` contains `hostname`, `trustBundleRef`, then `contentBaseURL`.
`hostname` is required when the block is present and is a bare host with no
scheme, path, whitespace, or surrounding whitespace. `trustBundleRef`, when
set, names a `caBundle` Secret, an access credential. `contentBaseURL`, when set, is an absolute
HTTP(S) URL; omission derives
`https://<hostname>/pulp/content` during normalization.

`management: managed` registers each Bootwright-installed virtual Machine whose
install profile selects the Entitlement in `spec.subscription`, proving and
removing only the consumer its installation created
([managed OS](../managed-os.md#registration)); `management: external` is a
declaration and runs no registration. `satellite` and `connectToInsights: true`
are admitted and refuse before registration, as rows of the managed-OS
installation's [refusal table](../managed-os.md#refusal-table).

### Registry and license

`registry` contains `url`, `credentialsRef`, then `trustBundleRef`:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `url` | string | no | `registry.redhat.io` for `redhat-ceph`; `cp.icr.io/cp` for `ibm-storage-ceph` | Scheme-less `host[:port][/namespace]`. |
| `credentialsRef` | string | conditional | — | Required for `redhat-ceph` and `ibm-storage-ceph`; `usernamePassword` registry credential Secret. Access credential: the registry login, which cephadm also retains [native-side](../secrets.md#native-side-retention). |
| `trustBundleRef` | string | no | — | `caBundle` Secret. When set, it is the only TLS anchor that Ceph image resolution and the guest pull broker use for this registry, for the Ceph image and every auxiliary image; without it they verify against the host trust store ([storage](../storage.md#bootstrap-identity-and-continuation)). Access credential. |

A registry address contains no scheme, inline credentials, query, fragment,
whitespace, leading or trailing slash, empty path segment, `.` segment, or
`..` segment. `license` contains only `accept`, a boolean defaulting to
`false`; the IBM product type requires it to be explicitly `true`, either in
the Entitlement or in an applicable authored Environment kind default. This
declarative acceptance is distinct from command authority for any later
license or registration operation.
