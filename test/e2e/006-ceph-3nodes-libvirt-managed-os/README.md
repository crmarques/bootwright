# 006 Ceph 3 Nodes Libvirt Managed OS

This fixture provisions a Ceph-only 3-node lab on a laptop libvirt host. It
uses Bootwright-managed RHEL installation through emulated Redfish virtual media
before running the community (OSS) Ceph storage flow. Each YAML file contains one
desired-state object with short names so the fixture stays easy to inspect.

The current fixture keeps only state supported by the current code path:

- one provided libvirt host named `bastion`
- three lean managed Ceph VMs named `ceph-0`, `ceph-1`, and `ceph-2`
- a tiny `ceph-node` libvirt profile: 1 vCPU, 2048 MiB RAM, a 16 GiB root
  disk, and two 1 GiB OSD data disks per VM
- a managed `lab-dns` dnsmasq name-resolution service on the bastion, bound to
  `192.168.134.1`, which authoritatively serves lab records and forwards every
  other query (for example `download.ceph.com` and `quay.io`) to the public
  resolvers in its `forwarders` list
- Ceph MON, MGR, OSD, MDS, RGW, and ingress roles on all three nodes
- RBD, CephFS metadata, CephFS data, and RGW pools
- the community (OSS) Ceph distribution, so Bootwright configures the upstream
  community Ceph repository (release pinned to `squid` in the StorageCluster) and
  cephadm pulls the matching upstream Ceph image; no Red Hat entitlement or
  registry pull secret is required

The requested managed infra-services VM, artifact service, and storage-owned
RGW/dashboard ingress endpoints are listed below as implementation work because
the current workflow cannot model them without Go or Ansible changes. The
managed DNS service itself now runs on the bastion: a storage cluster schedules
its `dnsRefs` name-resolution component, and dnsmasq forwards public names so
the connected OSS Ceph flow can reach `download.ceph.com` and `quay.io`.

## Layout

- `environment.yaml`: base domain, storage cluster selection, the managed
  `lab-dns` name-resolution reference, and secret names.
- `infra/machines/*.yaml`: provider host and Ceph VM machine definitions.
- `infra/components/lab-dns.yaml`: the managed dnsmasq name-resolution
  `InfraComponent` on the bastion, with its public DNS `forwarders`.
- `infra/providers/libvirt.yaml`: libvirt provider, BMC emulation defaults, and
  lean VM profile.
- `infra/networkconfigs/ceph-bridge.yaml`: lab network, resolver, and route
  template.
- `infra/images/rhel-9-dvd.yaml` and `infra/profiles/rhel-9-ceph.yaml`: managed
  OS install inputs.
- `clusters/storage/ceph-libvirt/*.yaml`: Ceph cluster topology, placement
  policy, pools, filesystem, and export.
- `add-ons/`: reserved for future add-on definitions; this fixture does not
  need add-on objects today.

## Prerequisites

- A RHEL 9.7 x86_64 DVD ISO stored locally on the bastion.
- Working upstream internet on the libvirt host. The Ceph nodes reach the
  community Ceph repository (`download.ceph.com`), the EPEL bootstrap RPM
  (`dl.fedoraproject.org`), and the cephadm container image (`quay.io/ceph`)
  through the bastion: the managed `lab-dns` dnsmasq forwards public names to the
  resolvers in `infra/components/lab-dns.yaml`, and the libvirt NAT network
  carries egress. The OSS distribution adds no subscription-backed repo;
  Bootwright configures the community repo on each node with cephadm using
  `spec.ceph.community.release`. Because cephadm's `add-repo` enables EPEL and
  these RHEL nodes are unregistered, Bootwright also pre-installs `epel-release`
  from `dl.fedoraproject.org`. For a disconnected lab, set
  `spec.ceph.community.mirror` to an internal mirror of `download.ceph.com`,
  override `bootwright_ceph_community_epel_release_url` to an internal EPEL
  mirror, and point `forwarders` at an internal resolver.

Bootwright owns bastion host preparation for this fixture. After
`bootwright bastion setup --yes` and
`bootwright apply --stage infra --clusters ceph-libvirt --yes`, the required
libvirt tooling, `qemu-img`, `sushy-tools`, `mkksiso`, Ansible requirements,
and firewall rules should be installed or configured by Bootwright. Missing
host preparation is a Bootwright bug, not a manual prerequisite for this lab.

Create the SSH key used for the local provider host:

```bash
mkdir -p ~/.ssh
test -f ~/.ssh/bootwright-ssh-key || \
  ssh-keygen -t ed25519 -N '' -C bootwright-lab -f ~/.ssh/bootwright-ssh-key
chmod 600 ~/.ssh/bootwright-ssh-key
```

Register the RHEL ISO in the root-managed media store before applying:

```bash
bootwright media add rhel-9.7-x86_64-dvd.iso --from-file /path/to/rhel.iso
```

The fixture references that ISO with:

```yaml
spec:
  type: iso
  mediaType: dvd
  url: local-media:rhel-9.7-x86_64-dvd.iso
```

## Run

```bash
bootwright context init 006-ceph-3nodes-libvirt-managed-os \
  -f test/e2e/006-ceph-3nodes-libvirt-managed-os --yes
bootwright secret materialize
bootwright host trust --hosts bastion --yes
bootwright bastion setup --yes
bootwright apply --stage infra --clusters ceph-libvirt --yes
bootwright apply --stage clusters --clusters ceph-libvirt --yes
```

The infra stage prepares the bastion/provider host, starts the emulated BMC,
creates the three VMs with a root disk and two small OSD data disks, installs
RHEL through virtual media, waits for SSH, and records managed SSH trust. The
clusters stage runs the existing Ceph prerequisites and cephadm flow against the
three installed nodes.

## Laptop DNS

The fixture configures Ceph machines to use the Bootwright-managed `lab-dns`
dnsmasq on the bastion (`192.168.134.1`). Point the laptop resolver at the same
DNS endpoint before accessing Ceph names such as future dashboard and S3 ingress
records.

For a temporary `systemd-resolved` setup on the libvirt bridge:

```bash
sudo resolvectl dns vbr-cb-ceph 192.168.134.1
sudo resolvectl domain vbr-cb-ceph '~bootwright.test' '~ceph.bootwright.test'
resolvectl query dashboard.ceph.bootwright.test
resolvectl query s3.ceph.bootwright.test
```

For a persistent split-DNS setup:

```bash
sudo mkdir -p /etc/systemd/resolved.conf.d
printf '%s\n' \
  '[Resolve]' \
  'DNS=192.168.134.1' \
  'Domains=~bootwright.test ~ceph.bootwright.test' \
  | sudo tee /etc/systemd/resolved.conf.d/bootwright-ceph.conf
sudo systemctl restart systemd-resolved
```

Use the bridge name and DNS IP from `infra/providers/libvirt.yaml` and
`environment.yaml` if the lab network changes.

## Implementation Plan

The remaining requested desired state needs implementation support:

1. Host managed infra services (DNS, artifact) on managed-OS service VMs rather
   than only on the provided bastion. Bastion-hosted managed DNS already works:
   a storage cluster now schedules its `dnsRefs` name-resolution component.
2. Add a bootstrap path for artifact publication when the artifact service is
   itself hosted on a managed VM.
3. Add storage-owned endpoint declarations for Ceph dashboard and RGW ingress
   so storage-only fixtures do not depend on `ContainerCluster` endpoints.
4. Render cephadm ingress service specs from `StorageObjectGateway` state so
   cephadm installs and configures keepalived and HAProxy itself.
5. Extend managed DNS rendering with storage endpoint records for dashboard and
   S3 VIP names. Today the storage-cluster dnsmasq emits only the public
   `forwarders`; it serves no local `host-record`/`address=` entries yet.
6. Add validation and render tests for the service-VM bootstrap, storage
   endpoints, cephadm ingress, and DNS records, then enable those objects in
   this fixture.
