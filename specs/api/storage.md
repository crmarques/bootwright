# Ceph and storage services

This page owns storage admission. The closed schemas in
`api/v1alpha1/storage.go` and `api/v1alpha1/storage_native.go` own accepted
fields. [Native service input](../storage-native-api.md) owns the approved
service vocabulary; [resource contracts](../storage-resources.md) own the
separate declarative objects. Bootwright fields use camel case; complete
native service documents retain native snake case.

Current admission requires nonempty `spec.ceph.services`. Core MON, MGR and
OSD documents are admitted; retired daemon helpers, topology roles/devices and
drivegroup wrappers refuse with their replacement path. Machine bindings,
hostnames, labels, sites and locations remain under topology. Unsupported
service families, options with unqualified effects and protected leaves
without a release-qualified binding refuse explicitly. There is no arbitrary
provider-command escape hatch.

`StorageClient` is admitted with required `clusterRef`, `poolRef` and
`access: read-only|read-write`. The pool must be a same-cluster RBD pool; no
implicit administrator capability is granted. Other P2 resource additions
remain target contracts, not admitted kinds. Future placement, filesystem,
object, NFS and export feature families are refused by current pure admission.
Their historical field tables in [field detail](../deferred/storage-api.md)
are retained only for the later owning slices.

Ordinary apply realizes the core cluster, replicated RBD pool and client for
the one pinned release over `verified-tunnel`: IBM Storage Ceph 9.9.1.2,
image `cp.icr.io/cp/ibm-ceph/ceph-9-rhel9` at tag `v9.9.1-25074`, packages
`20.2.1-405.el9cp` and cephadm-ansible `5.0.3-1`. Anything else, `native`
included, refuses `lifecycle.unsupported` before registration, credential
reads or native effects, and the remedy names the pinned values; a newer
release needs a reviewed pin bump. An Entitlement whose `registry.url` is not
`cp.icr.io` refuses the same way, with the remedy to set it to `cp.icr.io`. `image.version` accepts the pinned tag or
the digest of its linux/amd64 child manifest, not the index digest that
`skopeo` or `podman` print for the tag. The [release qualification](../storage-coverage.md#release-qualification) owns
the resolution and freezing. Successful validation proves no live
qualification. [M2b](../milestones/m2.md#m2b) owns delivery state.

`ceph.cephadm.clusterSSH.transport` accepts `native` or `verified-tunnel`
(D135). Omission selects `native`; an empty, null or unknown value refuses.
The frozen core request records the effective transport for the cluster and
its children. A verified tunnel never qualifies the direct native channel, and
this field grants no exception to host-key verification. The three-node
example selects `verified-tunnel` explicitly; its protected mounts and staged
bootstrap remain subject to
[release qualification](../storage-coverage.md#release-qualification).
Core request v4 refuses continuation of v3 operations; finish or remove old
contexts with their original applying build.

The existing native-section config map admits explicit fixed-memory and
pool-default pairs for the IBM 9.9.1.2 candidate:

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
  use compatible pool roles and protection modes; and
- addresses, CIDRs, DNS labels, paths, ports, service IDs, VRRP IDs, config
  ownership, and Secret types do not conflict across the aggregate graph.

Stretch admission remains unavailable until its provider-qualified slice.

These checks qualify desired intent only. Live capacity, device safety,
release support, external-cluster identity, and native-schema compatibility
remain gates of explicitly supported renderer or lifecycle slices.
