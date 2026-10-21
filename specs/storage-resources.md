# Managed storage resource graph

This page owns the target resource contract approved through P2 in the
[M2a API review](storage-api-review.md). The
[native service contract](storage-native-api.md) owns daemon specifications
and their bindings; [Storage](storage.md) owns lifecycle, device safety and
custody. [Provider coverage](storage-coverage.md) owns release qualification.

**Current:** `StoragePlacementPolicy`, `StoragePool`, `StorageRBDImage`,
`StorageFilesystem`, `StorageSubvolume`, `StorageClient` and
`StorageNFSExport`, under its [field record](#storagenfsexport), are admitted
and realized. `StorageSMBResource` is admitted under its
[field record](#storagesmbresource) and realized by its SMB block once its
samba images are authored under `spec.ceph.images` of its StorageCluster
([refusal table](storage.md#refusal-table)). Native effects
remain artifact-gated. **Not yet met:** later P2
kinds and their qualified lifecycles. [Storage admission](api/storage.md)
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
| `StorageFilesystem` | Metadata/data-pool composition, default/layout, active ranks and standby/replay resource policy, declared subvolume groups; several filesystems per cluster. | `spec.mdsServiceRef` binds one declared MDS service whose `service_id` is the filesystem name; placement and container configuration belong to that service. One `ceph fs new` creates the native filesystem. |
| `StorageObjectGateway` | The realm, zone group and zone it creates (never with `--default`), its zone placement binding, its derived zone pools and its endpoint identity. | `spec.serviceRef` binds exactly one declared rgw service by full native identity; the gateway executes that service and the ingress whose `spec.backend_service` is it. It embeds no daemon placement, frontend, configuration or ingress specification. |
| `StorageExport` | Existing Data Foundation handoff, or the external-details arm. Admission refuses it until [B762](milestones/m4.md#b762) admits a managed export, whose material is [B86](milestones/m4.md#b86)'s, or parked item [B107](milestones/backlog.md#b107) the external-details arm. | It waits for the resources and access material it describes; it neither creates an add-on nor becomes a general native-command resource. |

Their existing non-daemon fields remain unless an owning approved migration
changes them. B474 records retired daemon helper paths explicitly. A
filesystem's subvolume groups remain a set of named children; `group` references
below resolve within that filesystem, so a separate group kind is unnecessary.
Group layout must select an attached eligible data pool, not merely any pool
in the cluster. Multiple data pools require one default; the existing sole-pool
normalization is recorded by the current admission contract until migrated.

`StorageFilesystem.spec.mdsServiceRef` names its full native MDS service;
`StorageObjectGateway.spec.serviceRef` names its full native RGW service.
These references are required for the corresponding managed resource, and
both are current. The MDS `service_id` agrees with the native filesystem name.
The rgw document's `rgw_realm`, `rgw_zonegroup` and `rgw_zone`, when present,
agree with the gateway's `realm`, `zoneGroup` and `zone`. Canonical service
identities are looked up in the qualified identity table, not recovered by
naively splitting on dots. The gateway's endpoint is terminated by the declared
ingress whose backend is that RGW service, else by the RGW service; the
endpoint does not create another daemon owner.

Provider-created internal pools need not all be hand-authored. B477 records a
derived owner, effective pool map, native identity and protection for every
such pool before gateway readiness. A separately declared StoragePool remains
its own owner and cannot also be claimed as generated. Failure to determine
the effective map or prove compatible protection prevents readiness and any
unqualified cascading cleanup.

Filesystem creation respects the separate pool and MDS owners. A filesystem
is created with `ceph fs new <fs> <metadata> <default> set max_mds <n>
allow_standby_replay <true|false> standby_count_wanted <n>` over its owned
pools, and further data pools attach with `ceph fs add_data_pool`. The
[native volume interface](https://docs.ceph.com/en/tentacle/cephfs/fs-volumes/),
which creates and removes pools and orchestrates MDS services, is never used,
and no creation is ever forced.

### StorageObjectGateway field record

[B477](milestones/m2.md#b477) owns this record. Admission is current; the
[gateway block](storage.md#resources-readiness-and-access) realizes the realm,
zone group, zone, zone placement, its rgw service with or without TLS and the
ingress that fronts it, which plans once its auxiliary images are authored;
per-resource removal is [B491](milestones/m2.md#b491)'s.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageObjectGateway
metadata:
  name: objects

spec:
  clusterRef: ceph-example
  serviceRef: rgw.objects
  realm: objects
  zoneGroup: objects
  zone: objects
  pools:
    dataPoolRef: objects-data
  endpoint: {}
```

| Field | Contract | Native projection |
| --- | --- | --- |
| `clusterRef` | Required managed StorageCluster. | None. |
| `serviceRef` | Required qualified name `rgw.<service_id>` of one declared rgw service of that cluster, resolved by full-name equality. One gateway binds each rgw service. | The service the gateway executes; its placement, `rgw_frontend_port`, `ssl` and networks are that document's. |
| `realm`, `zoneGroup`, `zone` | Required DNS labels, each unique per cluster across gateways. | `radosgw-admin realm\|zonegroup\|zone create --rgw-realm/--rgw-zonegroup/--rgw-zone`; the rgw document's `rgw_realm`, `rgw_zonegroup` and `rgw_zone`. |
| `pools.indexPoolRef` | Optional replicated StoragePool of role `rgw`; omitted derives `<zone>.rgw.buckets.index`. | Zone placement `index_pool`. |
| `pools.dataPoolRef` | Optional StoragePool of role `rgw`, replicated or erasure-coded; omitted derives `<zone>.rgw.buckets.data`. | STANDARD storage class `data_pool`. |
| `pools.dataExtraPoolRef` | Optional replicated StoragePool of role `rgw`; omitted derives `<zone>.rgw.buckets.non-ec`. | Zone placement `data_extra_pool`. |
| `endpoint.fqdn` | DNS name of the public endpoint; an `endpoint` mapping without it defaults to `<gateway>.<cluster>.<storage domain>`. | None; scheme, port and TLS come from the terminating service. |

The DNS-label grammar of `realm`, `zoneGroup` and `zone` is an explicit
effect restriction of Bootwright; Ceph accepts more. Bootwright never passes
`--default`, so a gateway never sets the cluster's default realm; inside its
own realm, native creation records the first zone group and zone as that
realm's defaults. The storage domain is the single Environment's
`domains.storageClusters`, else `domains.clusters`, else `domains.base`, the
same domain that derives node FQDNs.

The gateway owns derived children it never declares as objects: the zone pools
`<zone>.rgw.meta`, `.control`, `.log`, `.otp` and `.dedup` and every omitted
placement pool, the shared `.rgw.root`, which the first gateway that proves it
absent claims and its sibling gateways share, the per-realm `dashboard` system user
that cephadm's dashboard integration creates (never read for keys) and the
ingress passwords cephadm generates. No declared StoragePool can carry a
derived pool name, because a StoragePool name is a DNS label without `.`.
Derived pools take the cluster's default replica size: the
`osd_pool_default_size` pair under `spec.ceph.config.global` when declared,
else the replicated default, and that size must satisfy the replicated-pool
host rule against the declared OSD-bearing hosts.

Each derived zone pool is realized by its own block
`ceph-gateway-<gateway>-pool-<suffix>`, the suffix written with `-` for `.`
(`meta`, `control`, `dedup`, `log`, `otp`, `buckets-index`,
`buckets-non-ec`, `buckets-data`), the set
[`init_zone_pool_names`](https://github.com/ceph/ceph/blob/v20.2.1/src/rgw/rgw_zone.cc#L1277-L1312)
names once its namespaces are stripped. The block has Kind
StorageObjectGateway, is exclusive on its cluster and depends on the cluster
and image blocks by block identity. Its pool is a replicated `rgw` pool on
`replicated_rule` with the initial 32 placement groups and autoscale `on`
that an undeclared autoscale freezes, the default replica pair and
application `rgw`; it is created and observed exactly as a StoragePool's pool,
with the same records and refusals. The blocks need no realm, so they precede
the gateway's realm, zone group and zone. A declared placement ref replaces
its derived block. A gateway name longer than 30 characters refuses at plan,
because its block identities would exceed 63 characters, and a default size
that the declared OSD-bearing hosts cannot hold refuses at plan before any
effect.

The endpoint FQDN is the gateway's endpoint identity. It is unique across the
graph, against every storage node hostname and every other gateway's FQDN,
and it is not published to DNS. The terminating service is the ingress whose
`spec.backend_service` is the bound rgw service, else that rgw service; an
HTTPS terminating service needs its `/spec/ssl_cert` and `/spec/ssl_key`
bindings ([native services](storage-native-api.md)). A terminating service with
TLS needs `spec.endpoint`: the certificate must name its FQDN, and readiness
proves the served leaf by SHA-256 with that FQDN as SNI.

Retired fields refuse at decode with their replacement: `spec.serviceID`
becomes `spec.serviceRef`; `spec.placement` and `spec.frontendPort` become the
bound rgw service's placement and `spec.rgw_frontend_port`;
`spec.endpoint.dnsLabel` becomes `spec.endpoint.fqdn`; `spec.endpoint.scheme`,
`.port`, `.tls` and `.ingresses` become an ingress service whose
`spec.backend_service` is the bound rgw service, with its TLS bindings.
`spec.config` is removed: per-service RGW configuration options have no
qualified effect contract.

Admission refuses, before any effect: a `serviceRef` that resolves to no
declared service, names a non-rgw service or is bound by another gateway; a
realm, zone group or zone that disagrees with the bound rgw document or is
already owned by another gateway of the cluster; a pool reference of another
role or cluster, an erasure-coded index or data-extra pool, a pool bound by
another gateway or one pool in two placement roles; derived pools the declared
OSD-bearing hosts cannot hold; an endpoint without a derivable FQDN, with an
invalid one or with one that equals a storage node hostname or another
gateway's FQDN; and, on the cluster, an rgw service no gateway of the cluster
binds, because cephadm would deploy it before its realm, zone group and zone
exist.

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
pool/data pool, native image ID and effective features. Online removal, with
its snapshot, clone and watcher proof, belongs to
[B491](milestones/m2.md#b491) (D160, OD2); an image retires with its cluster's
hosts.

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

### Promotion record

Upstream facts are Ceph v20.2.1 and stay unverified for the IBM image
until [B472](milestones/m2.md#b472) or the owner's run confirms them.

| Field | Grammar and rule | Native projection |
| --- | --- | --- |
| `metadata.name` | DNS label of at most 54 characters, so the block `ceph-rbd-<name>` fits 63. | Image name; positional `<pool>/<name>`. |
| `clusterRef` | Required managed cluster. | None. |
| `poolRef` | Required same-cluster replicated pool of role `rbd`. | The `<pool>` of `<pool>/<name>`. |
| `sizeBytes` | Required integer from 1 to 9223372036854775807; with object-map at most 1073741824000000. | `--size <n>B`, exact bytes; a bare number would be MiB ([ArgumentTypes.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/tools/rbd/ArgumentTypes.cc#L404-L421), [strtol.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/common/strtol.cc#L137-L215)). |
| `dataPoolRef` | Optional same-cluster pool of role `rbd`, replicated or erasure-coded, other than `poolRef`. | `--data-pool <pool>`; librbd then adds feature `data-pool`. |
| `features` | Optional set of `layering`, `exclusive-lock`, `object-map`, `fast-diff`, `deep-flatten`, `journaling`, frozen in librbd's feature-bit order. | One `--image-feature <name>` per name, or `--image-feature ''` when explicitly empty ([ArgumentTypes.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/tools/rbd/ArgumentTypes.cc#L472-L496)). |
| (fixed) | Image format 2. | `--image-format 2`. |

An omitted `features` freezes librbd's `rbd_default_features`:
`layering`, `exclusive-lock`, `object-map`, `fast-diff`, `deep-flatten`
([rbd.yaml.in](https://github.com/ceph/ceph/blob/v20.2.1/src/common/options/rbd.yaml.in#L44-L54)). The worker
always passes the frozen set, so a cluster option never changes it.

Admission refuses, before any effect:

- `fast-diff` without `object-map`, and `object-map` or `journaling` without
  `exclusive-lock`, which librbd refuses
  ([CreateRequest.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/librbd/image/CreateRequest.cc#L40-L61));
- `object-map` without `fast-diff`, since librbd adds `fast-diff` and the
  created image would differ from the declared set;
- `dataPoolRef` equal to `poolRef`;
- with `object-map`, a size above 256000000 objects of 4 MiB, which librbd
  refuses at create
  ([ObjectMap.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/librbd/ObjectMap.cc#L73-L76));
- a name longer than 54 characters;
- a `poolRef` or `dataPoolRef` of another cluster or without role `rbd`, and
  an erasure-coded `poolRef`, whose image header needs a replicated pool.

Ownership: the worker writes its attempt record before `rbd create`. A
lost reply is adopted only with that record and an image of exactly the frozen
shape with no snapshot; otherwise the image refuses `foreign-rbd-image`. A
StorageRBDImage whose declared `sizeBytes`, `dataPoolRef` or `features` differ
from the recorded ones, a native image whose ID differs from the recorded one,
or a recorded image that is gone refuses `rbd-image-identity`; a native image
that drifted from the frozen size, data pool or features is not ready.
Bootwright never recreates, resizes, moves or re-features it. A trashed image has left the pool
directory, so it never blocks a create.

Readiness proves the recorded image ID, the size, format 2, the data pool, and
the features plus the implicit `data-pool`, and that the image is no clone
([Info.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/tools/rbd/action/Info.cc#L226-L238)). A consumer's
snapshot does not make the image unready.

Not admitted, as unknown fields:

- RBD namespaces, parked by D160 (OD26); they also change client cap scope,
  which [B489](milestones/m2.md#b489) owns;
- striping and object size, parked by D160 (OD26);
- encryption, parked by D160 (OD26); it also needs a Secrets binding;
- mirroring image mode, which [B483](milestones/m2.md#b483) owns;
- QoS.

## StorageClient

A StorageClient declares one CephX principal independently; it never borrows
the cluster administrator or invents an implicit lab identity.
[B80](milestones/m2.md#b80) owns the first RBD consumer and protected produced
custody. [B489](milestones/m2.md#b489) extends the client model to further
resource scopes and public access/handoff after their field contracts are
qualified.

Each principal grants exactly one scope (least privilege, one custody entry,
one removal), so a StorageClient declares exactly one of `poolRef`,
`filesystemRef` and `subvolumeRef`. Admission refuses two or three of them at
`$.spec`, also in a defaults fragment, and refuses none at `$.spec` in the
effective object; a consumer that needs another scope declares another
StorageClient.

| Field | Target rule |
| --- | --- |
| `clusterRef` | Required managed cluster reference. |
| `poolRef` | RBD arm: a same-cluster initialized pool of role `rbd`. |
| `filesystemRef` | CephFS filesystem arm: a same-cluster StorageFilesystem. |
| `subvolumeRef` | CephFS subvolume arm: a same-cluster StorageSubvolume. |
| `access` | Required `read-only` or `read-write`; no implicit administrator access. |

The native principal is `client.<metadata.name>`, with collision and ownership
checks before the key is applied with `auth add` (never `auth import`,
`auth get-or-create`, `auth caps` or `fs authorize`) and read back exactly with
`auth get`. The capabilities are derived from the selected access and the
frozen scope, frozen and observed. The RBD permission covers the declared pool,
not only one image; the plan reports each arm's scope from the frozen request.

The client has no data-pool field: a StorageRBDImage's `spec.dataPoolRef` is
the single owner of that fact. Plan derives the distinct `dataPoolRef` values
of the same-cluster StorageRBDImages on the client's `poolRef`, sorts them,
freezes them in the client request, requires those StoragePools and propagates
their plan refusals. The frozen caps follow Ceph's
[RBD user grammar](https://docs.ceph.com/en/tentacle/rbd/rados-rbd-cmds/), where
grants inside one cap are separated by a comma and a space; `<profile>` is `rbd` for
`read-write` and `rbd-read-only` for `read-only`:

| Cap | Frozen value |
| --- | --- |
| `mon` | `profile rbd` |
| `mgr` | `profile <profile> pool=<poolRef>` |
| `osd` | `profile <profile> pool=<poolRef>`, then one more `profile <profile> pool=<data pool>` grant for each derived data pool in sorted order |

With no derived data pool the caps name only `poolRef`. The `mgr` cap names
only `poolRef`: least privilege keeps the data pools off it, and whether a
consumer needs them there is unverified until the owner's live client row. The
worker proves every data pool is owned under its recorded native pool ID
before it generates a key. A client whose `poolRef` is erasure-coded still
refuses at plan; an erasure-coded pool reaches a client only as an image's
data pool. An image later added to or removed from the pool changes the
client's frozen request, which follows the destroy/fresh-apply rule of
[lifecycle and migration gates](#lifecycle-and-migration-gates).
User-supplied arbitrary capabilities and imported keys are not part of the
StorageClient shape.

The CephFS arms freeze the filesystem name, and the subvolume arm also its
group and subvolume name; they carry no data pool and no `mgr` cap. Their caps
are spelled exactly as `ceph fs authorize` renders them
([CephFS client capabilities](https://docs.ceph.com/en/tentacle/cephfs/client-auth/)),
where `<a>` is `rw` for `read-write` and `r` for `read-only`, `<fs>` is the
StorageFilesystem's name and `<path>` is the path the StorageSubvolume's
creation proof reports:

| Arm | `mon` | `osd` | `mds` |
| --- | --- | --- | --- |
| `filesystemRef` | `allow r fsname=<fs>` | `allow <a> tag cephfs data=<fs>` | `allow <a> fsname=<fs>` |
| `subvolumeRef` | `allow r fsname=<fs>` | `allow <a> tag cephfs data=<fs>` | `allow <a> fsname=<fs> path=<path>` |

The subvolume path is never constructed: the plan freezes none, and each apply,
observation and removal binds the path the subvolume's creation evidence
reports, refusing when that proof is missing or names another filesystem or
group. Before generating a key the worker proves the filesystem's recorded
native ID and, for a subvolume, its recorded marker, its recorded path and the
path `fs subvolume getpath` reports now. A client of a filesystem or subvolume
plan propagates that resource's plan refusals and orders after its block.

The generated key enters produced-material custody before client readiness;
a lost response requires safe recapture of the same principal/key. No native
keyring is emitted through ordinary render, status or evidence. No Bootwright
harness consumes the key: B476's client-data test runs through `cluster exec`
with the bootstrap node's administrator keyring
([storage contract](storage.md#resources-readiness-and-access)). An operator
exports one client's keyring only through the
[client keyring export](cli.md#client-keyring-export). Removal waits for declared consumers to be quiescent, revokes the
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

A CephFS consumer of one subvolume:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageClient
metadata:
  name: documents-writer

spec:
  clusterRef: ceph-example
  subvolumeRef: documents
  access: read-write
```

## StorageFilesystem

One object declares one CephFS filesystem over owned pools, bound to its own
MDS service. [B492](milestones/m2.md#b492) owns its schema and lifecycle;
[Storage admission](api/storage.md) owns its admission rules.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageFilesystem
metadata:
  name: files

spec:
  clusterRef: ceph-example
  mdsServiceRef: mds.files
  metadataPoolRef: files-metadata
  dataPoolRefs:
  - files-data
  subvolumeGroups:
  - name: csi
```

### Promotion record

Upstream facts are Ceph v20.2.1 and stay unverified for the IBM image
until [B472](milestones/m2.md#b472) or the owner's run confirms them.

| Field | Frozen value | Native projection |
| --- | --- | --- |
| `metadata.name` | The filesystem name, at most 47 characters so the block `ceph-filesystem-<name>` fits 63. | `<fs>` of `fs new`. |
| `mdsServiceRef` | `mds.<name>`, a declared mds service of the cluster. | None; its daemons serve the ranks. |
| `metadataPoolRef` | Owned replicated pool of role `cephfs-metadata`. | `<metadata>` of `fs new`. |
| `dataPoolRefs` | The default pool first, then the others in declared order; the default is replicated. | `<default>` of `fs new`; each other pool through `fs add_data_pool <fs> <pool>`. |
| `mds.activeCount` | Value, or 1 ([MDSMap.h](https://github.com/ceph/ceph/blob/v20.2.1/src/mds/MDSMap.h#L622)). | `set max_mds <n>`. |
| `mds.standbyReplay` | Value, or false. | `set allow_standby_replay <true\|false>`. |
| `mds.standbyCountWanted` | Value, or 0. | `set standby_count_wanted <n>`. |
| `subvolumeGroups[].name` | Sorted by name. | `fs subvolumegroup create <fs> <group>`. |
| `subvolumeGroups[].poolLayoutRef` | Value, or the default data pool. | `--pool_layout <pool>`. |
| `subvolumeGroups[].mode` | Three octal digits; a leading `0` is dropped; omission `755` ([module.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/module.py#L723-L733)). | `--mode <mode>`. |
| `subvolumeGroups[].uid`, `gid` | Value, or 0 ([group.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/fs/operations/group.py#L212-L233)). | `--uid <n> --gid <n>`. |
| `subvolumeGroups[].sizeBytes` | Positive value, or 0 for no quota. | `--size <n>` only when positive. |

A CephFS pool's frozen request names its derived filesystem: the
StorageFilesystem of the same cluster whose metadata or data pool it is, or
none. `fs new` records the application key `cephfs: {metadata|data: <fs>}` on
its pools and sets `pg_num_min` 16, `recovery_priority` 5 and
`pg_autoscale_bias` 4.0 on the metadata pool
([FSCommands.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/mon/FSCommands.cc#L315-L331));
pool readiness accepts exactly those once the key names the pool's own
filesystem, and that `pg_num_min` then replaces a frozen one.

`fs new` carries the MDS settings in its creating proposal
([FSCommands.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/mon/FSCommands.cc#L251-L307)),
so no `fs set` follows; `fs set max_mds` refuses while any filesystem health
warning exists. Bootwright never passes `--force`,
`--allow-dangerous-metadata-overlay`, `--fscid` or `--recover`, never sets an
FSMap flag and never uses `fs volume`. An attached data pool is never added
again: the monitor's pool check refuses it before its no-op
([FSCommands.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/mon/FSCommands.cc#L1077-L1092)).
Subvolume groups are created only after every active rank is held, each with
every attribute explicit; an owned group whose attributes differ is created
once more, which rewrites them
([volume.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/fs/volume.py#L1093-L1111)).

Before `fs new` the worker proves, in order: its ownership mark, the pools it
created under their recorded IDs carrying no other filesystem's key, that no
filesystem of the name exists without its creation record, that the FSMap
admits another filesystem when one exists (`enable_multiple`,
[FSCommands.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/mon/FSCommands.cc#L216-L221)),
and that enough standby daemons of the bound service are available for the
active ranks plus the larger of the wanted standbys and one standby-replay
follower per rank. Its attempt record precedes `fs new`; a lost reply is
adopted only with that record and the frozen pools.

| Refusal | Reached when |
| --- | --- |
| `foreign-filesystem` | A filesystem of the name exists, or one of its pools carries another filesystem's key, without this operation's creation record. |
| `filesystem-identity` | The declared filesystem differs from the recorded one, the native filesystem ID differs from the recorded one, its pools differ, or a recorded filesystem is gone. |
| `filesystem-multiple-disabled` | Another filesystem exists and the FSMap disables multiple filesystems. |
| `foreign-subvolume-group` | A declared group exists that this operation did not create. |
| `mds-coverage` | Fewer standby daemons of the bound service than the ranks, followers and wanted standbys need. |

Readiness proves the recorded filesystem ID, the metadata and data pools by
their recorded IDs in frozen order, `max_mds`, `allow_standby_replay` and
`standby_count_wanted`, every rank below `max_mds` held by an `up:active`
daemon of the bound service, one `up:standby-replay` follower per rank when
replay is declared, at least the wanted standbys, and every declared group
with its owner, mode, layout pool and quota
([group.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/fs/operations/group.py#L113-L152)).
A drifted value is not ready, never a refusal; an undeclared group is not read.
Online removal moved to [B491](milestones/m2.md#b491) by D160 (OD2 (a)); a
filesystem retires with its cluster's hosts.

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
| `uid`, `gid` | Optional integer 0..2147483647, zero preserved. Omission freezes the declared group's frozen uid/gid, or 0 in the default group. |
| `mode` | Optional octal string under the existing group-mode grammar; omission freezes `755` ([module.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/module.py#L777)), never the group's mode. |

`metadata.name` is the native subvolume name within its parent scope. A
positive quota is a limit, not a reservation or guarantee of free capacity.
Readiness checks the native subvolume identity/path, layout, quota and owner
metadata. Online removal moved to [B491](milestones/m2.md#b491) by D160
(OD2 (a)), with the proof that the native `fs subvolume rm`, which moves a
subvolume to the trash and purges it later, has settled; a subvolume retires
with its cluster's hosts. This does not introduce snapshot retention or
cloning by implication.

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

### Promotion record

Upstream facts are Ceph v20.2.1 and stay unverified for the IBM image
until [B472](milestones/m2.md#b472) or the owner's run confirms them. The
block is `ceph-subvolume-<name>`: `metadata.name` has at most 48 characters so
it fits 63. It requires its StorageFilesystem and depends by ID on its
cluster's core and image blocks, never through `Requires StorageCluster`
(D159 clause 10), so a destroy removes the subvolume before its filesystem and
the filesystem before its pools.

| Field | Frozen value | Native projection |
| --- | --- | --- |
| `metadata.name` | The subvolume name. | `<sub_name>`. |
| `filesystemRef` | The filesystem name. | `<vol_name>`. |
| `group` | Value, or `""` for the native default group; never `_nogroup`, which an explicit `--group_name` refuses ([group.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/fs/operations/group.py#L21-L32)). | `--group_name <g>` only when non-empty. |
| `poolLayoutRef` | Value; else the declared group's frozen pool; else the filesystem's default data pool. | `--pool_layout <pool>`, always. |
| `uid`, `gid` | Value with zero preserved; else the declared group's frozen value; else 0. | `--uid <n> --gid <n>`, always. |
| `mode` | Three octal digits, a leading `0` dropped; else `755`; never the group's mode. | `--mode <mode>`, always. |
| `sizeBytes` | Positive value, or 0 for no quota. | `--size <n>` only when positive. |

The plan builds the filesystem's own request first, so every filesystem and
pool plan refusal also refuses the subvolume.

Before any `fs subvolume` command the worker proves, in order: its ownership
mark; the filesystem's creation mark and its recorded filesystem ID in
`fs dump`; the layout pool it created, under its recorded ID and attached to
the filesystem; and, for a named group, the group's creation mark and its
presence in `fs subvolumegroup ls`. It then proves the subvolume absent with
`fs subvolume ls`, writes the attempt record `ceph-subvolume-attempt-v1`, and
runs `fs subvolume create` with every attribute explicit. A fresh create sets
the mode only through `mkdirs` under the manager's umask
([subvolume_v2.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/fs/operations/versions/subvolume_v2.py#L157-L183)),
so when the mode alone differs the worker creates once more, which sets it,
before it records the path `fs subvolume info` reports. Once the path is
recorded it never creates again: a repeat create rewrites the attributes and
resets the earmark
([volume.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/fs/volume.py#L249-L290)).
It then sets the user metadata `bootwright-owner` to the operation ID
([fs-volumes.rst](https://github.com/ceph/ceph/blob/v20.2.1/doc/cephfs/fs-volumes.rst#L572-L575))
and reads it back with `fs subvolume metadata ls`. A lost reply is adopted
only with the attempt record and the frozen shape. Bootwright never runs
`fs subvolume rm`, `resize`, `snapshot`, `clone`, `authorize`, `evict`,
`earmark`, `metadata get` or `fs volume`.

| Refusal | Reached when |
| --- | --- |
| `foreign-subvolume` | A subvolume of the name exists in the filesystem and group without this operation's creation record. |
| `subvolume-identity` | The declared subvolume differs from the recorded one, the recorded path differs or the subvolume is gone, the created shape differs from the frozen one, or the owner metadata names another operation. |

Readiness proves the path equals the recorded one, `type` `subvolume`, `state`
`complete`, `data_pool`, an empty `pool_namespace`, `bytes_quota` (the size or
`infinite`), uid, gid, `mode & 0o7777` and the owner metadata
([subvolume_base.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/fs/operations/versions/subvolume_base.py#L473-L547)).
A drifted value is not ready, never a refusal; the earmark, normalization and
case sensitivity are not read. Evidence reports the subvolume's UUID and its
path `/volumes/<group or _nogroup>/<name>/<uuid>`. A consumer binds the path
`ProvedSubvolume` reports from the applied creation proof and never constructs
one. Namespace isolation and the earmark refuse as unknown fields and stay
parked by D160 (OD26 (a)); normalization and case sensitivity are
[B482](milestones/m2.md#b482)'s.

## StorageNFSExport

One object owns exactly one export of one declared nfs service.
[B478](milestones/m2.md#b478) owns this record. Admission and execution are
current; [Storage](storage.md#resources-readiness-and-access) owns the export
block, and per-resource removal is [B491](milestones/m2.md#b491)'s. Daemon
placement, ports and ingress belong to the [native services](storage-native-api.md).

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
    fsal:
      name: CEPH
    clients:
      - addresses: [192.0.2.0/24]
        access_type: RO
        squash: root_squash
```

| Field | Contract |
| --- | --- |
| `clusterRef` | Required managed StorageCluster. |
| `serviceID` | Required service ID of one declared nfs service of that cluster; it resolves `nfs.<serviceID>` by full-name equality, so an rgw or other service with the same ID never satisfies it. |
| `filesystemRef` | CephFS backing: a StorageFilesystem of the same cluster. |
| `subvolumeRef` | CephFS backing: a StorageSubvolume of the same cluster; its filesystem is the subvolume's `filesystemRef`. |
| `native` | Required mapping holding the export in the native export JSON shape, checked against the closed member table below. |

Exactly one of `filesystemRef` and `subvolumeRef` is required; both, or
neither, refuse. Object backing is not admitted yet, and there is no
`spec.secrets`: an export declaration carries no credential.

The `native` mapping follows the
[native export interface](https://docs.ceph.com/en/tentacle/mgr/nfs/#create-or-update-export-via-json-specification)
and keeps cephadm's snake_case spelling. Its members are closed:

| Member | Contract | Frozen default |
| --- | --- | --- |
| `pseudo` | Required clean absolute path other than `/`; unique among the exports of one nfs service. | None. |
| `path` | Clean absolute path in the bound filesystem. | `/` for a `filesystemRef` binding; none for a subvolume binding. |
| `access_type` | `RW`, `RO` or `NONE`. | `RO`. |
| `squash` | One of the native squash tokens `root`, `root_squash`, `rootsquash`, `rootid`, `root_id_squash`, `rootidsquash`, `all`, `all_squash`, `allsquash`, `all_anomnymous`, `allanonymous`, `no_root_squash`, `none` and `noidsquash`. | `no_root_squash`. |
| `security_label` | Boolean. | `true`. |
| `protocols` | Required in complete input and exactly `[4]`; NFSv3 refuses. | None: an omitted list would serve NFSv3 and NFSv4, so omission refuses. |
| `transports` | `[TCP]`; UDP refuses as an incompatible transport for NFSv4. | `[TCP]`. |
| `sectype` | `[sys]` when present; Kerberos (`krb5`, `krb5i`, `krb5p`) refuses and native `none` is not a supported option. | None. |
| `fsal.name` | Required `CEPH`; `RGW` refuses because object backing is not admitted yet. | None. |
| `fsal.fs_name` | Name of the bound filesystem; an authored value must agree with the binding. | The bound filesystem. |
| `fsal.cmount_path` | Clean absolute path that is `/`, equal to `path`, or an ancestor directory of `path`. | `/`. |
| `clients[]` | Each entry requires a non-empty `addresses` set of literal IP addresses or CIDRs, plus `access_type` and `squash` with the export-level tokens. | Addresses take their canonical form (masked prefix or bare address). |

Bootwright narrows native in three explicit restrictions. The cmount rule is
an ancestor-directory rule, stricter than native's substring test, so a
cmount of `/a` never covers a path `/ab`. Access and squash tokens take the
exact spelling above, although native accepts any case. A client address is
unique within the export after canonicalization, because NFS-Ganesha applies
the first client block that matches and a repeat would be a silent no-op.
Normalization freezes the defaults into effective state, keeps authored
values and is idempotent; `protocols`, `sectype` and a subvolume
binding's `path` are never added.

Refused members name the field, the reason and the next step: `export_id`
(the NFS manager generates the export ID, which Bootwright records),
`cluster_id` (the NFS cluster is the service `spec.serviceID` names),
`fsal.user_id` (the NFS manager derives the CephFS principal),
`fsal.cephx_key`, `fsal.access_key_id` and `fsal.secret_access_key` (an export
declaration carries no key) and `fsal.sec_label_xattr` (no qualified effect
contract). No refusal echoes a member's value.

Retired fields refuse at decode with their replacement: `spec.port` and
`spec.placement` become the `spec.port` and placement of the nfs service
`spec.serviceID` names; `spec.ingresses` becomes an ingress service whose
`spec.backend_service` is that nfs service; `spec.exports` becomes one
StorageNFSExport per export entry, each binding `spec.filesystemRef` or
`spec.subvolumeRef` and holding that entry's native export under
`spec.native`.

The export block resolves at execution what admission cannot prove: a
subvolume binding's `path` comes from the proved subvolume when omitted, an
authored one must resolve within that subvolume and is never reinterpreted as
subvolume-relative, and the cmount check then runs against that resolved path.
An authored subvolume `path` outside the subvolume, or a cmount that is not an
ancestor of the resolved path, refuses `nfs-export-path-escape` before any
write. The resolved path and the generated export ID are recorded once, and
observation and retry use that record. The execution artifact is JSON; a
structured mapping inside Bootwright YAML does not create a native NFS YAML
format.

## StorageSMBResource

One object owns one native SMB manager resource, preserving
[IBM's resource types and fields](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=method-smb-service-resource-specifications).
[B482](milestones/m2.md#b482) owns schema qualification and the native manager
boundary. The resource-type discriminator remains native; Bootwright does not
rename cluster/share/auth fields or expose the generated daemon as a second
managed service.

| Field | Contract |
| --- | --- |
| `clusterRef` | Required managed StorageCluster; an external cluster refuses. |
| `native` | Required mapping holding one native SMB manager resource; its `resource_type` selects the closed per-type record below. |
| `smbClusterRef` | Required on a `ceph.smb.share`, forbidden on every other type; names a `StorageSMBResource` whose native type is `ceph.smb.cluster`. |
| `filesystemRef` | Share backing: a StorageFilesystem of the same cluster. Forbidden on every type but `ceph.smb.share`. |
| `subvolumeRef` | Share backing: a StorageSubvolume of the same cluster. Forbidden on every type but `ceph.smb.share`. |
| `resourceRefs` | Optional set of `StorageSMBResource` names, admitted only on a `ceph.smb.cluster`, naming its `ceph.smb.join.auth` and `ceph.smb.usersgroups` resources. |
| `secrets` | Required on `ceph.smb.join.auth` and `ceph.smb.usersgroups`, forbidden elsewhere; the leaves in [secret bindings](storage-native-api.md#secret-bindings). Each binding names an `opaque` or `token` Secret. |

Exactly one of `filesystemRef` and `subvolumeRef` is required for a share;
both, or neither, refuse.

The `native` mapping keeps the SMB manager module's snake_case spelling, its
types and its enumerations. `resource_type` selects one closed record:

| Type | Members |
| --- | --- |
| `ceph.smb.cluster` | Required `cluster_id`, `auth_mode` (`user` or `active-directory`) and `placement` (the native placement grammar); optional `intent`, `domain_settings` (required `realm` and `join_sources[]` of `source_type: resource` and a required `ref`), `user_group_settings[]` (`source_type` and a required `ref`), `custom_dns` (a set), `clustering` (`default`, `always` or `never`) and `public_addrs[]` (a required `address`). |
| `ceph.smb.share` | Required `cluster_id` and `share_id`; optional `intent`, `name`, `readonly`, `browseable`, `cephfs` (`volume`, `path`, `subvolumegroup`, `subvolume` and `provider`), `restrict_access` and `login_control[]` (a required `name`, `category` `user` or `group`, and `access`). |
| `ceph.smb.join.auth` | Required `auth_id` and `auth` with a required `username`; optional `intent`. |
| `ceph.smb.usersgroups` | Required `users_groups_id` and `values` with required `users[]` and `groups[]`, each entry a required `name`; optional `intent`. |

A member outside its record refuses as not in the supported schema at its
parent field. `ceph.smb.tls.credential` refuses because Bootwright does not
realize TLS credential resources; any other type refuses with the four
admitted types as the next step. The share's `cephfs` and its `volume` are
optional although the manager requires them: the binding supplies omitted
`cephfs` reference leaves.

Native members Bootwright does not realize refuse at their own field, before
the closed-record check, and no refusal echoes a value:

| Type | Member | Reason | Next step |
| --- | --- | --- | --- |
| cluster | `custom_smb_global_options` | Bootwright does not realize custom Samba global options. | Omit it. |
| cluster | `client_compat` | Bootwright does not realize client_compat. | Omit it. |
| cluster | `custom_ports` | Bootwright does not realize custom SMB ports. | Omit it. |
| cluster | `bind_addrs` | Bootwright does not realize SMB bind addresses. | Omit it. |
| cluster | `remote_control` | Bootwright does not realize the SMB remote control service. | Omit it. |
| cluster | `public_addrs[i].destination` | Bootwright does not realize public address destinations. | Omit `destination` from that entry. |
| share | `comment` | Bootwright does not realize SMB share comments. | Omit it. |
| share | `max_connections` | Bootwright does not realize SMB share connection limits. | Omit it. |
| share | `custom_smb_share_options` | Bootwright does not realize custom Samba share options. | Omit it. |
| join.auth, usersgroups | `linked_to_cluster` | Bootwright does not realize linked SMB auth resources; each StorageSMBResource applies and removes its own native resource. | Omit it; the SMB cluster names the resource in its `spec.resourceRefs`. |
| join.auth | `auth.password` | An SMB password is never written in `spec.native`. | Bind `/auth/password` through `spec.secrets`. |
| usersgroups | `values.users[i].password` | An SMB password is never written in `spec.native`. | Bind `/values/users/<i>/password` through `spec.secrets`. |

Value rules, each at its field:

- `cluster_id`, `share_id`, `auth_id`, `users_groups_id` and every native
  `ref` are 1 to 18 letters, digits or inner hyphens, the manager's ID
  grammar.
- A share `name` starts with a letter, digit or underscore and holds at most
  64 letters, digits, spaces, dots, underscores or hyphens. Login, user and
  group names hold no space, tab or newline and at most 128 characters, and
  are unique in their list (a login entry by its `category`, default `user`,
  and `name`). A usersgroups resource declares at least one user.
- `domain_settings.realm` is a DNS domain name in any case; each `custom_dns`
  entry is a literal IP address, in either authentication mode.
- `cephfs.path` is clean and absolute, `/` for the root of the backing.
  `cephfs.provider` is `samba-vfs` or omitted; another provider refuses as not
  realized. A login `access` of `r` or `rw` refuses: the manager stores the
  long spelling `read` or `read-write`, which observation compares.
- `intent: removed` refuses: it is not apply-time removal authorization, and
  deletion comes only from the authorized removal lifecycle. `present` and
  omission are admitted.
- `auth_mode: user` takes no `domain_settings` and needs a non-empty
  `user_group_settings`; `auth_mode: active-directory` takes no
  `user_group_settings` and needs `domain_settings` with at least one join
  source. A `user_group_settings` entry with `source_type: empty` refuses as
  not realized: a user-mode cluster binds a usersgroups resource.
- `placement.count_per_host` above 1 refuses: one SMB daemon per host binds
  the SMB port. `clustering: never` refuses with more than one daemon; the
  daemon count is `placement.count`, else the eligible host count.
  `default` clusters unless `placement.count` is 1, as the manager module
  decides.
- A share with `restrict_access: true` needs at least one `login_control`
  entry.

Cross-object rules, each refusal naming the other object by its
`metadata.name` and never echoing a native value:

- Native identities are unique per StorageCluster: a cluster's `cluster_id`,
  a share's (`cluster_id`, `share_id`), a join.auth's `auth_id` and a
  usersgroups resource's `users_groups_id`. A second StorageSMBResource of
  the same type and identity refuses at the identity's last field.
- A share's effective name is `name`, else `share_id`, as the manager
  derives it; two shares of one `cluster_id` with the same effective name
  refuse, compared exactly. One share name on two SMB clusters is admitted.
- A share's `smbClusterRef` names a same-cluster `ceph.smb.cluster`
  StorageSMBResource, and the share's `cluster_id` equals that resource's
  `cluster_id`.
- The share's `cephfs` reference leaves agree with its backing. With
  `filesystemRef`, `cephfs.subvolumegroup` and `cephfs.subvolume` refuse,
  also in a fragment: a filesystem reference cannot smuggle an unbound
  subvolume name. With `subvolumeRef`, `cephfs.subvolume` equals the bound
  StorageSubvolume's name and `cephfs.subvolumegroup` its `group` (a
  subvolume in the native default group takes none). With either backing,
  `cephfs.volume` equals the bound filesystem's name. `cephfs.path: /` is the
  root of the backing and is admitted; a clean absolute path stays inside the
  backing.
- NFS and SMB never expose one backing: a share refuses unless every NFS
  export of its filesystem and the share bind distinct StorageSubvolumes; two
  SMB clusters never share one subvolume. Admission cannot see resolved
  subvolume paths, so a filesystem-wide export or share on either side
  refuses with the other on the same filesystem. The refusal names the
  StorageNFSExport, or the share of the other SMB cluster, whose subvolume
  earmark `smb.cluster.<cluster_id>` the manager would refuse at apply.
- A cluster's `resourceRefs` name same-cluster `ceph.smb.join.auth` and
  `ceph.smb.usersgroups` resources only. Each `domain_settings.join_sources[]`
  `ref` resolves to exactly one join.auth member by `auth_id`, and each
  `user_group_settings[]` `ref` of `source_type: resource` or omitted to
  exactly one usersgroups member by `users_groups_id`; a reference with no
  match or several matches refuses, and so does a member no native reference
  consumes. Several SMB clusters may name one auth resource.
- SMB clusters of one StorageCluster place on disjoint hosts: each binds the
  SMB port on every eligible host. Each cluster holds SMB (445) and SMB
  metrics (9922), and CTDB (4379) when clustered, on every eligible host,
  binding all addresses, in the
  [listener comparison](api/storage.md#complete-graph-invariants); another
  listener on one of those ports and hosts refuses, naming the port, the
  host and the other listener.
- `public_addrs[].address` is `address/prefix`, also in a fragment; a
  non-empty `public_addrs` needs clustering, also in a fragment, because CTDB
  assigns the addresses. Each address lies inside a declared public CIDR with
  that CIDR's prefix length, is neither its network nor its broadcast
  address, and equals no Machine address, no ingress virtual IP of any
  StorageCluster, no earlier address of its own list and no public address
  of another SMB cluster.

The SMB manager generates the orchestrator service `smb.<cluster_id>` and its
daemons, the pool `.smb` and the subvolume earmarks `smb.cluster.<cluster_id>`;
none can be declared: an `smb` service and `smb` in `spec.ceph.mgrModules`
refuse, and no StoragePool can be named `.smb`.

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

The SMB cluster and its users-and-groups resource; the cluster's
`user_group_settings` names the `users_groups_id`, and the user's password is
bound from a Secret:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageSMBResource
metadata:
  name: files-smb

spec:
  clusterRef: ceph-example
  resourceRefs:
    - files-users

  native:
    resource_type: ceph.smb.cluster
    cluster_id: files
    auth_mode: user
    user_group_settings:
      - source_type: resource
        ref: files
    placement:
      hosts:
        - node-03.ceph-example.clusters.example.com
      count: 1

---
apiVersion: bootwright.io/v1alpha1
kind: StorageSMBResource
metadata:
  name: files-users

spec:
  clusterRef: ceph-example

  native:
    resource_type: ceph.smb.usersgroups
    users_groups_id: files
    values:
      users:
        - name: smbuser
      groups: []

  secrets:
    - path: /values/users/0/password
      secretRef: files-smb-user
```

StorageSMBResource is admitted and realized by its SMB block once the samba
images of its StorageCluster are authored under `spec.ceph.images`; until then
planning refuses it as the [refusal table](storage.md#refusal-table) states.

## StorageExport

A `StorageExport` is the Ceph side of one Data Foundation consumer. Its
managed arm names what the consumer attaches; the export block realizes the
principals and the external-cluster details that a consuming package places
in its cluster ([add-on lifecycle](add-ons.md#built-in-packages-and-selection)).

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageExport
metadata:
  name: odf-compact

spec:
  clusterRef: ceph-example
  dataFoundation:
    rbdPoolRef: odf-compact-rbd
    filesystemRef: shared
    objectGatewayRef: objects
```

| Field | Contract |
| --- | --- |
| `type` | `dataFoundation`, normalized when that arm is present. |
| `clusterRef` | Required `StorageCluster`. |
| `dataFoundation.rbdPoolRef` | Required replicated StoragePool of role `rbd` of the same cluster. |
| `dataFoundation.filesystemRef` | Required StorageFilesystem of the same cluster declaring the subvolume group `csi`. |
| `dataFoundation.objectGatewayRef` | Optional StorageObjectGateway of the same cluster with an endpoint. |
| `dataFoundation.cephxKeyType` | `aes` (default) or `aes256k`: the type of every key the export mints. |
| `externalDetails.fromSecretRef` | An `opaque` Secret for an external cluster; refused, parked as [B107](milestones/backlog.md#b107). |

Admission also requires a declared prometheus service on the cluster, because
ocs-operator refuses details without a monitoring endpoint, and an export
name of at most 51 bytes, so the block identity `ceph-export-<export>` stays
within 63. A consuming package names each export at most once across the
input. `aes` is the default because kernel CephFS and RBD clients need
Linux 7.0 or later for `aes256k` keys; the operator chooses `aes256k` for
nodes that run it.

The export block depends on the cluster, the pool, the filesystem, the
gateway and the monitoring unit, is exclusive on its cluster, and runs on the
bootstrap host. Before any effect it proves the cluster's ownership record and
FSID, the pool's `rbd` application under its recorded ID, the `csi` group, a
non-empty `prometheus` endpoint in `ceph mgr services`, the gateway's realm,
zone group and zone, and that the monitors allow the declared key type; any
failure refuses, naming the missing prerequisite or the allowed key types.
It then mints, for this export and this apply operation, five CephX
principals and, with a gateway, one RGW user, each named from a digest of the
apply operation's identity and the export's name:

| Principal | `mon` | `mgr` | `osd` | `mds` |
| --- | --- | --- | --- | --- |
| health | `allow r, allow command quorum_status, allow command version` | `allow command config` | `profile rbd-read-only`, plus with a gateway `allow rwx pool=<zone>.rgw.meta, allow r pool=.rgw.root, allow rw pool=<zone>.rgw.control, allow rx pool=<zone>.rgw.log, allow x pool=<index pool>` | `allow *` |
| RBD node | `profile rbd, allow command 'osd blocklist'` | none | `profile rbd pool=<pool>` | none |
| RBD provisioner | `profile rbd, allow command 'osd blocklist'` | `allow rw` | `profile rbd pool=<pool>` | none |
| CephFS node | `allow r, allow command 'osd blocklist'` | `allow rw` | `allow rw tag cephfs *=<filesystem>` | `allow rw` |
| CephFS provisioner | `allow r, allow command 'osd blocklist'` | `allow rw` | `allow rw tag cephfs metadata=<filesystem>` | `allow *` |

These are the exporter's restricted-authorization capabilities, whose
privileged grants serve ocs-operator's health checks and CSI provisioning.
The RGW user carries `buckets=*;users=*;usage=read;metadata=read;zone=read`
and `info=read`, created in the gateway's realm, zone group and zone; its caps
apply to every user and bucket of the realm. It serves the operator's object
bucket provisioning.

Trust extent: an export trusts its consuming cluster's administrators with
every RGW user and bucket of the gateway's realm, read and write of the
zone's RGW metadata pool, which holds every RGW user's keys, and every path of
the named filesystem. Exports that share a cluster, filesystem or gateway are
not isolated from each other: per-export principals give per-export removal,
not isolation.

Each CephX key is generated at the declared type and retained under client
custody before its native add; a lost reply recaptures it and never
regenerates. The RGW user is created by `radosgw-admin user create
--uid=<minted> ... --gen-access-key --gen-secret`, never with
`--access-key` or `--secret-key` on any argument list; its JSON reply is read
privately and the key pair retained under client custody before the block
records progress. A lost reply recaptures the pair from `radosgw-admin user
info --uid=<minted>`, read privately, and refuses unless the user has exactly
one S3 key pair and the minted caps. An entity or RGW user of a minted name
that this block has no attempt record for refuses as foreign; a key of
another type refuses. The export never adopts, rotates, re-caps or removes an
entity it did not mint and never changes the monitors' cipher policy.

The block then assembles the details in the exporter's entry vocabulary and
offers them as its produced output `external-details` only from a proved
completion:

| Entry | Kind | Data |
| --- | --- | --- |
| `external-cluster-user-command` | ConfigMap | `args`: a fixed Bootwright marker |
| `rook-ceph-mon-endpoints` | ConfigMap | `data`: the quorum leader recorded at the first assembly as `<name>=<address>:3300`; `maxMonId` `0`; `mapping` `{}` |
| `rook-ceph-mon` | Secret | `fsid`; `admin-secret` and `mon-secret` as fixed placeholders |
| `rook-ceph-operator-creds` | Secret | the health principal |
| `monitoring-endpoint` | CephCluster | `MonitoringEndpoint`: the public addresses of the MGR hosts; `MonitoringPort`: the `prometheus` port |
| `rook-<principal>` for the four CSI principals | Secret | `userID`, `userKey` |
| `ceph-rbd` | StorageClass | `pool` and the RBD secret names |
| `cephfs` | StorageClass | `fsName`, the default data pool as `pool`, and the CephFS secret names |
| `ceph-rgw`, with a gateway | StorageClass | `endpoint`: `<fqdn>:<port>`; `poolPrefix`: the zone |
| `rgw-admin-ops-user`, with a gateway | Secret | `accessKey`, `secretKey` |
| `ceph-rgw-tls-cert`, with a TLS gateway | Secret | `cert`: the gateway certificate chain, never its key |

The details are a pure function of the retained keys and the non-secret facts
the block recorded at its first assembly, so a continuation reproduces the
same bytes. Before offering them the block checks them: every key is a
non-empty string of the declared type, the recorded leader is a monitor the
cluster's request declares, by name, and its address is that monitor's
declared public address, the monitoring endpoint names at least one address
and a port, `ceph-rgw` names exactly the gateway's endpoint, the
RGW keys are non-empty, and `ceph-rgw-tls-cert` is present exactly when the
gateway terminates TLS. Its evidence names the principals, their capabilities
and key type, the RGW user, the recorded leader and the entries by name and
kind; no key, key digest, certificate private key or detail reaches a
request, record, evidence, log, diagnostic or output. Observation compares
each principal's key and capabilities privately and never writes.

The export block retires with its cluster when every host is a
Bootwright-owned virtual Machine the same destroy removes; otherwise its
removal refuses with the cluster's other resources, and revoking one export
while the cluster stays is [B491](milestones/m2.md#b491)'s.

Not yet met: every StorageExport refuses as a later resource family; tracked
as [B762](milestones/m4.md#b762). No export block mints a principal
or produces details; tracked as [B86](milestones/m4.md#b86). The gateway's
endpoint name is not resolvable from a consuming cluster; tracked as
[B763](milestones/m4.md#b763). Whether ocs-operator and Rook accept a
`ceph-rgw-tls-cert` holding the certificate chain without the private key is
unverified, because Red Hat's document says the file includes private keys;
tracked as [B761](milestones/m4.md#b761). "Never its key" is Bootwright's
security decision, not a native requirement.

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
| CephX/client access, B80/B489 | `StorageClient` owns one principal with exactly one scope: an RBD pool with its derived data pools, a StorageFilesystem or a StorageSubvolume; B489 extends references to gateway resources and explicit access scope. | No default full administrator caps. Qualified caps derive from requested resource access or validated native capability syntax; unrestricted caps need an explicit privileged purpose. Manager-generated NFS/SMB principals are derived children, not duplicate resources. |
| NVMe-oF gateway resources, B480 | Native service/group, subsystem NQN, listener address/transport, namespace ID and `StorageRBDImage` reference; initiator NQN/host authorization links to the subsystem. | No automatic unowned image creation or wildcard host access. Defaults and generated IDs are frozen/captured. Listener reachability, ANA/HA and release-supported security are qualified together. |
| iSCSI resources, B481 | Not realized: no iSCSI kind exists, and admission refuses `service_type: iscsi` at that field. | Refused before any effect; block access over the network uses NVMe-oF (B480). No NVMe field substitutes for an iSCSI one. |
| Replication peers, B483 | Explicit source and destination cluster/resource references, protocol/replication mode, peer identity, endpoint trust and credential ownership. | Both endpoints are declared managed resources. One relationship owner coordinates bootstrap and removal; no ambient peer lookup, context crossing or foreign adoption. |
| Access material/handoff, B489 | Declared principal/resource set, selected trust/endpoints and produced-material identity; managed `StorageExport` material (its produced schema, principals and custody) belongs to [B86](milestones/m4.md#b86), which reuses this boundary; `StorageExport` retains its own consumer shape. | Material is scoped, privately captured and withdrawn through custody. A StorageClient's keyring is revealed only through the [client keyring export](cli.md#client-keyring-export); no other reveal command exists. |

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
| NVMe-oF client | RBD pool/image and native gateway service/group → subsystem → listener and image-backed namespace → explicit initiator authorization → qualified client endpoint/material. |
| Native CephFS client | Metadata/data pools → filesystem and bound MDS service → declared group → subvolume → scoped client access and resolved path → produced material. |
| NFS on CephFS | Filesystem/subvolume and native NFS service → individual export with derived principal; NFS service → mode-qualified ingress, when declared; client access waits for export and endpoint readiness. |
| SMB on CephFS | Filesystem/group/subvolume plus native SMB auth/usersgroups resources → SMB cluster → share; the SMB manager derives its daemons → qualified client access. |
| S3 | Realm/zone and pool layout → native RGW service bound to logical gateway → account/user → bucket/policy; declared ingress/public endpoint → scoped S3 access material. |
| NFS on RGW | Owned bucket, gateway and object principal plus native NFS service → bucket-bound native export → qualified endpoint and client restrictions. |
| Replication | Both declared cluster/resource endpoints and their access dependencies → explicit peer relationship → initial synchronization proof; native replication direction is not a cyclic Bootwright creation edge. |
| Data Foundation handoff | Declared RBD pool, filesystem, optional object gateway and scoped access dependencies → existing StorageExport material; downstream installation remains separate. Its material is [B86](milestones/m4.md#b86)'s. |

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
