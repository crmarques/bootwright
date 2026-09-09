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

`spec.source` contains fields in the order `contextStore`, `file`, then
`generated`. At most one arm may be present. An omitted source first receives
any applicable Environment kind default; if it remains omitted, it selects
context storage and stays omitted canonically. An authored `source: {}` blocks
Environment source inheritance and explicitly selects context storage;
normalization emits `source: {contextStore: {}}` so internal effective-state
serialization preserves that choice. An authored `contextStore: {}` likewise
remains explicit.
`contextStore` is an empty object and rejects parameters.

| Arm | Meaning |
| --- | --- |
| `contextStore: {}` | Material lives only in confidential per-context storage. This is the semantic default and is legal for every type. |
| `file` | Material comes from operator-owned path fields selected by `spec.type`. |
| `generated` | Material is minted from type-scoped parameters; legal only for `token`, `usernamePassword`, `tlsCertificate`, `caBundle`, and `sshKeyPair`. |

Each Secret owns its source. An Environment cannot change a file source into
context storage through a custody mode. A file source continues to name
operator-owned files; importing supplied material into Bootwright storage
requires selecting `contextStore` and a separately authorized materialization
operation. Changing a declaration neither imports nor generates bytes.

For `contextStore`, `metadata.name` identifies the entry within the selected
local context. A material consumer must refuse a missing entry without
falling back to a former file, another context, or ambient credentials. The
compiler validates the declaration without accessing the confidential store
or requiring an entry to exist. Store formats, encryption, key management and
materialization belong to their separately qualified consumers; declaring this
source does not implement them.

### File source

`source.file` contains fields in the order `path`, `cert`, `key`,
`privateKey`, then `publicKey`. Only the fields selected by the Secret type are
allowed:

| Secret type | Required fields | Optional fields |
| --- | --- | --- |
| `opaque`, `token`, `usernamePassword`, `dockerConfigJson`, `caBundle` | `path` | none |
| `tlsCertificate` | `cert`, `key` | none |
| `sshKeyPair` | `privateKey` | `publicKey` |

When an SSH public-key path is omitted, a materializer derives it from the
private key. Paths may be relative to the YAML file declaring the
`Secret`, absolute, or `~`-rooted. After resolving location only for lexical
containment, a path inside the environment input tree must be below an exact
`secrets` segment so discovery excludes it before reading;
`Environment.spec.resources` cannot override that exclusion. Secret descriptor
objects stay outside that reserved directory. A key not used by the selected
type is rejected. Desired-state compilation preserves authored path spelling
without accessing the path.

### Generated source

`source.generated` contains fields in this order: `username`, `commonName`,
`dnsNames`, `ipAddresses`, `validityDays`, `keyType`, `comment`, then `bytes`.
Parameters are flat and type-scoped:

| Field | Type | Secret types | Required | Default | Rule |
| --- | --- | --- | --- | --- | --- |
| `username` | string | `usernamePassword` | no | `admin` | No whitespace, colon, or newline. The password itself is randomly generated material. |
| `commonName` | string | `tlsCertificate`, `caBundle` | yes | — | Common name for the self-signed certificate. |
| `dnsNames` | array of strings | `tlsCertificate`, `caBundle` | no | omitted | DNS subject-alternative names. |
| `ipAddresses` | array of strings | `tlsCertificate`, `caBundle` | no | omitted | IP subject-alternative names. |
| `validityDays` | integer | `tlsCertificate`, `caBundle` | no | `3650` | Inclusive range `1..36500`. |
| `keyType` | string | `sshKeyPair` | no | `ed25519` | `ed25519`, `rsa`, `ecdsa-p256`, `ecdsa-p384`, or `ecdsa-p521`. |
| `comment` | string | `sshKeyPair` | no | omitted | No leading/trailing whitespace or newline. |
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
bytes.

`Entitlement.spec` emits fields in the order `type`, `rhsm`, `registry`, then
`license`:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `type` | string | yes | — | `redhat-rhel`, `redhat-ceph`, or `ibm-storage-ceph`. |
| `rhsm` | object | conditional | — | Required for both Red Hat types; forbidden for `ibm-storage-ceph`. |
| `registry` | object | conditional | product registry | Credentials required for `redhat-ceph` and `ibm-storage-ceph`. |
| `license` | object | conditional | `accept: false` | `accept` must be `true` for `ibm-storage-ceph`. |

The required arm set is:

| `spec.type` | Required shape |
| --- | --- |
| `redhat-rhel` | `rhsm`; managed RHSM requires `organizationRef` and `activationKeyRef`. |
| `redhat-ceph` | `rhsm` plus `registry.credentialsRef`. |
| `ibm-storage-ceph` | `registry.credentialsRef` plus `license.accept: true`; `rhsm` is rejected. |

IBM Storage Ceph does not itself entitle the RHEL operating system. A
`redhat-rhel` Entitlement is selected separately by the owning machine-install
or storage-cluster field.

### RHSM

`rhsm` contains fields in the order `management`, `organizationRef`,
`activationKeyRef`, `connectToInsights`, then `satellite`:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `management` | string | no | `managed` | `managed` or `external`; identifies who runs registration. |
| `organizationRef` | string | conditional | — | Required for managed RHSM; `opaque` Secret containing the organization identifier. |
| `activationKeyRef` | string | conditional | — | Required for managed RHSM; `opaque` or `token` Secret containing the activation key. |
| `connectToInsights` | boolean | no | `false` | Future managed-registration enrollment intent. |
| `satellite` | object | no | omitted | Optional managed Satellite/Capsule redirect. |

An external RHSM arm may contain only `management: external`.
`organizationRef`, `activationKeyRef`, `connectToInsights`, and `satellite`
are rejected there.

`satellite` contains `hostname`, `trustBundleRef`, then `contentBaseURL`.
`hostname` is required when the block is present and is a bare host with no
scheme, path, whitespace, or surrounding whitespace. `trustBundleRef`, when
set, names a `caBundle` Secret. `contentBaseURL`, when set, is an absolute
HTTP(S) URL; omission derives
`https://<hostname>/pulp/content` during normalization.

Both RHSM management postures are declarations only. A lifecycle planner must
refuse remote registration until it can bind, re-prove, unregister, and
positively prove absence of one immutable remote consumer identity.

### Registry and license

`registry` contains `url`, `credentialsRef`, then `trustBundleRef`:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `url` | string | no | `registry.redhat.io` for `redhat-ceph`; `cp.icr.io/cp` for `ibm-storage-ceph` | Scheme-less `host[:port][/namespace]`. |
| `credentialsRef` | string | conditional | — | Required for `redhat-ceph` and `ibm-storage-ceph`; `usernamePassword` registry credential Secret. |
| `trustBundleRef` | string | no | — | `caBundle` Secret for registry trust. |

A registry address contains no scheme, inline credentials, query, fragment,
whitespace, leading or trailing slash, empty path segment, `.` segment, or
`..` segment. `license` contains only `accept`, a boolean defaulting to
`false`; the IBM product type requires it to be explicitly `true`, either in
the Entitlement or in an applicable authored Environment kind default. This
declarative acceptance is distinct from command authority for any later
license or registration operation.
