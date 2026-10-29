# Six-host pools profile

Profile [P1](../../../specs/ceph-coverage.md#profile-catalog): six RHEL
storage guests running an IBM Storage Ceph cluster (`ceph-lab`) with five MONs,
two MGRs and twelve OSDs. Two OSD drive groups (SSD data, and HDD data with DB
on SSD, the HDD group with TPM2 unlock), a device-class placement rule
`ssd-hosts`, a replicated RBD pool, two erasure-coded RBD data pools (4+2 and
2+2) with the replicated `rbd-headers` metadata pool, and three owned 1 GiB
RBD images (`replicated-disk`, `ec42-disk`, `ec22-disk`).

## Worth noting

- Topology: guests 1-2 (18 GiB), 3-5 (15 GiB), 6 (12 GiB), each 4 vCPU and a
  60 GiB root with 40 GiB SSD, 40 GiB HDD and 10 GiB SSD DB data disks.
  Total 24 vCPUs and 93 GiB RAM. Reserve 4 GiB controller memory; the
  provider's `capacityReserve` is example headroom.
- Two NICs per guest on one lab bridge: `nic0` is the public subnet and
  `nic1` the cluster subnet. Separate IP networks on a shared bridge give no
  physical isolation. Execution range stays three to six storage hosts.
- OSD memory target is 4 GiB (`4294967296`) with autotuning off; not a cap.
- `rbd-replicated` inherits protection 3/2 from `ssd-hosts` and carries quota,
  compression and PG bounds; the EC images use `rbd-headers` for metadata.
- Every provider profile declares a TPM; `cryptsetup` and `tpm2-tools` are
  fixture prerequisites, not live qualification. No physical HDD or SSD
  qualification, package, registry or live run is claimed, and the measured
  budget is unrun.
- Partial or nonempty DB/WAL state fails closed; [B662](../../../specs/milestones/m2.md#b662) owns that recovery.
- Required inputs: the IBM entitlement key for Secret `ibm-registry` and RHEL
  media. The Entitlement `license.accept: true` is a placeholder; review IBM's
  terms before use. Replace documentation addresses for your environment.
