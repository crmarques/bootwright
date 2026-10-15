# Managed storage resource graph

This page owns the target resource contract approved through P2 in the
[M2a API review](storage-api-review.md). The
[native service contract](storage-native-api.md) owns daemon specifications
and their bindings; [Storage](storage.md) owns lifecycle, device safety and
custody. [Provider coverage](storage-coverage.md) owns release qualification.

**Current:** `StoragePlacementPolicy`, `StoragePool` and minimal
`StorageClient` are admitted and realized. Native
effects remain artifact-gated. **Not yet met:** later P2 kinds, NFS ownership
migration and their qualified lifecycles. [Storage admission](api/storage.md)
states current refusals.
The target additions and migrations below belong to
[B80](milestones/m2.md#b80), [B81](milestones/m2.md#b81),
[B477](milestones/m2.md#b477) to
[B483](milestones/m2.md#b483), [B489](milestones/m2.md#b489) and
[B492](milestones/m2.md#b492), with schema/projection work in
[B474](milestones/m2.md#b474). Approval defines the direction and resource
model; it does not implement new kinds or authorize native execution. Examples
are synthetic target documents, not runnable current-version examples.

## Shared resource contract

Every resource retains the `bootwright.io/v1alpha1` kind/metadata/spec
envelope and has a required scalar `spec.clusterRef`. Except for the existing
external `StorageExport` arm, this resolves to a declared managed
`StorageCluster` in the same compiled graph. No resource discovers its owner
from a matching native name, singleton object or directory. Cross-context
adoption and external replication peers are not introduced here.

Bootwright references name declared objects, not arbitrary native paths.
Local references require the same cluster. An explicit replication relationship
is the cross-cluster exception: each endpoint's resources validate against its
own declared cluster. Native service references resolve the
full qualified identity, such as `mds.shared` or `rgw.objects`, within the
selected cluster's service declarations. A service ID alone is accepted only
where its owning field fixes the service type, as `StorageNFSExport.serviceID`
fixes `nfs`. The same bare ID in two service types is not a collision.

`metadata.name` supplies the native name for a pool, filesystem, RBD image or
subvolume under its explicitly selected parent. Native-format resources retain
their native identity fields instead: an NFS export's pseudo path, or an SMB
resource's type and IDs. The executor records the mapping from Bootwright
identity to native identity; generated pool/image/export IDs and resolved
subvolume paths are observation results, not new authoring aliases.

Two declared resources cannot own the same native object, even when their
Bootwright names differ. Scope matters: image names are unique within their
pool/namespace; subvolume names within their filesystem/group; NFS pseudo
paths within their NFS service; SMB share IDs within their SMB cluster.
Native names are checked with the qualified provider grammar as well as the
existing metadata grammar. A mismatch reports the owner and conflicting
field before effects.

Native resource wrappers may carry `spec.secrets`, a list of
`{path, secretRef}` entries under the
[shared secret-binding contract](storage-native-api.md#secret-bindings).
The exact JSON Pointer is relative to `spec.native`; its qualified leaf record
selects the Secret type and serialization. There is no user-selected component,
format, template, file inclusion or arbitrary patch. Authored secret bytes,
unsupported pointers and collisions with the public payload refuse.

Each resource has one complete desired-state declaration. It is not a patch
or an imperative command list. Native maps preserve field names, scalar types,
arrays and omission; a binding may supply a qualified reference or secret
leaf, but cannot silently override authored data. A repeated non-secret native
name must agree with its resolved reference. The
[input grammar](api.md#compiler-boundary) still governs duplicate keys, nulls
and aliases. Omitted native defaults are resolved from the selected artifacts
and frozen for execution; observation verifies the effective value. Unknown
fields and unsupported combinations refuse, with no silently dropped options.

Sets reject duplicate members and use deterministic canonical ordering;
ordered native arrays retain order. Presence-sensitive zero is interpreted
only by the owning field. Existing pool quota zero remains its documented
unlimited value; the new image/subvolume sizes below do not reuse that meaning.
Release support, numeric/native limits and graph capacity are checked before
effects. No new field inherits an undocumented default from another resource.

## Retained resource owners

| Resource | Retained responsibility | Relationship to native services |
| --- | --- | --- |
| `StoragePlacementPolicy` | Reusable CRUSH placement and pool protection policy. | OSD topology supplies its proved failure domains; policy does not deploy OSDs. |
| `StoragePool` | Pool identity, replicated/EC protection, application, PG/autoscaler, quota, compression and supported mirroring intent. EC `rbd`/`cephfs-data` pools derive irreversible overwrite enablement; mirroring is [B483](milestones/m2.md#b483)'s. | Services and resources reference owned pools; native auto-created pools require an explicitly recorded derived owner. |
| `StorageFilesystem` | Metadata/data-pool composition, default/layout, active ranks and standby/replay resource policy, declared subvolume groups. | One declared MDS service binds the native filesystem identity; placement and container configuration belong to that service. |
| `StorageObjectGateway` | Logical object-storage scope, realm/zone relationships, owned pool layout and public endpoint identity/trust. | B477 binds its RGW service and any ingress services by full native identity. It no longer embeds daemon placement, frontend or ingress specifications. |
| `StorageExport` | Existing Data Foundation handoff, or the external-details arm. | It waits for the resources and access material it describes; it neither creates an add-on nor becomes a general native-command resource. |

Their existing non-daemon fields remain unless an owning approved migration
changes them. B474 records retired daemon helper paths explicitly. A
filesystem's subvolume groups remain a set of named children; `group` references
below resolve within that filesystem, so a separate group kind is unnecessary.
Group layout must select an attached eligible data pool, not merely any pool
in the cluster. Multiple data pools require one default; the existing sole-pool
normalization is recorded by the current admission contract until migrated.

`StorageFilesystem.spec.mdsServiceRef` names its full native MDS service;
`StorageObjectGateway.spec.serviceRef` names its full native RGW service.
These references are required for the corresponding managed resource. The
MDS `service_id` agrees with the native filesystem name. RGW realm/zone names
agree with the gateway's owned object scope. Canonical service identities are
looked up in the qualified identity table, not recovered by naively splitting
on dots. B477's endpoint binding selects declared ingress services whose
backend is that RGW service; the endpoint does not create another daemon owner.

Provider-created internal pools need not all be hand-authored. B477 records a
derived owner, effective pool map, native identity and protection for every
such pool before gateway readiness. A separately declared StoragePool remains
its own owner and cannot also be claimed as generated. Failure to determine
the effective map or prove compatible protection prevents readiness and any
unqualified cascading cleanup.

Filesystem creation/removal must respect the separate pool and MDS owners.
The [native volume interface](https://docs.ceph.com/en/tentacle/cephfs/fs-volumes/)
can create/remove pools and orchestrate MDS services. B492 must choose qualified
operations that preserve these owners, rather than treating a convenient
aggregate command as permission to delete another resource's dependencies.

## StorageRBDImage

One object declares one image. [B81](milestones/m2.md#b81) owns its schema,
native identity and lifecycle; gateway items consume it rather than creating
untracked images. The
[native RBD interface](https://docs.ceph.com/en/tentacle/rbd/rados-rbd-cmds/)
creates images in initialized pools; Bootwright always supplies the pool
explicitly and does not rely on the CLI's default pool or administrator user.

| Field | Target rule |
| --- | --- |
| `clusterRef` | Required managed cluster reference. |
| `poolRef` | Required same-cluster RBD pool; replicated metadata when a separate EC data pool is used. |
| `sizeBytes` | Required positive integer bytes, at most signed 64-bit and within native limits. Zero, omission and fractional values refuse; native unit conversion cannot round silently. |
| `dataPoolRef` | Optional same-cluster qualified RBD data pool. Omission uses the image pool; EC requires the provider's overwrite and workload prerequisites. |
| `features` | Optional set of native image-feature names. Omission freezes qualified creation defaults; explicit empty means no optional features and must pass provider/client requirements. |

The initial native image name is `metadata.name`. Non-default RBD namespaces,
striping, encryption and other supported creation options need a source-backed
field record in B81 before promotion; they are neither guessed nor hidden in
an arbitrary parameter map. Existing images with matching names remain foreign
unless recorded ownership and native identity agree. Readiness verifies size,
pool/data pool, native image ID and effective features. Snapshots, clones or
watchers discovered during deletion must satisfy the qualified dependency and
quiescence proof; force removal is not an ownership shortcut.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageRBDImage
metadata:
  name: application-disk

spec:
  clusterRef: ceph-example
  poolRef: block-data
  sizeBytes: 10737418240
```

## StorageClient

The minimal RBD journey declares its CephX principal independently; it never
borrows the cluster administrator or invents an implicit lab identity.
[B80](milestones/m2.md#b80) owns this first consumer and protected produced
custody. [B489](milestones/m2.md#b489) extends the client model to other resource
scopes and public access/handoff after their field contracts are qualified.

| Field | Minimal target rule |
| --- | --- |
| `clusterRef` | Required managed cluster reference. |
| `poolRef` | Required same-cluster initialized RBD pool. |
| `access` | Required `read-only` or `read-write`; no implicit administrator access. |

The native principal is `client.<metadata.name>`, with collision and ownership
checks before `get-or-create`. Pool-scoped native RBD profiles are derived from
the selected access, and exact monitor/manager/OSD capabilities are frozen and
observed. The permission covers the declared pool, not only one image; the
plan reports that scope. An EC data pool or other additional access dependency
requires B489's explicit extension, rather than silently widening these caps.
User-supplied arbitrary capabilities and imported keys are not part of the
minimal shape.

The generated key enters produced-material custody before client readiness;
a lost response requires safe recapture of the same principal/key. No native
keyring is emitted through ordinary render, status or evidence. No Bootwright
harness consumes the key: B476's client-data test runs through `cluster exec`
with the bootstrap node's administrator keyring
([storage contract](storage.md#resources-readiness-and-access)), and this
introduces no public credential-export command. Removal waits for declared consumers to be quiescent, revokes the
owned principal and withdraws its material through the lifecycle transaction.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageClient
metadata:
  name: application-reader

spec:
  clusterRef: ceph-example
  poolRef: block-data
  access: read-only
```

## StorageSubvolume

One object declares one CephFS subvolume under an owned filesystem/group.
[B492](milestones/m2.md#b492) owns it. The native volumes manager creates and
reports subvolume paths; consumers bind the reported path rather than
constructing the native internal directory layout.

| Field | Target rule |
| --- | --- |
| `clusterRef` | Required managed cluster reference. |
| `filesystemRef` | Required same-cluster filesystem reference. |
| `group` | Optional name in that filesystem's declared `subvolumeGroups`. Omission selects the native default group, recorded as such; empty refuses. |
| `sizeBytes` | Optional positive integer byte quota, at most signed 64-bit and within native limits. Omission requests no subvolume-specific quota; ancestor quotas still apply. Explicit zero refuses. |
| `poolLayoutRef` | Optional eligible data pool attached to the filesystem. Omission inherits the group/filesystem layout; the effective selection is frozen. |
| `uid`, `gid` | Optional non-negative integer ownership IDs, with zero preserved. Omission freezes the qualified native defaults. |
| `mode` | Optional octal string under the existing group-mode grammar; omission freezes the qualified native default. |

`metadata.name` is the native subvolume name within its parent scope. A
positive quota is a limit, not a reservation or guarantee of free capacity.
Readiness checks the native subvolume identity/path, layout, quota and owner
metadata. Removal can be asynchronous; B492 defines evidence that deletion is
settled and preserves unresolved native work through continuation. This does
not introduce snapshot retention or cloning by implication.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageSubvolume
metadata:
  name: documents

spec:
  clusterRef: ceph-example
  filesystemRef: shared
  group: applications
  sizeBytes: 10737418240
```

## StorageNFSExport

The target kind owns exactly one export, not an NFS daemon plus an export
array. [B478](milestones/m2.md#b478) owns the export contract and qualified
lifecycle; B474 delivers the existing kind's common shape migration with
explicit diagnostics. It does not accept both ownership models simultaneously.

| Field | Target rule |
| --- | --- |
| `clusterRef` | Required managed cluster reference. |
| `serviceID` | Required native NFS service ID; resolves `nfs.<serviceID>` in that cluster. |
| `filesystemRef` | CephFS backing alternative: export a path within this owned filesystem. |
| `subvolumeRef` | CephFS backing alternative: export within this owned subvolume, deriving filesystem and native root path. |
| `bucketRef` | Object backing alternative: resolve a B477-owned bucket and its gateway/identity dependencies. |
| `native` | Required mapping in the qualified native NFS export JSON shape; exactly one export. |

Exactly one backing alternative is required. The wrapper supplies ownership;
the native mapping keeps `pseudo`, `path`, `access_type`, `squash`, `protocols`,
`transports`, FSAL and client-block semantics. Native field completeness and
defaults follow the frozen provider schema. The `pseudo` identifies the export
within the NFS service and must be a clean absolute path. For a filesystem
binding, `path` is an absolute path in that filesystem. A subvolume binding
supplies the observed native root when `path` is omitted. An authored `path`
retains its native filesystem-absolute meaning and must resolve within that
proved subvolume; it is never silently reinterpreted as subvolume-relative.
B478 freezes the resolved path and refuses escape from the bound subvolume.

CephFS binding supplies `fsal.fs_name`; a repeated authored value must agree.
It validates `cmount_path` against the selected backing and native path. RGW
binding supplies only the release-qualified FSAL leaves from the owned bucket
and principal, never an ambiguous bare bucket name. B478 records these exact
bindings before promotion. No author-supplied CephFS `fsal.user_id` or secret
key may replace NFS manager-generated authentication. Native-generated export
IDs are captured once, and observation/retry uses that recorded identity.

The execution artifact is JSON, following the
[native export interface](https://docs.ceph.com/en/tentacle/mgr/nfs/#create-or-update-export-via-json-specification).
A structured mapping inside Bootwright YAML does not create a native NFS YAML
format. Copying an observed export must omit generated IDs and credentials;
binding mismatches refuse rather than silently selecting another filesystem.
Inline client addresses remain native declarative restrictions; when an export
uses authenticated principals, their identity/custody must have a declared or
derived owner. Ingress modes and client recovery follow B478's qualified
contract, not an assumption that every VIP provides transparent failover.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageNFSExport
metadata:
  name: documents-nfs

spec:
  clusterRef: ceph-example
  serviceID: files
  subvolumeRef: documents

  native:
    pseudo: /documents
    access_type: RW
    squash: root_squash
    protocols: [4]
    transports: [TCP]
    fsal:
      name: CEPH
    clients:
      - addresses: [192.0.2.0/24]
        access_type: RO
        squash: root_squash
```

## StorageSMBResource

One object owns one native SMB manager resource, preserving
[IBM's resource types and fields](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=method-smb-service-resource-specifications).
[B482](milestones/m2.md#b482) owns schema qualification and the native manager
boundary. The resource-type discriminator remains native; Bootwright does not
rename cluster/share/auth fields or expose the generated daemon as a second
managed service.

| Field | Target rule |
| --- | --- |
| `clusterRef` | Required managed Ceph cluster reference. |
| `native` | Required single native resource mapping, including `resource_type` and its native ID fields. |
| `smbClusterRef` | Required for a share; refers to a same-Ceph-cluster `StorageSMBResource` whose native type is `ceph.smb.cluster`. |
| `filesystemRef` | Share backing alternative: selects an owned same-cluster CephFS filesystem for a native filesystem/path share. |
| `subvolumeRef` | Share backing alternative: selects an owned same-cluster CephFS subvolume. |
| `resourceRefs` | Optional set of other same-cluster `StorageSMBResource` names for native auth/usersgroups dependencies. Each must be consumed by a matching native reference. |
| `secrets` | Protected leaf bindings under the [shared secret-binding contract](storage-native-api.md#secret-bindings); no inline passwords or private keys. |

The initial qualified types are `ceph.smb.cluster`, `ceph.smb.share`,
`ceph.smb.join.auth` and `ceph.smb.usersgroups`; B482 adds other supported
native types only with their identity, references, secret and removal record.
The native discriminator decides required/forbidden fields. A share's
`cluster_id` must agree with `smbClusterRef`; its CephFS volume/group/subvolume
names must agree with its backing reference. Exactly one of `filesystemRef`
and `subvolumeRef` is required for a share, preserving the provider's support
for both filesystem/path and subvolume shares. A filesystem reference cannot
smuggle an unbound subvolume name into the native payload. The binding can
supply omitted native reference leaves; copied explicit matching values remain
valid. Native `cephfs.path` keeps its meaning within the selected filesystem or
subvolume and cannot escape that backing. B482 checks protocol-exposure
restrictions against resolved backing paths; a filesystem/path share cannot
bypass a forbidden NFS/SMB overlap by omitting a subvolume reference.

Native auth references resolve by native resource type and ID within the
objects named by `resourceRefs`; unused, ambiguous or foreign references
refuse. Dependency edges are extracted from the qualified native schema, not
guessed from strings. Native `linked_to_cluster` records association and
removal ownership; it does not add a reciprocal creation/readiness dependency
to the cluster's auth-reference edge. Native generated services, authentication
children and placement belong to the SMB manager resource that creates them. No competing
`ceph.services` declaration may claim them. Public native input omits secret
leaves, while the protected execution artifact contains the bound native form.
Native `intent: removed` is not apply-time destructive authorization; deletion
is generated only by the authorized removal lifecycle.

The file-chain example below uses a separate SMB subvolume from NFS; shared
NFS/SMB exposure remains forbidden. Assume `documents-smb-data` is an owned
subvolume named `documents-smb-data` in filesystem `shared`, group
`applications`, and `files-smb` is an owned native SMB cluster resource with
`cluster_id: files` and qualified authentication.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageSMBResource
metadata:
  name: documents-smb

spec:
  clusterRef: ceph-example
  smbClusterRef: files-smb
  subvolumeRef: documents-smb-data

  native:
    resource_type: ceph.smb.share
    cluster_id: files
    share_id: documents
    cephfs:
      volume: shared
      subvolumegroup: applications
      subvolume: documents-smb-data
      path: /
    readonly: false
```

## Remaining resource families and consuming gates

These families use the shared resource envelope and explicit dependency model.
The table fixes their ownership boundaries and native identity tuples. Their
consuming items publish exact kind/field records for the qualified provider
before promotion; this page does not fabricate native declarative schemas for
imperative interfaces or admit arbitrary extension maps. All supported
provisioning rows remain required by the coverage matrix.

| Family and owner | Identity and references that the field record must preserve | Default and lifecycle boundary |
| --- | --- | --- |
| Object realm/zonegroup/zone, B477/B483 | Resource identity includes the owned realm/zonegroup/zone IDs and parent references; zone pool layout references owned StoragePools. An object gateway binds its zone and native RGW service. | No ambient default realm/zone or untracked pools. One owner publishes shared realm/period state; peers reference it. |
| Object accounts/users, B477 | Native account/user identity scoped by gateway/realm and tenant where supported; explicit account relationship and principal purpose. | No implicit administrator or account adoption. Bound or generated keys enter custody once; retries do not rotate them. |
| Object buckets/policies, B477 | Gateway scope, explicit owning principal/account, native bucket name and recorded native identity; policy principal/resource references resolve declared owners. | Supported native policy documents retain their format. No ambient owner, untracked generated user or guessed tenant. Bucket deletion needs data-loss authorization and proved child ownership; foreign objects/dependents prevent unqualified cascade. |
| CephX/client access, B80/B489 | `StorageClient` owns the minimal RBD principal; B489 extends references to owned pools, filesystems, subvolumes or gateway resources and explicit access scope. | No default full administrator caps. Qualified caps derive from requested resource access or validated native capability syntax; unrestricted caps need an explicit privileged purpose. Manager-generated NFS/SMB principals are derived children, not duplicate resources. |
| NVMe-oF gateway resources, B480 | Native service/group, subsystem NQN, listener address/transport, namespace ID and `StorageRBDImage` reference; initiator NQN/host authorization links to the subsystem. | No automatic unowned image creation or wildcard host access. Defaults and generated IDs are frozen/captured. Listener reachability, ANA/HA and release-supported security are qualified together. |
| iSCSI resources, B481 | If IBM-supported: native service, target IQN, image-backed LUN mapping, initiator/group and authentication bindings. | Support decision precedes effects. No substitution of NVMe fields or assumptions from upstream type availability; unsupported input refuses. |
| Replication peers, B483 | Explicit source and destination cluster/resource references, protocol/replication mode, peer identity, endpoint trust and credential ownership. | Both endpoints are declared managed resources. One relationship owner coordinates bootstrap and removal; no ambient peer lookup, context crossing or foreign adoption. |
| Access material/handoff, B489 | Declared principal/resource set, selected trust/endpoints and produced-material identity; existing `StorageExport` retains its own consumer shape. | Material is scoped, privately captured and withdrawn through custody. Its sensitive-output interface remains B489's separate approval gate; P2 authorizes no reveal command. |

A native name in an imperative provider request is always projected from this
declared identity/reference graph. These records also settle availability of
native account IDs, bucket IDs, namespace/LUN IDs, quotas, security modes and
client limits. They cannot silently defer a supported family because its old
Bootwright wrapper lacked a field. B477/B480/B481/B483/B489 own bounded API
extensions and qualified projection before their implementation promotion.

## Consumer chains and ordering

The chains below describe dependencies, not one global serial execution order.
Creation orders administrative prerequisites before resources; removal orders
dependent access and mappings before backing data. Independent branches may
run concurrently under Storage's claims and lease rules.

| Journey | Complete declared chain |
| --- | --- |
| Native RBD client | Cluster/OSDs → placement and initialized RBD pool → independently owned image and scoped pool client; client → produced material; consumer waits for both branches. |
| NVMe-oF or supported iSCSI client | RBD pool/image and native gateway service/group → subsystem/target → listener and image-backed namespace/LUN → explicit initiator authorization → qualified client endpoint/material. |
| Native CephFS client | Metadata/data pools → filesystem and bound MDS service → declared group → subvolume → scoped client access and resolved path → produced material. |
| NFS on CephFS | Filesystem/subvolume and native NFS service → individual export with derived principal; NFS service → mode-qualified ingress, when declared; client access waits for export and endpoint readiness. |
| SMB on CephFS | Filesystem/group/subvolume plus native SMB auth/usersgroups resources → SMB cluster → share; the SMB manager derives its daemons → qualified client access. |
| S3 | Realm/zone and pool layout → native RGW service bound to logical gateway → account/user → bucket/policy; declared ingress/public endpoint → scoped S3 access material. |
| NFS on RGW | Owned bucket, gateway and object principal plus native NFS service → bucket-bound native export → qualified endpoint and client restrictions. |
| Replication | Both declared cluster/resource endpoints and their access dependencies → explicit peer relationship → initial synchronization proof; native replication direction is not a cyclic Bootwright creation edge. |
| Data Foundation handoff | Declared RBD pool, filesystem, optional object gateway and scoped access dependencies → existing StorageExport material; downstream installation remains separate. |

For example, the NFS example consumes `documents` only after the
`StorageSubvolume` exists and its native path is observed; `documents` consumes
`shared` and group `applications`; `shared` consumes its declared pools and
MDS service. The SMB example consumes a different subvolume and an explicitly
declared SMB cluster. Neither protocol invents pools, parent filesystems or
credentials to repair a missing edge. Export readiness and the separate
operator read/write acceptance gate are not interchangeable.

Some native systems generate services/resources only after a child exists.
The graph distinguishes recording a parent's native configuration from
waiting for its resulting service readiness. B482 orders SMB resource apply
accordingly; it cannot require a share's generated daemon ready before applying
that share. Similarly, CephFS creation uses its owned pools before the bound
MDS service can prove filesystem readiness; a filesystem-to-MDS binding does
not require a serving MDS before creating the filesystem. Each family
identifies equivalent deferred native effects before
promotion, so resource references never become accidental readiness cycles.

## Lifecycle and migration gates

Plan freezes the resource identity, references, resolved defaults, qualified
native payload and non-secret binding identities. Before create, observation
proves absence or exact ownership. A native idempotent-create command that
accepts an existing name does not establish ownership. The journal binds
generated identities before dependent effects; ambiguous responses require
read-only resolution and never a fresh allocation or credential rotation.

Readiness inspects the actual resource and its references against that frozen
intent. Native asynchronous creation, deletion and initial synchronization
have bounded waits and explicit unfinished/unknown outcomes. Resources do not
repair drift or create data during inspection. Client data tests remain
operator acceptance. Changes to completed declarations retain the existing
destroy/fresh-apply rule; this contract introduces no resize, snapshot,
failover-management or continuous reconciliation API.

Destroy consumes the frozen applied graph, client-quiescence proof and any
required data-loss authorization. It removes owned exports/shares, access
mappings and peer links before their backing resources, keeping administrative
access and credentials needed for recovery until the transaction permits
withdrawal. A parent delete may remove native-generated children only when its
qualified effect contract names and proves them. Foreign dependencies,
unexplained identity changes and unbounded cascades refuse. Cluster teardown
does not substitute for per-resource absence and ownership evidence.

B474 and each consuming item must update schema, admission, effective state,
diagnostics, documentation and migration examples together. The old combined
NFS service/export shape and retired daemon helpers receive precise migration
errors; files are not rewritten or silently interpreted through two competing
models. New kinds other than minimal `StorageClient` remain unadmitted until
that implementation delivers.
Provider-specific native leaves, type limits and unsupported combinations are
closed promotion records, with tests for reference collision, foreign identity,
secret non-disclosure, ambiguous create/remove, default handling and complete
dependency ordering. Operator evidence covers every supported resource family
in the required emulated and physical profiles.
