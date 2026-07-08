---
title: Provisioning a Ceph cluster
description: Build a 3-node IBM Storage Ceph lab on libvirt with bootwright — two full nodes plus a monitor-only tie-breaker — with RHEL installed for you as managed OS, all the way to Ceph health.
---

# Provisioning a Ceph cluster

This guide builds a small, self-contained IBM Storage Ceph lab end to end:
**three libvirt VMs on one host** — two full storage nodes plus one monitor-only
tie-breaker — with **RHEL installed by bootwright** (managed OS, via the Anaconda
installer) and a managed Ceph cluster bootstrapped with `cephadm`, all the way to
`HEALTH_OK`. All three storage types are configured: block (RBD), file (CephFS),
and object (RGW behind an ingress VIP). No real server hardware is required.

It assumes you have completed [Installation and Setup](installation.md) — the CLI
is installed and you understand the context, secret, host-trust, and bastion-prep
mechanics this guide reuses.

| Node | Profile | Ceph roles | OSDs | Purpose |
| --- | --- | --- | --- | --- |
| `ceph-1` | `ceph-full` | mon, mgr, osd, mds, rgw, ingress | 3 | full node (block + file + object) |
| `ceph-2` | `ceph-full` | mon, mgr, osd, mds, rgw, ingress | 3 | full node (block + file + object) |
| `ceph-3` | `ceph-mon` | mon | 0 | monitor-only tie-breaker (quorum) |

!!! note "Tie-breaker, not Ceph stretch mode"
    This lab builds three monitors — one on a dedicated, OSD-less node — so the
    cluster keeps quorum if a node is lost. That is *not* Ceph stretch mode, which
    needs two data sites with two monitors each plus a separate tie-breaker site
    (a minimum of five nodes). With only two OSD hosts, pools replicate with
    `size: 2, minSize: 2` across hosts; losing one OSD host pauses I/O while
    quorum holds. For true site-level data HA, see
    [Ceph topologies](../advanced/ceph-topologies.md).

This lab installs the node operating system itself — **managed OS** — unlike the
single-node OpenShift lab, where the cluster nodes run the agent installer. For
how managed-OS installs work in general, see
[Managed-OS installs](../advanced/managed-os.md).

## Get The Input Tree

There is no `example init` provider for Ceph, so start from the ready-made
example tree in the bootwright source repository. If you installed only the
release binary, get the source first — clone the repo (or download and unpack a
source tarball from
[GitHub Releases](https://github.com/crmarques/bootwright/releases)):

```bash
git clone https://github.com/crmarques/bootwright.git
```

Then copy the example into a working directory and enter it:

```bash
cp -r bootwright/examples/ceph-ibm-libvirt-lab ./my-ceph-lab
cd ./my-ceph-lab
```

The example is a complete, valid bootwright tree. Its layout:

```text
my-ceph-lab/
  environment.yaml                                   Environment: cluster selection, lab DNS
  secrets.yaml                                       Secrets: node/bastion SSH, BMC, RHSM, IBM registry
  infra/
    providers/libvirt.yaml                           InfraProvider: libvirt + VM profiles + bridge
    machines/bastion.yaml                            Machine: the libvirt host (localhost)
    networkconfigs/ceph-net.yaml                     NetworkConfig: 192.168.140.0/24, static IPs
    components/lab-dns.yaml                           InfraComponent: dnsmasq resolver + forwarders
    entitlements/{rhel,ibm-storage-ceph}.yaml         Entitlements: RHEL subscription + IBM Storage Ceph
    os/rhel-9-x86-64-dvd.yaml                        MachineImage: RHEL 9.7 DVD (local-media)
    os/rhel-9-ceph-node.yaml                         MachineInstallProfile: Anaconda RHEL install
  clusters/storage/ceph-ibm/
    cluster.yaml                                     StorageCluster: distribution ibm, release 9
    nodes/{ceph-1,ceph-2,ceph-3}.yaml                Machines: ceph-1, ceph-2 (full), ceph-3 (mon)
    placement-policy.yaml                            StoragePlacementPolicy: size 2 / minSize 2
    pools/{rbd,cephfs-data,cephfs-metadata,rgw}.yaml StoragePools
    filesystems/cephfs.yaml                          StorageFilesystem (CephFS)
    object-gateways/rgw.yaml                         StorageObjectGateway (RGW + ingress VIP)
```

## Understand The Input Objects

Read them in dependency order; each owns exactly one slice of the truth.

### Environment (`environment.yaml`)

The catalog that ties the tree together. It sets `spec.baseDomain`
(`bootwright.test`), activates the cluster under `spec.storageClusters`, and
selects the managed lab DNS
under `spec.infraComponents.nameResolution`. It also sets
`spec.safety.destroyProtection: requiredOverride`, so `destroy` refuses to run
without `--override`.

### Entitlements (`infra/entitlements/`)

The two `Entitlement` objects that license the install are their own first-class
files (referenced by name from the `StorageCluster`), decoupled by concern — IBM
Storage Ceph runs on RHEL it does not itself entitle:

- `rhel` (`type: redhat-rhel`) registers each node with
  `subscription-manager` so the RHEL BaseOS/AppStream repos cephadm needs are
  available. Its `rhsm.organizationRef` and `rhsm.activationKeyRef` resolve to the
  `rhel-org` and `rhel-activation-key` secrets.
- `ibm-storage-ceph` (`type: ibm-storage-ceph`) logs each node
  into the IBM registry `cp.icr.io/cp` for the container images, accepts the
  product license (`license.accept: true`), and names the RHEL entitlement via
  `rhelEntitlementRef: rhel`. Its `registry.credentialsRef` resolves to the
  `ibm-ceph-registry` secret (username `cp`, the IBM entitlement key as the
  password).

### Secret (`secrets.yaml`)

Six `kind: Secret` objects, one per named secret, each with a `spec.type` and an
optional `spec.source`. The bytes never go in YAML — `spec.source` says where
they come from: `bastion-host-ssh` (`sshKeyPair`) is a `file` reference to an
operator-owned key; `ceph-node-ssh` (`sshKeyPair`) and `bmc-credentials`
(`usernamePassword`) are `generated`; and `rhel-org`, `rhel-activation-key`
(`opaque`), and `ibm-ceph-registry` (`usernamePassword`) are context-local,
set with `bootwright secret set`.

### InfraProvider (`infra/providers/libvirt.yaml`)

The libvirt substrate facts (`type: libvirt`). It drives libvirt on this host via
the `bastion` Machine (`machineRef: bastion`, `uri: qemu:///system`), enables an
emulated Redfish BMC per VM (`bmcEmulationDefaults`, credentials from
`bmc-credentials`), defines the VM sizing `machineProfiles`, and names the libvirt
bridge under `networkAttachments`:

- `ceph-full`: 2 vCPU, 4096 MiB RAM, a 20 GiB root, and three 8 GiB OSD data disks
  (`/dev/vdb`–`/dev/vdd`).
- `ceph-mon`: 1 vCPU, 2048 MiB RAM, a 16 GiB root, no OSD disks.

### Machine (`infra/machines/bastion.yaml`, `clusters/storage/ceph-ibm/nodes/*.yaml`)

Four machines: the shared bastion under `infra/machines/`, and the three Ceph
nodes alongside their cluster under `clusters/storage/ceph-ibm/nodes/`. Machines
own substrate binding, OS mode, durable addresses, and SSH access — never install
intent.

- `bastion`: the local workstation/libvirt host. `os.provided: true`,
  `capabilities` (`container-runtime`, `libvirt`, `name-resolution`), addresses
  `ssh: localhost` and `cluster-lan: 192.168.140.1`, SSH key `bastion-host-ssh`.
- `ceph-1`, `ceph-2`, `ceph-3`: the Ceph nodes (`capabilities: [ceph-node]`).
  Each binds the libvirt provider (`ceph-1`/`ceph-2` use profile `ceph-full`,
  `ceph-3` uses `ceph-mon`), sets `os.provided: false` with
  `installProfileRef: rhel-9-ceph-node` (bootwright installs RHEL onto it), gets a
  static IP (`.21`, `.22`, `.23`), and connects over SSH as `root` with the shared
  `ceph-node-ssh` key — one SSH identity per `StorageCluster`.

### MachineImage and MachineInstallProfile (`infra/os/`)

These drive the managed RHEL install — the part that distinguishes this lab from
the agent-installed OpenShift SNO lab.

- `MachineImage` `rhel-9-x86-64-dvd`: a full DVD (no `packageSource`) whose
  `bootMedia: local-media:rhel-9.7-x86_64-dvd.iso` resolves to the managed media
  store (you stage the ISO with `bootwright media add` below).
- `MachineInstallProfile` `rhel-9-ceph-node`: the Anaconda install
  (`installer.anaconda.imageRef: rhel-9-x86-64-dvd`) for
  `os.family: rhel`, `version: "9.7"`. Its `customizations` authorize the
  generated `ceph-node-ssh` key for root login, wipe and lay down the root device,
  install a minimal base plus the runtime prerequisites (`podman`, `lvm2`,
  `chrony`, `firewalld`), and — because the cluster requests FIPS — set
  `security.fips.enabled: true` so each node is installed with `fips=1`.

### NetworkConfig (`infra/networkconfigs/ceph-net.yaml`)

The `192.168.140.0/24` machine network. It owns the CIDR, points
`nameResolutionRefs` at the `lab-dns` component, and carries an NMState `template`
that gives `primary` a static IPv4, resolves DNS via `192.168.140.1`, and routes
the default gateway through the bastion/bridge (which NATs the nodes out to RHSM,
`cp.icr.io`, and IBM's repo host).

### InfraComponent (`infra/components/lab-dns.yaml`)

A `nameResolution` (`dnsmasq`) component pinned to `bastion`, bound to
`192.168.140.1:53`. It authoritatively serves the lab records and forwards
everything else (`cp.icr.io`, RHSM, IBM's repo host, NTP pools) to public
resolvers. It also publishes the RGW S3 endpoint
(`additionalIngressHosts: [rgw.ceph.bootwright.test]`).

### StorageCluster and its surfaces (`clusters/storage/ceph-ibm/`)

The Ceph install intent. `StorageCluster` `ceph-ibm` is `type: ceph`,
`management: managed` (bootwright installs and owns it via cephadm). Its `spec.ceph`
block sets `distribution: ibm`, `release: "9"`, `entitlementRef: ibm-storage-ceph`,
FIPS (`security.fips.enabled: true`), the cephadm SSH address and bootstrap host
(`ceph-1`), the public/cluster networks, the HA dashboard
(`management` → mgmt-gateway with a `keepalive_only` ingress VIP on `.81`), and
the `topology.hosts` that assign roles and OSD devices per node.

The surrounding objects fill in the pools and services:

- `StoragePlacementPolicy` `replicated-host`: `failureDomain: host`,
  `replicated: {size: 2, minSize: 2}`, referenced by every pool.
- `StoragePool` `rbd`, `cephfs-data`, `cephfs-metadata`, `rgw`: one pool each, by
  `ceph.role`.
- `StorageFilesystem` `cephfs`: the CephFS filesystem over the two CephFS pools,
  with one active MDS and a standby across `ceph-1`/`ceph-2`.
- `StorageObjectGateway` `rgw`: the RGW service on both full nodes, its public S3
  endpoint (`rgw.ceph.bootwright.test`), and a cephadm ingress floating a single
  VIP (`192.168.140.80`).

See [Storage](../concepts/storage.md) for the full storage model.

## Edit The Required Values

The example forms a working `192.168.140.0/24` lab; change values only where your
environment differs.

| File | Field | Set it to |
| --- | --- | --- |
| `environment.yaml` | `spec.baseDomain` | A DNS base domain for the lab (default `bootwright.test`). |
| `infra/providers/libvirt.yaml` | `spec.libvirt.uri` | The libvirt URI, usually `qemu:///system`. |
| `infra/providers/libvirt.yaml` | `spec.networkAttachments[].libvirt.bridge` | The libvirt bridge on the machine network (default `vbr-ceph-ibm`). |
| `infra/networkconfigs/ceph-net.yaml` | `spec.machineNetwork[].cidr` | The machine network CIDR (default `192.168.140.0/24`). |
| `clusters/storage/ceph-ibm/nodes/*.yaml` | each `Machine` `spec.addresses` (`ssh`) | The node IPs (defaults `.21`, `.22`, `.23`). |
| `infra/os/rhel-9-x86-64-dvd.yaml` | `spec.bootMedia` | The staged RHEL DVD (`local-media:<your-iso-name>`). |
| `infra/os/rhel-9-ceph-node.yaml` | `spec.os.version` | The RHEL release on the DVD (default `9.7`). |
| `clusters/storage/ceph-ibm/cluster.yaml` | `spec.ceph.release` | The IBM Storage Ceph product stream (default `"9"`). |
| `clusters/storage/ceph-ibm/cluster.yaml` | `spec.ceph.networks` and `management.ingress.address` | The dashboard VIP and the public/cluster CIDRs, if you changed the network. |
| `clusters/storage/ceph-ibm/object-gateways/rgw.yaml` | `spec.public.dnsName` and `spec.ceph.ingresses[].address` | The RGW endpoint and ingress VIP, if you changed the network. |

If you change the `192.168.140.*` network, update every place it appears:
`infra/networkconfigs/ceph-net.yaml`, `infra/machines/bastion.yaml`,
`clusters/storage/ceph-ibm/nodes/*.yaml`, `infra/components/lab-dns.yaml`, and
`clusters/storage/ceph-ibm/{cluster.yaml,object-gateways/rgw.yaml}`.

!!! note "Drop FIPS to simplify"
    Two blocks request FIPS: `spec.ceph.security.fips.enabled` in
    `cluster.yaml` and `customizations.security.fips.enabled` in
    `infra/os/rhel-9-ceph-node.yaml`. Remove both to build the cluster without
    FIPS — bootwright requires every Ceph node's install profile to agree.

## Prerequisites Unique To This Lab

This lab needs three pieces of credential material plus a staged RHEL ISO. None
goes in YAML — the secrets are stored, encrypted, in the bootwright context with
`bootwright secret set`, using the secret **names** the example's `Secret`
objects declare.

### Stage the RHEL DVD ISO

Download `rhel-9.7-x86_64-dvd.iso` from Red Hat, then stage it into the managed
media store. The `MachineImage` references it as
`local-media:rhel-9.7-x86_64-dvd.iso`, so the media name must match:

```bash
bootwright media add --name rhel-9.7-x86_64-dvd.iso --from-file /path/to/rhel-9.7-x86_64-dvd.iso
bootwright media list
```

### Get the Red Hat and IBM credentials

- **Red Hat subscription** (`rhel-org`, `rhel-activation-key`): a Red Hat account
  with RHEL entitlements (the no-cost Developer Subscription or a RHEL trial),
  then an **activation key** and your numeric **organization ID** from the Hybrid
  Cloud Console. The nodes register with `subscription-manager` to enable the
  RHEL repos.
- **IBM Storage Ceph entitlement** (`ibm-ceph-registry`): an IBM account with
  IBM Storage Ceph (trial/eval or entitled), then an **entitlement key** from the
  IBM Container Software Library. The registry login is username `cp` with that
  key as the password.

### Set the secrets

Set the three operator-supplied secrets by the names the `Secret` objects
declare, then converge the generated and `file`-sourced entries:

```bash
# Red Hat subscription: organization ID and activation key (plain strings).
printf '%s' 'YOUR_ORG_ID'  > /tmp/rhel-org.txt
printf '%s' 'YOUR_KEY'     > /tmp/rhel-activation-key.txt
bootwright secret set --name rhel-org            --raw-file /tmp/rhel-org.txt
bootwright secret set --name rhel-activation-key --raw-file /tmp/rhel-activation-key.txt
shred -u /tmp/rhel-org.txt /tmp/rhel-activation-key.txt

# IBM entitlement key for cp.icr.io (username "cp", key as the password).
printf '%s\n' 'YOUR_IBM_ENTITLEMENT_KEY' | \
  bootwright secret set --name ibm-ceph-registry --username cp --password-stdin

# Generate the ceph-node-ssh keypair and bmc-credentials, and import the
# bastion-host-ssh file: source into the context.
bootwright secret generate
bootwright secret check
bootwright secret list
```

The remaining secrets are not "set": `ceph-node-ssh` and `bmc-credentials` are
`generated`, and `bastion-host-ssh` is the operator-owned key referenced as a
`file:` source — `secret generate` brings them all into the context.

## Validate And Set Up The Context

The setup commands follow the sequence from
[Installation and Setup](installation.md). Validate the tree, import it into a
context, record host trust, then prepare the bastion and run the read-only
checks:

```bash
bootwright validate -f .
bootwright context init --name ceph-ibm-lab -f .
bootwright context current
bootwright machine trust
bootwright bastion setup --yes
bootwright preflight all
bootwright plan
```

`preflight all` checks the bastion, the libvirt host, and the storage-cluster
prerequisites; scope it to this cluster with
`bootwright preflight storage-cluster --clusters ceph-ibm` if you prefer.

## Apply

Converge the lab:

```bash
bootwright apply --yes
bootwright status --watch
```

`apply` runs in two stages. **infra** defines the NAT'd libvirt network, brings
up the dnsmasq resolver, creates the three VMs with emulated Redfish BMCs, and
installs RHEL 9.7 on each via the Anaconda kickstart. **clusters** then, on every
node, registers with RHSM, enables the RHEL and IBM Storage Ceph repos, accepts
the IBM license, logs in to `cp.icr.io`, installs cephadm, bootstraps the cluster
from `ceph-1`, adds `ceph-2` and the `ceph-3` tie-breaker monitor, and creates the
OSDs, pools, CephFS filesystem, and the RGW service with its ingress VIP.
Re-running `apply --yes` is idempotent; for a focused storage rerun use
`bootwright apply --stage clusters --clusters ceph-ibm --yes`.

## Verify

List the cluster and its access details:

```bash
bootwright cluster list
bootwright cluster access --name ceph-ibm
```

`cluster access` reports the seed node, the SSH and health-check commands, the
dashboard URL, and the dashboard password file. Run the health check it prints;
`HEALTH_OK` from `ceph -s` confirms the cluster is reachable and healthy. Expect
3 mons (`ceph-1`, `ceph-2`, `ceph-3`), 2 mgr, 6 OSDs, 1 CephFS, an RGW service,
and the mgmt-gateway plus ingress services.

The Ceph Dashboard is served HA through the native mgmt-gateway at
`https://dashboard.ceph.bootwright.test:8443` (its keepalived VIP is
`192.168.140.81`); the S3 endpoint is `http://rgw.ceph.bootwright.test` (RGW
ingress VIP `192.168.140.80`).

### The dashboard admin password

`cephadm bootstrap` generates a one-time random `admin` password. Bootwright
captures it **during the install** and saves it on the controller at
`clusters/ceph-ibm/secrets/dashboard-password` (mode 0600). `cluster access`
prints that file path plus a `sudo cat` command — never the bytes. View it, then
log in as `admin`:

```bash
sudo cat /var/lib/bootwright/contexts/ceph-ibm-lab/clusters/ceph-ibm/secrets/dashboard-password
```

!!! note "Resolving the lab names from your workstation"
    The Ceph nodes resolve `*.bootwright.test` through the lab dnsmasq
    automatically, but your workstation does not, so the dashboard and RGW names
    will not resolve in your browser until you point the host at the lab dnsmasq
    (`192.168.140.1`) — typically with split DNS for `~bootwright.test`, or a
    static `/etc/hosts` fallback. The Ceph example README walks through both.

## Next steps

- Read [Storage](../concepts/storage.md) for the full Ceph object model — pools,
  placement policies, filesystems, and gateways.
- See [Ceph topologies](../advanced/ceph-topologies.md) for multi-site stretch
  mode, OSD device selection, and production layouts.
- See [Managed-OS installs](../advanced/managed-os.md) for how bootwright installs
  the node operating system.
- See [Troubleshooting](../troubleshooting.md) when validation, SSH trust, the
  managed-OS install, or Ceph apply checks fail.
