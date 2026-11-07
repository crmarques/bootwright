# Multi-datacenter platform

This synthetic graph is an authoring reference for a future platform: four
OpenShift clusters, one stretch Ceph cluster, physical and KubeVirt Machines,
shared networks and storage-dependent add-ons. It deliberately contains
unsupported features and fictional software coordinates, so it does not pass
current validation and cannot be deployed. All addresses, identities,
credential references, devices and topology are synthetic; consult the
[API owners](../../../specs/index.md) and
[milestone status](../../../specs/milestones.md) for current contracts.

## Worth noting

- Topology: clusters `hub-01` and `hub-02` (9 Machines each, KubeVirt-backed),
  `ocp-01` and `ocp-02` (3 physical Machines each), and a stretch CephCluster
  of 7 Machines whose witness holds MON placement without OSD devices. Start at
  [environment.yaml](environment.yaml); the four Data Foundation bindings select
  the built-in `fdf` package with separate managed CephConsumers. The two
  physical clusters also select the built-in `nmstate` and `kubevirt` packages
  and declare the hub virtual-cluster networks as typed
  `spec.networking.secondaryNetworks`. The hosted KubeVirt providers attach to
  those networks, the `nmstate` package provides the capability they require,
  and the `kubevirt` instance owns and labels the providers' namespace, so no
  authored add-on is needed.
- `infra/networks/` declares four provided [`Network`](../../../specs/api/networks.md) objects scoped to the
  sites that reach them: `machine` (VLAN 100, 192.0.2.0/24) and `ceph-public`
  (VLAN 200, 198.51.100.0/25) at `site-a` and `site-b`, `ceph-cluster` (VLAN
  300, 203.0.113.0/24), and `ceph-public-witness` (198.51.100.128/25) at
  `site-c`. The CephCluster's public and cluster CIDRs equal them.
- Resolving those errors would not make it deployable: physical OpenShift,
  KubeVirt, security choices and add-on realization have further lifecycle limits.
- Placeholders: the Ceph entitlement sets `license.accept: true` for authoring
  only (accept the license yourself in a copy), and add-on catalog digests,
  channels and starting CSVs are synthetic. Descriptors hold no secret material.

## Expected validation result

The expected result is failure with explicit diagnostics for:

- External endpoint sources on the two baremetal OpenShiftClusters.
- Manager-module intent.
- Unqualified global Ceph options, including missing members of the currently
  qualified pool-default pair.

The exact set is pinned by `TestActualCLIRefusesFutureReferenceFeatures` in
[admission acceptance tests](../../../cmd/bootwright/admission_acceptance_test.go).
It is an authoring reference that admission refuses; no
[end-to-end case](../../../e2e/README.md) runs it.
