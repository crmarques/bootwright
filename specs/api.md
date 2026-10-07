# Desired-State API

Bootwright accepts one complete selected environment as strict
`bootwright.io/v1alpha1` documents. This page owns common grammar, graph
validation, normalization and canonical effective output. Kind-specific fields
live in:

- [Environment](api/environment.md);
- [machines and infrastructure](api/machines.md);
- [infrastructure services](api/infrastructure-services.md);
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

One selecting `Environment` sets the complete desired-state scope every
operation consumes. [Desired-state input](api/input.md) owns discovery of the
source universe, the selection order, the fixed input ceilings and the parser
boundary.

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
prefixes or separators, and a multi-digit integer must not start with `0`,
as `0644` does, because YAML 1.1 readers read it as octal; `0`, `+0` and `-0`
remain valid. Their lexical value is parsed without a machine-word
size limit before the owning field applies its range. Quoted booleans and
integers are strings and therefore type errors.

Outside explicitly open native fields, YAML numeric scalars other than
schema-declared integers are rejected except at these finite-number fields:

- an OSD declaration's `dataAllocateFraction`;
- `StoragePool.spec.autoscale.targetSizeRatio`; and
- `StoragePool.spec.compression.requiredRatio`.

Those fields accept a base-10 integer, fractional, or exponent spelling without
a base prefix or digit separator and decode with IEEE-754 binary64 semantics.
Infinity, NaN, and finite spellings that overflow to a non-finite value are
invalid; underflow may produce zero. Canonical output uses the shortest base-10
spelling, including exponent form when needed, that round-trips to the same
binary64 value. The owning storage rules define zero-as-omission and the
non-zero ranges. Open-field numeric values follow the
[native scalar rules](#native-and-implementation-shaped-fields).

A mapping key is refused for the construct it carries, as a value is. A merge
key, tagged or not, and an anchored or alias key receive `yaml.alias`. A key
with an explicit tag that the same node could not carry as a value, such as a
custom tag, `!!binary` or `!!timestamp`, receives `yaml.tag`. Any other key
that is not a string, such as an explicit `null`, an integer, a boolean or a
collection, receives `yaml.shape`, as does a document that is not a mapping,
an explicit `null` document included. An explicit `null` used for any schema
field or collection element is an `api.type` error, including when the field is
required. A document with such an error does not count as decoded. A missing
`apiVersion` receives `api.version`, a missing `kind` receives `api.kind`, and
an absent required envelope field receives `api.required`. Missing required spec fields are checked after Environment kind
defaults and owning-schema normalization; a field still missing then receives
`api.required`.

The registered schema fixes scalar and collection types, and strict decoding
rejects unknown fields at every typed level. An unknown field is reported at
its own path when its key is a schema identifier: at most 64 bytes of letters,
digits, `-` and `_`, starting with a letter or digit. Any other unknown key is
reported at its containing field and is not repeated. The diagnostic names the
fields its containing field permits, and offers a rename when exactly one of
them equals the key ignoring case or, when none does, when exactly one of them
is nearest to the key within at most two edits. Explicitly
documented open native or implementation maps remain open only at those exact
fields; their presence does not make a containing object extensible.
Unsupported API versions, retired kinds, alternate spellings, aliases between
schema generations, and translation fallbacks are rejected.

Every refusal carries what the operator needs to correct it:

- the object, as its kind and name, whenever the document's `apiVersion`, its
  registered `kind` and its DNS-label `metadata.name` decode as strings,
  including on a decode failure elsewhere in the document;
- the path of the field it concerns;
- the schema's expectation: the permitted values, the numeric bounds, the
  expected YAML type, noting that a quoted value is a string, the referenced
  kinds, or the accepted Secret types, each list bounded to 16 entries and a
  count of the rest;
- a remediation naming the next step; and
- no authored scalar value, since a value written in the wrong field may be a
  credential. A diagnostic repeats only an unknown key that is a schema
  identifier, a reference name that is a DNS label, a repeated entry's name
  that is a DNS label, and a Secret's declared type that is one of the Secret
  types.

Loading returns `filesSeen`, `objectsDecoded`, sorted diagnostics, and a state
only when there are no errors. `filesSeen` counts every unique discovered YAML
file, including files later excluded by `resources`, but not files in skipped
directory subtrees. `objectsDecoded` counts
selected documents whose envelope, version, kind, field names, and YAML types
decode into a registered kind, even if later required-value or semantic
validation fails. A scan-only excluded document does not count as decoded.

Processing is deterministic:

1. discover paths, scan for the selecting Environment, validate its partial
   kind-default entries, and apply its self-default entry once;
2. resolve resource selection and parse every selected stream;
3. strictly decode the envelope and registered kind;
4. validate authored-only grammar that normalization would otherwise erase;
5. apply Environment kind defaults once to each other selected object; recheck
   authored-intent and forbidden-input constraints on inherited values before
   normalizing remaining reference-independent defaults and values;
6. build provisional indexes, apply the Environment cluster-selection closure,
   and resolve references within the retained selected state;
7. normalize reference-derived defaults and composed values; and
8. validate every independent invariant whose prerequisites are valid.

Every error prevents a returned state. Independent objects continue through
the phases they can validly reach, while dependent checks are suppressed when
a required value or reference is missing or invalid. The Secret-type check on
a reference is such a dependent check: while the referenced Secret's own
`type` is absent or not a Secret type, the Secret's own diagnostic is the only
one. Every `api.reference` check against a selected document that fails
decoding is another: while a selected document of a referenced kind and name
does not decode, a reference field naming it, and an Environment cluster
selection entry naming it, add no diagnostic, so the document's own
diagnostics are the only ones.

## Common envelope

Every document has exactly these top-level fields:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `apiVersion` | string | yes | Exactly `bootwright.io/v1alpha1`. |
| `kind` | string | yes | One of the 26 registered kinds; case-sensitive. |
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
only within the schema's selected owner.

The deliberate exceptions are:

- `Environment.spec.containerClusters` and `spec.storageClusters` are cluster
  selection lists, not references;
- `CustomPlaybook.spec.target.{clusters,machines,hostGroups}` and the matching
  cluster-add-on step target lists are inventory selections;
- `StorageFilesystem.spec.dataPoolRefs` accepts either scalar names or the
  owning schema's `{name, default}` records;
- `InfraProvider` KubeVirt `networkRef` is the sole object-form external
  reference and carries its Kubernetes group, kind, name, and a namespace only
  for namespaced resource kinds.

The object form `{name: ...}` is rejected for ordinary Bootwright references
outside that explicitly documented data-pool record.

### Unions

A discriminated union retains `type` when it identifies a meaningful domain
variant, including `StorageCluster`, `StoragePool`, `StorageExport`, and
container installation platforms. Inactive configuration arms are rejected.
A matching configuration arm need not exist where the owning schema defines
only a type value. `ContainerCluster.spec.install.platform` may omit its
matching configuration arm. `StoragePool.spec` requires `erasure` configuration
for that type, while a replicated pool may omit `replicated`, whose unset value
shape [replicated protection](api/storage.md#replicated-protection) owns.

A presence union carries no discriminator. Exactly one implementation arm
selects `InfraProvider` and `ClusterAddon`; their former
`type` fields are unknown. An implementation arm must satisfy its own required
fields after defaulting. Presence choices also cover provider attachments,
machine-install backends and package sources, authentication, proxy choices,
and `Secret.spec.source`. The owning field specifies whether an empty arm is a
valid selection and whether omission is invalid or selects a default. In
particular, Secret source omission selects `contextStore`.

`Entitlement.spec.type` is the one set-of-arms discriminator: each product type
requires the `rhsm`, `registry`, and `license` arm set defined in
[secrets.md](api/secrets.md). That page states any forbidden additional arm;
required arms do not imply that every other arm is automatically rejected.
An authored `ibm-storage-ceph` type, which forbids `rhsm`, never inherits
`rhsm` from Entitlement kind defaults
([kind-default rule 6](api/environment.md#kind-defaults)).

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
`management: managed|external`. Direct proxy access is the presence choice
`direct: {}`; `none` is not a proxy-selection or management value.

## Kind catalog

The `v1alpha1` catalog contains exactly 26 authored kinds:

| Schema catalog group | Kinds | Architectural semantic owner |
| --- | --- | --- |
| Environment | `Environment` | Environment |
| Secrets and entitlements | `Secret`, `Entitlement` | Secrets owns `Secret` custody; Managed OS owns `redhat-rhel` semantics; Storage owns `redhat-ceph` and `ibm-storage-ceph` semantics. |
| Machines and networks | `Machine`, `NetworkConfig` | Machine |
| Managed operating systems | `MachineImage`, `MachineInstallProfile` | Managed OS |
| Substrates | `InfraProvider` | Substrate |
| Infrastructure services | `Proxy`, `DNSServer`, `NTPServer`, `ArtifactServer`, `Registry`, `LoadBalancer` | Infrastructure services |
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
- the selected controller and local-access cardinality satisfy
  [Environment controller selection](api/environment.md#controller-machine);
- a `Machine` is bound by at most one node entry across all selected container
  and storage clusters;
- every reference resolves to the required kind, variant, capability, nested
  name, and Secret type;
- every union has exactly its required active arm or arm set;
- names are unique in their documented local scope; and
- values satisfy the owning IP, CIDR, DNS, URL, port, duration, size,
  checksum, image, device, and relationship rules.

An HTTP(S) URL is absolute: scheme `http` or `https`, a DNS or IP host, an
optional port `1..65535` and no userinfo. It is written in RFC 3986's own
characters: printable ASCII other than space, `"`, `<`, `>`, `\`, `^`, the
backtick, `{`, `|` and `}`, so no whitespace, control or non-ASCII character
reaches a consumer.

An image reference is a registry `host[:port]`, then `/`-separated repository
components each matching `[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*`, then either
`:<tag>`, where the tag matches `[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}` and is not
`latest`, or `@sha256:<64 hex>`. The component rule is the repository name
grammar of the
[OCI distribution specification](https://github.com/opencontainers/distribution-spec/blob/v1.1.0/spec.md#pulling-manifests),
outside of which a container runtime refuses to pull. Registry and
registry-base values follow the same host and path rule.

## Defaults, normalization, and effective state

[Environment kind defaults](api/environment.md#kind-defaults) are applied before
owning-kind fallback and validation. Partial specs keep their declared scalar
types and absence information; they are not independently normalized with
built-in defaults or completed into objects. This prevents an unused default
entry from silently supplying additional fields. Canonical defaults emit kind
keys in the catalog's canonical kind order, and each partial spec in its
owning kind's field order, without creating absent keys.

An expanded candidate desired-state tree remains within the depth and aggregate
representation-node [input ceilings](api/input.md#fixed-input-ceilings) (64 and
1,000,000). Count inherited copies
at each recipient, not just once at their declaration; exceeding either bound
returns `input.limit` without partial state or further expansion. Retain source
provenance separately for safe diagnostics; it is not authored/effective data.

Normalization returns a deep copy, reads no secret or custom-code source,
performs no random generation or network lookup, and selects no concrete
runtime implementation. API-owned defaults consumed across multiple stages are
materialized once so validators and renderers consume the same value. A
semantic omission default that the owning schema deliberately keeps absent,
such as `Secret.spec.source` selecting `contextStore`, remains in its canonical
omitted representation. Defaults owned by a native consumer remain
absent until an authorized version-specific renderer deliberately applies them.

Normalization and internal effective-state serialization must preserve the
resolved meaning of defaults. Do not erase an explicit empty/zero value when
that would lose a suppression of an Environment fallback. Preserve its
permitted literal or emit the owning schema's explicit equivalent; for
example, an authored empty Secret source emits `contextStore: {}` instead of
disappearing. Explicit conditional-mode choices continue to suppress
incompatible inherited branches under the Environment rules.

Authored input and the normalized effective inspection representation are
distinct immutable values. Derived Machine access and managed endpoint
addresses may appear in effective inspection output even where the authored
schema forbids those fields. Effective output is not a replacement authored
input format: the compiler never accepts a trust flag, marker, alternate
entrypoint or decoder mode that bypasses authored-field checks. Internal
serialization stability does not imply that inspection YAML can be submitted
to `validate`. The [context store](contexts.md#frozen-input-and-provenance)
retains exact authored input and its origins separately, never reconstructing them from
inspection output.

Names, enums, reference names, versions, paths, and URLs satisfy their authored
lexical rules and otherwise remain unchanged. MAC addresses normalize to
lowercase colon notation. IPs normalize to compressed canonical text. Network
CIDRs mask host bits; Machine interface-address IP/prefix values retain host
bits and normalize the host and prefix separately. Digest algorithms and
hexadecimal digests normalize to lowercase. Set-valued lists sort only where
their owning schema says so; ordered lists retain authored order.
Not yet met: ContainerCluster `spec.distribution.release.image` and ClusterAddon
`spec.olm.catalogSource.image` digests are not yet lowercased; tracked as
[B411](milestones/backlog.md#b411).

Composed machine and cluster hostnames, Environment defaults, provider and
component defaults, cluster networking defaults, and other normalized values
follow their owning pages. A diagnostic concerning a reference injected from
an Environment or naming convention identifies it as defaulted and tells the
operator where to override it. A diagnostic concerning a field the object does
not hold is not marked defaulted; it keeps its own remediation.

Canonical effective output:

1. orders kinds as `Environment`, `Entitlement`, `Machine`, `MachineImage`,
   `MachineInstallProfile`, `NetworkConfig`, `InfraProvider`, `Proxy`,
   `DNSServer`, `NTPServer`, `ArtifactServer`, `Registry`, `LoadBalancer`,
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
   YAML 1.2 retains string type, and double-quotes every other string,
   including one the emitter cannot write plain, such as one ending in `:` or
   holding a Unicode line separator; and
6. contains source declarations but never source provenance, secret bytes,
   generated material, private native artifacts, or excluded objects.

JSON items follow the same kind, object, field, and map ordering. Canonical
encoder golden fixtures are normative for encoder details not fixed above.

## Native and implementation-shaped fields

Only explicitly documented native/open fields accept passthrough, preserving
their consumer's spelling and structured values. Open mappings require string
keys. Their values may recursively contain mappings, lists, strings, lowercase
YAML booleans, arbitrary-width decimal integers, and finite base-10 binary64
numbers under the numeric lexical rules above. Preserve large integers exactly;
never round them through binary64. Integer-versus-floating representation is
not semantic for a native numeric value, but its value is: canonical output
may select an equivalent integer spelling for an integral binary64 value.
Nulls, aliases, duplicate keys, unsupported tags, non-finite numbers and
non-decimal numeric spellings, a multi-digit integer with a leading zero
among them, remain forbidden. A field documented as
`map<string,string>` continues to require strings; an open nested field never
opens its containing typed schema.

Native maps remain inert during desired-state compilation, except for the
explicit [NMState composition subset](api/machines.md#nmstate-composition-subset)
needed to validate Machine declarations. A native renderer composes references
and Secrets in memory, projects only documented values and validates them
against its pinned supported release. Upgrading a native dependency must never
silently reinterpret authored `bootwright.io/v1alpha1` state.
