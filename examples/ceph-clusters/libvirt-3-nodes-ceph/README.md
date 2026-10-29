# Three-node Ceph on libvirt and sushy

This example declares three managed RHEL 9.8 guests and an IBM Storage Ceph
9.9.2.0 cluster (`ceph-lab`) on them: MON, MGR, OSD and MDS services, an
SSD placement policy (owned CRUSH rule `ssd-hosts`), a size-2/min-size-2 RBD
pool `rbd-data`, a 1 GiB RBD image `rbd-disk`, a CephFS filesystem `files` on
two size-2 pools with one MDS daemon, a 1 GiB subvolume `docs` and the
pool-scoped client `rbd-canary`. Addresses and names are synthetic; copy the
input to an operator-owned directory before adjusting it.

The minimal virtual journey has operator acceptance; full Ceph feature and
release qualification remain open in [M2](../../../specs/milestones/m2.md#m2b).

## Worth noting

- Topology: guests `rhel-01` to `rhel-03` (4 vCPU, 8 GiB, 60 GiB root, one
  80 GiB SSD data disk each) on one libvirt host, one bridge
  `198.51.100.0/24`. The three guests share one hypervisor failure domain.
  Total 12 vCPUs, 24 GiB guest RAM; reserve at least 4 GiB further host
  memory for the controller (the provider's `capacityReserve` is example
  headroom: choose your own).
- Sizes are below IBM's production memory recommendations: OSD autotuning is
  off, the OSD memory target is 2 GiB, global pool defaults are 2/2.
- The data disk is presented as nonrotational `/dev/sda`; root stays
  `/dev/vda`. The OSD ServiceSpec selects only `/dev/sda`.
- Release coordinates (`release`, `packageVersion`, cephadm-ansible package,
  image `base` and build tag `v9.9.2-26947`, `ibm.callHome`) are authored
  choices, not code pins or a claim of release acceptance; they move together
  when you pick another release.
- Required inputs: RHEL 9.8 boot and DVD ISOs from your Red Hat subscription,
  and the IBM entitlement key bound to Secret `ibm-registry`. The Entitlement
  `license.accept: true` is a placeholder; review IBM's terms before use.
- Replace the placeholder controller, DNS forwarder and NTP addresses.

Run it with the e2e [`lab-3-nodes-ceph`](../../../e2e/lab-3-nodes-ceph/runbook.md) case.
