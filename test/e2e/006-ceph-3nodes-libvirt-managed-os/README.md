# 006 Ceph 3 Nodes Libvirt Managed OS

This fixture provisions a Ceph-only 3-node lab on a laptop libvirt host. It
uses Bootwright-managed RHEL installation through emulated Redfish virtual media
before running the existing Ceph storage flow.

## Prerequisites

- Linux host with KVM/libvirt available to the Bootwright bastion.
- `qemu-img`, libvirt, `sushy-tools`, `mkksiso` from `lorax`, Ansible
  requirements, and firewall permissions expected by the libvirt fixtures.
- A RHEL 9 x86_64 DVD ISO stored locally on the bastion.
- Valid Red Hat registry credentials for Ceph container images.
- An SSH key at `~/.ssh/bootwright-ssh-key` for the local provider host.

Register the RHEL ISO in the root-managed media store before applying:

```bash
bootwright media add rhel-9-x86_64-dvd.iso --from-file /path/to/rhel.iso
```

The fixture references that ISO with:

```yaml
spec:
  type: iso
  url: media:rhel-9-x86_64-dvd.iso
```

## Run

```bash
bootwright context init 006-ceph-3nodes-libvirt-managed-os \
  -f test/e2e/006-ceph-3nodes-libvirt-managed-os --yes
bootwright secret materialize
bootwright secret set ceph-registry-credentials --from-file /path/to/registry-credentials.txt
bootwright host trust --hosts lab-host --yes
bootwright apply bastion --yes
bootwright apply --stage infra --clusters ceph-libvirt --yes
bootwright apply --stage clusters --clusters ceph-libvirt --yes
```

The infra stage prepares the bastion/provider host, starts the emulated BMC,
creates the three VMs with root and data disks, installs RHEL through virtual
media, waits for SSH, and records managed SSH trust. The clusters stage runs the
existing Ceph prerequisites and cephadm flow against the three installed nodes.
