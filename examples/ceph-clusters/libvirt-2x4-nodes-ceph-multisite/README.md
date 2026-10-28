# Two managed Ceph clusters with RBD replication

Profile [P7](../../../specs/ceph-coverage.md#profile-catalog): two four-host
IBM Storage Ceph clusters, `primary` and `secondary`, one per libvirt network,
with one-way journal RBD mirroring of the 1 GiB image `replicated-disk` from
`primary-volumes` to `secondary-volumes` (CephReplication
`volumes-to-secondary`, both pools in image mode), plus local CephFS and RGW
services on each cluster. The receiving pool starts empty.

It does not realize RGW multisite: the
[native credential-interface blocker](../../../specs/ceph.md#rgw-multisite-blocker)
remains open. CephFS snapshot mirroring is refused. Full P7/B484 qualification
and all operator acceptance are unrun.

## Worth noting

- Topology per cluster: h1/h2 MON, MGR, OSD and MDS (16 GiB); h3 MON, OSD and
  RGW (11 GiB); h4 OSD and rbd-mirror (8 GiB). Every Ceph guest has 4 vCPU, a
  60 GiB root and one 60 GiB SSD. Plus one client (2 vCPU, 4 GiB, 40 GiB
  root): 9 guests, 34 vCPUs, 106 GiB RAM, 520+480 GiB disk. Reserve 4 GiB
  controller memory. OSD memory is fixed at 4 GiB; no reduced-memory waiver.
- Networks: `primary` is `198.51.100.0/24`, `secondary` `192.0.2.0/24`, on
  separate bridges. They must be mutually routed through the provider host
  for MON and OSD traffic; libvirt NAT alone does not promise this. Verify
  routes before the clusters stage.
- The four-host example also needs the
  [B688](../../../specs/milestones/m2.md#b688) execution-budget work.
- The `client` is a budgeted test machine installed beside the eight
  topology Machines; remove it to use your own client.
- Bootwright does not promote, force-promote or fail back images; failover is
  an operator exercise with native tools. Retiring the relationship while a
  cluster survives refuses under
  [B491](../../../specs/milestones/m2.md#b491).
- Required inputs: RHEL 9.8 boot and DVD ISOs and the IBM entitlement key for
  Secret `ibm-registry`. The Entitlement `license.accept: true` is a
  placeholder; review IBM's terms before use. Release coordinates are the
  three-node example's authored choices.

Runbook: [e2e/runbooks/libvirt-2x4-nodes-ceph-multisite.md](../../../e2e/runbooks/libvirt-2x4-nodes-ceph-multisite.md).
