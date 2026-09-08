# Ceph and storage services

This page defines `StorageCluster`, `StoragePlacementPolicy`, `StoragePool`,
`StorageFilesystem`, `StorageObjectGateway`, `StorageNFSExport` and
`StorageExport`. Fields use Bootwright camel case; only the explicitly declared
`config`, `spec` and `parameters` maps accept open payloads.
[The compiler boundary](../api.md#compiler-boundary) and
[native-field rules](../api.md#native-and-implementation-shaped-fields) apply.

## Shared shapes

Every reference on this page is a plain scalar string. Object-reference fields
use the fixed namespaces below; `addressRef` instead names one address nested
on the already selected Machine.

| Reference suffix or field | Target namespace |
| --- | --- |
| `clusterRef` | `StorageCluster` |
| `placementPolicyRef` | `StoragePlacementPolicy` |
| pool refs | `StoragePool` |
| `filesystemRef` | `StorageFilesystem` |
| `objectGatewayRef` | `StorageObjectGateway` |
| `machineRef` | `Machine` |
| `tls.secretRef` | `Secret` |
| entitlement refs | `Entitlement` |
| key, certificate, password, client-secret, cookie-secret, and external-detail refs | `Secret` |

Storage children select their owner through `spec.clusterRef`; they do not
repeat the cluster's `ceph` implementation arm. The reference is required and
is never inferred from directory placement or a sole cluster. A
`placementPolicyRef` is likewise explicit; a sole policy is not selected
automatically. `StorageCluster.spec.ceph` and the consumer-specific
`StorageExport.spec.dataFoundation` arm retain their own domains.

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
networks obey the complete-graph invariants below. An ingress collection is a
set keyed by `name`.

### Replicated protection

The replicated-pool shape `{size, minSize}` is shared by placement policies and
pools. Both members are optional non-negative integers. Zero and omission mean
that the applicable cluster, policy, or native default selects the value; a
non-zero value is positive. Validation resolves those effective values first
and requires effective `minSize` not to exceed effective `size`, including when
only one member is authored.

The ordinary managed-cluster effective pair is `3/2`; bootstrap
`singleHostDefaults` selects `2/1`, and stretch selects `4/2`. A referenced
placement policy supplies its resolved pair to the pool. These values qualify
validation but remain native or policy defaults unless an owning normalization
rule explicitly materializes them.

## StorageCluster

`StorageCluster` declares either one managed Ceph cluster or one externally
managed Ceph identity.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.type` | string | yes | — | Exactly `ceph`; it discriminates `spec.ceph`. |
| `spec.management` | string | no | `managed` | `managed` or `external`. |
| `spec.ceph` | object | conditional | — | Required for `managed`; forbidden for `external`. |

### Ceph identity, distribution, and bootstrap

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `ceph.distribution` | string | no | `oss` | `oss`, `redhat`, or `ibm`. |
| `ceph.release` | string | yes | — | Release spelling under the distribution-coordinate table below. |
| `ceph.packageVersion` | string | no | — | Native Ceph RPM coordinate under the distribution-coordinate table below. |
| `ceph.image` | object | no | — | Optional `{base, version}` daemon-image coordinate. |
| `ceph.image.base` | string | no | omitted | Bare registry/repository path without scheme, tag, or digest. |
| `ceph.image.version` | string | no | derived for an exact OSS release | Image tag or `sha256:` digest; mutable `latest` is not a pin. |
| `ceph.community` | object | no | — | OSS-only `{mirror, checksum}` source. |
| `ceph.community.mirror` | string | no | omitted | HTTPS package/bootstrap base URL. |
| `ceph.community.checksum` | string | no | — | SHA-256 checksum, normalized to `sha256:<lowercase-hex>`. |
| `ceph.ibm.callHome` | string | conditional | — | Required for IBM; `enabled` or `disabled`. |
| `ceph.ibm.packages.source` | string | conditional | `vendor` when the block is absent | `vendor` or `subscription`; once the block is authored, `source` is required. |
| `ceph.ibm.packages.subscriptionRepos` | array of strings | conditional | — | Non-empty only with `source: subscription`; rejected with `vendor`. |
| `ceph.entitlementRef` | string | conditional | — | `redhat-ceph` entitlement for Red Hat or `ibm-storage-ceph` for IBM; forbidden for OSS. |
| `ceph.osSubscriptionRef` | string | no | — | `redhat-rhel` entitlement for provided-OS nodes. |
| `ceph.cephadm` | object | yes | — | Bootstrap and cluster SSH policy. |
| `ceph.cephadm.addressRef` | string | no | — | Default Machine address name for Ceph hosts. |
| `ceph.cephadm.workarounds` | array of strings | no | `[]` | Set of closed tokens; the only accepted value is `mgmt-gateway-spec-dependency-recording`. |
| `ceph.cephadm.ansible.packageVersion` | string | no | — | Provider `cephadm-ansible` RPM coordinate. |
| `ceph.cephadm.clusterSSH.user` | string | no | `cephadm` for managed; `root` for external | Cluster-wide Ceph orchestration login. |
| `ceph.cephadm.clusterSSH.keyRef` | string | conditional | — | `sshKeyPair` Secret; required for a non-root cluster login. |
| `ceph.cephadm.bootstrap.node` | string | yes | — | One topology node name, not a Machine name. |
| `ceph.cephadm.bootstrap.addressRef` | string | no | cephadm default, then node SSH address | Bootstrap monitor address name. |
| `ceph.cephadm.bootstrap.singleHostDefaults` | boolean | no | `false` | Valid only for one non-stretch node with at least two declared OSDs. |

Distribution coordinates obey these complete lexical and presence rules:

| Coordinate | Rule |
| --- | --- |
| OSS `release` | `[A-Za-z][A-Za-z0-9-]*` or an exact three-part decimal `major.minor.patch`. Preserve spelling; there is no release-alias catalog or alias rewriting. |
| Red Hat and IBM `release` | `[0-9]+(?:\.[0-9]+)+`; preserve the dot-separated numeric product version. |
| `packageVersion`, `cephadm.ansible.packageVersion` | `[A-Za-z0-9][A-Za-z0-9._+~^:-]*`; match the complete value. |
| OSS `image.version` | If omitted and `release` is an exact three-part version, derive `v<release>`. An OSS name does not infer an image version. |
| Red Hat and IBM `image.base` | When supplied, a strict descendant of the selected Entitlement's `registry.url`, comparing registry/repository path segments. A textual prefix or equal path is insufficient. |
| Omitted `image.base` and `community.mirror` | Remain omitted; admission supplies no native image or mirror default. |

| Distribution | Required and forbidden coordinates |
| --- | --- |
| `oss` | Forbids `entitlementRef`, the `ibm` arm, `packageVersion`, and `cephadm.ansible.packageVersion`. |
| `redhat` | Requires a `redhat-ceph` Entitlement. Package and image coordinates are optional and follow the table above. |
| `ibm` | Requires an `ibm-storage-ceph` Entitlement, `packageVersion`, `cephadm.ansible.packageVersion`, explicit `image.base` and `image.version`, and `ibm.callHome`. |

Vendor image validation has no hardcoded product repository or release-image
catalog. Version syntax validation does not claim that a syntactically valid
combination is vendor-supported.

`clusterSSH.keyRef` obeys the
[fleet-key separation rule](environment.md#remote-machine-access-and-install-defaults).

### Networks, security, configuration, and services

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `ceph.networks.publicCIDRs` | array of strings | no | `[]` | Set of valid public-network CIDRs; effective state masks host bits. |
| `ceph.networks.clusterCIDRs` | array of strings | no | `[]` | Set of valid replication-network CIDRs; effective state masks host bits. |
| `ceph.security.fips.enabled` | boolean | no | `false` | Only for `redhat` or `ibm`; installed nodes select a FIPS install profile. |
| `ceph.security.cephx.keyType` | string | conditional | — | Required when `cephx` is present; `aes` or `aes256k`. |
| `ceph.config` | object of object of string | no | `{}` | Ceph config section → key → non-empty value. |
| `ceph.mgrModules` | array of strings | no | `[]` | Set of manager modules to enable. |
| `ceph.services` | array of objects | no | `[]` | Generic cephadm services not owned by first-class fields. |
| `ceph.services[].serviceType` | string | yes | — | Native service type. |
| `ceph.services[].serviceID` | string | no | — | The `(serviceType, serviceID)` pair is unique. |
| `ceph.services[].placement` | `StoragePlacement` | yes | — | Must set `hosts` or `sites`. |
| `ceph.services[].spec` | object | no | `{}` | Native service map preserved field-for-field. |

Public and cluster CIDRs are internally non-overlapping and mutually disjoint.
Service networks and VIPs must be reachable through relevant declared
networks. `ceph.config` rejects keys owned by typed fields, including
`public_network`, `cluster_network`, and `container_image`. First-class MON,
MGR, OSD, MDS, RGW, NFS, ingress, monitoring, and management-gateway surfaces
cannot be duplicated through `ceph.services`.

### Monitoring

The `ceph.monitoring` block is presence-enabled. If absent, a cephadm renderer
leaves the upstream default stack alone. When present,
`monitoring.enabled` has effective default `true`; `false` selects native skip
behavior.

The optional service keys are `prometheus`, `grafana`, `alertmanager`,
`nodeExporter`, `loki`, and `promtail`. Each service object has:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `placement` | `StoragePlacement` | no | role-derived where a role exists | Role-less services need explicit placement. |
| `port` | integer | no | native default | Zero or omission leaves the native default; a non-zero value is `1..65535`. |
| `retentionTime` | string | no | native default | Positive duration under the shared lexical table; Prometheus only. |
| `retentionSize` | string | no | renderer policy | Positive size under the shared lexical table; Prometheus only. |
| `networks` | array of strings | no | `[]` | Ordered unique valid CIDRs; effective state masks host bits. |
| `initialAdminPasswordRef` | string | no | — | `opaque` or `token` `Secret`; Grafana only. |

`enabled: false` rejects service blocks.

### Management gateway

Authoring `ceph.mgmtGateway` enables the management-gateway shape and requires
its `ingress`.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `dnsLabel` | string | no | `mgr` | One DNS label. |
| `port` | integer | no | effective `8443` for HTTPS; `8888` for HTTP | Zero or omission selects the consumer default but remains absent canonically; a non-zero value is `1..65535`. |
| `exposure` | string | no | `https` | `https` or `http`. |
| `enableAuth` | boolean | no | `false` | `true` requires `oauth2Proxy`; false rejects it. |
| `tls.secretRef` | string | with `tls` | — | One `tlsCertificate` Secret supplying both certificate and key. |
| `oauth2Proxy.providerDisplayName` | string | conditional | — | Required when OAuth is enabled. |
| `oauth2Proxy.clientId` | string | conditional | — | Required when OAuth is enabled. |
| `oauth2Proxy.clientSecretRef` | string | conditional | — | `opaque` or `token` Secret reference. |
| `oauth2Proxy.oidcIssuerUrl` | string | conditional | — | HTTPS OIDC issuer URL. |
| `oauth2Proxy.redirectUrl` | string | no | — | Optional redirect URL. |
| `oauth2Proxy.httpsAddress` | string | no | — | Optional HTTPS listener. |
| `oauth2Proxy.allowlistDomains` | array of strings | no | `[]` | Ordered unique domain allowlist. |
| `oauth2Proxy.cookieSecretRef` | string | no | — | `opaque` or `token` Secret reference. |
| `ingress` | `StorageIngress` | yes | — | Management-gateway VIP; transport and authentication belong to the management gateway. |

HTTP rejects TLS and OAuth. The workaround token requires the
HTTP/no-TLS/no-OAuth shape and is never inferred from a release.

### Topology

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `ceph.topology.nodes` | array of objects | yes | — | Non-empty set keyed by node `name`. |
| `nodes[].name` | string | yes | — | DNS label used as the cluster-local node identity. |
| `nodes[].fqdn` | string | no | composed from node, cluster, and storage domain | Explicit external FQDN override. |
| `nodes[].machineRef` | string | yes | — | `Machine` with the `ceph-node` capability. |
| `nodes[].site` | string | no | bound Machine site | Must agree with the Machine; required by stretch and site placement. |
| `nodes[].roles` | array of strings | yes | — | Non-empty set of `mon`, `mgr`, `osd`, `mds`, `rgw`, `ingress`, `prometheus`, `grafana`, and `alertmanager`. |
| `nodes[].labels` | array of strings | no | `[]` | Additional unique cephadm labels, disjoint from roles. |
| `nodes[].devices` | array of strings | no | `[]` | Literal data-device shorthand; mutually exclusive with `osd`. |
| `nodes[].osd` | object | no | — | Per-node OSD drivegroup shape. |
| `ceph.topology.osdDrivegroups` | array of objects | no | `[]` | Cluster-wide OSD drivegroups. |
| `osdDrivegroups[].serviceID` | string | yes | — | Unique service ID. |
| `osdDrivegroups[].placement` | `StoragePlacement` | no | every OSD-role host | A host is owned by at most one OSD declaration. |
| `osdDrivegroups[].osd` | object | yes | — | Same OSD shape as `nodes[].osd`. |

Every OSD-role node has exactly one per-node device declaration or cluster
drivegroup. Bootstrap, placement and tiebreaker tokens name storage nodes,
not Machines; the common graph rules enforce unique Machine binding.

An OSD object contains:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `dataDevices` | device selector | yes | — | Data devices. |
| `dbDevices` | device selector | no | — | BlueStore DB devices. |
| `walDevices` | device selector | no | — | BlueStore WAL devices. |
| `encrypted` | boolean | no | `false` | Enables LUKS OSDs. |
| `osdsPerDevice` | integer | no | native default | Non-negative. |
| `crushDeviceClass` | string | no | — | Drivegroup CRUSH class. |
| `filterLogic` | string | no | `AND` | `AND` or `OR`. |
| `blockDBSize` | string | no | — | Positive size under the shared lexical table. |
| `blockWALSize` | string | no | — | Positive size under the shared lexical table. |
| `dbSlots` | integer | no | — | Non-negative. |
| `walSlots` | integer | no | — | Non-negative. |
| `dataAllocateFraction` | finite number | no | native `1` | Zero or omission leaves the field unset for the native default; a non-zero value is greater than zero and at most one. Integer `1` is accepted. |
| `tpm2` | boolean | no | `false` | Requires `encrypted: true` and TPM prerequisites on every covered Machine. |
| `unmanaged` | boolean | no | `false` | Native unmanaged service flag. |
| `serviceOverrides` | object | no | — | Common service overrides below. |

`serviceOverrides` contains arrays `extraContainerArgs`,
`extraEntrypointArgs`, and `networks`, plus
`customConfigs[]: {mountPath, content}`. Networks are valid CIDRs and normalize
to canonical text; `mountPath` is a clean absolute path; `content` is
non-empty; and argument and content strings are preserved.

Each device selector has exactly these fields:

| Field | Type | Rule |
| --- | --- | --- |
| `paths` | array of strings | Literal clean absolute device paths beneath `/dev/`. |
| `pathSpecs` | array of `{path, crushDeviceClass?}` | Expanded per-path CRUSH-class form; every path is clean, absolute, and beneath `/dev/`. |
| `all` | boolean | May be true only for `dataDevices`. |
| `model` | string | Native device-model filter. |
| `vendor` | string | Native vendor filter. |
| `rotational` | boolean | Presence-sensitive native rotational filter. |
| `size` | string | Exact size or range under the shared lexical table. |
| `limit` | integer | Non-negative match cap. |

`paths`, `pathSpecs`, and `all` are mutually exclusive. Literal paths cannot
combine with model/vendor/rotational/size filters; `all` cannot combine with
them. A selector must select something, and `limit` alone is insufficient.
DB/WAL selectors cannot use `all`. Desired-state validation checks authored
bounds but does not inspect live disks. A mutator must resolve dynamic
selectors and prove device identity and safety before any destructive effect.

### Stretch

The presence of `ceph.topology.stretch` declares the established two-data-site
shape. A complete tiebreaker block enables native stretch mode; an omitted or
empty block remains valid desired state but does not enable that mode.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `failureDomain` | string | yes | — | CRUSH failure-domain token. |
| `dataSites` | array of strings | no | derived non-tiebreaker sites | Exactly two MON-bearing data sites. |
| `tiebreaker` | object | no | — | Optional independent-tiebreaker declaration; an empty block is deferred. |
| `tiebreaker.node` | string | conditional | — | Required once either tiebreaker member is non-empty; mon-only, non-OSD topology node in a third site. |
| `tiebreaker.site` | string | no | tiebreaker node site | Must differ from both data sites. |
| `ruleName` | string | no | `stretch-rule` | Stable stretch CRUSH rule. |

An omitted or all-empty `tiebreaker` defers native activation. Once either
member is non-empty, `node` is required and `site` defaults from that node, so
both effective members must be present. A deferred declaration makes
`validate` emit an `api.deferred` warning that a native renderer will not
enable native stretch; `render effective` accepts the state but has no
success-warning channel. Stretch-block presence always rejects erasure pools
and fixes replicated protection at size four and minimum size two; a policy or
pool that authors non-zero values must agree.

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
those values; an empty or all-zero block is the unset value shape.
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
| `spec.replicated` | `{size?, minSize?}` | no | native policy | Non-zero members are only for `replicated` and without a placement policy; an empty or all-zero block is unset. |
| `spec.erasure` | object | conditional | — | Required exactly for `type: erasure`. |
| `spec.autoscale` | object | no | — | PG autoscale policy. |
| `spec.quota` | object | no | — | Pool quota. |
| `spec.compression` | object | no | — | BlueStore compression. |
| `spec.mirroring.mode` | string | no | — | `image` or `pool`; RBD only. |

With `type: replicated`, the `erasure` arm is forbidden. With `type: erasure`,
`erasure` is required and non-zero `replicated` values are forbidden; an empty
or all-zero `replicated` block remains the unset value shape. If `type` is
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
effective state; authored values are preserved. This includes role-derived
placement, inferred pool application, NFS export
path/access defaults, monitoring enablement when merely implicit, and native
service defaults.

## Complete-graph invariants

In addition to the field-local rules above, desired-state validation checks:

- management mode, distribution, entitlement, image, package, bootstrap,
  topology, network, FIPS, CephX, and cluster-login declarations agree;
- node roles support every derived or explicit placement and stretch/site
  capacity is sufficient;
- OSD selectors are bounded and internally consistent and exclude the
  Machine's root device from explicit data, DB, and WAL paths;
- policies, pools, filesystems, gateways, NFS exports, and storage exports
  reference the same cluster under each kind's management-mode constraints and
  use compatible pool roles and protection modes; and
- addresses, CIDRs, DNS labels, paths, ports, service IDs, VRRP IDs, config
  ownership, and Secret types do not conflict across the aggregate graph.

These checks qualify desired intent only. Live capacity, device safety,
release support, external-cluster identity, and native-schema compatibility
remain gates of explicitly supported renderer or lifecycle slices.
