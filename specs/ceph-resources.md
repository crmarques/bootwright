# Managed storage resource graph

This page owns the target resource contract approved through P2 in the
[M2a API review](storage-api-review.md). The
[native service contract](ceph-native-api.md) owns daemon specifications
and their bindings; [Storage](ceph.md) owns lifecycle, device safety and
custody. [Provider coverage](ceph-coverage.md) owns release qualification.

Native projections cite Ceph v20.2.1; [Provider coverage](ceph-coverage.md#release-qualification)
qualifies the IBM image. [Storage admission](api/ceph.md) states the refusals,
and the [milestones](milestones.md) own delivery state.

## Shared resource contract

Every resource retains the `bootwright.io/v1alpha1` kind/metadata/spec
envelope and has a required scalar `spec.clusterRef`. Except for the existing
external `CephConsumer` arm, this resolves to a declared managed
`CephCluster` in the same compiled graph. No resource discovers its owner
from a matching native name, singleton object or directory. Cross-context
adoption and external replication peers are not introduced here.

Bootwright references name declared objects, not arbitrary native paths.
Local references require the same cluster. An explicit replication relationship
is the cross-cluster exception: each endpoint's resources validate against its
own declared cluster. Native service references resolve the
full qualified identity, such as `mds.shared` or `rgw.objects`, within the
selected cluster's service declarations. A service ID alone is accepted only
where its owning field fixes the service type, as `CephNFSExport.serviceID`
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
[shared secret-binding contract](ceph-native-api.md#secret-bindings).
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
Numeric limits and graph capacity are checked before effects; the native
tools judge their own limits and report a refusal through `nativeError`. No
new field inherits an undocumented default from another resource.

Removal of a resource while its cluster remains refuses; a resource retires with
its cluster's hosts, as [removal](ceph.md#removal-and-concurrency) states.

## Retained resource owners

| Resource | Retained responsibility | Relationship to native services |
| --- | --- | --- |
| `CephPlacementPolicy` | Reusable CRUSH placement and pool protection policy. | OSD topology supplies its proved failure domains; policy does not deploy OSDs. |
| `CephPool` | Pool identity, replicated/EC protection, application, PG/autoscaler, quota, compression and supported mirroring intent. EC `rbd`/`cephfsData` pools derive irreversible overwrite enablement; mirroring is [B483](milestones/m2.md#b483)'s. | Services and resources reference owned pools; native auto-created pools require an explicitly recorded derived owner. |
| `CephFilesystem` | Metadata/data-pool composition, default/layout, active ranks and standby/replay resource policy, declared subvolume groups; several filesystems per cluster. | `spec.mdsServiceRef` binds one declared MDS service whose `service_id` is the filesystem name; placement and container configuration belong to that service. One `ceph fs new` creates the native filesystem. |
| `CephObjectGateway` | The realm, zone group and zone it creates (never with `--default`; only an export of one of its users makes them the defaults, under [CephNFSExport](#cephnfsexport)), its zone placement binding, its derived zone pools and its endpoint identity. | `spec.serviceRef` binds exactly one declared rgw service by full native identity, as the [field record](#cephobjectgateway-field-record) states; the gateway embeds no daemon placement, frontend, configuration or ingress specification. |
| `CephConsumer` | Managed Data Foundation handoff with per-consumer principals and private external-cluster details. The external-details arm remains refused under parked [B107](milestones/backlog.md#b107). | It waits for the resources and access material it describes; it neither creates an add-on nor becomes a general native-command resource. |

Their non-daemon fields remain unless an owning approved change alters them. A
filesystem's subvolume groups remain a set of named children; `group` references
below resolve within that filesystem, so a separate group kind is unnecessary.
Group layout must select an attached eligible data pool, not merely any pool
in the cluster. Multiple data pools require one default; the sole-pool
normalization is recorded under [CephFilesystem](#cephfilesystem).

`CephFilesystem.spec.mdsServiceRef` and `CephObjectGateway.spec.serviceRef`
name their full native services; [CephFilesystem](#cephfilesystem) and the
[gateway field record](#cephobjectgateway-field-record) own how each resolves.
Canonical service identities are looked up in the qualified identity table,
not recovered by naively splitting on dots.

Provider-created internal pools need not all be hand-authored. The gateway
records a derived owner, effective pool map, native identity and protection for every
such pool before gateway readiness. A separately declared CephPool remains
its own owner and cannot also be claimed as generated. Failure to determine
the effective map or prove compatible protection prevents readiness and any
unqualified cascading cleanup.

Filesystem creation respects the separate pool and MDS owners. A filesystem
is created with `ceph fs new <fs> <metadata> <default> set max_mds <n>
standby_count_wanted <n>` over its owned pools, `ceph fs set <fs>
allow_standby_replay <true|false>` follows whenever the observed flag differs,
and further data pools attach with `ceph fs add_data_pool`. The
[native volume interface](https://docs.ceph.com/en/tentacle/cephfs/fs-volumes/),
which creates and removes pools and orchestrates MDS services, is never used,
and no creation is ever forced.

### CephObjectGateway field record

The [gateway block](ceph.md#resources-readiness-and-access) realizes the realm,
zone group, zone, zone placement, its rgw service with or without TLS and the
ingress that fronts it; planning refuses an ingress whose auxiliary image
`spec.images` of its CephCluster does not set, as the
[refusal table](ceph.md#refusal-table) states.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephObjectGateway
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
| `clusterRef` | Required managed CephCluster. | None. |
| `serviceRef` | Required qualified name `rgw.<service_id>` of one declared rgw service of that cluster, resolved by full-name equality. One gateway binds each rgw service. | The service the gateway executes; its placement, `rgw_frontend_port`, `ssl` and networks are that document's. |
| `realm`, `zoneGroup`, `zone` | Required DNS labels, each unique per cluster across gateways. | `radosgw-admin realm\|zonegroup\|zone create --rgw-realm/--rgw-zonegroup/--rgw-zone`; the rgw document's `rgw_realm`, `rgw_zonegroup` and `rgw_zone`. |
| `pools.indexPoolRef` | Optional replicated CephPool of role `rgw`; omitted derives `<zone>.rgw.buckets.index`. | Zone placement `index_pool`. |
| `pools.dataPoolRef` | Optional CephPool of role `rgw`, replicated or erasure-coded; omitted derives `<zone>.rgw.buckets.data`. | STANDARD storage class `data_pool`. |
| `pools.dataExtraPoolRef` | Optional replicated CephPool of role `rgw`; omitted derives `<zone>.rgw.buckets.non-ec`. | Zone placement `data_extra_pool`. |
| `endpoint.fqdn` | DNS name of the public endpoint; an `endpoint` mapping without it defaults to `<gateway>.<cluster>.<storage domain>`. | None; scheme, port and TLS come from the terminating service. |
| `authentication` | Optional record of the sign-in methods RGW adds to its local users; it must declare one. | None itself. |
| `authentication.ldap.ldapServerRef` | Required [`LDAPServer`](api/infrastructure-services.md#oidcprovider-and-ldapserver), Active Directory included, that RGW authenticates S3 users against. | The rgw document's derived `spec.config` LDAP options and `custom_configs` files ([authentication](#cephobjectgateway-authentication)). |
| `authentication.sts.keyRef` | Required `opaque` or `token` Secret whose value is exactly 16 letters and digits, the AES-128 key RGW's STS signs temporary credentials with; not a generated Secret. | `spec.config` `rgw_sts_key` and `rgw_s3_auth_use_sts: true` ([authentication](#cephobjectgateway-authentication)). |
| `authentication.sts.oidcProviderRefs` | Optional set of [`OIDCProvider`](api/infrastructure-services.md#oidcprovider-and-ldapserver) objects whose web identities the operator means RGW to trust, each without `trustBundleRef`. | None: recorded for the plan only; the operator registers each provider and its roles. |

The DNS-label grammar of `realm`, `zoneGroup` and `zone` is an explicit
effect restriction of Bootwright; Ceph accepts more. Bootwright never passes
`--default`, so a gateway never sets the cluster's default realm; inside its
own realm, native creation records the first zone group and zone as that
realm's defaults. The only writer of a default is the export block of a
[CephNFSExport](#cephnfsexport) bound to one of the gateway's users. The storage domain is the single Environment's
`domains.storageClusters`, else `domains.clusters`, else `domains.base`, the
same domain that derives node FQDNs.

The gateway owns derived children it never declares as objects: the zone pools
`<zone>.rgw.meta`, `.control`, `.log`, `.otp` and `.dedup` and every omitted
placement pool, the shared `.rgw.root`, whose claim every gateway of the cluster shares
([gateway block](ceph.md#resources-readiness-and-access)), the per-realm `dashboard` system user
that the mgr dashboard module creates (never read for keys) and the
ingress passwords cephadm generates. No declared CephPool can carry a
derived pool name, because a CephPool name is a DNS label without `.`.
Derived pools take the size of the cluster default pair under
[replicated protection](#replicated-protection), and that size must satisfy
the replicated-pool host rule against the declared OSD-bearing hosts.

Each derived zone pool is realized by its own block
`ceph-gateway-<gateway>-pool-<suffix>`, the suffix written with `-` for `.`
(`meta`, `control`, `dedup`, `log`, `otp`, `buckets-index`,
`buckets-non-ec`, `buckets-data`), the set
[`init_zone_pool_names`](https://github.com/ceph/ceph/blob/v20.2.1/src/rgw/rgw_zone.cc#L1277-L1312)
names once its namespaces are stripped. The block has Kind
CephObjectGateway, claims nothing beyond its cluster's Ceph
[throttle](ceph.md#claims-and-throttle) and depends on the cluster
and image blocks by block identity. Its pool is a replicated `rgw` pool on
`replicated_rule` with the initial 32 placement groups and autoscale `on`
that an undeclared autoscale freezes, the default replica pair and
application `rgw`; it is created and observed exactly as a CephPool's pool,
with the same records and refusals. The blocks need no realm, so they precede
the gateway's realm, zone group and zone. A declared placement ref replaces
its derived block. A default size that the declared OSD-bearing hosts cannot
hold refuses at plan before any effect; a long gateway name
[hashes an identity over 63 bytes](state-reconciliation.md#durable-identities-and-private-paths)
of each block identity.

The endpoint FQDN is the gateway's endpoint identity. It is unique across the
graph, against every storage node hostname and every other gateway's FQDN,
and it is not published to DNS. The terminating service is the ingress whose
`spec.backend_service` is the bound rgw service (on a stretch cluster its one
ingress over both data sites, or each of its per-site ingresses, all bound to
one Secret), else that rgw service; an
HTTPS terminating service needs its `/spec/ssl_cert` and `/spec/ssl_key`
bindings ([native services](ceph-native-api.md)). A terminating service with
TLS needs `spec.endpoint`: the certificate must name its FQDN, and readiness
proves the served leaf by SHA-256 with that FQDN as SNI.

The gateway has no authored per-service RGW configuration option, because
they have no qualified effect contract; `authentication` derives the one set
the gateway applies ([authentication](#cephobjectgateway-authentication)).

Admission refuses, before any effect: a `serviceRef` that resolves to no
declared service, names a non-rgw service or is bound by another gateway; a
realm, zone group or zone that disagrees with the bound rgw document or is
already owned by another gateway of the cluster; a pool reference of another
role or cluster, an erasure-coded index or data-extra pool, a pool bound by
another gateway or one pool in two placement roles; derived pools the declared
OSD-bearing hosts cannot hold; an endpoint without a derivable FQDN, with an
invalid one or with one that equals a storage node hostname or another
gateway's FQDN; an `authentication` its
[rules](#cephobjectgateway-authentication) refuse; and, on the cluster, an rgw service no gateway of the cluster
binds, because cephadm would deploy it before its realm, zone group and zone
exist.

### CephObjectGateway authentication

`authentication.ldap` and `authentication.sts` are the one derived RGW
configuration of a gateway, each optional and combinable: the gateway block
writes them into the rgw document it applies
([realization](ceph.md#object-gateway-authentication)), so cephadm sets each option at
`client.rgw.<service_id>` before it deploys a daemon and removes them with the
service.

| Option or file | Value |
| --- | --- |
| `rgw_ldap_uri` | the server's `urls`, joined by one space, tried in order |
| `rgw_ldap_binddn` | the server's `bindDN` |
| `rgw_ldap_searchdn` | `users.baseDN` |
| `rgw_ldap_dnattr` | `users.nameAttribute` |
| `rgw_ldap_searchfilter` | only with `users.filter`: `(&<filter>(<nameAttribute>=@USERNAME@))`, because without the token RGW wraps the filter in parentheses again; omitted, RGW searches `(<nameAttribute>=<name>)` |
| `rgw_ldap_secret` | `/etc/bootwright/rgw-ldap-bind`, the bind password file below |
| `rgw_s3_auth_use_ldap` | `true` |
| `custom_configs[0]` | `/etc/bootwright/rgw-ldap-bind`: the value of the server's `bindPasswordRef` |
| `custom_configs[1]` | `/etc/openldap/ldap.conf`: `TLS_CACERT` the CA file below, else the image's `/etc/pki/tls/certs/ca-bundle.crt`, `TLS_REQCERT demand`, `NETWORK_TIMEOUT 5` and `TIMEOUT 10`, because RGW sets no LDAP TLS option or timeout itself |
| `custom_configs[2]` | only with the server's `trustBundleRef`: `/etc/bootwright/rgw-ldap-ca.pem`, its certificates |
| `rgw_sts_key` | `sts` only: the value of `keyRef` |
| `rgw_s3_auth_use_sts` | `sts` only: `true` |

RGW's default sign-in order `sts, external, local` keeps local users last: the
LDAP engine only denies, so a request signed with a CephObjectUser's key never
contacts the directory, and local users keep working while it is down. An LDAP
user whose sign-in name equals a CephObjectUser's ID acts as that user, so keep
directory names and CephObjectUser names disjoint. The bind password and the CA
bundle are effect material of the gateway block; the bind password must be one
line of 1 to 1023 bytes without leading or trailing whitespace, which RGW would
trim or cut, and the bundle parsable PEM certificates, or the apply refuses
`ceph.secret-invalid` when it binds them, naming the Secret and never the
value. Both are stored in cephadm's service specification, which `ceph orch ls
--export` and the dashboard's service API show to Ceph administrators, as the
plan's impact lines say; use a least-privilege bind account
([Q154](milestones/m2.md#identity-program-open-decisions)). The block is the
gateway's frozen input: adding, changing or removing `authentication`, or
changing the server's values or Secrets, on a completed gateway is an input
change, a destroy and a fresh apply, and so is rotating the bind password or
bundle ([B1839](milestones/backlog.md#b1839)).

`authentication.sts` lets RGW answer `AssumeRoleWithWebIdentity` with
temporary S3 credentials for a web identity token an OpenID Connect provider
issued. Bootwright registers no OpenID Connect provider and no role: after
handoff the operator creates each provider (`CreateOpenIDConnectProvider` with
its thumbprints) and each role with its trust policy through RGW's IAM API
under an RGW account, because IBM Storage Ceph 9.9.1 deprecates tenant-level
OIDC, and both are objects the operator keeps in its own GitOps configuration
([D246](milestones/backlog.md#d246)). `oidcProviderRefs` names the providers the
operator means to register, and the plan lists each; it changes nothing on the
cluster. RGW verifies an issuer's HTTPS only against the image's system trust
store, which only the `CURL_CA_BUNDLE` environment variable replaces as a
whole, so a referenced OIDCProvider with `trustBundleRef` refuses at its
reference ([B1837](milestones/backlog.md#b1837)), and each RGW host reaches the
issuer directly, without a proxy. The STS and LDAP engines only deny, so a
request signed with a CephObjectUser's key never contacts the identity
provider; credentials STS already issued keep working through a provider
outage until they expire, and new `AssumeRoleWithWebIdentity` calls fail until
it returns. The key is effect material of the gateway block and is the same on
every daemon of the zone by construction: one gateway owns one rgw service and
its zone, and a second gateway of that zone refuses as the zone's owner. A
value other than 16 letters and digits refuses `ceph.secret-invalid` when the
apply binds it, naming the Secret and never the value (an opaque Secret keeps
its exact bytes, so store it without a final newline); a generated Secret
refuses at admission, because a generated token is base64url of at least 22
characters. The key is stored in cephadm's service specification and in the
configuration database, readable by every Ceph administrator
([Q154](milestones/m2.md#identity-program-open-decisions)), and rotating it is
an input change, a destroy and a fresh apply ([B1839](milestones/backlog.md#b1839)).

Admission refuses an `authentication` that declares no method, and one whose
terminating service does not terminate TLS (`ssl: true` on the rgw service, or
on every ingress that fronts it), because an LDAP user's access key carries the
user's directory password and STS returns temporary credentials in its
response; with an ingress, the hop from haproxy to RGW stays plaintext on the
cluster network, as the plan's impact lines say.

## CephPlacementPolicy

A `CephPlacementPolicy` is one owned replicated CRUSH rule named
`spec.ruleName`: host failure domain under the bootstrap root `default`,
optionally restricted to `spec.crushDeviceClass`. Its block is
`ceph-policy-<name>`, and `metadata.name` follows the
[one name rule](api.md#object-names). The rule
name uses only letters, digits, `-`, `_` and `.`, starts with a letter or digit
and has at most 128 characters; `replicated_rule`, which the monitor creates
at bootstrap, is reserved; two policies of one cluster never share a rule; and
no rule takes the name of an erasure-coded pool of the cluster, which derives
its own rule. A device class is `hdd`, `ssd` or `nvme`, which OSDs set
automatically, or a `crush_device_class` that an OSD service of the cluster
declares at spec or path level, a path's class overriding its service's, which
its OSDs take ([device contract](ceph.md#devices-and-destructive-ownership)).
For a declared class, host-domain replicas count only the hosts whose OSD
services declare it. A failure domain other
than `host` refuses at plan; site and rack domains are parked by D160 (OD25).
A stretch cluster places every pool with its own stretch rule, so a policy
whose `clusterRef` names one refuses at validation and at plan
([stretch topology](ceph.md#stretch-topology)).

| Field | Contract |
| --- | --- |
| `clusterRef` | Required managed `CephCluster`. A pool that names the policy names the same cluster, and a sole policy is never selected automatically. |
| `ruleName` | Required CRUSH rule name. |
| `failureDomain` | Optional; the realized rule uses the `host` failure domain, and another value refuses at plan. |
| `crushDeviceClass` | Optional device class the rule is restricted to. |
| `replicated` | Optional `{size, minSize}` under [replicated protection](#replicated-protection); a pool that names the policy takes these values. |

### Replicated protection

The replicated-pool shape `{size, minSize}` is shared by placement policies and
pools. Both members are optional non-negative integers. Zero and omission mean
that the applicable cluster, policy, or native default selects the value; a
non-zero value below two refuses at admission. Validation resolves effective
values first and requires effective `minSize` not to exceed effective `size`, including when
only one member is authored.

On a pool, `replicated` and `erasure` are the protection arms: exactly one is
present, and omitting both selects `replicated: {}`, which effective output
shows. An empty or all-zero `replicated` block leaves every value to the
cluster, policy or native default, and only its non-zero members conflict with
a referenced placement policy, which owns those values for its pools.

The cluster default pair is `4/2` under stretch; otherwise the
`osd_pool_default_size` and `osd_pool_default_min_size` pair under
`spec.config.global` when declared; otherwise `2/2` with bootstrap
`singleHostDefaults`; otherwise the ordinary managed-cluster `3/2`. Pool and
policy admission and the gateway's derived pools read this one default. A
referenced placement policy supplies its resolved pair to the pool. These
values qualify validation but remain native or policy defaults unless an owning
normalization rule explicitly materializes them; the pool and placement-policy
requests the plan builds carry the same default for a size they leave unset.

Not yet met: the gateway's derived zone pools and its placement-group budget take their size from `zonePoolReplicas` in `internal/storage/ceph/gateway.go`, which yields `4/2` under stretch, `2/2` under `singleHostDefaults` or a declared global `osd_pool_default_size` of 2, and `3/2` otherwise, so a declared pair of another value never reaches them; tracked as [B1789](milestones/m2.md#b1789).

The [single-host test profile](api/ceph.md#single-host-testing) places a pool
without a policy, and gateway-derived pools, across explicitly selected OSD
disks instead of hosts. The selected disk count must be at least its replica
size. Policies and erasure coding continue to use host-domain capacity.

For ordinary (non-stretch) host-domain replication on every Ceph
distribution, the declared OSD-bearing host count must exceed the effective
replica size. This is a native replica-safety rule, not a vendor support
statement: a replicated pool of size N keeps one replica per host, so it needs at least N+1 hosts to
re-create a lost host's replicas and return to full size; three replicas need
four hosts. The three-node development profile therefore explicitly
selects `size: 2` and `minSize: 2`, or declares the `2/2` cluster default
pair; omission or zero otherwise selects `3/2` and requires an additional
OSD-bearing host. MON-only hosts do not add data
capacity. A pool or placement policy on a bounded device class, a
`crush_device_class` OSD services declare or an automatic class they permit on
fewer hosts than carry OSDs, also needs more hosts of that class than its
effective size. Stretch protection is fixed at `4/2` on the cluster's stretch
rule, and a stretch pool names no placement policy
([stretch topology](ceph.md#stretch-topology)).

## CephPool

A `CephPool` declares one pool of a managed cluster. Its protection is a
presence arm, `replicated` or `erasure`, and the cluster is the storage
engine. `CephPool.spec.nativeName` is an optional DNS label. It defaults to the
pool's `metadata.name`; object references always retain metadata identity.
Native names must be unique within a cluster. Every pool consumer projects
that name, including RBD, clients, CephFS and RGW placement. Native mirroring
requires the same pool name in both clusters, while their object names remain
distinct. Changing either identity after apply is changed intent.

| Field | Contract |
| --- | --- |
| `clusterRef` | Required managed `CephCluster`. |
| `nativeName` | Optional DNS label naming the native pool; default `metadata.name`. |
| `placementPolicyRef` | Optional same-cluster `CephPlacementPolicy`; exclusive with `erasure`. |
| `role` | Required at plan: `rbd`, `cephfsMetadata`, `cephfsData` or `rgw`. |
| `application` | Optional; it must equal the application its role derives. |
| `replicated` | Presence arm of optional `size` and `minSize`, under [replicated protection](#replicated-protection). |
| `erasure` | Presence arm: required positive `dataChunks` and `codingChunks`, and optional `plugin`, `technique`, `crushDeviceClass`, `crushRoot`, `stripeUnit` and `parameters`, under [erasure coding](#erasure-coding). |
| `autoscale` | Optional PG autoscale policy: `mode`, `targetSizeRatio`, `targetSizeBytes`, `pgNumMin`, `pgNumMax` and `bulk`, under [autoscale, quota and compression](#autoscale-quota-and-compression). |
| `quota` | Optional `maxBytes` and `maxObjects`, under the same section. |
| `compression` | Optional BlueStore `mode`, `algorithm`, `requiredRatio`, `minBlobSize` and `maxBlobSize`, under the same section. |
| `mirroring.mode` | Optional `image` or `pool`, RBD pools only. |

A pool requires `spec.role` at plan; the role derives the application
(`rbd`, `cephfs` for both CephFS roles, `rgw`), and an authored
`spec.application` must equal it. A replicated pool uses its policy's rule, or
`replicated_rule` without one, and takes `replicated` values from the pool,
then the policy, then the [cluster default pair](#replicated-protection). A policy and an erasure-coded pool are exclusive:
an erasure-coded pool places itself through `spec.erasure.crushDeviceClass`.

Pool intent, attempt, native ID and setting records use disjoint keys for
each record type and native name. Pool attempt, ID and adoption follow
[resources, readiness and access](ceph.md#resources-readiness-and-access).

### Erasure coding

An erasure-coded pool owns one erasure-code profile and one CRUSH rule, both
named after the pool, with host failure domain under `default`. The rule
places each of the `dataChunks+codingChunks` chunks on a distinct host, so the
declared OSD hosts (only those whose OSD services declare
`spec.erasure.crushDeviceClass`, when it names a declared class) number at
least `dataChunks+codingChunks`, and apply proves the same count from
`ceph osd tree` before any effect (`crush-capacity`). No other `k`/`m`
restriction applies; the selected release's monitor refuses a profile it does
not accept, reported through `nativeError`. The plugin is `isa` when omitted.
`isa` and `jerasure` are the plugins Bootwright realizes, because its profile
readback proves exactly their stored keys; `clay`, `lrc` and `shec` refuse at
admission as not realized. The technique is `reed_sol_van`. The pool's `size`
is `dataChunks+codingChunks` and its `min_size` is the monitor's own
derivation at pool creation, `dataChunks + min(1, codingChunks-1)`, so a pool
with one coding chunk has `min_size` equal to `dataChunks`; Bootwright freezes
and observes both and never sets either. The stripe unit is
a multiple of 4096 bytes (4096 when omitted), always passed, and `dataChunks`
times it fits the native 32-bit stripe width. `spec.erasure.parameters` and a non-default
`crushRoot` refuse at plan. An erasure-coded `rbd` or `cephfsData` pool
derives irreversible overwrite enablement, as IBM's
[erasure-coding overwrites](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=overview-erasure-coding-overwrites)
require. A CephClient on an erasure-coded pool refuses at plan; an
erasure-coded pool reaches a client only as the `dataPoolRef` of a
CephRBDImage on the client's pool, whose caps then name it.

### Autoscale, quota and compression

Autoscale contains `mode` (`on`, `off` or `warn`), `targetSizeRatio` (a finite
non-negative number), `targetSizeBytes`, `pgNumMin`, `pgNumMax` and
presence-sensitive `bulk`. `targetSizeRatio` and `targetSizeBytes` are
mutually exclusive, and zero or omission leaves the ratio unspecified.
`targetSizeBytes` is `N` optionally followed by `K`, `M`, `G`, `T`, `P`, `E`,
`KB`, `MB`, `GB`, `TB`, `PB` or `EB`, case-sensitive, with no sign or
fraction. The PG bounds are non-negative, the minimum does not exceed the
maximum when both are positive.

Quota contains integers `maxBytes` and `maxObjects` from 0
to 9223372036854775807; an explicit zero and omission both freeze as `0`, the
native no-limit value, which the worker sets and readiness observes. Compression contains `mode`
(`none`, `passive`, `aggressive` or `force`), `algorithm` (empty, `lz4`,
`snappy`, `zlib` or `zstd`), `requiredRatio` (a finite number, zero or omitted
or within `(0,1]`) and the blob sizes `minBlobSize` and `maxBlobSize`
(integers from 0 to 18446744073709551615, with the minimum not above the
maximum when both are positive). Another compression field requires a `mode`.

The initial `pg_num` is `clamp(32, pgNumMin, pgNumMax)` over the positive
bounds, which Go freezes as the pool request's `pgNum`; the worker applies it. `autoscale.mode` is `on` when omitted or `off`; `warn` refuses at plan
(D160, OD26). `targetSizeBytes` converts with 1024-based units,
`targetSizeRatio` and `compression.requiredRatio` freeze as decimal strings,
and quota and compression values freeze as native values, with 0 and empty
meaning unset. `pgNumMin` and `pgNumMax` are at most 32768 and the byte
values fit a signed 64-bit integer. `mirroring` is realized only when a
[CephReplication](#cephreplication) names the pool;
without one, planning refuses it and names that owner. A `cephfsMetadata`
pool declares no PG bounds: `ceph fs new` sets its `pg_num_min`.

### Placement-group budget

Before any effect, a plan refuses a pool whose CRUSH rule would exceed the
monitor's per-OSD placement-group bound: the sum of `pgNum × width` over the
cluster's pools on the rule (width is the replica size, or `k+m`; a
`cephfsMetadata` pool counts at least 16), and, on the cluster's pool rule only, one
`.mgr` placement group, 32 placement groups for each zone pool a
CephObjectGateway of the cluster derives and 32 placement groups once for the
shared `.rgw.root` when the cluster has a CephObjectGateway, each times the
zone pool size. The zone pool size is the size of the
[cluster default pair](#replicated-protection). The sum must not
exceed 250 times the selected OSD count, at least
3, or 3 for a rule restricted to a device class.

## CephReplication

The closed `CephReplication` record realizes one-way journal-based RBD
replication. Both clusters must be declared managed clusters in the same
context, on disjoint Machines. There is at most one relationship involving
each cluster, because native bootstrap credentials have cluster-wide identity.

| Field | Contract |
| --- | --- |
| `clusterRef` | Required primary managed CephCluster. |
| `type` | Required, exactly `rbd`. |
| `poolRef` | Required primary CephPool. |
| `peerClusterRef` | Required distinct receiving managed CephCluster. |
| `peerPoolRef` | Required receiving CephPool with the same native name. |
| `imageRefs` | Required nonempty set of source CephRBDImage references in image mode; forbidden in pool mode. Pool mode derives all declared source images. |

Both pools must explicitly author the same `mirroring.mode`, either `image`
or `pool`, use replicated protection and have role `rbd`. Mirroring without
a relationship owner refuses. The receiving cluster declares `rbd-mirror`.
One to three source images are admitted; each explicitly enables `journaling`
and has no `dataPoolRef`. The receiving pool has no independent image
declarations and must initially be empty. In pool mode the source's complete
native image set must match its declarations before mirroring is enabled.
Snapshot-based mirroring, forced promotion, automatic failover, context
crossing, external clusters and foreign peer adoption are not realized.

The relationship depends on both owned pools, all selected source images,
both original cluster and image proofs, and the receiving mirror service.
It holds both cluster execution claims in sorted order. Native work is
sequential across endpoints. Completion requires matching global image IDs,
owned native IDs, source primary state and receiving `up+replaying` state
for every image. Native initial synchronization must finish before readiness.
An unavailable peer retains partial owned state and retry uses the original
apply identity; no bootstrap credential or peer token is produced material.
The [lifecycle owner](ceph.md#rbd-replication) defines custody and budgets.
Removal retires only with all installed Machines of both clusters, a
[provided stretch tiebreaker](ceph.md#a-provided-tiebreakers-removal) left to
its cluster block; removal while either cluster survives refuses.

RGW multisite and its secondary gateway reference remain unimplemented because
of the [native credential-interface blocker](ceph.md#rgw-multisite-blocker).
This record admits no RGW arm. CephFS snapshot mirroring continues to refuse.

## CephRBDImage

One object declares one image. Gateway items consume it rather than creating
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

The initial native image name is `metadata.name`. Creation options outside
the promotion record are refused as unknown fields ([Not admitted](#cephrbdimage)).
Existing images with matching names remain foreign
unless recorded ownership and native identity agree. Readiness verifies size,
pool/data pool, native image ID and effective features. An image retires with its cluster's hosts.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephRBDImage
metadata:
  name: application-disk

spec:
  clusterRef: ceph-example
  poolRef: block-data
  sizeBytes: 10737418240
```

### Promotion record

| Field | Grammar and rule | Native projection |
| --- | --- | --- |
| `metadata.name` | The [one name rule](api.md#object-names); the block is `ceph-rbd-<name>`. | Image name; positional `<pool>/<name>`. |
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
- a `poolRef` or `dataPoolRef` of another cluster or without role `rbd`, and
  an erasure-coded `poolRef`, whose image header needs a replicated pool.

Each refusal is `api.invariant`. A defaults fragment is checked only for the
feature and data-pool rules and, when it declares `features`, the size rule.
`features` is a set of the six names above, so librbd's internal features
refuse as values. Plan refuses an erasure-coded `poolRef` again and propagates
every plan refusal of the image pool and data pool.

[Storage](ceph.md#resources-readiness-and-access) owns the image's
attempt record, adoption, identity refusals and readiness comparison. A
trashed image has left the pool directory, so it never blocks a create.

Readiness proves the recorded image ID, the size, format 2, the data pool, and
the features plus the implicit `data-pool`, and that the image is no clone
([Info.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/tools/rbd/action/Info.cc#L226-L238)). A consumer's
snapshot does not make the image unready.

RBD namespaces, striping, object size, encryption and QoS are not fields and
refuse `api.field`.

Basis: D160 (OD26) for namespaces, striping, object size and encryption.

Image-mode mirroring is declared through an explicit
[CephReplication](#cephreplication).

## CephClient

A CephClient declares one CephX principal independently; it never borrows
the cluster administrator or invents an implicit lab identity.
[B80](milestones/m2.md#b80) owns the first RBD consumer and protected produced
custody. [B489](milestones/m2.md#b489) extends the client model to further
resource scopes and public access/handoff after their field contracts are
qualified.

Each principal grants exactly one scope (least privilege, one custody entry,
one removal), so a CephClient declares exactly one of `poolRef`,
`filesystemRef` and `subvolumeRef`. Admission refuses two or three of them at
`$.spec` with `api.invariant`, also in a defaults fragment, and refuses none
at `$.spec` with `api.required` in the effective object; a consumer that needs another scope declares another
CephClient.

| Field | Target rule |
| --- | --- |
| `clusterRef` | Required managed cluster reference. |
| `poolRef` | RBD arm: a same-cluster initialized pool of role `rbd`. |
| `filesystemRef` | CephFS filesystem arm: a same-cluster CephFilesystem. |
| `subvolumeRef` | CephFS subvolume arm: a same-cluster CephSubvolume. |
| `access` | Required `readOnly` or `readWrite`; no implicit administrator access. |

The native principal is `client.<metadata.name>`. The key is applied with
`auth add` (never `auth import`, `auth get-or-create`, `auth caps` or
`fs authorize`) and read back exactly with `auth get`. Go derives the
capabilities from the selected access and the frozen scope and freezes them as
the client request's `caps`; for the subvolume arm Go's prepare step binds the
proved path into the frozen `mds` grant. The worker applies the frozen grants
without deriving them, and Go judges the created principal against the same
value ([language responsibility](architecture.md#language-responsibility)). The
RBD permission covers the declared pool, not only one image; the plan reports
each arm's scope from the frozen request.

The client has no data-pool field: a CephRBDImage's `spec.dataPoolRef` is
the single owner of that fact. Plan derives the distinct `dataPoolRef` values
of the same-cluster CephRBDImages on the client's `poolRef`, sorts them,
freezes them in the client request, requires those CephPools and propagates
their plan refusals. The frozen caps follow Ceph's
[RBD user grammar](https://docs.ceph.com/en/tentacle/rbd/rados-rbd-cmds/), where
grants inside one cap are separated by a comma and a space; `<profile>` is `rbd` for
`readWrite` and `rbd-read-only` for `readOnly`:

| Cap | Frozen value |
| --- | --- |
| `mon` | `profile rbd` |
| `mgr` | `profile <profile> pool=<poolRef>` |
| `osd` | `profile <profile> pool=<poolRef>`, then one more `profile <profile> pool=<data pool>` grant for each derived data pool in sorted order |

With no derived data pool the caps name only `poolRef`. The `mgr` cap names
only `poolRef`: least privilege keeps the data pools off it. The
worker proves every data pool is owned under its recorded native pool ID
before it generates a key. An erasure-coded `poolRef` refuses at plan, as
[erasure coding](#erasure-coding) states. An image later added to or removed from the pool changes the
client's frozen request, which follows the destroy/fresh-apply rule of
[lifecycle and migration gates](#lifecycle-and-migration-gates).
User-supplied arbitrary capabilities and imported keys are not part of the
CephClient shape.

The CephFS arms freeze the filesystem name, and the subvolume arm also its
group and subvolume name; they carry no data pool and no `mgr` cap. Their caps
are spelled exactly as `ceph fs authorize` renders them
([CephFS client capabilities](https://docs.ceph.com/en/tentacle/cephfs/client-auth/)),
where `<a>` is `rw` for `readWrite` and `r` for `readOnly`, `<fs>` is the
CephFilesystem's name and `<path>` is the path the CephSubvolume's
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
path `fs subvolume getpath` reports at that point. A client of a filesystem or subvolume
plan propagates that resource's plan refusals and orders after its block.

The generated key enters produced-material custody before client readiness,
under the retention and binding of the
[secret bindings](ceph-native-api.md#secret-bindings).
Before it applies the key the worker proves the block's ownership mark, the
owned cluster and the incarnation of its frozen scope (the pool, the filesystem
or the subvolume with its recorded path), and that no principal of the name
exists without this block's records. A scope that differs refuses
`pool-identity`, `filesystem-identity` or `subvolume-identity` as that resource
section states. The worker writes the client attempt record
`client-attempt-<metadata.name>.json` after the key is retained and before
`auth add`; it binds the proved scope to the custody identity of the retained
key. A lost reply is adopted only with the ownership mark, that attempt record
and exactly one existing principal whose key and caps equal the retained key
and the frozen caps; the worker then recaptures the key with `auth get` and
never generates or applies another. The worker never overwrites a principal.

| Refusal | Reached when |
| --- | --- |
| `foreign-client` | `client.<metadata.name>` exists without this block's ownership mark or attempt record, even when its key and caps equal the retained ones. |
| `client-identity` | More than one principal of the name exists; its key or caps differ from the retained material or the frozen caps; the attempt or applied record differs from the current binding; an applied record has no principal; or `auth get` differs from the retained material. |

No native
keyring is emitted through ordinary render, status or evidence. No Bootwright
harness consumes the key: B476's client-data test runs through `cluster exec`
with the bootstrap node's administrator keyring
([storage contract](ceph.md#resources-readiness-and-access)). An operator
exports one client's keyring only through the
[client keyring export](cli.md#client-keyring-export). The principal and its
material retire with the cluster; revoking one client while the cluster
remains is [B491](milestones/m2.md#b491)'s.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephClient
metadata:
  name: application-reader

spec:
  clusterRef: ceph-example
  poolRef: block-data
  access: readOnly
```

A CephFS consumer of one subvolume:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephClient
metadata:
  name: documents-writer

spec:
  clusterRef: ceph-example
  subvolumeRef: documents
  access: readWrite
```

Code: `ResourceWorker.client` in the collection's `ceph_native_resources` module.
Held by: `test_lost_auth_creation_reply_recovers_exact_original_identity`,
`test_foreign_principal_matching_expected_key_and_caps_is_still_foreign`,
`test_reserved_material_without_auth_attempt_does_not_adopt_matching_principal`,
`test_changed_native_principal_is_never_adopted_or_overwritten` and
`test_racing_foreign_auth_add_cannot_be_overwritten` in
`test_ceph_client_custody.py`.

## CephFilesystem

One object declares one CephFS filesystem over owned pools, bound to its own
MDS service. [B492](milestones/m2.md#b492) owns its schema and lifecycle.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephFilesystem
metadata:
  name: files

spec:
  clusterRef: ceph-example
  metadataPoolRef: files-metadata
  dataPoolRefs:
  - files-data
  subvolumeGroups:
  - name: csi
```

### Admission

Its schema requires `clusterRef`, `metadataPoolRef` and `dataPoolRefs`;
`mdsServiceRef` is optional and derived as `mds.<filesystem>`, its one valid
value; `mds.activeCount` is an integer from 1 to 256 (default 1),
`mds.standbyCountWanted` from 0 to 2147483647 (default 0) and
`mds.standbyReplay` a boolean (default false). Each `dataPoolRefs` entry is
the scalar pool name or `{name, default}`; a sole entry normalizes to
`default: true`, even when `false` was authored. A subvolume group's `uid` and
`gid` are integers from 0 to 2147483647 and its `sizeBytes` from 1 to
9223372036854775807; an omitted `sizeBytes` sets no quota. MDS placement
and service spec belong to the CephCluster's `spec.services` entry with
`service_type: mds` and `service_id` equal to the filesystem name. Admission refuses with `api.invariant`,
before any effect:

- `metadata.name` starting with a digit, since its MDS `service_id` cannot;
- an authored `mdsServiceRef` other than `mds.<filesystem>` (a cluster that
  declares no native service `mds.<filesystem>` refuses as an unresolved
  [native-service reference](api.md#references), `api.reference`);
- a `metadataPoolRef` of another cluster or without role `cephfsMetadata`,
  or a data pool of another cluster or without role `cephfsData`; repeated
  data pools; several data pools without exactly one `default: true` (a sole
  data pool is the default);
- an erasure-coded default data pool, which `ceph fs new` refuses unless
  forced; erasure-coded `cephfsData` pools attach as additional data pools;
- an MDS service that places fewer daemons than `activeCount +
  max(standbyCountWanted, standbyReplay ? activeCount : 0)`, where the
  placed daemons are its resolved hosts, capped by `count`; MDS
  `count_per_host` above one refuses at admission;
- a metadata or data pool that another CephFilesystem of the same cluster
  already uses; a cluster admits several filesystems, each with its own pools
  and its own MDS service;
- a subvolume group named `_nogroup`, `_index`, `_deleting` or `_legacy`, or
  outside `^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`, also in a defaults fragment;
- a subvolume group `mode` outside `^0?[0-7]{3}$`, also in a defaults
  fragment; a YAML integer is a type error, so quote it (`"0755"`);
- a group `poolLayoutRef` that is not an attached data pool.

A `cephfsMetadata` pool must be replicated under [CephPool](#cephpool). Plan
re-refuses an erasure-coded default data pool and a cluster that projects no
`mds.<filesystem>` service, and propagates every plan refusal of its pools.
The core capability realizes the filesystem as block
`ceph-filesystem-<name>`, which requires its metadata and data pools and
depends on the cluster and image blocks.

### Promotion record

| Field | Frozen value | Native projection |
| --- | --- | --- |
| `metadata.name` | The filesystem name, under the [one name rule](api.md#object-names); the block is `ceph-filesystem-<name>`. | `<fs>` of `fs new`. |
| `mdsServiceRef` | `mds.<name>`, derived when omitted; a declared mds service of the cluster. | None; its daemons serve the ranks. |
| `metadataPoolRef` | Owned replicated pool of role `cephfsMetadata`. | `<metadata>` of `fs new`. |
| `dataPoolRefs` | The default pool first, then the others in declared order; the default is replicated. | `<default>` of `fs new`; each other pool through `fs add_data_pool <fs> <pool>`. |
| `mds.activeCount` | Value, or 1 ([MDSMap.h](https://github.com/ceph/ceph/blob/v20.2.1/src/mds/MDSMap.h#L622)). | `set max_mds <n>`. |
| `mds.standbyReplay` | Value, or false. | `fs set <fs> allow_standby_replay <true\|false>` after `fs new`, when the observed flag differs. |
| `mds.standbyCountWanted` | Value, or 0. | `set standby_count_wanted <n>`. |
| `subvolumeGroups[].name` | Sorted by name. | `fs subvolumegroup create <fs> <group>`. |
| `subvolumeGroups[].poolLayoutRef` | Value, or the default data pool. | `--pool_layout <pool>`. |
| `subvolumeGroups[].mode` | Three octal digits; a leading `0` is dropped; omission `755` ([module.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/module.py#L723-L733)). | `--mode <mode>`. |
| `subvolumeGroups[].uid`, `gid` | Value, or 0 ([group.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/fs/operations/group.py#L212-L233)). | `--uid <n> --gid <n>`. |
| `subvolumeGroups[].sizeBytes` | Positive value, or 0 for no quota. | `--size <n>` only when positive. |

A CephFS pool's frozen request names its derived filesystem: the
CephFilesystem of the same cluster whose metadata or data pool it is, or
none. `fs new` records the application key `cephfs: {metadata|data: <fs>}` on
its pools and sets `pg_num_min` 16, `recovery_priority` 5 and
`pg_autoscale_bias` 4.0 on the metadata pool
([FSCommands.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/mon/FSCommands.cc#L315-L331));
pool readiness accepts exactly those once the key names the pool's own
filesystem, and that `pg_num_min` then replaces a frozen one.

`fs new` carries `max_mds` and `standby_count_wanted` in its creating proposal
([FSCommands.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/mon/FSCommands.cc#L251-L307)),
so no `fs set` of either follows, and `fs set max_mds` refuses while any
filesystem health warning exists. `allow_standby_replay` is set by a separate
`fs set` only while the observed flag differs (`fs_new_args`, held by
`test_fs_new_sets_the_frozen_mds_counts_and_standby_replay_only_after_creation`).
Why: the v20.2.1 monitor looks that pair's filesystem up before `fs new`
commits it, and its leader aborts.

Bootwright never passes `--force`,
`--allow-dangerous-metadata-overlay`, `--fscid` or `--recover`, never sets an
FSMap flag and never uses `fs volume`. An attached data pool is never added
again: the monitor's pool check refuses it before its no-op
([FSCommands.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/mon/FSCommands.cc#L1077-L1092)).
Subvolume groups are created only after every active rank is held, each with
every attribute explicit; within the filesystem block's own apply, an owned
group whose attributes differ is created once more, which rewrites them, and
inspection never rewrites one
([volume.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/fs/volume.py#L1093-L1111)).

Before `fs new` the worker proves, in order: its ownership mark, the pools it
created under their recorded IDs carrying no other filesystem's key, that no
filesystem of the name exists without its creation record, that the FSMap
admits another filesystem when one exists (`enable_multiple`,
[FSCommands.cc](https://github.com/ceph/ceph/blob/v20.2.1/src/mon/FSCommands.cc#L216-L221)),
and that the bound service has at least as many standby daemons as the
[admission coverage](#cephfilesystem) count. Its attempt record `ceph-filesystem-attempt-v1` precedes `fs new`, and each
subvolume group's `ceph-subvolume-group-attempt-v1` precedes `fs subvolumegroup
create`; a lost reply is adopted only with that record and the frozen pools or
group.

| Refusal | Reached when |
| --- | --- |
| `foreign-filesystem` | A filesystem of the name exists, or one of its pools carries another filesystem's key, without this operation's creation record. |
| `filesystem-identity` | The declared filesystem differs from the recorded one, the native filesystem ID differs from the recorded one, its pools differ, or a recorded filesystem is gone. |
| `filesystem-multiple-disabled` | Another filesystem exists and the FSMap disables multiple filesystems. |
| `foreign-subvolume-group` | A declared group exists that this operation did not create. |
| `mds-coverage` | Fewer standby daemons of the bound service than the [admission coverage](#cephfilesystem) count. |

Readiness proves the recorded filesystem ID, the metadata and data pools by
their recorded IDs in frozen order, `max_mds`, `allow_standby_replay` and
`standby_count_wanted`, every rank below `max_mds` held by an `up:active`
daemon of the bound service, one `up:standby-replay` follower per rank when
replay is declared, its `up:standby-replay` followers plus its `up:standby`
daemons numbering at least `standbyCountWanted`, and every declared group
with its owner, mode, layout pool and quota
([group.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/fs/operations/group.py#L113-L152)).
A drifted value is not ready, never a refusal; an undeclared group is not read.
A filesystem retires with its cluster's hosts.

## CephSubvolume

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
| `mode` | Optional octal string under the [group-mode grammar](#cephfilesystem); omission freezes `755` ([module.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/volumes/module.py#L777)), never the group's mode. |

`metadata.name` is the native subvolume name within its parent scope. A
positive quota is a limit, not a reservation or guarantee of free capacity.
Readiness checks the native subvolume identity/path, layout, quota and owner
metadata. A subvolume retires with its cluster's hosts. This introduces no snapshot
retention or cloning by implication.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephSubvolume
metadata:
  name: documents

spec:
  clusterRef: ceph-example
  filesystemRef: shared
  group: applications
  sizeBytes: 10737418240
```

### Admission

Its schema requires `clusterRef` and `filesystemRef`; `group` and `mode` are
non-empty strings, `sizeBytes` an integer from 1 to 9223372036854775807 (an
omission sets no quota), `uid` and `gid` integers from 0 to 2147483647 and
`poolLayoutRef` a CephPool reference. `namespaceIsolated`, `earmark`,
`normalization` and `casesensitive` are not fields and refuse `api.field`.
Admission refuses with `api.invariant`, before any effect:

- a `filesystemRef` of another cluster;
- a `group` that the filesystem's `spec.subvolumeGroups` does not declare;
- a `poolLayoutRef` of another cluster, without role `cephfsData`, or not
  one of the filesystem's `dataPoolRefs`;
- a `mode` outside `^0?[0-7]{3}$`, also in a defaults fragment.

### Promotion record

The block is `ceph-subvolume-<name>`, and `metadata.name` follows the
[one name rule](api.md#object-names). It requires its CephFilesystem and depends by ID on its
cluster's core and image blocks, never through `Requires CephCluster`
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
and reads it back with `fs subvolume metadata ls`. A
[lost reply](state-reconciliation.md#mutation-safety) is adopted only with the
attempt record and the frozen shape. Before the path is recorded, an adopted
subvolume that differs from the frozen shape in mode alone is created once
more; any other difference refuses `subvolume-identity`. Bootwright never runs
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

## CephNFSExport

One object owns exactly one export of one declared nfs service.
[Storage](ceph.md#resources-readiness-and-access) owns the export
block. Daemon
placement, ports and ingress belong to the [native services](ceph-native-api.md).

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephNFSExport
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
    protocols:
      - 4
    fsal:
      name: CEPH
    clients:
      - addresses:
          - 192.0.2.0/24
        access_type: RO
        squash: root_squash
```

| Field | Contract |
| --- | --- |
| `clusterRef` | Required managed CephCluster. |
| `serviceID` | Required service ID of one declared nfs service of that cluster; it resolves `nfs.<serviceID>` by full-name equality, so an rgw or other service with the same ID never satisfies it. |
| `filesystemRef` | CephFS backing: a CephFilesystem of the same cluster. |
| `subvolumeRef` | CephFS backing: a CephSubvolume of the same cluster; its filesystem is the subvolume's `filesystemRef`. |
| `objectUserRef` | RGW backing: a [CephObjectUser](#cephobjectuser-field-record) of the same cluster, whose buckets the export lists at its root (a user export, `ceph nfs export create rgw --user-id`). |
| `native` | Required mapping holding the export in the native export JSON shape, checked against the closed member table below. |

Exactly one of `filesystemRef`, `subvolumeRef` and `objectUserRef` is
required; two, or none, refuse. There is no `spec.secrets`: an export
declaration carries no credential. A single-bucket export is not admitted.

The `native` mapping follows the
[native export interface](https://docs.ceph.com/en/tentacle/mgr/nfs/#create-or-update-export-via-json-specification)
and keeps cephadm's snake_case spelling. Its members are closed:

| Member | Contract | Frozen default |
| --- | --- | --- |
| `pseudo` | Required clean absolute path other than `/`; unique among the exports of one nfs service. | None. |
| `path` | Clean absolute path in the bound filesystem; exactly `/` for an `objectUserRef` binding. | `/` for a `filesystemRef` or `objectUserRef` binding; none for a subvolume binding. |
| `access_type` | `RW`, `RO` or `NONE`. | `RO`. |
| `squash` | One of the native squash tokens `root`, `root_squash`, `rootsquash`, `rootid`, `root_id_squash`, `rootidsquash`, `all`, `all_squash`, `allsquash`, `all_anomnymous`, `allanonymous`, `no_root_squash`, `none` and `noidsquash`. | `no_root_squash`. |
| `security_label` | Boolean. | `true`. |
| `protocols` | Required in complete input and exactly `[4]`; NFSv3 refuses. | None: an omitted list would serve NFSv3 and NFSv4, so omission refuses. |
| `transports` | `[TCP]`; UDP refuses as an incompatible transport for NFSv4. | `[TCP]`. |
| `sectype` | Exactly `[sys]`; Kerberos (`krb5`, `krb5i`, `krb5p`) refuses and native `none` is not a supported option. | `[sys]`. |
| `fsal.name` | Required: `CEPH` with a filesystem or subvolume binding, `RGW` with `objectUserRef`; any other pairing refuses. | None. |
| `fsal.fs_name` | `CEPH` only: name of the bound filesystem; an authored value must agree with the binding. | The bound filesystem. |
| `fsal.cmount_path` | `CEPH` only: clean absolute path that is `/`, equal to `path`, or an ancestor directory of `path`. | `/`. |
| `clients[]` | Optional; when omitted, the export serves every address at the export-level `access_type` and `squash`. Each entry requires a non-empty `addresses` set of literal IP addresses or CIDRs, plus `access_type` and `squash` with the export-level tokens. | Addresses take their canonical form (masked prefix or bare address). |

Bootwright narrows native in three explicit restrictions. The cmount rule is
an ancestor-directory rule, stricter than native's substring test, so a
cmount of `/a` never covers a path `/ab`. Access and squash tokens take the
exact spelling above, although native accepts any case. A client address is
unique within the export after canonicalization, because NFS-Ganesha applies
the first client block that matches and a repeat would be a silent no-op.
Normalization freezes the defaults into effective state, keeps authored
values and is idempotent; `protocols` and a subvolume binding's `path` are
never added. The frozen `squash` default is `no_root_squash`, so an export that
omits `squash` serves client root as root. Explicit empty `protocols`, `transports` or `sectype` lists refuse.

Refused members name the field, the reason and the next step: `export_id`
(the NFS manager generates the export ID, which Bootwright records),
`cluster_id` (the NFS cluster is the service `spec.serviceID` names),
`fsal.user_id` (the binding supplies the principal: the NFS manager derives
a CephFS one, and `objectUserRef` names the RGW user, which the frozen export
carries as `fsal.user_id`),
`fsal.cephx_key`, `fsal.access_key_id` and `fsal.secret_access_key` (an export
declaration carries no key) and `fsal.sec_label_xattr` (no qualified effect
contract). No refusal echoes a member's value.

The port and placement belong to the nfs service `spec.serviceID` names, and
ingress belongs to an ingress service whose `spec.backend_service` is that nfs
service. Each export entry is one CephNFSExport binding `spec.filesystemRef` or
`spec.subvolumeRef` and holding that entry's native export under `spec.native`.

An export of an object user resolves that user through the cluster's
default realm: the NFS manager reads the user's first S3 key with
`radosgw-admin user info --uid` and no realm, and NFS-Ganesha's RGW library
starts with no `rgw_realm`, so both read the default realm and its default
zone, and RGW creates a `default` zone when either is missing. This is an
explicit effect restriction, the only place Bootwright sets a default:

- Admission refuses an `objectUserRef` export whose cluster declares more
  than one CephObjectGateway, because one realm is the default realm.
- The export block requires the user's block and proves, before any write,
  the gateway's records and committed period, the user's completion record
  and its one active S3 key, and that the default realm is unset or the
  gateway's realm. A default realm of another realm refuses
  `nfs-export-realm-default`; Bootwright never moves a default realm another
  tool set.
- It then makes the gateway's zone its realm's default zone, a pointer of the
  gateway's own realm, and, when no default realm is set, the gateway's realm
  the default realm: the zone first, so the default realm never lacks a
  default zone. Readiness proves both defaults; a moved default is not ready.
- On add the export carries no key and the NFS manager reads it; an update
  must carry the stored key pair unchanged, so the worker reads the user's
  pair from `radosgw-admin user info` into its memory and adds it to the
  apply's input only, never to a record, argument, log or evidence. An
  existing export holding another key than the user's refuses
  `nfs-export-foreign`.

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

## CephSMBResource

One object owns one native SMB manager resource, preserving
[IBM's resource types and fields](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=method-smb-service-resource-specifications).
[B482](milestones/m2.md#b482) owns schema qualification and the native manager
boundary. The resource-type discriminator remains native; Bootwright does not
rename cluster/share/auth fields or expose the generated daemon as a second
managed service.

| Field | Contract |
| --- | --- |
| `clusterRef` | Required managed CephCluster; an external cluster refuses. |
| `native` | Required mapping holding one native SMB manager resource; its `resource_type` selects the closed per-type record below. |
| `smbClusterRef` | Required on a `ceph.smb.share`, forbidden on every other type; names a `CephSMBResource` whose native type is `ceph.smb.cluster`. |
| `filesystemRef` | Share backing: a CephFilesystem of the same cluster. Forbidden on every type but `ceph.smb.share`. |
| `subvolumeRef` | Share backing: a CephSubvolume of the same cluster. Forbidden on every type but `ceph.smb.share`. |
| `resourceRefs` | Optional set of `CephSMBResource` names, admitted only on a `ceph.smb.cluster`, naming its `ceph.smb.join.auth` and `ceph.smb.usersgroups` resources. When omitted it is derived from the native references: each same-cluster resource whose native ID a `domain_settings.join_sources[].ref` or a `user_group_settings[].ref` of `source_type: resource` names, in that order. The native references stay the authored home. |
| `ldapServerRef` | Optional [`LDAPServer`](api/infrastructure-services.md#oidcprovider-and-ldapserver) on a `ceph.smb.cluster` with `auth_mode: active-directory`, naming the Active Directory domain the cluster joins. The server declares `activeDirectory`, whose realm an omitted `domain_settings.realm` takes. Forbidden on every other type and on `auth_mode: user`. |
| `secrets` | Required on `ceph.smb.join.auth` and `ceph.smb.usersgroups`, forbidden elsewhere; the leaves in [secret bindings](ceph-native-api.md#secret-bindings). Each binding names an `opaque` or `token` Secret. |

Exactly one of `filesystemRef` and `subvolumeRef` is required for a share;
both, or neither, refuse.

The `native` mapping keeps the SMB manager module's snake_case spelling, its
types and its enumerations. `resource_type` selects one closed record:

| Type | Members |
| --- | --- |
| `ceph.smb.cluster` | Required `cluster_id`, `auth_mode` (`user` or `active-directory`) and `placement` (the native placement grammar); optional `intent`, `domain_settings` (required `realm`, derived from `spec.ldapServerRef` when omitted, and `join_sources[]` of `source_type: resource` and a required `ref`), `user_group_settings[]` (`source_type` and a required `ref`), `custom_dns` (a set), `clustering` (`default`, `always` or `never`) and `public_addrs[]` (a required `address`). |
| `ceph.smb.share` | Required `share_id`; `cluster_id`, derived from the `smbClusterRef` resource's `cluster_id` when omitted; optional `intent`, `name`, `readonly`, `browseable`, `cephfs` (`volume`, `path`, `subvolumegroup`, `subvolume` and `provider`), `restrict_access` and `login_control[]` (a required `name`, `category` `user` or `group`, and `access`). |
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
| join.auth, usersgroups | `linked_to_cluster` | Bootwright does not realize linked SMB auth resources; each CephSMBResource applies and removes its own native resource. | Omit it; the SMB cluster names the resource in its `spec.resourceRefs`. |
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
- `spec.ldapServerRef` refuses at its field on a type other than
  `ceph.smb.cluster` and on `auth_mode: user`, also in a fragment. Normalization
  derives an omitted `domain_settings.realm` from the server's
  `activeDirectory.realm`, as a share's `cluster_id` derives from its cluster,
  so the native projection, the request and the effective output carry the
  realm and no contract moves; a `domain_settings` without `join_sources`
  still refuses at `join_sources`. A server that declares no `activeDirectory`
  refuses at `$.spec.ldapServerRef`, naming the server, and an authored realm
  that differs from the server's, compared without case, refuses at
  `$.spec.native.domain_settings.realm`, naming the server and echoing no
  realm. The server's bind DN, bind password, URLs and trust bundle are not
  used: the join authenticates with the `ceph.smb.join.auth` resource.

An Active Directory SMB cluster has no local-user fallback. The SMB manager
forbids `user_group_settings` in that mode and one subvolume backs one SMB
cluster, so no local SMB user can reach the data while the domain controllers
are down. The operator reaches the data through CephFS with a Ceph key (a
[CephClient](#cephclient)) or `bootwright cluster exec`, as the
[operator guide](../docs/operator-guide.md#reach-ceph-while-an-identity-provider-is-down)
describes.

Cross-object rules, each refusal naming the other object by its
`metadata.name` and never echoing a native value:

- Native identities are unique per CephCluster: a cluster's `cluster_id`,
  a share's (`cluster_id`, `share_id`), a join.auth's `auth_id` and a
  usersgroups resource's `users_groups_id`. A second CephSMBResource of
  the same type and identity refuses at the identity's last field.
- A share's effective name is `name`, else `share_id`, as the manager
  derives it; two shares of one `cluster_id` with the same effective name
  refuse, compared exactly. One share name on two SMB clusters is admitted.
- A share's `smbClusterRef` names a same-cluster `ceph.smb.cluster`
  CephSMBResource, and an authored `cluster_id` of the share equals that
  resource's `cluster_id`.
- The share's `cephfs` reference leaves agree with its backing. With
  `filesystemRef`, `cephfs.subvolumegroup` and `cephfs.subvolume` refuse,
  also in a fragment: a filesystem reference cannot smuggle an unbound
  subvolume name. With `subvolumeRef`, `cephfs.subvolume` equals the bound
  CephSubvolume's name and `cephfs.subvolumegroup` its `group` (a
  subvolume in the native default group takes none). With either backing,
  `cephfs.volume` equals the bound filesystem's name. `cephfs.path: /` is the
  root of the backing and is admitted; a clean absolute path stays inside the
  backing.
- NFS and SMB never expose one backing: a share refuses unless every NFS
  export of its filesystem and the share bind distinct CephSubvolumes; two
  SMB clusters require distinct subvolumes of a shared filesystem. Admission
  cannot see resolved subvolume paths, so a filesystem-wide export or share on either side
  refuses with the other on the same filesystem. The refusal names the
  CephNFSExport, or the share of the other SMB cluster, whose subvolume
  earmark `smb.cluster.<cluster_id>` the manager would refuse at apply.
- A cluster's `resourceRefs` name same-cluster `ceph.smb.join.auth` and
  `ceph.smb.usersgroups` resources only. Each `domain_settings.join_sources[]`
  `ref` resolves to exactly one join.auth member by `auth_id`, and each
  `user_group_settings[]` `ref` of `source_type: resource` or omitted to
  exactly one usersgroups member by `users_groups_id`; a reference with no
  match or several matches refuses, and so does a member no native reference
  consumes. Several SMB clusters may name one auth resource.
- SMB clusters of one CephCluster place on disjoint hosts. Their listeners
  follow the [listener registry](api/ceph.md#listener-registry); a collision
  names the port, host and other listener.
- `public_addrs[].address` is `address/prefix`, also in a fragment; a
  non-empty `public_addrs` needs clustering, also in a fragment, because CTDB
  assigns the addresses. Each address lies inside a declared public CIDR with
  that CIDR's prefix length, is neither its network nor its broadcast
  address, and equals no Machine address, no ingress virtual IP of any
  CephCluster, no earlier address of its own list and no public address
  of another SMB cluster.

The SMB manager generates the orchestrator service `smb.<cluster_id>` and its
daemons, the pool `.smb` and the subvolume earmarks `smb.cluster.<cluster_id>`;
none can be declared: an `smb` service and `smb` in `spec.mgrModules`
refuse, and no CephPool can be named `.smb`.

Dependency edges are extracted from the qualified native schema, not guessed
from strings. Native generated services, authentication children and placement
belong to the SMB manager resource that creates them. No competing `services`
declaration may claim them. Public native input omits secret leaves, while the
protected execution artifact contains the bound native form.

The file-chain example below uses a separate SMB subvolume from NFS; shared
NFS/SMB exposure remains forbidden. Assume `documents-smb-data` is an owned
subvolume named `documents-smb-data` in filesystem `shared`, group
`applications`, and `files-smb` is an owned native SMB cluster resource with
`cluster_id: files` and qualified authentication.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephSMBResource
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
kind: CephSMBResource
metadata:
  name: files-smb

spec:
  clusterRef: ceph-example

  native:
    resource_type: ceph.smb.cluster
    cluster_id: files
    auth_mode: user
    user_group_settings:
      - source_type: resource
        ref: files
    placement:
      hosts:
        - storage-03.example.com
      count: 1

---
apiVersion: bootwright.io/v1alpha1
kind: CephSMBResource
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

Planning refuses a CephSMBResource whose CephCluster does not set both `samba`
and `samba_metrics` under `spec.images`, as the
[refusal table](ceph.md#refusal-table) states.

## CephSignOn

One object signs users in to one managed cluster's management gateway, and
through it the dashboard, Grafana, Prometheus and Alertmanager, with
oauth2-proxy and one [OIDCProvider](api/infrastructure-services.md). Its block
and readiness are the [management sign-on](ceph.md#management-sign-on).

| Field | Contract |
| --- | --- |
| `clusterRef` | Required managed CephCluster that declares a `mgmt-gateway` service; an external cluster, or one without a gateway, refuses at `$.spec.clusterRef`. A cluster has at most one CephSignOn: a second one, later in name order, refuses as a duplicate at `$.spec.clusterRef`. |
| `oidcProviderRef` | Required OIDCProvider. Its `issuerURL` becomes the proxy's `oidc_issuer_url`, and its `trustBundleRef`, when set, the proxy's one `custom_configs` entry, mounted at `/etc/pki/tls/certs/oauth2-proxy-idp-ca.pem` and added to the container's trust store. |
| `native` | Required cephadm `oauth2-proxy` service document in native spelling: required `service_type: oauth2-proxy`, `placement` (the native placement grammar, its hosts resolved to the cluster's topology nodes) and `spec` with required `client_id` and `provider_display_name`; optional `redirect_url`, `allowlist_domains`, `https_address` and `networks`. |
| `secrets` | Required bindings `{path, secretRef}`: `/spec/client_secret` and `/spec/cookie_secret`, each an `opaque` or `token` Secret, both required; optional `/spec/ssl_cert` and `/spec/ssl_key`, one `tlsCertificate` Secret bound at both or at neither. Without the pair cephadm signs oauth2-proxy's certificate. |

Native members Bootwright does not realize refuse at their own field, and no
refusal echoes a value:

| Member | Reason | Next step |
| --- | --- | --- |
| `spec.oidc_issuer_url` | The issuer URL comes from the OIDCProvider's issuerURL. | Remove it. |
| `custom_configs` | The identity provider's CA bundle comes from the OIDCProvider's trustBundleRef. | Remove it; set `trustBundleRef` on the OIDCProvider. |
| `spec.client_secret`, `spec.cookie_secret` | Native documents never carry credentials inline. | Bind the path in `spec.secrets`. |
| `spec.ssl_cert`, `spec.ssl_key` | Native documents never carry TLS material inline. | Bind both paths in `spec.secrets`. |
| `spec.ssl`, `spec.certificate_source`, `spec.custom_sans` | TLS options of this service are not supported by Bootwright yet. | Omit it. |
| `spec.ssl_insecure_skip_verify` | oauth2-proxy must verify the identity provider's certificate. | Omit it. |
| `spec.scope`, `spec.email_domains` | No oauth2-proxy spec Bootwright realizes has this member. | Omit it. |
| `service_id`, `service_name` | oauth2-proxy is a singleton service named by its type. | Remove it. |
| `config`, `extra_container_args`, `extra_entrypoint_args`, `unmanaged`, `preview_only` | Not qualified by this consumer. | Remove it. |
| `placement.count_per_host` | oauth2-proxy places at most one daemon per host. | Omit it. |

Value rules: `https_address` is `host:port` with the fixed
[oauth2-proxy port](api/ceph.md#listener-registry) 4180, whatever the address
names; `redirect_url` is an `https` URL without user information; each
`allowlist_domains` entry is `[.|*.]host[:port|:*]`. The 4180 listener on the
placement hosts clashes with any other listener there, refused at
`$.spec.native.placement`. A bound `cookie_secret` must be 16, 24 or 32 bytes
after URL-safe base64 decoding when it decodes, else as text, as IBM's
`OAuth2ProxySpec.validate` requires; binding time, not admission, checks it.

`CephCluster.spec.images.oauth2_proxy` is admitted whenever the cluster
declares a `mgmt-gateway` and required by a CephSignOn, which
[refuses at planning](ceph.md#refusal-table) without it. Every host pre-pulls
that image, because the image block resolves it before any sign-on placement
is known.

The block's plan impacts are, in order: its realization; "the management
gateway signs users in through oauth2-proxy and OIDCProvider <name>"; "Bootwright
enables the dashboard's OAuth2 single sign-on; local sign-in returns only after
bootwright cluster sign-on suspend"; "cephadm reconfigures the management
gateway, Grafana, Prometheus and Alertmanager for single sign-on; their blocks
do not change"; "while sign-on is on, a manager's dashboard port 8443 accepts a
forged token (an upstream defect); keep it reachable only from the management
gateway hosts" ([B1829](milestones/m2.md#b1829)); without the TLS pair
"cephadm signs oauth2-proxy's TLS certificate"; and without a trust bundle
"oauth2-proxy verifies the issuer of OIDCProvider <name> with its image's
system trust store".

The frozen request is `ceph.SignOnRequest`: the cluster, object and
controller, the cluster's Machines and host key references, the OIDCProvider's
name, issuer URL and whether it binds a trust bundle, the authored
`oauth2_proxy` image reference, the oauth2-proxy document with the derived
`oidc_issuer_url` and `custom_configs` mount, the bindings (the authored ones
and the derived `/custom_configs/0/content` to the provider's trust bundle
Secret), and the management gateway's hosts, port, virtual IP, endpoint name,
daemon count and whether it fronts Grafana. Its request digest moves only
with those members, never with whether the cluster's apply came before it:
a CephSignOn added to a completed apply is a
[sign-on addition](ceph.md#sign-on-addition). The block retires with its
cluster's hosts; there is no per-block removal while they survive.

## CephNVMeoFSubsystem

One object owns one NVMe-oF subsystem of one gateway group, with its
listeners, allowed hosts and namespaces ([B480](milestones/m2.md#b480)). The
gateway group is the native `nvmeof` service of its CephCluster
([NVMe-oF gateway record](ceph-native-api.md#nvme-of-gateway-record)); no
native declarative document exists for a subsystem, so the fields are typed
camel-case Bootwright fields named after the gateway's `ceph nvmeof`
parameters. The capability freezes the service, listener addresses and backing
images before effects; native identity and readiness proofs guard completion.

| Field | Contract |
| --- | --- |
| `metadata.name` | The [one name rule](api.md#object-names); the block is `ceph-nvmeof-<name>`. |
| `clusterRef` | Required managed CephCluster. |
| `serviceRef` | Required qualified native name `nvmeof.<service_id>` of a declared nvmeof service of that cluster; an absent or other-type target is a field error. It fixes the gateway pool and group. |
| `nqn` | Required subsystem NQN: `nqn.yyyy-mm.<reverse domain>:<name>` or `nqn.2014-08.org.nvmexpress:uuid:<UUID>`, 11 to 223 printable ASCII characters, never the discovery NQN; unique among the cluster's subsystems, which is stricter than the gateway's per-group scope because an initiator identifies a subsystem by its NQN alone. |
| `listeners` | Required set keyed by `host`, a topology node name, with optional `trsvcid`. The hosts are exactly the hosts the service places gateways on: a gateway serves only the namespaces of the load-balancing groups it owns, so a namespace is reachable only through a listener on its owning gateway ([upstream](https://github.com/ceph/ceph-nvmeof/blob/tentacle_9.0_base/control/grpc.py#L5040-L5080)). `trsvcid`, when given, is the fixed NVMe/TCP port in the [listener registry](api/ceph.md#listener-registry), which cephadm reserves and opens on a gateway host. |
| `hosts` | Required nonempty set of at most 128 initiator host NQNs, in the `nqn` grammar; `*`, which would let any host connect, refuses. |
| `namespaces` | Required nonempty set of at most 128 entries keyed by `nsid` (an NVMe namespace ID, 1 to 4294967294), each with a required `imageRef` naming a CephRBDImage of the same cluster. An image backs at most one namespace across every subsystem. |

Planning refuses a subsystem whose request-derived apply bound exceeds six
hours, before any effect. This execution bound can be stricter than the
128-entry shape limits; it is not a vendor capacity limit. The bound counts
native identity reads, all first-apply writes and snapshots, held image probes,
ANA and listener checks, a separate bounded cluster observation (including
manager retries), and the readiness retry window. Original-attempt replay
requires no more work than first apply. The implementation owns the formula in
`internal/storage/ceph/nvmeof.go` and `ceph_native_nvmeof.py`.

The subsystem never creates an image, never allows any host and passes no
`force`. Maximum namespace, serial number, HA, load-balancing group,
read-only, visibility, DH-HMAC-CHAP key and PSK fields are absent: the
worker freezes stable original-attempt serial and namespace UUIDs, enables
HA, assigns load-balancing groups, and requires 512-byte blocks and automatic
visibility. Native calls use explicit safe defaults and opaque errors. No
diagnostic echoes an authored NQN.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephNVMeoFSubsystem
metadata:
  name: block-a

spec:
  clusterRef: ceph-example
  serviceRef: nvmeof.block
  nqn: nqn.2016-06.io.example:block-a

  listeners:
    - host: ceph-node-1
    - host: ceph-node-2

  hosts:
    - nqn.2014-08.org.nvmexpress:uuid:00000000-0000-4000-8000-000000000001

  namespaces:
    - nsid: 1
      imageRef: application-disk
```

## CephConsumer

A `CephConsumer` is the Ceph side of one Data Foundation consumer. Its arm says what it is, so it has no `type`; the
managed arm names what the consumer attaches, and the consumer block
`ceph-consumer-<name>` realizes the
principals and the external-cluster details that a consuming package places
in its cluster ([add-on lifecycle](add-ons.md#built-in-packages-and-selection)).

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephConsumer
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
| `clusterRef` | Required `CephCluster`. |
| `dataFoundation.rbdPoolRef` | Required replicated CephPool of role `rbd` of the same cluster. |
| `dataFoundation.filesystemRef` | Required CephFilesystem of the same cluster declaring the subvolume group `csi`. |
| `dataFoundation.objectGatewayRef` | Optional CephObjectGateway of the same cluster with an endpoint. |
| `dataFoundation.cephxKeyType` | `aes` (default) or `aes256k`: the type of every key the consumer mints. When its cluster declares `spec.security.cephx.keyType`, it must equal that type ([CephX proof](ceph.md#frozen-release-and-trust)). |
| `externalDetails.fromSecretRef` | An `opaque` Secret for an external cluster; refused, parked as [B107](milestones/backlog.md#b107). |

Admission also requires a declared prometheus service on the cluster, because
ocs-operator refuses details without a monitoring endpoint; a consumer name
follows the [one name rule](api.md#object-names). A consuming package names each consumer at most once across the
input. `aes` is the default because kernel CephFS and RBD clients need
Linux 7.0 or later for `aes256k` keys; the operator chooses `aes256k` for
nodes that run it.

### Consumer refusals

| Unsupported condition | Boundary and correction |
| --- | --- |
| External-cluster details | Admission refuses the external arm under B107; use a managed cluster. |
| Erasure-coded RBD pool, missing `csi` group, monitoring service or gateway FQDN | Admission names the reference; select a replicated RBD pool and declare each prerequisite. |
| A consumer key type other than the key type its cluster declares | Admission refuses at `dataFoundation.cephxKeyType`; declare the cluster's type, or remove the cluster's `security.cephx.keyType`. |
| Ambiguous public address on a selected MON or MGR host, or a shared MON short name | Planning requires one address in the Ceph public networks per selected host and unique MON short names. |
| Direct RGW placement selects fewer daemons than eligible hosts | Planning requires an ingress VIP or a placement whose count covers its selected hosts. |
| Disallowed key type, missing native prerequisite or mismatched owned identity | Realization refuses before creating credentials; correct the declared prerequisites or restore the original owned state. |
| Removing a consumer while its hosts survive, or with physical hosts | Removal refuses; consumer retirement requires every installed host to be an owned virtual Machine removed by the same destroy, a [provided stretch tiebreaker](ceph.md#a-provided-tiebreakers-removal) left to its cluster block. |
| Removing a consumer while a consuming container cluster survives the removal | Removal refuses before registration, naming the consuming cluster and the consumer; retirement also requires every container cluster whose `fdf` package names the consumer to be removed by the same destroy, its nodes owned virtual Machines that destroy stops and deletes. A consumer whose `fdf` block started but never completed still makes the destroy refuse. |
| An external-details check that fails after the credentials exist | The block offers nothing, clears the assembled material and ends with outcome unknown: apply stops `ceph.unproved` and observation stops `ceph.evidence-mismatch`; repeat the operation, which observes the Ceph state again ([Consumer key custody](#consumer-key-custody) lists the checks). |

The consumer block depends on the cluster, the pool, the filesystem, the
gateway and the monitoring unit, is exclusive on its cluster, and runs on the
bootstrap host. Before any effect it proves the cluster's ownership record and
FSID, the pool's `rbd` application under its recorded ID, the `csi` group, a
non-empty `prometheus` endpoint in `ceph mgr services`, the gateway's realm,
zone group and zone, and that the monitors allow the declared key type; any
failure refuses, naming the missing prerequisite or the allowed key types.
It then mints, for this consumer and the apply operation of the block's
original attempt, five CephX principals and, with a gateway, one RGW user, each
named from a digest of that original operation's identity and the consumer's
name. The digest's domain tag is `bootwright.storage-export.principal-v1`, the
ownership mark is `bootwright-managed-export` and retained material lives under
`/var/lib/bootwright/ceph`:

| Principal | `mon` | `mgr` | `osd` | `mds` |
| --- | --- | --- | --- | --- |
| health | `allow r, allow command quorum_status, allow command version` | `allow command config` | `profile rbd-read-only`, plus with a gateway `allow rwx pool=<zone>.rgw.meta, allow r pool=.rgw.root, allow rw pool=<zone>.rgw.control, allow rx pool=<zone>.rgw.log, allow x pool=<index pool>` | `allow *` |
| RBD node | `profile rbd, allow command 'osd blocklist'` | none | `profile rbd pool=<pool>` | none |
| RBD provisioner | `profile rbd, allow command 'osd blocklist'` | `allow rw` | `profile rbd pool=<pool>` | none |
| CephFS node | `allow r, allow command 'osd blocklist'` | `allow rw` | `allow rw tag cephfs *=<filesystem>` | `allow rw` |
| CephFS provisioner | `allow r, allow command 'osd blocklist'` | `allow rw` | `allow rw tag cephfs metadata=<filesystem>` | `allow *` |

`exportCaps` in `internal/storage/ceph/export.go` owns the table, which
`TestConsumerCapsTableMatchesExportCaps` holds equal. These are the exporter's restricted-authorization capabilities, whose
privileged grants serve ocs-operator's health checks and CSI provisioning.
Go freezes them as the request's `principals` and judges the created
principals against them; the worker applies them.
The RGW user carries `buckets=*;users=*;usage=read;metadata=read;zone=read`
and `info=read`, created in the gateway's realm, zone group and zone; its caps
apply to every user and bucket of the realm. It serves the operator's object
bucket provisioning.

Trust extent: a consumer trusts its consuming cluster's administrators with
every RGW user and bucket of the gateway's realm, read and write of the
zone's RGW metadata pool, which holds every RGW user's keys, and every path of
the named filesystem. Consumers that share a cluster, filesystem or gateway are
not isolated from each other: per-consumer principals give per-consumer removal,
not isolation.

### Consumer key custody

Each CephX key is generated at the declared type and retained under client
custody ([native-side retention](secrets.md#native-side-retention)) before its
native add; a lost reply recaptures it and never regenerates. A continuation
derives the same names and reuses the retained keys; an apply of a completed
consumer mints nothing. The RGW user is
created by `radosgw-admin user create
--uid=<minted> ... --gen-access-key --gen-secret`, never with
`--access-key` or `--secret-key` on any argument list; its JSON reply is read
privately and the key pair retained under client custody before the block
records progress. A lost reply recaptures the pair from `radosgw-admin user
info --uid=<minted>`, read privately, and refuses unless the user has exactly
one S3 key pair and the minted caps. An entity or RGW user of a minted name
that this block has no attempt record for refuses as foreign; a key of
another type refuses. The consumer never adopts, rotates, re-caps or removes an
entity it did not mint and never changes the monitors' cipher policy. Its
observation reads an apply that wrote no scope record and finds no minted
principal as no effect, and one whose scope record carries no completion as
partial, so the next apply continues it with what it retained.

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
| `rook-csi-{rbd,cephfs}-{node,provisioner}-<id>` for the four CSI principals | Secret | `userID`, `userKey` |
| `ceph-rbd` | StorageClass | `pool` and the RBD secret names |
| `cephfs` | StorageClass | `fsName`, the default data pool as `pool`, and the CephFS secret names |
| `ceph-rgw`, with a gateway | StorageClass | `endpoint`: `<fqdn>:<port>`; `poolPrefix`: the zone |
| `rgw-admin-ops-user`, with a gateway | Secret | `accessKey`, `secretKey` |
| `ceph-rgw-tls-cert`, with a TLS gateway | Secret | `cert`: the gateway certificate chain, never its key |

The details are a pure function of the retained keys and the non-secret facts
the block recorded at its first assembly, so a continuation reproduces the
same bytes. Before offering them the block checks them: every key is a
non-empty string of the declared type, the recorded leader is the MON ID
(the host's short name) of a monitor the request declares, at its declared
public address, the monitoring endpoint names at least one address
and a port, `ceph-rgw` names exactly the gateway's endpoint, the
RGW keys are non-empty, and `ceph-rgw-tls-cert` is present exactly when the
gateway terminates TLS. Its evidence names the principals, their capabilities
and key type, the RGW user, the recorded leader and the entries by name and
kind; no key, key digest, certificate private key or detail reaches a
request, record, evidence, log, diagnostic or output. A failed check follows
the external-details row of [Consumer refusals](#consumer-refusals), which
`TestExportProducedDetailsValidator` holds for the checks. Observation compares
each principal's key and capabilities privately and never writes.

The consumer block retires with its cluster under the two removal rows of
[Consumer refusals](#consumer-refusals); the hosts' stop proof then stands in
for the consumer's quiescence.
A consumer that survives the removal, such as a cluster on physical nodes,
makes the removal refuse before registration, naming the consuming cluster and
the consumer, and the cluster's own retirement refuses with it
(`TestRetirementRefusesBeforeRegistrationWhileAConsumerSurvives`,
`TestADestroyRefusesWhileAPhysicalCompactConsumerSurvives`). A context
whose Ceph is virtual and whose consuming cluster is not therefore has no full
destroy, and `apply --dry-run` says so before the first apply: the apply's
plan carries a `plan.consumer-outlives-destroy` warning naming the consuming
cluster and the consumer, which the removal engine derives from the same decision
that refuses the destroy
([quiescence before removal](state-reconciliation.md#quiescence-before-removal)).
Otherwise its removal refuses with the cluster's other resources.

### Attached gateway DNS

An attached gateway contributes its endpoint FQDN and terminating addresses to
all retained managed DNS services, which already serve the retained graph's
machine and cluster names. The addresses are the fronting ingress virtual IP
(on a stretch cluster, the virtual IP of its ingress over both data sites or
those of its [per-site ingresses](ceph.md#stretch-services-and-front-doors)),
or the public-network addresses of the RGW placement when no ingress fronts
it. Direct RGW placement must put a daemon on every eligible host; a smaller
count requires an ingress VIP because native scheduling does not freeze which
hosts terminate the endpoint. An unresolved address refuses planning; two
contributions for one name must agree, and a conflicting existing record refuses. An unattached gateway
contributes no record. The storage domain derives these records in one pure
function, which the add-on consumer facts read through the same attached-gateway
lookup; no other package reads `objectGatewayRef`. The consumer declaration owns
the contribution; the DNS
service owns its frozen publication and inverse, so shared gateway contributions
are deduplicated and retire with that service.

The consumer depends on the managed DNS blocks. Before creating credentials and
when observing readiness, it checks resolution read-only from the bootstrap
host within a bounded process. The resolved address set must equal the declared
terminating set; an absent, additional or wrong address, malformed reply or
timeout refuses naming the FQDN. This check also applies when no managed DNS
service is retained: the operator supplies resolution, and Bootwright never
changes external DNS. Bootstrap resolution does not prove node or pod resolution;
that remains the live [B761](milestones/m4.md#b761) acceptance gate.

### CephObjectUser field record

One RGW user in the zone of one CephObjectGateway, realized by its
[user block](ceph.md#resources-readiness-and-access). Accounts, tenants,
subusers, Swift keys, capabilities and quotas are not admitted.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephObjectUser
metadata:
  name: app-writer

spec:
  clusterRef: ceph-example
  objectGatewayRef: objects
  displayName: App writer
```

| Field | Contract | Native projection |
| --- | --- | --- |
| `metadata.name` | The native user ID under the [one name rule](api.md#object-names) (its block identity is `ceph-object-user-<name>`), never `dashboard`. | `radosgw-admin user create --uid`; no tenant. |
| `clusterRef` | Required managed CephCluster. | None. |
| `objectGatewayRef` | Required CephObjectGateway of the same cluster. | The user is created in that gateway's realm, zone group and zone with `--rgw-realm`, `--rgw-zonegroup` and `--rgw-zone`. |
| `displayName` | Optional text of at most 256 bytes without control characters and not beginning with `-`, which `radosgw-admin` would read as an option; omitted, it is `metadata.name`. | `--display-name`. |

The user ID `dashboard` is reserved because the mgr dashboard module
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/dashboard/services/rgw_client.py))
creates a system user of that ID in every realm, which the gateway owns as a
derived child. Bootwright passes no key argument: `radosgw-admin user create`
generates one S3 key pair when none is given
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/rgw/radosgw-admin/radosgw-admin.cc#L6872-L6875)),
and it accepts keys only as command-line arguments, which Bootwright never
fills with secret bytes. The pair crosses only the command's output into the
worker, is retained on the bootstrap host under
[native-side retention](secrets.md#native-side-retention) before the block
reports, and becomes the block's produced material `object-user`, the
canonical JSON `{"accessKey", "secretKey", "uid"}`; a retry reads it back and
never rotates it. Bootwright retains any key of 1 to 128 visible ASCII
characters other than `&`, `<` and `>`, which the canonical JSON escapes, and
pins no build's key format. The pair is revealed only by the one command
[produced material](secrets.md#produced-material) lists for it;
[B489](milestones/m2.md#b489) owns the rest of access handoff.

Admission refuses, before any effect: a gateway of another cluster, the user ID
`dashboard` and a display name over 256 bytes,
with control characters or beginning with `-`. Planning propagates the bound gateway's planning
refusals.

### CephObjectBucket field record

One S3 bucket of one CephObjectUser in that user's CephObjectGateway, with an
optional native bucket policy, realized by its
[bucket block](ceph.md#resources-readiness-and-access). Tenants, versioning,
object lock, lifecycle, quotas, CORS, website and access-control lists are not
admitted.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CephObjectBucket
metadata:
  name: app-data

spec:
  clusterRef: ceph-example
  objectGatewayRef: objects
  ownerRef: app-writer

  policy:
    Version: "2012-10-17"

    Statement:
      - Effect: Allow
        Action:
          - s3:GetObject
        Principal:
          AWS:
            - arn:aws:iam:::user/app-reader
        Resource:
          - arn:aws:s3:::app-data/*
```

| Field | Contract | Native projection |
| --- | --- | --- |
| `metadata.name` | The native bucket name, a DNS label of 3 to 63 characters under the [one name rule](api.md#object-names) (its block identity is `ceph-object-bucket-<name>`), so always a valid S3 bucket name. | The bucket of S3 CreateBucket; no tenant. |
| `clusterRef` | Required managed CephCluster. | None. |
| `objectGatewayRef` | Required CephObjectGateway of the same cluster. | The bucket is created through a running daemon of the gateway's rgw service, in its zone group. |
| `ownerRef` | Required CephObjectUser of the same cluster whose `objectGatewayRef` is this one. | The bucket's owner: every S3 request is signed with that user's key pair. |
| `policy` | Optional mapping: the native S3 bucket policy document, with a `Version` string and a `Statement` of one mapping or a list that is not empty, of strings, integers, booleans, lists and mappings, at most 65536 bytes of JSON. | The body of S3 PutBucketPolicy, its canonical JSON (sorted keys). |

`radosgw-admin` has no command that creates a bucket or puts a bucket policy
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/rgw/radosgw-admin/radosgw-admin.cc#L946-L977)),
so the bucket block signs S3 CreateBucket and PutBucketPolicy with AWS
Signature Version 4 under the owner's key pair, which it reads from
`radosgw-admin user info` into the worker's memory only and never answers,
logs, records or passes as an argument. RGW stores the policy bytes it is sent
and GetBucketPolicy answers them
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/rgw/rgw_op.cc#L8803-L8905)),
so the frozen canonical JSON is also what readiness compares. RGW parses and
judges the policy; Bootwright validates only its shape and bound, and a policy
RGW rejects refuses `object-bucket-s3` with RGW's error code and message. RGW's
parser ([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/rgw/rgw_iam_policy.cc#L1865-L1873))
reads `Version`, `Id` and `Statement` at the top of the document, takes only
`2008-10-17` or `2012-10-17` as `Version`
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/rgw/rgw_iam_policy.cc#L612-L620)),
refuses an unknown key, and requires neither `Version` nor a statement, so an
empty document is stored and grants nothing. Bootwright requires both as the
native IAM policy grammar does, so a policy that grants nothing is refused
before any effect.

Admission refuses, before any effect: a gateway or owner of another cluster, an
owner of another gateway, a name shorter than 3 characters, the S3 minimum,
and a policy without a `Version` string or without a `Statement` that is a
mapping or a list holding at least one entry. Planning refuses a policy value that is not a string, integer,
boolean, list or mapping, a policy over 65536 bytes of JSON, and propagates
the bound gateway's planning refusals.

## Remaining resource families and consuming gates

These families have no kind of their own. Each row states what a declaration
of the family meets and where to declare the need instead. This page admits no
arbitrary extension map and no native declarative schema for an imperative
interface.

| Family | Refusal at admission | Next step |
| --- | --- | --- |
| Object realm, zone group and zone | No such kind exists; the realm, zone group and zone are the `realm`, `zoneGroup` and `zone` fields of a CephObjectGateway, and no ambient default realm or zone is used. | Declare a [CephObjectGateway](#cephobjectgateway-field-record). |
| Object accounts and tenants | Not admitted by [CephObjectUser](#cephobjectuser-field-record) or [CephObjectBucket](#cephobjectbucket-field-record). | Omit them; users and buckets are without tenant or account. |
| iSCSI | Admission refuses `service_type: iscsi` at that field, and no iSCSI kind exists; no NVMe field substitutes for an iSCSI one. | Use [CephNVMeoFSubsystem](#cephnvmeofsubsystem) for block access over the network. |
| Replication peers beyond [CephReplication](#cephreplication) | A peer outside the two declared managed clusters, RGW multisite, snapshot-based mirroring and CephFS snapshot mirroring are not realized; no ambient peer lookup, context crossing or foreign adoption. | Declare one CephReplication between two managed clusters. |
| Access material and handoff beyond [CephClient](#cephclient), [CephObjectUser](#cephobjectuser-field-record) and [CephConsumer](#cephconsumer) | No other reveal command exists: a CephClient's keyring is revealed only through the [client keyring export](cli.md#client-keyring-export), and the dashboard password and object-user key pairs only through [storage dashboard access](cli.md#storage-dashboard-access). Managed `CephConsumer` material (its produced schema, principals and custody) belongs to [B86](milestones/m4.md#b86), which reuses this boundary. | Use the reveal command named in the row. |

A native name in an imperative provider request is always projected from the
declared identity/reference graph.

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
| NFS on RGW | Gateway and object user plus native NFS service → the gateway's realm and zone as the defaults → user-bound native export of the user's buckets → qualified endpoint and client restrictions. |
| Replication | Both declared cluster/resource endpoints and their access dependencies → explicit peer relationship → initial synchronization proof; native replication direction is not a cyclic Bootwright creation edge. |
| Data Foundation handoff | Declared RBD pool, filesystem, optional object gateway and scoped access dependencies → existing CephConsumer material; downstream installation remains separate. Its material is [B86](milestones/m4.md#b86)'s. |

For example, the NFS example consumes `documents` only after the
`CephSubvolume` exists and its native path is observed; `documents` consumes
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
intent. Each native create, delete and initial synchronization ends ready,
unfinished or unknown within the bounded wait its block states in
[Storage](ceph.md#resources-readiness-and-access). Inspection never repairs
drift and never creates data. Client data tests remain
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
