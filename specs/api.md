# Desired-State API

Bootwright accepts one complete selected environment as strict
`bootwright.io/v1alpha1` documents. This page owns discovery, selection, common
grammar, graph validation, normalization and canonical effective output.
Kind-specific fields live in:

- [Environment](api/environment.md);
- [machines and infrastructure](api/machines.md);
- [container clusters](api/container-clusters.md);
- [storage](api/storage.md);
- [add-ons](api/addons.md);
- [custom playbooks](api/custom-playbooks.md); and
- [Secrets and entitlements](api/secrets.md).

These pages form one closed API contract. Their grouping is documentation
placement; [architecture](architecture.md#bounded-contexts) owns domain
boundaries.

## Compiler boundary

`validate` and `render effective` discover desired-state YAML and the permitted
add-on provenance marker, then decode, normalize, validate and render selected
state. They never follow typed payload paths to stat, open, hash, extract or
execute Secret, media, manifest, playbook or other payload bytes. They perform
no filesystem writes, random generation, subprocesses, network or live-target
access, native rendering or platform mutation. Accepting a declaration claims
no executable support for its effectful consumer.

## Version authority

Only `bootwright.io/v1alpha1` is accepted. An API version change requires an
explicit user request; breaking schema changes, upgrades or refactoring never
authorize another version, alias, conversion or migration. Authorized schema
changes stay in `v1alpha1` unless explicitly requested otherwise and update
owning specs, types, examples, goldens and tests together.

## Environment directory and selected state

The [CLI contract](cli.md#parsing-and-input-conventions) owns public input
acquisition and flag cardinality. The compiler receives one ordered source
universe: the selected context input, the union of file and directory sources
resolved for context-free validation, or the one file or directory supplied to
context-free whole rendering. An input file must be a readable, regular
lowercase `.yaml` or `.yml` file and must not be a symlink. An input directory
must be readable and must not be a symlink. Kind selectors and object selectors
are not supported.

For multiple sources, discovery cleans each operand, removes repeated cleaned
path strings, recursively discovers every directory operand, combines the
result, removes repeated cleaned file paths, and orders the files lexically.
Flag order gives no precedence and distinct source files never override one
another. The combined universe undergoes one Environment selection, and the
selected documents enter one graph. A duplicate authored object identity in
that graph is an API error under the rules below. A resource path declared by
the one `Environment` resolves relative to that Environment's actual source
file, independent of which input operand discovered it.

For each directory source, Bootwright recursively discovers descendant regular
files ending in lowercase `.yaml` or `.yml`. Before reading a file, discovery
does not descend into a non-root dot-prefixed directory or a non-root directory
whose basename is exactly `vendor`, `node_modules`, `playbooks`, `roles`,
`collections`, `manifests`, or `secrets`. The last five names are fixed payload
roots and are never desired-state inputs.
Directory symlinks are not followed, and a discovered YAML symlink is an input
error. Paths are cleaned to absolute paths and ordered lexically. A missing,
unreadable, wrong-type, or symlink source is an input error. An empty source
universe reaches graph validation and fails because it has no `Environment`.

Discovery establishes the input universe. The selecting `Environment` then
sets the complete desired-state scope:

1. Scan discovered YAML streams for exactly one valid `Environment`; always
   select its declaring file.
2. Apply its `resources` list, or select all discovered files when omitted.
   Every resource must be covered by an acquired file/directory source or
   selected context input; authored YAML never widens filesystem authority.
   [Environment selection](api/environment.md#resource-and-cluster-selection)
   owns path rules, the marker-proven add-on snapshot exception, and cluster
   root closure.
3. Strictly decode selected files in lexical path/document order and follow
   [the compilation phases](#yaml-streams-and-decoding). Resolve references only
   inside the retained graph after cluster selection and normalization.

An authored empty resource or cluster-selection list is invalid. Operations
all consume the same complete selected state; selection is not a partial
operation flag. `validate` warns deterministically for excluded files declaring
Bootwright objects and for excluded cluster roots. `render effective` has no
success-warning channel.

Non-YAML files are not desired-state documents. The five payload basenames
above are exact, case-sensitive path-segment rules. A corresponding co-located
Secret, playbook or add-on field must use its schema's reserved segment, and
`resources` cannot select that root or a descendant. A lowercase YAML file
elsewhere under an input root remains a desired-state candidate even if an
object also names it as payload. The [compiler boundary](#compiler-boundary)
forbids following payload references.

### Fixed input ceilings

Input ceilings are inclusive. Diagnostic storage reserves one of its 1,000
slots for the `input.limit` sentinel.

| Resource | Ceiling |
| --- | --- |
| Descendant path depth | 32 segments below the input root |
| Filesystem entries enumerated | 65,536 across all visited directories |
| YAML-suffix candidate paths | 4,096 |
| Native add-on marker candidate paths | 4,096 |
| One native add-on marker | 64 bytes |
| All native add-on markers | 262,144 bytes (256 KiB) |
| One YAML file | 2,097,152 bytes (2 MiB) |
| All discovered YAML files | 33,554,432 bytes (32 MiB) |
| YAML stream documents | 256 per file and 8,192 in aggregate |
| YAML representation depth | 64 parent-to-child edges below the document node |
| YAML representation nodes | 100,000 per document and 1,000,000 in aggregate |
| Returned diagnostics | 1,000 total: at most 999 ordinary diagnostics plus one reserved limit diagnostic |

The root has depth zero; each descendant segment adds one. Count each visited
directory entry before type, name, suffix, link or skip checks; skipped
subtree descendants are not enumerated or counted. Read each directory up to
the remaining aggregate budget plus one and sort admitted names before descent.
Process directories and paths lexically.

Count lowercase YAML-suffix non-directory candidates before link/type checks,
and admitted regular YAML files before Environment selection. Count exact bytes
from verified opened handles, checking per-file size before proportional
buffer/parse allocation. Each admitted file counts once across Environment
scanning and selected-state decoding, even if later excluded.

Count marker candidates at the Environment schema's exact sibling
position/basename before type, link, stability, size or grammar checks. Process
candidates lexically; check per-marker bytes before grammar and aggregate bytes,
in table order. Read at most the remaining applicable budget plus one byte;
never allocate from an untrusted declared size.

Count every stream document, including empty ones. Its document node has depth
zero; every child edge adds one. Count document, mapping, sequence, scalar and
alias nodes once on encounter, even if grammar later rejects them. Traverse
mapping keys and values; key use, tags and anchors add no count. Never traverse
or expand alias targets. Per-file/document counters reset only at their named
boundary; aggregate counters never reset. Scanning and decoding share counts.

Crossing a filesystem, byte, document, depth or node ceiling immediately stops
the current read and all further admission, including other streams. Return no
state and one `input.limit` error naming the resource and ceiling; prior
diagnostics may remain. No normalization, publication or effects follow. The
first failure in processing order wins; simultaneous checks use table order.

Deduplicate diagnostics before counting. Retain at most the first 999 distinct
ordinary diagnostics in processing order. At the 1,000th, stop validation and
fill the reserved slot with one source-free `input.limit`, then sort all 1,000
by CLI diagnostic order. Every limit failure makes the command-specific result
`null`; partial file/object counts are not completion evidence.

## YAML streams and decoding

Each selected YAML file may contain one or more documents. Syntactically empty
documents are ignored; a document containing an explicit `null` is not empty.
Every non-empty document must be a mapping with string keys and must decode to
the common envelope and one registered kind. Operator-authored input may use
block or flow collection style; block style is a repository-fixture convention,
not public grammar.

Duplicate mapping keys, aliases, anchors, merge keys, custom tags, nulls,
binary values, timestamps, non-UTF-8 input, and non-finite numbers are rejected.
An unrecoverable YAML error stops parsing later documents in that selected file;
other selected files are still processed. File and document order never choose
a value or resolve a collision.

Schema strings require YAML `!!str`. Booleans require unquoted lowercase
`true` or `false`. Integers require decimal `!!int` notation without base
prefixes or separators. Their lexical value is parsed without a machine-word
size limit before the owning field applies its range. Quoted booleans and
integers are strings and therefore type errors.

YAML numeric scalars are otherwise rejected except at these finite-number
fields:

- an OSD declaration's `dataAllocateFraction`;
- `StoragePool.spec.ceph.autoscale.targetSizeRatio`; and
- `StoragePool.spec.ceph.compression.requiredRatio`.

Those fields accept a base-10 integer, fractional, or exponent spelling without
a base prefix or digit separator and decode with IEEE-754 binary64 semantics.
Infinity, NaN, and finite spellings that overflow to a non-finite value are
invalid; underflow may produce zero. Canonical output uses the shortest base-10
spelling, including exponent form when needed, that round-trips to the same
binary64 value. The owning storage rules define zero-as-omission and the
non-zero ranges.

An explicit `null` document or mapping key is a `yaml.shape` error. An explicit
`null` used for any schema field or collection element is an `api.type` error,
including when the field is required. A document with such an error does not
count as decoded. A missing `apiVersion` receives `api.version`, a missing
`kind` receives `api.kind`, and every other absent required field reaches
required-value validation and receives `api.required`.

The registered schema fixes scalar and collection types, and strict decoding
rejects unknown fields at every typed level. Explicitly documented open native
or implementation maps remain open only at those exact fields; their presence
does not make a containing object extensible. Unsupported API versions,
retired kinds, alternate spellings, aliases between schema generations, and
translation fallbacks are rejected.

Loading returns `filesSeen`, `objectsDecoded`, sorted diagnostics, and a state
only when there are no errors. `filesSeen` counts every unique discovered YAML
file, including files later excluded by `resources`, but not files in skipped
directory subtrees. `objectsDecoded` counts
selected documents whose envelope, version, kind, field names, and YAML types
decode into a registered kind, even if later required-value or semantic
validation fails. A scan-only excluded document does not count as decoded.

Processing is deterministic:

1. discover paths and scan for the selecting Environment;
2. resolve resource selection and parse every selected stream;
3. strictly decode the envelope and registered kind;
4. validate authored-only grammar that normalization would otherwise erase;
5. normalize reference-independent defaults and values;
6. build provisional indexes, apply the Environment cluster-selection closure,
   and resolve references within the retained selected state;
7. normalize reference-derived defaults and composed values; and
8. validate every independent invariant whose prerequisites are valid.

Every error prevents a returned state. Independent objects continue through
the phases they can validly reach, while dependent checks are suppressed when
a required value or reference is missing or invalid.

## Common envelope

Every document has exactly these top-level fields:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `apiVersion` | string | yes | Exactly `bootwright.io/v1alpha1`. |
| `kind` | string | yes | One of the 21 registered kinds; case-sensitive. |
| `metadata` | object | yes | Contains only `name` and optional `labels`. |
| `metadata.name` | string | yes | DNS label matching `[a-z0-9]([-a-z0-9]*[a-z0-9])?`. |
| `metadata.labels` | object | no | String-to-string map; keys emit lexically in effective state. |
| `spec` | object | yes | Kind-specific schema. |

Metadata labels describe the Bootwright object. Native labels such as
OpenShift node labels and cephadm host labels remain on their owning native
fields.

## Public authoring grammar

### References

`Ref` and `Refs` fields are plain name strings on the wire and typed references
internally. Object references resolve case-sensitively by `metadata.name` in
the schema's target kind after Environment selection. Nested references resolve
only within the schema's selected owner or catalog.

The deliberate exceptions are:

- `Environment.spec.containerClusters` and `spec.storageClusters` are cluster
  selection lists, not references;
- `CustomPlaybook.spec.target.{clusters,machines,hostGroups}` and the matching
  cluster-add-on step target lists are inventory selections;
- `Environment.spec.proxyFor.*` names a proxy catalog entry or the literal
  `none`; and
- `InfraProvider` KubeVirt `networkRef` is the sole object-form external
  reference and carries its Kubernetes group, kind, name, and namespace.

The object form `{name: ...}` is rejected for ordinary Bootwright references.

### Unions

A discriminated union carries `type`, and its value is byte-identical to the
one populated arm key. This grammar is used by `InfraProvider`,
`InfraComponent`, `ClusterAddon`, `StoragePool.spec.ceph`, and the typed portion
of `StorageExport`. Inactive arms are rejected. The
`ContainerCluster.spec.install.platform` discriminator is the exception: its
matching configuration arm is optional, while every nonmatching arm remains
forbidden. `StoragePool.spec.ceph` is the value-shape exception: `erasure`
requires its matching arm, while `replicated` may omit its arm; an empty or
all-zero `replicated` block is semantically unpopulated, so only non-zero
replicated members conflict with `type: erasure` or a placement policy.

A presence union carries no discriminator; one populated arm selects the
variant, and the owning field states whether omission is invalid or selects a
default. It is used where the surrounding type already fixes the allowed
choice, including provider network attachments, machine-install backends and
package sources, access authentication, and `Secret.spec.source` (whose
omission selects `contextStore`).

`Entitlement.spec.type` is the one set-of-arms discriminator: each product type
requires the `rhsm`, `registry`, and `license` arm set defined in
[secrets.md](api/secrets.md). That page states any forbidden additional arm;
required arms do not imply that every other arm is automatically rejected.

### Collections, enablement, and reserved words

User-invented named sets are arrays of objects with a `name` field. A map is
used only for a closed key vocabulary or at an explicitly documented open
native/configuration surface. Ordered lists retain authored order. Lists whose
owning schema declares set semantics reject duplicates; normalization sorts one
only when that schema says the effective representation is sorted.

Optional features are normally enabled by block presence. An `enabled`
boolean is used only when its owning field defines how omission differs from
explicit `false`; otherwise absence and false are the same. `type` is reserved
for kind-of-thing discriminators. The who-runs-it axis is always
`management: managed|external`, and `none` is a catalog-selection sentinel,
not a management value.

## Kind catalog

The `v1alpha1` catalog contains exactly 21 authored kinds:

| Schema catalog group | Kinds | Architectural semantic owner |
| --- | --- | --- |
| Environment | `Environment` | Environment |
| Secrets and entitlements | `Secret`, `Entitlement` | Secrets owns `Secret` custody; Managed OS owns `redhat-rhel` semantics; Storage owns `redhat-ceph` and `ibm-storage-ceph` semantics. |
| Machines and networks | `Machine`, `NetworkConfig` | Machine |
| Managed operating systems | `MachineImage`, `MachineInstallProfile` | Managed OS |
| Substrates | `InfraProvider` | Substrate |
| Infrastructure services | `InfraComponent` | Infrastructure services |
| Container clusters | `ContainerCluster` | Container cluster |
| Storage | `StorageCluster`, `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`, `StorageNFSExport`, `StorageExport` | Storage |
| Add-ons | `ClusterAddon`, `ClusterAddonProfile`, `ClusterAddonBinding` | Add-ons |
| Reserved operator automation | `CustomPlaybook` | Desired state owns boundary validation; no executable domain capability is implied. |

## Graph validation

Exactly one `Environment` is required. Reference resolution and validation use
only its selected state. The complete graph enforces at least these shared
rules; each schema page adds its own:

- object names are unique within a kind, with Environment additionally using
  its stronger exactly-one cardinality rule; every colliding object receives a
  diagnostic, no value wins, and a reference to the name remains ambiguous;
- `ContainerCluster` and `StorageCluster` additionally share one cluster-name
  namespace;
- a `Machine` is bound by at most one node entry across all selected container
  and storage clusters;
- every reference resolves to the required kind, variant, capability, nested
  name, and Secret type;
- every union has exactly its required active arm or arm set;
- names are unique in their documented local scope; and
- values satisfy the owning IP, CIDR, DNS, URL, port, duration, size,
  checksum, image, device, and relationship rules.

## Defaults, normalization, and effective state

Normalization returns a deep copy, reads no secret or custom-code source,
performs no random generation or network lookup, and selects no concrete
runtime implementation. API-owned defaults consumed across multiple stages are
materialized once so validators and renderers consume the same value. A
semantic omission default that the owning schema deliberately keeps absent,
such as `Secret.spec.source` selecting `contextStore`, remains in its canonical
omitted representation. Defaults owned by a native consumer remain
absent until an authorized version-specific renderer deliberately applies them.

Names, enums, reference names, versions, paths, and URLs satisfy their authored
lexical rules and otherwise remain unchanged. MAC addresses normalize to
lowercase colon notation. IPs normalize to compressed canonical text; CIDRs
mask host bits. Digest algorithms and hexadecimal digests normalize to
lowercase. Set-valued lists sort only where their owning schema says so;
ordered lists retain authored order.

Composed machine and cluster hostnames, Environment defaults, provider and
component defaults, cluster networking defaults, and other normalized values
follow their owning pages. A diagnostic concerning a reference injected from
an Environment or naming convention identifies it as defaulted and tells the
operator where to override it.

Canonical effective output:

1. orders kinds as `Environment`, `Entitlement`, `Machine`, `MachineImage`,
   `MachineInstallProfile`, `NetworkConfig`, `InfraProvider`, `InfraComponent`,
   `ContainerCluster`, `StorageCluster`, `StoragePlacementPolicy`,
   `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`,
   `StorageNFSExport`, `StorageExport`, `ClusterAddon`,
   `ClusterAddonProfile`, `ClusterAddonBinding`, `CustomPlaybook`, then
   `Secret`;
2. orders objects within a kind by `metadata.name`;
3. emits fields in the order defined by the owning schema, closed map keys in
   schema order, and other string map keys lexically;
4. emits block-style YAML with two-space indentation, no leading `---`, `---`
   between documents, no trailing `...`, and one final newline;
5. retains authored order for ordered lists, applies only the owning schema's
   documented normalization to other lists, uses plain YAML strings only when
   YAML 1.2 retains string type, and double-quotes other strings; and
6. contains source declarations but never source provenance, secret bytes,
   generated material, private native artifacts, or excluded objects.

JSON items follow the same kind, object, field, and map ordering. Canonical
encoder golden fixtures are normative for encoder details not fixed above.

## Native and implementation-shaped fields

Only explicitly documented native/open fields accept passthrough, preserving
their consumer's spelling and structured values. They remain inert during
desired-state compilation. A native renderer composes references and Secrets
in memory, projects only documented values and validates them against its
pinned supported release. Upgrading a native dependency must never silently
reinterpret authored `bootwright.io/v1alpha1` state.
