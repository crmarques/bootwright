# Storage field detail

[B472](../milestones/m2.md#b472) revives this detail when it qualifies IBM Ceph
9.9.x lifecycle inputs; [B55](../milestones/m7.md#b55) later reuses that
qualification for standalone native files. Until then the [storage admission
contract](../api/storage.md) owns the kinds, references, replicated protection
and graph invariants, and the closed schema in `api/v1alpha1/storage.go` is
authoritative for field names, types and shapes. The core cluster, pool and
client fields are realized for the pinned release
([X70](../milestones/delivered.md#x70--ceph-through-the-ordinary-staged-apply));
later resource family tables stay historical, not the approved M2 target, until
[B472](../milestones/m2.md#b472) revives them. The [native
input](../storage-native-api.md) and [resource
contracts](../storage-resources.md) replace its role-derived placement, daemon
helpers and combined NFS service/export ownership. Current admission refuses
those unsupported families. Native defaults still require B472 qualification.

## Shared shapes

### Storage lexical values

The following grammars match the complete value, without surrounding
whitespace, signs, fractional parts or additional suffixes. `N` denotes one or
more decimal digits with a positive integer value; leading zeros are preserved.
Units and tokens are case-sensitive. These checks qualify declarative syntax,
not compatibility with a particular native release.

| Fields | Accepted value |
| --- | --- |
| Prometheus `retentionTime` | `N` followed by one of `y`, `w`, `d`, `h`, `m`, `s`. |
| Prometheus `retentionSize` | `N` followed by one of `B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`. |
| OSD `blockDBSize`, `blockWALSize`; pool `autoscale.targetSizeBytes` | `N`, optionally followed by `B`, `K`, `M`, `G`, `T`, `P`, `E`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`. |
| Device selector `size` | One bound, `LOW:HIGH`, `LOW:`, or `:HIGH`; each bound is `N` followed by `M`, `G`, `T`, `MB`, `GB`, or `TB`. Both bounds cannot be empty. Compare bounds using powers of 1024 and require `LOW <= HIGH`. |
| Pool `erasure.stripeUnit` | A positive YAML decimal integer measured in bytes. |
| Pool `compression.minBlobSize`, `compression.maxBlobSize` | YAML decimal integers in `0..18446744073709551615`; when both are positive, minimum must not exceed maximum. |
| Pool `quota.maxBytes`, `quota.maxObjects` | YAML decimal integers in `0..9223372036854775807`; preserve explicit zero as the native no-limit value. |
| Filesystem subvolume-group `mode` | YAML string matching `0?[0-7]{3}`; a YAML integer is a type error. |

### StoragePlacement

`StoragePlacement` is used by monitoring, generic services, MDS, RGW, NFS, and
ingress declarations.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `hosts` | array of strings | no | role-derived placement where the owner defines a role | Ordered unique storage node names. Machine names are not accepted. |
| `sites` | array of strings | no | — | Ordered unique topology site names. |
| `countPerHost` | integer | no | native default | Non-negative native `count_per_host`. |

When both `hosts` and `sites` are present, every selected host must belong to a
selected site. A generic `services[]` entry and an NFS service must explicitly
set `hosts` or `sites`; role-owned first-class services may derive placement
from topology roles. Placement never accepts a label, host pattern, count, or
free-form expression.

An explicit host list remains fixed when topology roles change, and every
listed host must still satisfy the owning service's role constraints. Omitted
role-owned placement follows the eligible role membership.

### StorageIngress

`StorageIngress` is a shared nested shape for a service-owned VIP, not a
resource kind. It does not choose a backend or share service ownership. The
management gateway, RGW endpoint, and NFS service retain their own transport,
TLS, authentication, and lifecycle rules.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Unique native ingress service name. |
| `address` | string | yes | — | Canonical VIP address. |
| `prefixLength` | integer | yes | — | Valid for the VIP address family. |
| `virtualInterfaceNetworks` | array of strings | no | `[]` | Ordered unique valid CIDRs; effective state masks host bits. |
| `placement` | `StoragePlacement` | no | ingress-role hosts | Stretch placement covers both data sites unless site-narrowed. |
| `firstVirtualRouterID` | integer | no | native default | Zero or omission leaves the native default; a non-zero value is `1..255` and unique on overlapping ingress networks. |

VIPs, prefixes, potentially overlapping ports, placements, and overlapping
networks obey the
[complete-graph invariants](../api/storage.md#complete-graph-invariants). An
ingress collection is a set keyed by `name`.

## StorageCluster

Identity, distribution, release/package/image, entitlement, bootstrap/clusterSSH,
networks and security retain their Bootwright envelope. The closed schema in
`api/v1alpha1/storage.go` owns these fields. [Native service input](../storage-native-api.md)
replaces daemon helpers with required complete `spec.ceph.services` documents;
current core family schemas live in `api/v1alpha1/storage_native.go`.

Topology nodes retain `name`, `fqdn`, `machineRef`, `site`, `labels` and
`location`. Daemon roles and devices belong to native placement and
DriveGroupSpec. Legacy `monitoring`, `mgmtGateway`, `osd`, `driveGroups`, node
`roles`/`devices` and camel-case native identities are retired and refused.
`serviceBindings` descriptors carry typed Secret references; no core secret
leaf is qualified. Stretch and future families remain refused by the
[current admission contract](../api/storage.md).

## StorageClient

Required scalar `clusterRef`, `poolRef` and `access` (`read-only` or
`read-write`) declare one same-cluster RBD principal. The
[resource contract](../storage-resources.md#storageclient) owns its protected
custody semantics. Native execution remains gated.

## StoragePlacementPolicy

This kind owns a reusable CRUSH placement policy and replicated defaults for
one managed Ceph cluster.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.clusterRef` | string | yes | — | Managed `StorageCluster`. |
| `spec.failureDomain` | string | no | — | CRUSH failure domain. |
| `spec.ruleName` | string | yes | — | Stable CRUSH rule name. |
| `spec.crushDeviceClass` | string | no | — | Optional device class. |
| `spec.replicated.size` | integer | no | native or stretch default | Non-negative replica count; zero is unspecified. |
| `spec.replicated.minSize` | integer | no | native or stretch default | Non-negative minimum; zero is unspecified and the effective minimum does not exceed effective size. |

A referenced policy and pool name the same cluster. A pool with a policy
cannot also author non-zero `spec.replicated` values because the policy owns
those values, under [replicated protection](../api/storage.md#replicated-protection).
Host-domain replica capacity cannot exceed the number of OSD hosts.

## StoragePool

This kind owns one pool. Its `spec.type` selects protection, while the
referenced cluster selects the storage engine.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.clusterRef` | string | yes | — | Managed `StorageCluster`. |
| `spec.placementPolicyRef` | string | no | — | Same-cluster `StoragePlacementPolicy`. |
| `spec.type` | string | no | `replicated` | `replicated` or `erasure`; discriminates the protection arm. |
| `spec.role` | string | no | — | `rbd`, `cephfs-metadata`, `cephfs-data`, or `rgw`. |
| `spec.application` | string | no | inferred from role by a native renderer | Native application name. |
| `spec.replicated` | `{size?, minSize?}` | no | native policy | Non-zero members are only for `replicated` and without a placement policy; see [replicated protection](../api/storage.md#replicated-protection). |
| `spec.erasure` | object | conditional | — | Required exactly for `type: erasure`. |
| `spec.autoscale` | object | no | — | PG autoscale policy. |
| `spec.quota` | object | no | — | Pool quota. |
| `spec.compression` | object | no | — | BlueStore compression. |
| `spec.mirroring.mode` | string | no | — | `image` or `pool`; RBD only. |

With `type: replicated`, the `erasure` arm is forbidden. With `type: erasure`,
`erasure` is required and non-zero `replicated` values are forbidden under
[replicated protection](../api/storage.md#replicated-protection). If `type` is
still omitted after Environment defaults, it selects `replicated`, even if
an erasure arm was authored. An erasure pool therefore needs `type: erasure`
from its own spec or an applicable default. A placement policy does not change
the protection discriminator.

The erasure arm contains `dataChunks`, `codingChunks`, `plugin`,
`technique`, `crushDeviceClass`, `crushRoot`, `stripeUnit`, and
`parameters: map<string,string>`. Data and coding chunks are positive and
required for erasure. `plugin` is empty or `jerasure`, `isa`, `clay`,
`lrc`, or `shec`. The parameters map cannot repeat keys owned by typed fields
or the derived failure domain. `stripeUnit` follows the shared lexical table.

Autoscale contains `mode` (`on`, `off`, or `warn`),
`targetSizeRatio` (finite non-negative number), `targetSizeBytes` (string),
`pgNumMin`, `pgNumMax`, and presence-sensitive `bulk`. Ratio and bytes are
mutually exclusive; zero or omission leaves the ratio unspecified. PG bounds
are non-negative and minimum does not exceed maximum. `targetSizeBytes`
follows the shared lexical table.

Quota contains presence-sensitive non-negative signed 64-bit `maxBytes` and
`maxObjects`; zero is the native no-limit value and remains explicit, while
omission means unspecified.
Compression contains `mode` (`none`, `passive`, `aggressive`, or `force`),
`algorithm` (empty, `lz4`, `snappy`, `zlib`, or `zstd`),
`requiredRatio` (finite number; zero/omitted or `(0,1]`), `minBlobSize`, and
`maxBlobSize`. The blob sizes follow the shared unsigned 64-bit bounds and
positive-pair ordering rule. Other compression fields require a mode.

Pool roles agree with filesystem, gateway, NFS, and export consumers.
CephFS metadata pools are replicated. Erasure is limited to supported roles,
cannot be used with stretch, and `k+m` cannot exceed OSD-host capacity.

## StorageFilesystem

This kind owns one CephFS filesystem, its composition of pool references, and
its MDS service policy. The referenced `StoragePool` objects own the pools.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.clusterRef` | string | yes | — | Managed `StorageCluster`. |
| `spec.metadataPoolRef` | string | yes | — | Same-cluster `cephfs-metadata` pool. |
| `spec.dataPoolRefs` | array | yes | — | Non-empty ordered unique same-cluster `cephfs-data` pools. |
| `spec.mds` | object | no | — | MDS service policy. |
| `spec.subvolumeGroups` | array | no | `[]` | Set keyed by group name. |

Each data-pool entry accepts either the scalar pool name or
`{name: <pool>, default: <boolean>}`. A single entry normalizes to
`default: true`; multiple entries require exactly one explicit default.

`mds` contains `activeCount`, `standbyReplay`, `standbyCountWanted`,
`placement`, and optional `serviceSpec`. Counts are non-negative. In an
authored block, consumers use effective `activeCount: 1` and
`standbyReplay: false` when those fields are absent, but normalization does not
write either value. An omitted `mds` block remains absent.
`serviceSpec` contains only `unmanaged`, `extraContainerArgs`,
`extraEntrypointArgs`, and `networks`.

Each subvolume group contains `name`, optional `poolLayoutRef`, octal string `mode`,
presence-sensitive non-negative `uid` and `gid`, and non-negative
`sizeBytes`. Names are unique, and a layout pool belongs to the same cluster.

## StorageObjectGateway

This kind owns one RGW service and its optional public endpoint. Ingresses
belong to that endpoint and cannot select another backend service.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.clusterRef` | string | yes | — | Managed `StorageCluster`. |
| `spec.serviceID` | string | yes | — | Unique RGW service ID. |
| `spec.placement` | `StoragePlacement` | no | role-derived from `rgw` hosts | RGW placement. |
| `spec.frontendPort` | integer | no | `8080` | Zero or omission selects `8080`; a non-zero value is `1..65535`. |
| `spec.realm` | string | no | — | Multisite realm; set with zone group and zone. |
| `spec.zoneGroup` | string | no | — | Multisite zone group. |
| `spec.zone` | string | no | — | Multisite zone. |
| `spec.config` | map of string to string | no | `{}` | Per-service config; values are non-empty. |
| `spec.endpoint` | object | no | — | Presence enables public ingress; absence creates none. |
| `spec.endpoint.dnsLabel` | string | no | object name | One DNS label under the storage domain. |
| `spec.endpoint.scheme` | string | no | `https` | `https` or `http`. |
| `spec.endpoint.port` | integer | no | `443` for HTTPS; `80` for HTTP | Explicit values are `1..65535`; zero is invalid. |
| `spec.endpoint.tls` | object | conditional | — | Required for HTTPS; forbidden for HTTP. |
| `spec.endpoint.tls.secretRef` | string | yes in `tls` | — | One `tlsCertificate` Secret supplying both certificate and key. |
| `spec.endpoint.ingresses` | array of `StorageIngress` | yes in `endpoint` | — | Non-empty set keyed by ingress name. |

Realm, zone group, and zone are all-or-nothing. The config map cannot repeat a
cluster config key for the same service or own `rgw_frontend_port`.

All ingresses share the endpoint's DNS identity, scheme, public port, and TLS
Secret. Per-ingress transport or TLS overrides are forbidden. The public port
is independent of the RGW backend `frontendPort`; neither defaults from the
other. Endpoint defaults apply only when the endpoint is present. An empty
endpoint, an empty ingress list, or HTTPS with an empty or omitted TLS block
is invalid. Explicit HTTP rejects a TLS block even when it is empty.

## StorageNFSExport

This kind owns one NFS-Ganesha service and zero or more export entries; there
is no separate `StorageNFSService` kind.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.clusterRef` | string | yes | — | Managed `StorageCluster`. |
| `spec.serviceID` | string | yes | — | Unique NFS service ID. |
| `spec.port` | integer | no | `2049` without ingress; `12049` with ingress | Zero or omission selects the default; a non-zero value is `1..65535`, and an ingress-fronted service cannot use backend `2049`. |
| `spec.placement` | `StoragePlacement` | yes | — | Must set hosts or sites. |
| `spec.ingresses` | array of objects | no | `[]` | `StorageIngress` fields plus the optional TLS block below. |
| `spec.exports` | array | no | `[]` | Set keyed by `pseudo`. |

An NFS ingress may additionally contain `tls: {secretRef}`. The required
reference names one `tlsCertificate` Secret supplying certificate and key. NFS
keeps its service-owned ingress transport; it does not use the RGW HTTP endpoint
shape.

Each export entry contains:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `pseudo` | string | yes | — | Unique clean absolute NFSv4 pseudo path. |
| `filesystemRef` | string | conditional | — | Same-cluster `StorageFilesystem`. |
| `path` | string | no | `/` for CephFS | Clean absolute path. |
| `bucket` | string | conditional | — | RGW bucket name. |
| `accessType` | string | no | `RW` | `RW`, `RO`, or `NONE`. |
| `squash` | string | no | native default | Native squash token. |
| `clients` | array of strings | no | `[]` | Ordered unique valid IPs or CIDRs; effective state canonicalizes them. |

Exactly one of `filesystemRef` and `bucket` is set. Service IDs, ingress IDs,
VIPs, and potentially overlapping ports are unique across the complete
same-cluster storage graph.

## StorageExport

`StorageExport` is a discriminated union for a downstream storage surface.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.type` | string | conditional | `dataFoundation` when that arm is present | Currently `dataFoundation`. External exports author it. |
| `spec.clusterRef` | string | yes | — | Managed or external `StorageCluster`. |
| `spec.dataFoundation` | object | conditional | — | Managed-cluster arm. |
| `spec.externalDetails` | object | conditional | — | External-cluster arm. |

For a managed cluster, `dataFoundation` is required and `externalDetails` is
forbidden. It contains required same-cluster `rbdPoolRef` and
`filesystemRef`, plus optional same-cluster `objectGatewayRef`. The pool has
role `rbd`.

For an external cluster, `externalDetails` is required and `dataFoundation`
is forbidden. Its `fromSecretRef` is a scalar `opaque` `Secret` reference.

## Normalization

After [Environment defaults](../api.md#defaults-normalization-and-effective-state)
have been applied, the kind normalizer materializes deterministic API-owned
defaults and normalizes these values:

- `StorageCluster.spec.management: managed` and
  `spec.ceph.distribution: oss`;
- an OSS image version derived from an exact OSS release and canonical SHA-256
  checksum spelling, preserving release and package-coordinate spelling;
- bootstrap `addressRef` from the cephadm default and the effective cluster
  SSH user;
- composed storage-node FQDNs, corresponding placement/bootstrap/tiebreaker
  tokens, Machine-derived sites, `mgmtGateway.dnsLabel: mgr`, stretch sites, a
  tiebreaker with a node's site, and `ruleName: stretch-rule`;
- `StoragePool.spec.type: replicated`;
- the sole CephFS data pool as `default: true`;
- object-gateway `frontendPort: 8080`; for a present endpoint only, its DNS
  label from object name, `scheme: https`, and public port `443` or `80`
  according to the effective scheme;
- NFS backend port `2049` or `12049` according to ingress presence; and
- `StorageExport.spec.type: dataFoundation` when that arm is authored.

Implicit defaults owned by Ceph or a native renderer remain absent from
effective state. Apart from the canonicalizations above, authored values are
preserved; in particular, the sole CephFS data pool becomes `default: true`
even if `false` was authored. Defaults left implicit include role-derived
placement, inferred pool application, NFS export
path/access defaults, monitoring enablement when merely implicit, and native
service defaults.
