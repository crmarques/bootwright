# P5: Ceph host and security variants

Profile [P5](../../../specs/ceph-coverage.md#profile-catalog): three RHEL
guests running an IBM Storage Ceph cluster (`ceph-lab`) with the host and
security variants. Each guest has two NICs in an active-backup bond with VLAN
140 carrying the cluster network, final NMState, RHSM registration, FIPS
without OSPP, a LUKS root unlocked by TPM2 with a recovery passphrase, and
CephX AES keys, over a size-2/min-size-2 RBD pool on one SSD per guest.

Synthetic qualification input for [M2c](../../../specs/milestones/m2.md#m2c).
No live execution or operator acceptance is claimed.

## Worth noting

- Topology: `rhel-01` to `rhel-03`, 4 vCPU each; RAM 13/13/10 GiB, a 60 GiB
  root and an 80 GiB SSD data disk. Total 12 vCPUs, 36 GiB RAM. Reserve 4 GiB
  controller memory. Public network `198.51.100.0/24`, cluster network
  `203.0.113.0/24`.
- Both NICs share one attachment and bridge, so the bond proves neither
  physical isolation nor independent links.
- The TPM2 policy is empty and selects no PCRs: it protects a copied disk
  without its TPM, not a tampered boot (see the
  [managed-OS contract](../../../specs/managed-os.md)). Recovery-passphrase
  use and post-install reboot access need live qualification.
- Both SSH keys (`bootwright-machine-key`, `ceph-cluster-key`) are ECDSA P-256.
  OSD autotuning is off with a 4 GiB target (`4294967296`), not a process cap.
- Required inputs: Secrets `rhsm-org` and `rhsm-key` (Red Hat activation),
  `luks-recovery`, and `ibm-registry`; RHEL 9.8 boot and DVD ISOs. The
  Entitlement `license.accept: true` is a placeholder; review IBM's terms.

Runbook: [e2e/runbooks/libvirt-3-nodes-ceph-host-variants.md](../../../e2e/runbooks/libvirt-3-nodes-ceph-host-variants.md).
