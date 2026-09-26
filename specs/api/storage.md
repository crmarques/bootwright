# Ceph and storage services

This page owns admission of `StorageCluster`, `StoragePlacementPolicy`,
`StoragePool`, `StorageFilesystem`, `StorageObjectGateway`, `StorageNFSExport`
and `StorageExport`. The closed schema in `api/v1alpha1/storage.go` is
authoritative for field names, types, defaults and shapes. The
[storage field detail](../deferred/storage-api.md) records its tables, the
field-local rules `internal/storage` admission enforces, and the renderer
defaults M2b revives; changing an enforced rule is an API change under
[version authority](../api.md#version-authority). Fields use Bootwright camel case; only the
explicitly declared `config`, `spec` and `parameters` maps accept open payloads.
[The compiler boundary](../api.md#compiler-boundary) and
[native-field rules](../api.md#native-and-implementation-shaped-fields) apply.

No lifecycle capability realizes a storage kind, so a selection containing one
refuses before registration under the
[unrealizable-kind rule](../state-reconciliation.md#stages-and-the-pause-boundary).
Accepting a declaration claims no renderer or lifecycle support.

## Kinds

| Kind | Declares |
| --- | --- |
| `StorageCluster` | One managed Ceph cluster or one externally managed Ceph identity. `spec.type` is exactly `ceph`; `spec.management` is `managed` (default) or `external`; `spec.ceph` is required for `managed` and forbidden for `external`. |
| `StoragePlacementPolicy` | A reusable CRUSH placement policy and replicated defaults for one managed cluster. |
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

## Complete-graph invariants

In addition to the field-local rules of the schema, desired-state validation
checks:

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

A stretch block whose tiebreaker is omitted or empty is valid desired state
that does not enable native stretch mode; `validate` reports it with an
`api.deferred` warning, and `render effective` accepts it without one.

These checks qualify desired intent only. Live capacity, device safety,
release support, external-cluster identity, and native-schema compatibility
remain gates of explicitly supported renderer or lifecycle slices.
