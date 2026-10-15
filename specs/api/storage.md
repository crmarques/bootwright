# Ceph and storage services

This page owns storage admission. The closed schemas in
`api/v1alpha1/storage.go` and `api/v1alpha1/storage_native.go` own accepted
fields. [Native service input](../storage-native-api.md) owns the approved
service vocabulary; [resource contracts](../storage-resources.md) own the
separate declarative objects. Bootwright fields use camel case; complete
native service documents retain native snake case.

Current admission requires nonempty `spec.ceph.services`. Core MON, MGR and
OSD documents are admitted, and so are MDS documents that a StorageFilesystem
of the same cluster binds by name. StorageFilesystem itself stays refused until
[B492](../milestones/m2.md#b492)'s filesystem lane, so no MDS service is
planned before then. RGW, ingress and NFS documents are admitted under their
[native records](../storage-native-api.md#rgw-ingress-and-nfs-records), and
planning refuses them `lifecycle.unsupported` until B477 and B478's execution
lanes. `spec.ceph.mgrModules` admits `cephadm` and `rbd_support`; `nfs`
refuses as derived from a declared NFS service (D159 clause 9); every other
module refuses until its service owner. Retired daemon helpers, topology roles/devices and
drivegroup wrappers refuse with their replacement path. Machine bindings,
hostnames, labels, sites and locations remain under topology. Unsupported
service families, options with unqualified effects and protected leaves
without a release-qualified binding refuse explicitly. There is no arbitrary
provider-command escape hatch.

`StorageClient` is admitted with required `clusterRef`, `poolRef` and
`access: read-only|read-write`. The pool must be a same-cluster RBD pool; no
implicit administrator capability is granted. `StoragePlacementPolicy` and
`StoragePool` are admitted under
[pool protection and erasure coding](#pool-protection-and-erasure-coding), and
`StorageRBDImage` under [RBD images](#rbd-images).
Other P2 resource additions remain target contracts, not admitted kinds.
Future filesystem, object, NFS and export feature families are refused by
current pure admission.
Their historical field tables in [field detail](../deferred/storage-api.md)
are retained only for the later owning slices.

Operators select IBM Ceph release identifiers, package and tool versions,
container tags or digests and related service-version coordinates in input.
Bootwright preserves those selections through native package, container and
Ceph commands, including malformed or nonexistent versions and incompatible combinations,
and reports native failures with their cause and selected coordinate, subject
to secret redaction. It neither checks version existence, vendor support or
mutual compatibility (D157, D163) nor maintains IBM's release table, an approved-version
catalog or per-release profiles. A tested example imposes no version allowlist.
Structural input validation and independent trust, authorization and immutable
artifact requirements still apply; version syntax is interpreted by native
tools rather than a Bootwright semantic-version grammar. The adapter adapts its
mechanics to capabilities the selected artifacts provably have or lack
([version specializations](../architecture.md#implementations-and-version-variation));
that adaptation neither approves nor refuses a selection.

Ordinary apply realizes the core cluster, placement policies, replicated and
erasure-coded pools, RBD images and the client over `verified-tunnel`. `ceph.release` is the operator's release label;
`ceph.packageVersion` selects the Ceph/cephadm RPM roots and
`ceph.cephadm.ansible.packageVersion` selects the actual cephadm-ansible RPM.
Optional `ceph.nfsGanesha.packageVersion` selects the NFS-Ganesha RPM without
admitting NFS orchestration. These strings are nonempty, bounded to 1024 UTF-8
bytes and control-free; native tools interpret their contents.
`ceph.packageRepository` optionally selects an HTTPS RPM repository without
userinfo or fragment, of at most 1798 bytes without trailing slashes, so
every metadata path the host fetches stays within 2048 bytes. Omission uses
`https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64` as a
source default, not a release catalog. Independent publisher signatures remain
required for its selected RPMs.

`ceph.ibm.callHome` is `enabled` or `disabled`. `enabled` keeps the
provider's Call Home reporting active after bootstrap; `disabled` leaves it
inactive. The selected build decides how an intent is realized: on a build
whose Call Home is opt-in, `disabled` is the native default and requires the
`call_home_agent` module not enabled, and `enabled` refuses
`call-home-unqualified`; on a build whose Call Home is automatic, `disabled`
refuses `call-home-unqualified`. Either refusal comes before the cluster is
claimed but after the ceph-host blocks ran from the frozen input, so a corrected
`callHome` is an input change: `bootwright destroy` takes back what the apply
owns, then `context update` and a fresh apply carry it (D162).

`ceph.image.base` names the repository and `ceph.image.version` supplies the
authored tag or SHA-256 digest. The Entitlement registry namespace must contain
the selected repository; credentials are scoped to its authority. A direct
digest is resolved independently of any tag. Native acquisition and platform
inspection freeze the selected immutable artifact for continuation.
The [release qualification](../storage-coverage.md#release-qualification) owns
those proofs. Validation proves no live qualification; [M2b](../milestones/m2.md#m2b)
owns delivery and operator acceptance.

`ceph.cephadm.clusterSSH.transport` accepts `native` or `verified-tunnel`
(D135). Omission selects `native`; an empty, null or unknown value refuses.
The frozen core request records the effective transport for the cluster and
its children. A verified tunnel never qualifies the direct native channel, and
this field grants no exception to host-key verification. The three-node
example selects `verified-tunnel` explicitly; its protected mounts and staged
bootstrap remain subject to
[release qualification](../storage-coverage.md#release-qualification).
Core request v8 refuses continuation of v7 operations, image request v2 and
package request v2 refuse continuation of older operations, and core
evidence v3 and host request v2 refuse continuation of operations an
earlier build made; finish or remove old contexts with their original
applying build. A host preparation request carries only its cluster's
scope, never the core request's resource action, pool, client or image,
so a resource change never moves it.

The existing native-section config map admits explicit fixed-memory and
pool-default pairs for the minimal core configuration:

```yaml
ceph:
  config:
    global:
      osd_pool_default_size: "2"
      osd_pool_default_min_size: "2"
    osd:
      osd_memory_target_autotune: "false"
      osd_memory_target: "2147483648"
```

Each pair is optional and requires both of its string values together.
The global pair admits only explicit 2/2 defaults. It affects newly created
pools, including provider-created pools, and does not resize an existing pool
or replace its StoragePool declaration. It is rendered in the initial global
bootstrap section and verified against the native configuration database.
Omission preserves the provider's pool defaults; there is no silent cluster
size-based change.
The memory target is canonical decimal
bytes, at least 2 GiB and within the native unsigned 64-bit size domain.
Unknown sections, other options, automatic tuning and competing scopes refuse;
there is no arbitrary config pass-through. The pair is frozen with cluster
ownership, assimilated before OSD deployment and verified against the native
configuration database and each running OSD's reported effective values.
Omission or an empty map preserves provider defaults. This best-effort target
is neither a process memory cap nor qualification of a reduced host budget;
provider sizing and measured resource acceptance remain separate.

## Kinds

| Kind | Declares |
| --- | --- |
| `StorageCluster` | One managed Ceph cluster or one externally managed Ceph identity. `spec.type` is exactly `ceph`; `spec.management` is `managed` (default) or `external`; `spec.ceph` is required for `managed` and forbidden for `external`. |
| `StoragePlacementPolicy` | A reusable CRUSH placement policy and replicated defaults for one managed cluster. |
| `StorageClient` | One pool-scoped RBD CephX principal with required same-cluster `poolRef` and explicit access. |
| `StoragePool` | One pool. `spec.type` is `replicated` (default) or `erasure` and selects protection; the referenced cluster selects the storage engine. |
| `StorageRBDImage` | One RBD image: same-cluster replicated rbd `poolRef`, exact `sizeBytes`, optional rbd `dataPoolRef` and a closed feature set; image format 2. |
| `StorageFilesystem` | One CephFS filesystem: a same-cluster `cephfs-metadata` pool, one or more ordered `cephfs-data` pools, and its MDS policy. |
| `StorageObjectGateway` | One RGW service and its optional public endpoint; ingresses belong to that endpoint and select no other backend. |
| `StorageNFSExport` | One NFS-Ganesha service and zero or more exports; there is no separate `StorageNFSService` kind. |
| `StorageExport` | One downstream storage surface: the `dataFoundation` arm for a managed cluster, `externalDetails` for an external one. |

## References

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

## Replicated protection

The replicated-pool shape `{size, minSize}` is shared by placement policies and
pools. Both members are optional non-negative integers. Zero and omission mean
that the applicable cluster, policy, or native default selects the value; a
non-zero value is positive. Validation resolves those effective values first
and requires effective `minSize` not to exceed effective `size`, including when
only one member is authored.

An empty or all-zero `replicated` block is the unset value shape: it selects no
union variant, and only its non-zero members conflict with an erasure pool or
with a referenced placement policy, which owns those values for its pools.

The ordinary managed-cluster effective pair is `3/2`; bootstrap
`singleHostDefaults` selects `2/1`, and stretch selects `4/2`. A referenced
placement policy supplies its resolved pair to the pool. These values qualify
validation but remain native or policy defaults unless an owning normalization
rule explicitly materializes them.

For ordinary IBM host-domain replication, the declared OSD-bearing host count
must exceed the effective replica size. IBM's
[colocation requirements](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=recommendations-colocation)
specify three all-flash hosts for two replicas and at least four hosts for
three replicas. The three-node development profile therefore explicitly
selects `size: 2` and `minSize: 2`; omission or zero still selects `3/2` and
requires an additional OSD-bearing host. MON-only hosts do not add data
capacity. Stretch protection keeps its separate provider contract.

## Pool protection and erasure coding

A `StoragePlacementPolicy` is one owned replicated CRUSH rule named
`spec.ruleName`: host failure domain under the bootstrap root `default`,
optionally restricted to `spec.crushDeviceClass`. Its block is
`ceph-policy-<name>`, so `metadata.name` has at most 51 characters. The rule
name uses only letters, digits, `-`, `_` and `.`, starts with a letter or digit
and has at most 128 characters; `replicated_rule`, which the monitor creates
at bootstrap, is reserved; two policies of one cluster never share a rule; and
no rule takes the name of an erasure-coded pool of the cluster, which derives
its own rule. A device class is `hdd`, `ssd` or `nvme`, which OSDs set
automatically, or a `crush_device_class` that an OSD service of the cluster
declares at spec or path level. For a declared class, host-domain replicas
count only the hosts whose OSD services declare it. A failure domain other
than `host` and a policy naming the stretch rule (owned by
[B486](../milestones/m2.md#b486)) refuse at plan; site and rack domains are
parked by D160 (OD25).

A pool requires `spec.role` at plan; the role derives the application
(`rbd`, `cephfs` for both CephFS roles, `rgw`), and an authored
`spec.application` must equal it. A replicated pool uses its policy's rule, or
`replicated_rule` without one, and takes `replicated` values from the pool,
then the policy, then 3/2. A policy and an erasure-coded pool are exclusive:
an erasure-coded pool places itself through `spec.erasure.crushDeviceClass`.

An erasure-coded pool owns one erasure-code profile and one CRUSH rule, both
named after the pool, with host failure domain under `default`. For
distribution `ibm`, `dataChunks+codingChunks` is a row of the
[IBM Storage Ceph 9.9.1 erasure-code profiles](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=overview-erasure-code-profiles)
Table 1, and the declared OSD hosts (of the declared class, when one is
declared) meet its minimum: 2+2 needs 5, 4+2 needs 6, 5+2 needs 7, 6+2 needs
8, 8+3 needs 11 and 8+4 needs 12. 8+6 (MSR) refuses. The plugin is `isa` when
omitted, `jerasure` is admitted only for 4+2, 8+3 and 8+4, and the other
[plugins](https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=overview-erasure-code-plugins)
refuse; the technique is `reed_sol_van`. Other distributions keep the
`k+m` within declared OSD hosts rule. The stripe unit is a multiple of 4096
bytes (4096 when omitted), always passed, and `dataChunks` times it fits the
native 32-bit stripe width. `spec.erasure.parameters` and a non-default
`crushRoot` refuse at plan. An erasure-coded `rbd` or `cephfs-data` pool
derives irreversible overwrite enablement, as IBM's
[erasure-coding overwrites](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=overview-erasure-coding-overwrites)
require. A StorageClient on an erasure-coded pool refuses at plan; B489 owns
caps on erasure-coded data pools. A StorageClient on a pool whose
StorageRBDImage declares any `dataPoolRef` refuses at plan.

The initial `pg_num` is `clamp(32, pgNumMin, pgNumMax)` over the positive
bounds. `autoscale.mode` is `on` when omitted or `off`; `warn` refuses at plan
(D160, OD26). `targetSizeBytes` converts with 1024-based units,
`targetSizeRatio` and `compression.requiredRatio` freeze as decimal strings,
and quota and compression values freeze as native values, with 0 and empty
meaning unset. `pgNumMin` and `pgNumMax` are at most 32768 and the byte
values fit a signed 64-bit integer. `mirroring` refuses at plan naming
[B483](../milestones/m2.md#b483). A `cephfs-metadata` pool declares no PG
bounds: `ceph fs new` sets its `pg_num_min`.

Before any effect, a plan refuses a pool whose CRUSH rule would exceed the
monitor's per-OSD placement-group bound: the sum of `pgNum × width` over the
cluster's pools on the rule (width is the replica size, or `k+m`; a
`cephfs-metadata` pool counts at least 16), plus one `.mgr` placement group
times the effective default size (2 with the global 2/2 pair, else 3) on
`replicated_rule`, must not exceed 250 times the selected OSD count, at least
3, or 3 for a rule restricted to a device class.

## RBD images

`StorageRBDImage` is admitted for a managed cluster; its field record,
ownership and readiness are in
[StorageRBDImage](../storage-resources.md#storagerbdimage). Admission refuses
with `api.invariant`, before any effect:

- `features` naming `fast-diff` without `object-map`, `object-map` or
  `journaling` without `exclusive-lock`, or `object-map` without `fast-diff`;
- `dataPoolRef` equal to `poolRef`;
- `sizeBytes` above 1073741824000000 while the effective features, the
  defaults when `features` is omitted, hold `object-map`;
- `metadata.name` longer than 54 characters;
- a `poolRef` or `dataPoolRef` of another cluster or without role `rbd`;
- an erasure-coded `poolRef`.

A defaults fragment is checked only for the feature, data-pool and, when it
declares `features`, size rules. `sizeBytes` is an integer from 1 to
9223372036854775807; `features` is a set of `layering`, `exclusive-lock`,
`object-map`, `fast-diff`, `deep-flatten` and `journaling`, so librbd's
internal features refuse as values. Plan refuses an erasure-coded `poolRef`
again and propagates every plan refusal of the image pool and data pool. A
StorageClient on a pool whose StorageRBDImage declares any `dataPoolRef`
refuses at plan, naming the lexically first such image, until
[B489](../milestones/m2.md#b489) derives the data-pool caps.

## Complete-graph invariants

In addition to the field-local rules of the schema, desired-state validation
checks:

- management mode, distribution, entitlement, image, package, bootstrap,
  topology, network, FIPS, CephX, and cluster-login declarations agree;
- native placement resolves to owned Machines and service counts fit finite
  candidate sets, with consistent site and label bindings;
- OSD selectors are bounded and internally consistent and exclude the
  Machine's root device from explicit data, DB, and WAL paths;
- policies, pools, filesystems, gateways, NFS exports, and storage exports
  reference the same cluster under each kind's management-mode constraints and
  use compatible pool roles and protection modes;
- addresses, CIDRs, DNS labels, paths, ports, service IDs, VRRP IDs, config
  ownership, and Secret types do not conflict across the aggregate graph;
- ingress virtual IPs are `address/prefix` inside a declared public CIDR with
  its prefix length, neither its network nor its broadcast address, and
  equal, after parsing, to no Machine address and no other ingress virtual
  IP; and
- an NFS service's mode follows its ingress, and the HAProxy mode that hides
  client addresses refuses
  ([NFS modes](../storage-native-api.md#rgw-ingress-and-nfs-records)).

Stretch admission remains unavailable until its provider-qualified slice.

These checks qualify desired intent only. Live capacity, device safety,
external-cluster identity and native-output interpretation remain gates of
the owning lifecycle slices. They do not authorize version-combination
screening: native tools determine version availability and compatibility.
