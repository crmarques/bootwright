# Desired-State API

Bootwright accepts one complete selected environment as strict
`bootwright.io/v1alpha1` documents. This page owns common grammar, graph
validation, normalization and canonical effective output. Kind-specific fields
live in:

- [Environment](api/environment.md);
- [machines and infrastructure](api/machines.md);
- [infrastructure services](api/infrastructure-services.md);
- [container clusters](api/openshift-clusters.md);
- [storage](api/ceph.md);
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

An admitted declaration that no capability of this build realizes refuses at
`plan`, before registration, with `lifecycle.unsupported`: that is the default
refusal point, and each capability's refusal table names its rows. Admission
refuses a declaration with `api.invariant` instead only when no build of this
API version has any consumer for it, as it refuses `spec.lifecycle.rescue`
under [D29](milestones/backlog.md#d29). Kind pages link this rule rather
than restate it.

### Admitted but unrealized

`validate`, `plan` and `apply` warn `api.unrealized` at the path of each
declaration below, naming the object and the item that tracks its
realization, so an operator learns before planning that `plan` refuses it.
The warning is also in `validate`'s `advisories`
([JSON output](cli/output.md#json-output)). The rows are data in
`api/v1alpha1` (`UnrealizedDeclarations`), and
`TestUnrealizedTableMatchesUnsupported` holds this table to that data and
proves each row warned and refused at `plan`.

| Kind | Declaration | Tracked by |
| --- | --- | --- |
| InfraProvider | `spec.vsphere` | [B99](milestones/m1.md#b99) |
| InfraProvider | `spec.kubevirt.hostClusterRef` | [B732](milestones/m3.md#b732) |
| MachineInstallProfile | `spec.installer.templateClone` | [B99](milestones/m1.md#b99) |
| Registry | `spec.management: managed` | [B68](milestones/m1.md#b68) |
| LoadBalancer | `spec.management: managed` | [B100](milestones/backlog.md#b100) |
| OpenShiftCluster | `spec.distribution.type: okd` | [B748](milestones/backlog.md#b748) |
| OpenShiftCluster | `spec.install.mode: disconnected` | [B69](milestones/m3.md#b69) |
| OpenShiftCluster | `spec.install.servingCertificates` | [B723](milestones/m3.md#b723) |
| ClusterAddonBinding | `spec.addonRefs` | [B90](milestones/m4.md#b90) |
| ClusterAddonBinding | `spec.profileRefs` | [B90](milestones/m4.md#b90) |
| CustomPlaybook | `spec.enabled: true` | [B102](milestones/backlog.md#b102) |

An install-profile row is refused on each Machine that selects the profile.
Other plan-time refusals, such as bounds, placements and trust shapes, stay in
their capabilities' refusal tables and raise no warning here. `api.deferred`
stays the warning for a declaration that plans but changes nothing: an
OpenShift endpoint `dnsName` and node `labels` and `taints`
([inert fields](openshift-clusters.md#selection-and-refusal)), an OS-ready
Machine's root-device hints other than `deviceName` and `wwn`
([root device](api/machines.md#bmc-and-root-device-shape)), a disabled
`CustomPlaybook`, and a Ceph stretch topology without a tiebreaker.

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
- `CephPool.spec.autoscale.targetSizeRatio`; and
- `CephPool.spec.compression.requiredRatio`.

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
schema generations, and translation fallbacks are rejected. A field, arm or
kind the schema has removed is unknown like any other name; the
[retired fields](#retired-fields) and [retired kinds](#retired-kinds) tables
name the replacement of each retired path and kind.

Every refusal carries what the operator needs to correct it:

- the object, as its kind and name, whenever the document's `apiVersion`, its
  registered `kind` and its DNS-label `metadata.name` decode as strings,
  including on a decode failure elsewhere in the document; when
  `metadata.name` is not a DNS label, its grammar refusal names the kind in its
  message and the document by its source location, and its remediation states
  the label grammar;
- the path of the field it concerns, in which a list index is the authored
  position in the document, even for a list the schema later sorts;
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
file, but not files in skipped directory subtrees. `objectsDecoded` counts
selected documents whose envelope, version, kind, field names, and YAML types
decode into a registered kind, even if later required-value or semantic
validation fails. A scan-only excluded document does not count as decoded.

Processing is deterministic:

1. discover paths, scan for the selecting Environment, validate its partial
   kind-default entries, and apply its self-default entry once;
2. select every discovered stream and parse it;
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
diagnostics are the only ones. A reference field that a domain invariant of
its own object refuses, such as an OS-ready Machine's `os.installProfileRef` or
`network.configRef`, is not also refused as an unresolved reference: the
invariant is the field's only diagnostic. The same holds for checks that refuse at a
containing field: while a selected add-on, add-on profile or add-on input
target fails decoding, the selection, input, configuration and capability
checks of a `ClusterAddonProfile` or `ClusterAddonBinding` that name it or
depend on its expansion add no diagnostic, and configuration and capability
checks run only on a complete expansion. A Machine whose substrate provider
does not resolve gets no provider-dependent refusal: neither the host-key
lifecycle refusal nor the BMC credentials required after inheritance.

## Common envelope

Every document has exactly these top-level fields:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `apiVersion` | string | yes | Exactly `bootwright.io/v1alpha1`. |
| `kind` | string | yes | One of the registered kinds in the [kind catalog](#kind-catalog); case-sensitive. |
| `metadata` | object | yes | Contains only `name` and optional `labels`. |
| `metadata.name` | string | yes | DNS label of 1 to 63 bytes matching `[a-z0-9]([-a-z0-9]*[a-z0-9])?`; the [one name rule](#object-names). |
| `metadata.labels` | object | no | String-to-string map; keys emit lexically in effective state. |
| `spec` | object | yes | Kind-specific schema. |

Metadata labels describe the Bootwright object. Native labels such as
OpenShift node labels and cephadm host labels remain on their owning native
fields.

### Object names

Every kind's `metadata.name` follows this one rule: a DNS label of at most 63
bytes. No kind shortens it for an internal prefix or for the services a cluster
declares; a lifecycle block whose identity would pass 63 bytes
[hashes an identity over 63 bytes](state-reconciliation.md#durable-identities-and-private-paths).
A name that is also a native identifier, such as an S3 bucket name or a CephFS
filesystem name, keeps the native grammar its kind page states, and a refusal
for such a limit names the native rule, never a block prefix.

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
- `CephFilesystem.spec.dataPoolRefs` accepts either scalar names or the
  owning schema's `{name, default}` records;
- `InfraProvider` KubeVirt `networkRef` is the sole object-form external
  reference and carries its Kubernetes group, kind, name, and a namespace only
  for namespaced resource kinds.

The object form `{name: ...}` is rejected for ordinary Bootwright references
outside that explicitly documented data-pool record.

Reference fields fall into these classes:

| Class | Written | Resolves against |
| --- | --- | --- |
| Object | `*Ref` and `*Refs` | `metadata.name` in the schema's target kind. |
| Nested | `addressRef`, a topology node name | One entry nested in its already selected owner, such as one address of the referenced Machine or one node of the cluster topology, never an object of its own. |
| Endpoint selection | `{serverRef, endpointRef}` | The server object, then the endpoint named on it. |
| Native service | `mdsServiceRef`, `serviceRef`, `serviceID` | One entry of the cluster's native service declarations, written `<service_type>.<service_id>`; the owning page fixes the form. |
| External object | KubeVirt `networkRef` | The object form above. |

The schema types every reference field with its class, and
`TestEveryReferenceFieldEndsInRefAndIsTyped` holds every registered schema to
it: a field whose name ends in `Ref` or `Refs` is a typed reference of one of
these classes, and a reference field outside the exceptions above ends in
`Ref` or `Refs`.

A nested, endpoint or native-service reference resolves through the same
`api.reference` diagnostic and suppression rules as an object reference: its
schema names the owner whose entries it names, and
`TestEveryNestedReferenceNamesWhereItResolves` holds every registered schema to
it. One naming no entry of its owner is refused at the field, naming the owner,
and echoes the authored value only when it is a name; while that owner does not
resolve or decode, while its object refuses the field naming that owner, or
while a required field leaves the entries absent, that other diagnostic is the
only one, and no kind's admission resolves such a reference again.

### Unions

One rule decides between a presence arm and an enum, and
`TestEveryUnionFollowsTheOneRule` holds every registered schema to it:

1. An alternative with configuration of its own is a presence arm: its block
   is its own field, the union selects exactly one arm (or at most one where
   the owning field says so), and no sibling `type`, `method` or other
   discriminator selects it.
2. An enum selects a variant only when no variant has a block of its own. The
   managed-service `implementation`, `management`, `Secret.spec.type` and
   `OpenShiftCluster.spec.distribution.type` are enums for this reason.
3. No enum value names a sibling block. `Entitlement.spec.type`, the
   set-of-arms form below, is the one enum that governs sibling blocks, and
   none of its values names one.
4. Omission may select one stated arm, which effective output shows.

Presence unions select `InfraProvider` and `ClusterAddon` (a `type` field on
either is unknown), the Machine [substrate arm](api/machines.md#substrate-arm),
provider attachments, machine-install backends and package sources,
authentication, proxy choices, `Secret.spec.source`,
`OpenShiftCluster.spec.install.agent` and `spec.install.platform`,
`CephCluster.spec.distribution`, the `CephPool` protection and the
`CephConsumer` arms. An arm must satisfy its own required fields after
defaulting, and an inactive arm is refused. The owning field specifies whether
an empty arm is a valid selection and whether omission is invalid or selects
an arm: `Secret.spec.source` selects `contextStore`, `install.agent` selects
`{}`, `CephPool` selects `replicated` and `CephCluster.spec.distribution`
selects `oss`.

`Entitlement.spec.type` is the one set-of-arms discriminator: each product type
permits only the `rhsm`, `registry`, and `license` arms that
[secrets.md](api/secrets.md) lists for it, requires some of them, and refuses
any other arm. Entitlement kind defaults never introduce an arm the authored
type does not permit: an authored `ibmStorageCeph` type, for example, never
inherits `rhsm`
([kind-default rule 6](api/environment.md#kind-defaults)).

### Collections, enablement, and reserved words

User-invented named sets are arrays of objects with a `name` field. A map is
used only for a closed key vocabulary or at an explicitly documented open
native/configuration surface. Ordered lists retain authored order. Lists whose
owning schema declares set semantics reject duplicates; normalization sorts one
only when that schema says the effective representation is sorted.

Optional features are normally enabled by block presence. An `enabled`
boolean is used only when its owning field defines how omission differs from
explicit `false`; otherwise absence and false are the same. A feature with no
settings of its own may be a plain `enabled` boolean when omission equals its
stated default, such as `fips.enabled` and `firewall.enabled`; a presence block
is not required for it. `type` is reserved
for kind-of-thing discriminators. The who-runs-it axis is always
`management: managed|external`. Direct proxy access is the presence choice
`direct: {}`; `none` is not a proxy-selection or management value.

### Naming new names

These rules govern every new kind, field, arm, enum value and reference.

- A kind name is a PascalCase noun that says what one object is. It abbreviates
  only an established acronym (DNS, NTP, RBD, NFS, SMB).
- Fields and union arms are lowerCamel, and so is every Bootwright-owned enum
  value. A value a native product, protocol or the kind catalog owns keeps
  its spelling; `TestEveryBootwrightEnumValueIsLowerCamel` holds every
  registered schema to the rule and lists those native fields.
- A new field whose value names a Bootwright object ends in `Ref`, and a list
  of such names in `Refs`. A row for a value the native product owns says
  "native value" or "native ID" and keeps the native spelling.
- `management: managed|external` says who runs a shared service, `os.provided`
  says only who installs the operating system, and an add-on's `provides` names
  ordering tokens. The three are never synonyms.
- A field with exactly one legal value enters `v1alpha1` only when a spec
  sentence names its consumer or the planned variant.

### Retired fields

| Kind | Retired path | Use instead |
| --- | --- | --- |
| Environment | `spec.controller.proxy` | The controller Machine's [proxy choice](api/machines.md#machine-proxy). |
| Environment | `spec.proxy`, `spec.infraComponents`, `spec.registries`, `spec.componentImages`, `spec.trustedCAs` | The owning consumer or service object: registry policy, image pins and connection facts stay there, and installation trust belongs to each [OpenShiftCluster](api/openshift-clusters.md#additional-installation-trust). |
| Environment | `spec.machineAccess` | `spec.remoteMachinesAccessKey.keyRef`. |
| Environment | `spec.safety` | Nothing: lifecycle authorization is command intent, never desired state. |
| Environment | `spec.resources` | Nothing: the Environment's directory is the scope, and `spec.containerClusters` and `spec.storageClusters` are the only [narrowing](api/environment.md#cluster-selection). |
| Machine | `spec.os.install.proxy` | The Machine's [proxy choice](api/machines.md#machine-proxy). |
| Machine | `spec.capabilities` | Nothing: node roles, the container runtime and the libvirt host follow from the objects that place the Machine ([substrate arm](api/machines.md#substrate-arm)). |
| Machine | `spec.substrate.profileRef` | `spec.substrate.libvirt.profileRef`, `kubevirt.profileRef` or `vsphere.profileRef`, the provider's arm. |
| Machine | `spec.hardware.management` | `spec.substrate.baremetal.bmc`. |
| Machine | `spec.hardware.nics[].macAddress` | `spec.substrate.baremetal.nics[]` or `spec.substrate.vsphere.nics[]`, each naming its NIC with `nicRef`. |
| Machine | `spec.os.install.rootDeviceHints` | `spec.hardware.rootDevice` for `deviceName`, `model`, `vendor`, `minSizeGigabytes` and `rotational`; `spec.substrate.baremetal.rootDevice` for `wwn`, `hctl` and `serialNumber`. |
| Machine | `spec.network.interfaceAttachments` | `spec.substrate.libvirt.interfaceAttachments` or `spec.substrate.kubevirt.interfaceAttachments`. |
| MachineInstallProfile | `spec.installer.anaconda.packageSource.mirror` | Nothing: omit `packageSource` to install from the boot media, or select `packageSource.hostedTree` ([installer arms](api/machines.md#installer-arms)); repositories the installed system uses stay in `spec.customizations.repositories.configure`. |
| MachineInstallProfile | `spec.installer.anaconda.packageSource.fromSubscription` | `packageSource.hostedTree` or the boot media for the packages, and `spec.subscription.entitlementRef` to register the installed system after its marker is proved ([registration](managed-os.md#registration)). |
| MachineInstallProfile | `spec.customizations.ssh.initialPassword` | Nothing: the installed system locks root and authorizes the public half of `spec.remoteMachinesAccessKey` on the `bootwright` account ([installation](managed-os.md#installation)). |
| InfraProvider | `spec.libvirt.bmcEmulationDefaults` | `spec.libvirt.bmcEmulation`. |
| InfraProvider | `spec.libvirt.bmcEmulation.enabled`, `spec.libvirt.bmcEmulation.protocol`, `spec.libvirt.bmcEmulation.emulator` | Nothing: the emulated BMC always runs, speaks Redfish and is sushy-tools. |
| InfraProvider | `spec.vsphere.nodeNetworking` | The cluster's `spec.install.platform.vsphere.nodeNetworking` ([platform union](api/openshift-clusters.md#platform-union-and-derivation)). |
| InfraProvider | `spec.baremetal.boot` | Nothing: a bare-metal Machine always boots its installer from Redfish virtual media. |
| OpenShiftCluster | `spec.install.method` | Nothing: the agent installer is the [`install.agent` arm](api/openshift-clusters.md#install-selection), which defaults to `{}`. |
| OpenShiftCluster | `spec.install.platform.type` | The platform's arm: `install.platform.baremetal`, `vsphere`, `none` or `external` ([platform union](api/openshift-clusters.md#platform-union-and-derivation)). |
| CephCluster | `spec.type` | Nothing: the kind is the engine. |
| CephCluster | `spec.ceph` | Its fields, directly under `spec` ([Ceph kinds](api/ceph.md#kinds)). |
| CephCluster | `spec.cephadm.bootstrap.node` | `spec.cephadm.bootstrap.nodeRef`, naming a `spec.topology.nodes` entry. |
| CephCluster | `spec.ibm`, `spec.community` | The [distribution arm](api/ceph.md): `spec.distribution.ibm` holds `callHome` and `packages`, and `spec.distribution.oss` holds `mirror` and `checksum`. |
| CephPool | `spec.type` | The protection arm: `spec.replicated`, the default, or `spec.erasure`. |
| CephConsumer | `spec.type` | Nothing: `spec.dataFoundation` or `spec.externalDetails` says what the consumer is. |
| ClusterAddonBinding | `spec.packages.fdf.storageExportRef` | `spec.packages.fdf.cephConsumerRef`. |
| ClusterAddonBinding | `spec.packages.gitops.bootstrap.credentials` | `spec.packages.gitops.bootstrap.credentialsRef`. |
| ClusterAddon | `spec.inputs[].effects[].storageExportAttachment` | `cephConsumerAttachment`. |

The unknown-field diagnostic for a listed path, authored on the object or under
its kind's `spec.defaults` entry, says it is retired and names its replacement
as the remedy; a `[]` segment stands for every list index. The rows are data in
`api/v1alpha1` (`RetiredFields`), and `TestRetiredTablesMatchTheData` holds
this table and the next two to it. The native-service migration's retired Ceph
fields stay described on their [owning page](ceph-native-api.md#migration-and-implementation-sequence), which names
each replacement.

### Retired values

| Kind | Retired value | Use instead |
| --- | --- | --- |
| OpenShiftCluster | `spec.nodes[].role: master`, `spec.security.diskEncryption.roles[]: master` | `controlPlane`, which Bootwright renders as `master` for the installer. |
| OpenShiftCluster | `spec.install.endpoints.api.source.type: openshift`, `spec.install.endpoints.api-int.source.type: openshift`, `spec.install.endpoints.ingress.source.type: openshift` | `installer`, still the default. |
| CephCluster | `spec.distribution: oss`, `spec.distribution: redhat`, `spec.distribution: ibm` | The distribution arm: `distribution: {oss: {}}`, `{redhat: {}}` or `{ibm: {...}}`. |
| ClusterAddon | `spec.inputs[].resourceKind: StorageCluster` | `CephCluster`. |
| ClusterAddon | `spec.inputs[].resourceKind: StorageExport` | `CephConsumer`. |
| Machine | `spec.substrate.baremetal.bmc.virtualMedia.tls.trust: disable-verification`, `spec.substrate.baremetal.bmc.virtualMedia.tls.trust: import-certificate` | `disableVerification` and `importCertificate`, respectively. |
| InfraProvider | `spec.baremetal.defaults.bmc.virtualMedia.tls.trust: disable-verification`, `spec.baremetal.defaults.bmc.virtualMedia.tls.trust: import-certificate` | `disableVerification` and `importCertificate`, respectively. |
| CephCluster | `spec.cephadm.clusterSSH.transport: verified-tunnel` | `verifiedTunnel`. |
| ArtifactServer | `spec.retention: install-only`, `spec.retention: installOnly` | `persistent`, the only retention: a managed artifact server keeps its published content until destroy. |
| CephClient | `spec.access: read-only`, `spec.access: read-write` | `readOnly` and `readWrite`, respectively. |
| CephPool | `spec.role: cephfs-metadata`, `spec.role: cephfs-data` | `cephfsMetadata` and `cephfsData`, respectively. |
| Entitlement | `spec.type: redhat-rhel`, `spec.type: redhat-ceph`, `spec.type: ibm-storage-ceph` | `redhatRhel`, `redhatCeph` and `ibmStorageCeph`, respectively. |
| Secret | `spec.source.generated.keyType: ecdsa-p256`, `spec.source.generated.keyType: ecdsa-p384`, `spec.source.generated.keyType: ecdsa-p521` | `ecdsaP256`, `ecdsaP384` and `ecdsaP521`, respectively. |
| CustomPlaybook | `spec.gates: add-ons` | `addOns`. |
| CustomPlaybook | `spec.follows: add-ons` | `addOns`. |

A listed value, authored on the object or under its kind's `spec.defaults`
entry, refuses `api.value` saying it is retired and naming its replacement as
the remedy; the rows are data in `api/v1alpha1` (`RetiredValues`).

### Retired kinds

| Retired kind | Use instead |
| --- | --- |
| `ContainerCluster` | `OpenShiftCluster`; OKD stays `spec.distribution.type: okd`. |
| `StorageCluster` | `CephCluster`, with its fields directly under `spec` and no `type`. |
| `StoragePlacementPolicy` | `CephPlacementPolicy`. |
| `StoragePool` | `CephPool`. |
| `StorageRBDImage` | `CephRBDImage`. |
| `StorageClient` | `CephClient`. |
| `StorageFilesystem` | `CephFilesystem`. |
| `StorageSubvolume` | `CephSubvolume`. |
| `StorageObjectGateway` | `CephObjectGateway`. |
| `StorageObjectUser` | `CephObjectUser`. |
| `StorageObjectBucket` | `CephObjectBucket`. |
| `StorageNFSExport` | `CephNFSExport`. |
| `StorageSMBResource` | `CephSMBResource`. |
| `StorageNVMeoFSubsystem` | `CephNVMeoFSubsystem`. |
| `StorageReplication` | `CephReplication`. |
| `StorageExport` | `CephConsumer`, without `spec.type`. |
| `InfraComponent` | The typed service object it described: Proxy, DNSServer, NTPServer, ArtifactServer, Registry or LoadBalancer, with `management` on its spec. |

A document of a listed kind refuses `api.kind` naming its replacement as the
remedy, and so does a listed kind named as an Environment `spec.defaults` key
(`api.field`) or as a ClusterAddon `spec.inputs[].resourceKind` value
(`api.value`, where a retired-value row above takes precedence); the rows are
data in `api/v1alpha1` (`RetiredKinds`). An operation
frozen with a retired kind, or a block record naming one, cannot be planned
again by a build that no longer registers it, so its continuation refuses
`lifecycle.state` naming the build that registered it
([cross-build continuation](state-reconciliation.md#continuation-and-removal)).

## Kind catalog

The `v1alpha1` catalog contains exactly these authored kinds. No page states a
count of kinds, and a new kind enters this table, the canonical kind order of
[effective output](#defaults-normalization-and-effective-state) and the Kinds
table of its field page in the change that adds it, which
`TestEveryKindHasItsCatalogRowOrderAndPageRow` checks:

| Schema catalog group | Kinds | Architectural semantic owner | Field page |
| --- | --- | --- | --- |
| Environment | `Environment` | Environment | [environment](api/environment.md#kinds) |
| Secrets and entitlements | `Secret`, `Entitlement` | Secrets owns `Secret` custody; Managed OS owns `redhatRhel` semantics; Storage owns `redhatCeph` and `ibmStorageCeph` semantics. | [secrets](api/secrets.md#kinds) |
| Machines and networks | `Machine`, `NetworkConfig` | Machine | [machines](api/machines.md#kinds) |
| Managed operating systems | `MachineImage`, `MachineInstallProfile` | Managed OS | [machines](api/machines.md#kinds) |
| Substrates | `InfraProvider` | Substrate | [machines](api/machines.md#kinds) |
| Infrastructure services | `Proxy`, `DNSServer`, `NTPServer`, `ArtifactServer`, `Registry`, `LoadBalancer` | Infrastructure services | [infrastructure services](api/infrastructure-services.md#kinds) |
| Container clusters | `OpenShiftCluster` | Container cluster (OpenShift implementation) | [OpenShift clusters](api/openshift-clusters.md#kinds) |
| Storage | `CephCluster`, `CephPlacementPolicy`, `CephPool`, `CephRBDImage`, `CephClient`, `CephFilesystem`, `CephSubvolume`, `CephObjectGateway`, `CephObjectUser`, `CephObjectBucket`, `CephNFSExport`, `CephSMBResource`, `CephNVMeoFSubsystem`, `CephReplication`, `CephConsumer` | Storage | [Ceph](api/ceph.md#kinds) |
| Add-ons | `ClusterAddon`, `ClusterAddonProfile`, `ClusterAddonBinding` | Add-ons | [add-ons](api/addons.md#kinds) |
| Reserved operator automation | `CustomPlaybook` | Desired state owns boundary validation; no executable domain capability is implied. | [custom playbooks](api/custom-playbooks.md#kinds) |

### Cluster family registry

Cluster kinds are registered in families (`api/v1alpha1` `Families()`), each
mapping one category to its kinds:

| Category | Environment selection and domain field | Kinds |
| --- | --- | --- |
| `containerCluster` | `spec.containerClusters`, `spec.domains.containerClusters` | `OpenShiftCluster` |
| `storageCluster` | `spec.storageClusters`, `spec.domains.storageClusters` | `CephCluster` |

The Environment's selection and domain fields keep their category names, so a
new kind of a category is selected by the existing field. A reference whose
target is a category, such as a `ClusterAddonBinding` `clusterRef`, an
InfraProvider `kubevirt.hostClusterRef`, or a profile's or step's static
`clusters`, accepts every kind of that category. Every cluster kind of every
category shares one name namespace, so a cluster command resolves a name to
one object: two cluster roots of one name refuse `api.duplicate` at
`metadata.name`, each naming the other.

A family owns its kinds and the child kinds whose required `clusterRef` names
one of them, not a category (`FamilyKinds()`); `ClusterAddonBinding`, whose
`clusterRef` names the `containerCluster` category, stays a general kind. A
family also names the kinds that fill its roles, so the general flow finds them
without naming them:

| Role | Read by | `storageCluster` kind |
| --- | --- | --- |
| `client` | `cluster keyring` and the keyring access row of `cluster info` | `CephClient` |
| `objectGateway`, `objectUser` | `cluster info --object-user` | `CephObjectGateway`, `CephObjectUser` |
| `consumer` | [cluster selection](api/environment.md#cluster-selection) of an attached consumer | `CephConsumer` |
| `consumerShare` | cluster selection: the kinds a retained consumer keeps on its cluster | `CephNFSExport` |

A child kind with a `peerClusterRef` to a cluster kind keeps both of its
clusters whole in selection, and normalization runs the family kinds in
registry order, each root before its children. Reconciliation, desired state,
Environment and the CLI iterate the registry and name no family kind or
family-owned child kind, which `TestGeneralFlowNamesNoImplementation` holds; a
new family therefore adds its registry row, its schemas and its domain without
editing the general flow.

The kinds lay out the facts that flows serving every cluster kind read in
different spec fields (`spec.nodes` against `spec.topology.nodes`), so each
cluster kind registers its layout and those flows read the facts through the
registry's accessors: the Machines its nodes bind (`ClusterNodeMachines`), the
SSH key fields its nodes trust (`ClusterNodeLoginField`), whether a managed
non-root keyed login can succeed a revoked root login
(`ClusterSuccessorLogin`), whether it demands a FIPS controller
(`ClusterRequiresControllerFIPS`) and whether the agent installer installs it
(`ClusterInstallsByAgent`). The container-cluster domain publishes per kind
what needs its derivations (`containercluster.FactsOf`): the controller tools,
the virtual IPs, the endpoints and the node names. Machine target and
admission, the controller's tool, host-package and FIPS selection and the
managed-service name records iterate the registry through these accessors and
name no family kind, which the same test holds with an empty allowlist whose
ceiling is zero: a `cluster-media` claim records the API kind of the cluster it
reserves for ([controller record](contexts/controller-record.md#record)), and
the controller's conflict diagnostics read it.

## Graph validation

Exactly one `Environment` is required. Reference resolution and validation use
only its selected state. The complete graph enforces at least these shared
rules; each schema page adds its own:

- object names are unique within a kind, with Environment additionally using
  its stronger exactly-one cardinality rule; every colliding object receives a
  diagnostic, no value wins, and a reference to the name remains ambiguous;
- `OpenShiftCluster` and `CephCluster` additionally share one cluster-name
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

A value with exactly one valid derivation is derived, not authored and
compared: a managed service's `bindAddress`
([deployment defaults](api/infrastructure-services.md#proxy-dns-ntp-and-registry-deployment)),
a generated certificate's names ([Secret generation](api/secrets.md#secret)),
a CephFilesystem's `mdsServiceRef` and an SMB share's `cluster_id` and an SMB
cluster's `resourceRefs` ([Ceph](ceph-resources.md#cephfilesystem)). Where an
authored value is admitted, it is an override that a disagreeing value refuses
at its field, or, for a node `fqdn`, an explicit override of the derived name;
examples author none of these values unless they show the override.

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
   `OpenShiftCluster`, `CephCluster`, `CephPlacementPolicy`,
   `CephPool`, `CephRBDImage`, `CephClient`, `CephFilesystem`,
   `CephSubvolume`, `CephObjectGateway`, `CephObjectUser`,
   `CephObjectBucket`, `CephNFSExport`, `CephSMBResource`,
   `CephNVMeoFSubsystem`, `CephReplication`, `CephConsumer`, `ClusterAddon`,
   `ClusterAddonProfile`, `ClusterAddonBinding`, `CustomPlaybook`,
   then `Secret`;
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
against the selected native release. Upgrading a native dependency must never
silently reinterpret authored `bootwright.io/v1alpha1` state.
