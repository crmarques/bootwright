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
  the built-in `fdf` package with separate managed CephConsumers.
- Resolving those errors would not make it deployable: physical OpenShift,
  KubeVirt, security choices and add-on realization have further lifecycle limits.
- Placeholders: the Ceph entitlement sets `license.accept: true` for authoring
  only (accept the license yourself in a copy), and add-on catalog digests,
  channels and starting CSVs are synthetic. Descriptors hold no secret material.

## Expected validation result

The expected result is failure with explicit diagnostics for:

- External endpoint sources on the two baremetal OpenShiftClusters.
- Stretch topology and manager-module intent.
- Unqualified global Ceph options, including missing members of the currently
  qualified pool-default pair.
- A FIPS Ceph node without a managed FIPS installation profile.
- Agent-install bridge interfaces on the two physical OpenShiftClusters.
- Unrealized-declaration warnings for the KubeVirt providers and the authored
  add-on profile selections.

The exact set is pinned by `TestActualCLIRefusesFutureReferenceFeatures` in
[admission acceptance tests](../../../cmd/bootwright/admission_acceptance_test.go).
It is an authoring reference that admission refuses; no
[end-to-end case](../../../e2e/README.md) runs it.
