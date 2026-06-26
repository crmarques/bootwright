# 009 Ceph 3 Nodes Bare-Metal Managed OS

This fixture provisions a Ceph-only 3-node cluster on **physical bare-metal**
servers driven over real Redfish BMCs. It is the bare-metal counterpart to
[006](../006-ceph-3nodes-libvirt-managed-os) (libvirt) and
[008](../008-ceph-3nodes-vsphere-managed-os) (vSphere): same managed-OS Ceph
flow, different substrate. Each YAML file contains one desired-state object.

The defining difference from 006/008 is that bare-metal machines have **no
provider host**. A libvirt node instantiates on its libvirt host and a vSphere
node through vCenter; a bare-metal node has neither. Its managed-OS install —
RHEL through Redfish virtual media + Anaconda — is driven entirely from the
controller over the BMC. The node must still land in its cluster's managed-OS
inventory group (`bootwright_machine_task_hosts_storage_ceph-baremetal`) with a
controller-local connection, or the machines-phase install task is planned but
skipped at runtime with "no hosts to target". This fixture guards that path
(see `TestManagedOSInstallVarsFromCephBaremetalFixture`).

## Layout

- `environment.yaml`: base domain, the managed `lab-dns` name-resolution
  reference, and secret names (`bmc-credentials`, `ceph-node-ssh`,
  `bastion-host-ssh`).
- `infra/providers/baremetal.yaml`: bare-metal provider with external boot and
  the `ceph-net` VLAN attachment. No machine profiles — bare-metal servers are
  reached over their BMC, not instantiated.
- `infra/machines/ceph-0.yaml`, `ceph-1.yaml`, `ceph-2.yaml`: the three physical
  Ceph nodes — bonded NICs, a Redfish virtual-media BMC, a root device hint, and
  `os.provided: false` + `installProfileRef` so Bootwright installs RHEL.
- `infra/machines/bastion.yaml`: provided host carrying the managed `lab-dns`.
- `infra/networkconfigs/ceph-net.yaml`: active-backup bond + VLAN 140 nmstate
  template, resolver, and default route.
- `infra/images/rhel-9-dvd.yaml` and `infra/profiles/rhel-9-ceph.yaml`: managed
  OS install inputs.
- `infra/components/lab-dns.yaml`: managed dnsmasq name-resolution on the bastion.
- `clusters/storage/ceph-baremetal/cluster.yaml`: Ceph cluster topology — MON,
  MGR, OSD, MDS, RGW, and ingress roles on all three nodes, two OSD data devices
  each, community (OSS) Ceph pinned to `squid`.

## Prerequisites

- Three physical x86_64 servers with reachable Redfish BMCs (the `address`es in
  `infra/machines/*.yaml`) and a RHEL 9.7 DVD ISO registered in the media store.
- Working upstream internet through the bastion so the OSS Ceph flow can reach
  `download.ceph.com`, the EPEL bootstrap RPM, and `quay.io/ceph`; the managed
  `lab-dns` dnsmasq forwards public names to the resolvers in
  `infra/components/lab-dns.yaml`.

Create the bastion SSH key and register the ISO:

```bash
mkdir -p ~/.ssh
test -f ~/.ssh/bootwright-ssh-key || \
  ssh-keygen -t ed25519 -N '' -C bootwright-lab -f ~/.ssh/bootwright-ssh-key
bootwright media add rhel-9.7-x86_64-dvd.iso --from-file /path/to/rhel.iso
```

## Run

```bash
bootwright context init --name 009-ceph-3nodes-baremetal-managed-os \
  -f test/e2e/009-ceph-3nodes-baremetal-managed-os --yes
bootwright secret generate
bootwright host trust --hosts bastion --yes
bootwright bastion setup --yes
bootwright apply --stage infra --clusters ceph-baremetal --yes
bootwright apply --stage clusters --clusters ceph-baremetal --yes
```

The infra stage installs RHEL on the three nodes through Redfish virtual media,
waits for SSH, and records managed SSH trust. The clusters stage runs the Ceph
prerequisites and cephadm flow against the installed nodes.
