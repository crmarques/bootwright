# 010 Ceph 3 Nodes Libvirt Boot ISO

This fixture is the boot-ISO variant of
`006-ceph-3nodes-libvirt-managed-os`. It provisions the same Ceph-only 3-node
lab on a laptop libvirt host with Bootwright-managed RHEL installation through
emulated Redfish virtual media, but installs from a minimal **boot ISO**
instead of the full DVD. A boot ISO carries only the installer, so the
`MachineImage` declares only `bootMedia` while the `MachineInstallProfile`
declares `installer.anaconda.packageSource.mirror` pointing Anaconda at a
network BaseOS install tree plus an AppStream repository. Bootwright renders
`url --url=` and `repo` Kickstart directives instead of `cdrom`. Everything else
— machines, provider, network, DNS, and the Ceph topology — is identical to 006.
Each YAML file contains one desired-state object with short names so the fixture
stays easy to inspect.

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
its `nameResolutionRefs` name-resolution component, and dnsmasq forwards public names so
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
- `infra/images/rhel-9-boot.yaml` and `infra/profiles/rhel-9-ceph.yaml`: managed
  OS install inputs (boot ISO plus the profile-owned Anaconda package mirror).
- `clusters/storage/ceph-libvirt/*.yaml`: Ceph cluster topology, placement
  policy, pools, filesystem, and export.
- `add-ons/`: reserved for future add-on definitions; this fixture does not
  need add-on objects today.

## Prerequisites

- A RHEL 9.7 x86_64 **boot** ISO stored locally on the bastion.
- A reachable HTTP(S) RHEL 9 package mirror exposing BaseOS and AppStream
  install trees, referenced by `infra/profiles/rhel-9-ceph.yaml`
  `spec.installer.anaconda.packageSource.mirror`. Unlike the DVD fixture, the
  boot ISO ships no packages, so Anaconda fetches them from this mirror during
  install. Replace the `mirror.example.test` URLs with a mirror the Ceph nodes
  can reach (the managed `lab-dns` forwarders and the libvirt NAT network must
  route to it). `bootwright apply` preflight probes these URLs and warns or
  fails early if the mirror is unreachable or the install tree is missing.
- Working upstream internet on the libvirt host. The Ceph nodes reach the
  community Ceph repository (`download.ceph.com`), the EPEL bootstrap RPM
  (`dl.fedoraproject.org`), the CentOS Stream community repositories that supply
  ceph-common's AppStream/CRB dependencies (`mirror.stream.centos.org`) and their
  signing key (`www.centos.org`), and the cephadm container image
  (`quay.io/ceph`) through the bastion: the managed `lab-dns` dnsmasq forwards
  public names to the resolvers in `infra/components/lab-dns.yaml`, and the
  libvirt NAT network carries egress. The OSS distribution adds no
  subscription-backed repo; Bootwright configures the community repo on each node
  with cephadm, scoped to `spec.ceph.release`. Because cephadm's `add-repo`
  enables EPEL and these RHEL nodes are unregistered, Bootwright also pre-installs
  `epel-release` from `dl.fedoraproject.org` and adds the CentOS Stream BaseOS,
  AppStream and CRB repositories (verified against the CentOS Official signing
  key) so ceph-common's dependencies — `librabbitmq`, `librdkafka` and
  `python3-prettytable` from AppStream, `libbabeltrace` from CRB — resolve. For a
  disconnected lab, set `spec.ceph.community.mirror` to an internal mirror of
  `download.ceph.com`, override `bootwright_ceph_community_epel_release_url` to an
  internal EPEL mirror, set `spec.ceph.community.dependencyMirror` to an internal
  CentOS Stream mirror, and point `forwarders` at an internal resolver.

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

Register the RHEL boot ISO in the root-managed media store before applying:

```bash
bootwright media add --name rhel-9.7-x86_64-boot.iso --from-file /path/to/rhel-boot.iso
```

The fixture references that ISO and its install-time package source with:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-9-x86-64-boot
spec:
  bootMedia: local-media:rhel-9.7-x86_64-boot.iso
---
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-9-ceph-node
spec:
  installer:
    anaconda:
      imageRef: rhel-9-x86-64-boot
      packageSource:
        mirror:
          baseURL: https://mirror.example.test/rhel/9/BaseOS/x86_64/os/
          repositories:
            - id: appstream
              baseURL: https://mirror.example.test/rhel/9/AppStream/x86_64/os/
```

## Run

```bash
bootwright context init --name 010-ceph-3nodes-libvirt-boot-iso \
  -f test/e2e/010-ceph-3nodes-libvirt-boot-iso --yes
bootwright secret generate
bootwright machine trust --machines bastion --yes
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
   a storage cluster now schedules its `nameResolutionRefs` name-resolution component.
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
