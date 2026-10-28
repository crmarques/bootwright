# Three-node Ceph block and file on libvirt and sushy

This example declares three managed RHEL 9.8 guests and an IBM Storage Ceph
cluster with the block and file resources that fit three OSD hosts: an SSD
placement policy (`ssd-hosts`), an RBD pool `rbd-data` placed by it, a 1 GiB
RBD image `rbd-disk` with an explicit feature set, size-2/min-size-2 CephFS
metadata and data pools, an `mds.files` service of two daemons, a filesystem
`files` (one active rank, one standby, subvolume group `apps`), a 1 GiB
subvolume `docs` and the pool client `rbd-canary`.

It is a functional lab example outside the
[profile catalog](../../../specs/ceph-coverage.md#profile-catalog) and claims no
[B484](../../../specs/milestones/m2.md#b484) row. Erasure-coded pools need more
OSD hosts than this lab has and belong to the larger catalog profiles.

## Worth noting

- Topology: `rhel-01` to `rhel-03`, each 4 vCPU, 10 GiB RAM, 60 GiB root and
  one 80 GiB SSD data disk. Total 12 vCPUs, 30 GiB guest RAM; reserve at
  least 4 GiB further host memory. Each guest has 2 GiB more than the
  [three-node example](../libvirt-3-nodes-ceph/) because two also run an MDS.
- The guests share one hypervisor failure domain; OSD autotuning is off and
  the OSD memory target is 2 GiB.
- `standbyReplay: false`; a copy with `mds.standbyReplay: true` is the
  standby-replay variant.
- Release coordinates and the IBM entitlement are the same authored choices as
  the three-node example. The Entitlement `license.accept: true` is a
  placeholder; review IBM's terms before use.
- Required inputs: RHEL 9.8 boot and DVD ISOs from your Red Hat subscription
  and the IBM entitlement key for Secret `ibm-registry`.
- Scoped CephFS client capabilities are owned by
  [B489](../../../specs/milestones/m2.md#b489); removal while hosts survive by
  [B491](../../../specs/milestones/m2.md#b491).

Runbook: [e2e/runbooks/libvirt-3-nodes-ceph-block-file.md](../../../e2e/runbooks/libvirt-3-nodes-ceph-block-file.md).
The templated e2e [`ceph` case](../../../e2e/README.md#case-ceph) covers the same
scenario with its own inputs.
