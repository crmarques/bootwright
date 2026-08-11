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

| Machine | Node | Profile | Ceph roles | OSDs | Purpose |
| --- | --- | --- | --- | --- | --- |
| `ceph-1` | `node-01` | `ceph-full` | mon, mgr, osd, mds, rgw, ingress | 3 | full node (block + file + object) |
| `ceph-2` | `node-02` | `ceph-full` | mon, mgr, osd, mds, rgw, ingress | 3 | full node (block + file + object) |
| `ceph-3` | `node-03` | `ceph-mon` | mon | 0 | monitor-only tie-breaker (quorum) |

The machines keep their `Machine` names (`ceph-1`…`ceph-3`); the cluster names
its nodes independently — each node name is declared explicitly as
`topology.nodes[].name` (`node-01`–`node-03`) and must be unique within the
cluster — and every cluster-facing reference (bootstrap node, placements) uses
those node names.

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

`bootwright example init` scaffolds a Ceph tree with the build you installed, so
what it writes always matches your binary's schema:

```bash
bootwright example init --name my-ceph --kind storage-cluster --ceph-release <release> --output-dir ./my-ceph
```

That writes the four kinds
[the desired-state model](../concepts/index.md#the-smallest-input) calls the
smallest Ceph input. `--ceph-release` is authored install intent; the scaffold
never chooses a compiled current release. The result passes
`bootwright validate -f ./my-ceph` as written:

```text
my-ceph/
  environment.yaml                                   Environment: base domain
  secrets.yaml                                       Secrets: cephadm cluster key, node login key
  clusters/storage/my-ceph/
    cluster.yaml                                     StorageCluster: community Ceph, three mons, one OSD device per node
    nodes/my-ceph-node-{1,2,3}.yaml                  Machines: three provided (already-installed) storage nodes
```

That scaffold is the whole tree when your storage nodes already run an operating
system you manage: give each node its address, point the `<name>-node-ssh` secret
at a key that logs in to them, pick the distribution and release, and apply.

**This lab is bigger**, because bootwright also builds the three VMs and installs
RHEL on them. That adds the substrate and managed-OS kinds the scaffold leaves
out — `InfraProvider`, `NetworkConfig`, `InfraComponent`, `MachineImage`,
`MachineInstallProfile`, `Entitlement` — plus the pool, filesystem, and gateway
kinds. They are already assembled in the `ceph-ibm-libvirt-lab` example tree,
which the rest of this guide walks through, so copy that tree into a directory of
its own and enter it:

```bash
git clone https://github.com/crmarques/bootwright.git
cp -r bootwright/examples/ceph-ibm-libvirt-lab ./my-ceph-lab
cd ./my-ceph-lab
```

Example trees track `main`'s schema; if `bootwright validate -f .` rejects one,
install from source ([Install the CLI](installation.md#install-the-cli),
Option B). The example is a complete, valid bootwright tree. Its layout:

```text
my-ceph-lab/
  environment.yaml                                   Environment: cluster selection, lab DNS
  secrets.yaml                                       Secrets: cluster/machine/bastion SSH, BMC, RHSM, IBM registry
  infra/
    providers/libvirt.yaml                           InfraProvider: libvirt + VM profiles + bridge
    machines/bastion.yaml                            Machine: the libvirt host (localhost)
    networkconfigs/ceph-net.yaml                     NetworkConfig: 192.168.140.0/24, static IPs
    components/lab-dns.yaml                           InfraComponent: dnsmasq resolver + forwarders
    entitlements/{rhel,ibm-storage-ceph}.yaml         Entitlements: RHEL subscription + IBM Storage Ceph
    images/rhel-9-x86-64-dvd.yaml                    MachineImage: RHEL 9.7 DVD (local-media)
    profiles/rhel-9-ceph-node.yaml                   MachineInstallProfile: Anaconda RHEL install
  clusters/storage/ceph-ibm/
    cluster.yaml                                     StorageCluster: IBM 9.9.0.3, exact package and image builds
    nodes/{ceph-1,ceph-2,ceph-3}.yaml                Machines: ceph-1, ceph-2 (full), ceph-3 (mon)
    placement-policy.yaml                            StoragePlacementPolicy: size 2 / minSize 2
    pools/{rbd,cephfs-data,cephfs-metadata,rgw}.yaml StoragePools
    filesystems/cephfs.yaml                          StorageFilesystem (CephFS)
    object-gateways/rgw.yaml                         StorageObjectGateway (RGW + ingress VIP)
```

## Understand The Input Objects

Read them in dependency order; each owns exactly one slice of the truth.

### Environment (`environment.yaml`)

The catalog that ties the tree together. It sets `spec.domains.base`
(`bootwright.test`), activates the cluster under `spec.storageClusters`, and
selects the managed lab DNS
under `spec.infraComponents.nameResolution`. It also sets
`spec.safety.destroyProtection: protected`, so `destroy` refuses to run
without `--authorize protected`.

`spec.machineAccess.keyRef: bootwright-machine-key` names the fleet SSH key
authorized for the `bootwright` service account on every node this lab installs.
It is required because the nodes set `os.installProfileRef`, and
`secret generate` mints it.

### Entitlements (`infra/entitlements/`)

The two `Entitlement` objects that license the install are their own first-class
files (referenced by name from the `StorageCluster`), decoupled by concern — IBM
Storage Ceph runs on RHEL it does not itself entitle:

- `rhel` (`type: redhat-rhel`) registers each node with
  `subscription-manager` — in the machines phase, right after RHEL lands on the
  node — so the RHEL BaseOS/AppStream repos cephadm needs are available. Its
  `rhsm.organizationRef` and `rhsm.activationKeyRef` resolve to the `rhel-org`
  and `rhel-activation-key` secrets. `rhsm.management` defaults to `managed`;
  `external` delegates registration to an operator `CustomPlaybook` and
  drops the org/key secrets (see the `ceph-external-rhsm` example).
- `ibm-storage-ceph` (`type: ibm-storage-ceph`) logs each node
  into the IBM registry `cp.icr.io/cp` for the container images, accepts the
  product license (`license.accept: true`). The RHEL subscription is named
  separately by the storage nodes (`MachineInstallProfile.spec.subscription` or
  the cluster `spec.ceph.osSubscriptionRef`). Its `registry.credentialsRef` resolves to the
  `ibm-ceph-registry` secret (username `cp`, the IBM entitlement key as the
  password).

### Secret (`secrets.yaml`)

Seven `kind: Secret` objects, one per named secret, each with a `spec.type` and
an optional `spec.source`. The bytes never go in YAML — `spec.source` says where
they come from: `bastion-host-ssh` (`sshKeyPair`) is a `file` reference to an
operator-owned key; `ceph-cluster-ssh` (`sshKeyPair`), `bootwright-machine-key`
(`sshKeyPair`), and `bmc-credentials` (`usernamePassword`) are `generated`; and
`rhel-org`, `rhel-activation-key` (`opaque`), and `ibm-ceph-registry`
(`usernamePassword`) are context-local, set with `bootwright secret set`.

### InfraProvider (`infra/providers/libvirt.yaml`)

The libvirt substrate facts (`type: libvirt`). It drives libvirt on this host via
the `bastion` Machine (`machineRef: bastion`, `uri: qemu:///system`), enables an
emulated Redfish BMC per VM (`bmcEmulationDefaults`, credentials from
`bmc-credentials`), defines the VM sizing `machineProfiles`, and names the libvirt
bridge under `networkAttachments`:

- `ceph-full`: 2 vCPU, 4096 MiB RAM, a 20 GiB root, and three 8 GiB OSD data disks
  (`/dev/vdb`–`/dev/vdd`).
- `ceph-mon`: 1 vCPU, 2048 MiB RAM, a 40 GiB root, no OSD disks. Its `mon`
  role computes a 35 GiB service budget; the extra room lets the installed OS
  coexist with the 20 GiB free-space floor enforced by live preflight.

### Machine (`infra/machines/bastion.yaml`, `clusters/storage/ceph-ibm/nodes/*.yaml`)

Four machines: the shared bastion under `infra/machines/`, and the three Ceph
nodes alongside their cluster under `clusters/storage/ceph-ibm/nodes/`. Machines
own substrate binding, OS mode, durable addresses, and SSH access — never install
intent.

- `bastion`: the local workstation/libvirt host. `os.provided: true`,
  `capabilities` (`container-runtime`, `libvirt`), addresses
  `ssh: localhost` and `cluster-lan: 192.168.140.1`, SSH key `bastion-host-ssh`.
- `ceph-1`, `ceph-2`, `ceph-3`: the Ceph nodes (`capabilities: [ceph-node]`).
  Each binds the libvirt provider (`ceph-1`/`ceph-2` use profile `ceph-full`,
  `ceph-3` uses `ceph-mon`), sets `os.provided: false` with
  `installProfileRef: rhel-9-ceph-node` (bootwright installs RHEL onto it), gets a
  static IP (`.21`, `.22`, `.23`), and authors **no** `access` block — the install
  creates the `bootwright` service account and authorizes
  `Environment.spec.machineAccess.keyRef`. Separately, the cluster's own cephadm
  login is `spec.ceph.cephadm.clusterSSH.keyRef: ceph-cluster-ssh` — one SSH
  identity per `StorageCluster`.

### MachineImage and MachineInstallProfile (`infra/images/`, `infra/profiles/`)

These drive the managed RHEL install — the part that distinguishes this lab from
the agent-installed OpenShift SNO lab.

- `MachineImage` `rhel-9-x86-64-dvd`: a full DVD whose
  `bootMedia: local-media:rhel-9.7-x86_64-dvd.iso` resolves to the managed media
  store (you stage the ISO with `bootwright media add` below). The install
  profile omits `installer.anaconda.packageSource`, so Anaconda installs from
  the DVD via `cdrom`.
- `MachineInstallProfile` `rhel-9-ceph-node`: the Anaconda install
  (`installer.anaconda.imageRef: rhel-9-x86-64-dvd`) for
  `os.family: rhel`, `version: "9.7"`. Its `customizations` disable SSH password
  authentication (`ssh.passwordAuthentication: false`) — the install itself
  creates the `bootwright` account and authorizes
  `Environment.spec.machineAccess.keyRef` for it — wipe and lay down the root
  device, install a minimal base plus the runtime prerequisites (`podman`,
  `lvm2`, `chrony`, `firewalld`), and — because the cluster requests FIPS — set
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
(`additionalIngressHosts: [rgw.ceph-ibm.bootwright.test]`).

### StorageCluster and its surfaces (`clusters/storage/ceph-ibm/`)

The Ceph install intent. `StorageCluster` `ceph-ibm` is `type: ceph`,
`management: managed` (bootwright installs and owns it via cephadm). Its `spec.ceph`
block sets `distribution: ibm`, `release: "9.9.0.3"`,
`packageVersion: "20.1.0-221.el9cp"`,
`cephadm.ansible.packageVersion: "5.0.2-1.el9cp"`,
`image.base: cp.icr.io/cp/ibm-ceph/ceph-9-rhel9`,
`image.version: "v9.0-20201"`,
`ibm.callHome: disabled`, `entitlementRef: ibm-storage-ceph`,
FIPS (`security.fips.enabled: true`), the cephadm SSH address and bootstrap node
(`node-01`), the public/cluster networks, the HA dashboard
(`cephadm.workarounds` records the affected gateway build and `mgmtGateway`
uses its safe HTTP shape with a `keepalive_only` ingress VIP on `.81`), and
the `topology.nodes` that assign roles and OSD devices per node.

The surrounding objects fill in the pools and services:

- `StoragePlacementPolicy` `replicated-host`: `failureDomain: host`,
  `replicated: {size: 2, minSize: 2}`, referenced by every pool.
- `StoragePool` `rbd`, `cephfs-data`, `cephfs-metadata`, `rgw`: one pool each, by
  `ceph.role`.
- `StorageFilesystem` `cephfs`: the CephFS filesystem over the two CephFS pools,
  with one active MDS and a standby across `node-01`/`node-02`.
- `StorageObjectGateway` `rgw`: the RGW service on both full nodes, its public S3
  endpoint (`rgw.ceph-ibm.bootwright.test`), and a cephadm ingress floating a single
  VIP (`192.168.140.80`).

See [Storage](../concepts/storage.md) for the full storage model.

## Edit The Required Values

The example forms a working `192.168.140.0/24` lab; change values only where your
environment differs.

| File | Field | Set it to |
| --- | --- | --- |
| `environment.yaml` | `spec.domains.base` | A DNS base domain for the lab (default `bootwright.test`). |
| `infra/providers/libvirt.yaml` | `spec.libvirt.uri` | The libvirt URI, usually `qemu:///system`. |
| `infra/providers/libvirt.yaml` | `spec.networkAttachments[].libvirt.bridge` | The libvirt bridge on the machine network (default `vbr-ceph-ibm`). |
| `infra/networkconfigs/ceph-net.yaml` | `spec.machineNetwork[].cidr` | The machine network CIDR (default `192.168.140.0/24`). |
| `clusters/storage/ceph-ibm/nodes/*.yaml` | each `Machine` `spec.addresses` (`ssh`) | The node IPs (defaults `.21`, `.22`, `.23`). |
| `infra/images/rhel-9-x86-64-dvd.yaml` | `spec.bootMedia` | The staged RHEL DVD (`local-media:<your-iso-name>`). |
| `infra/profiles/rhel-9-ceph-node.yaml` | `spec.os.version` | The RHEL release on the DVD (default `9.7`). Bootwright does not check it against the Ceph release; consult the vendor compatibility guide. |
| `clusters/storage/ceph-ibm/cluster.yaml` | `spec.ceph.release` | The IBM Storage Ceph VRMF product version (`9.9.0.3`). Its leading component selects the stream. |
| `clusters/storage/ceph-ibm/cluster.yaml` | `spec.ceph.packageVersion` | The full IBM Storage Package Version from the same [release-table](https://www.ibm.com/support/pages/what-are-red-hat-and-ibm-storage-ceph-releases-and-corresponding-ceph-package-versions) row (`20.1.0-221.el9cp`), including the RPM release component and not the Cephadm Ansible Package Version. Required for IBM. |
| `clusters/storage/ceph-ibm/cluster.yaml` | `spec.ceph.cephadm.ansible.packageVersion` | The full Cephadm Ansible RPM version-release for that row (`5.0.2-1.el9cp`). IBM's table abbreviates it to `5.0.2-1`; use the repository RPM's full EVR. Required for IBM. |
| `clusters/storage/ceph-ibm/cluster.yaml` | `spec.ceph.image.base` | The explicit IBM daemon repository, including its vendor OS stream suffix (`cp.icr.io/cp/ibm-ceph/ceph-9-rhel9`). Required by the subscription-backed provider policy. |
| `clusters/storage/ceph-ibm/cluster.yaml` | `spec.ceph.image.version` | The IBM daemon image tag from that row (`v9.0-20201`). Required for IBM. |
| `clusters/storage/ceph-ibm/cluster.yaml` | `spec.ceph.cephadm.workarounds[]` | Keep `mgmt-gateway-spec-dependency-recording` only while the selected cephadm build has that defect. The token requires the declared HTTP/no-TLS/no-OAuth gateway shape and is never inferred from a release number. |
| `clusters/storage/ceph-ibm/cluster.yaml` | `spec.ceph.ibm.callHome` | `disabled` keeps outbound Call Home off; choose `enabled` only when intended. |
| `clusters/storage/ceph-ibm/cluster.yaml` | `spec.ceph.networks` and `mgmtGateway.ingress.address` | The dashboard VIP and the public/cluster CIDRs, if you changed the network. |
| `clusters/storage/ceph-ibm/object-gateways/rgw.yaml` | `spec.public.dnsLabel` and `spec.ceph.ingresses[].address` | The RGW endpoint label and ingress VIP, if you changed the network. |

If you change the `192.168.140.*` network, update every place it appears:
`infra/networkconfigs/ceph-net.yaml`, `infra/machines/bastion.yaml`,
`clusters/storage/ceph-ibm/nodes/*.yaml`, `infra/components/lab-dns.yaml`, and
`clusters/storage/ceph-ibm/{cluster.yaml,object-gateways/rgw.yaml}`.

!!! note "Drop FIPS to simplify"
    Two blocks request FIPS: `spec.ceph.security.fips.enabled` in
    `cluster.yaml` and `customizations.security.fips.enabled` in
    `infra/profiles/rhel-9-ceph-node.yaml`. Remove both to build the cluster without
    FIPS — bootwright requires every Ceph node's install profile to agree. For
    an IBM FIPS deployment, obtain the FIPS-enabled product build through IBM;
    Bootwright's kernel check does not establish build provenance.

!!! note "Bootwright uses the installed provider artifacts"
    Bootwright installs the exact declared `cephadm`, `ceph-common`, and
    `cephadm-ansible` RPMs on every node and refuses a package downgrade. It
    verifies their coordinates, runs the `cephadm-preflight.yml` owned by that
    installed RPM locally with package upgrades disabled, then verifies the
    coordinates again. The native role receives `ceph_origin=custom` with an
    empty custom-repository list, so it uses only the vendor or subscription
    repositories Bootwright already converged. Before bootstrap, Bootwright also
    requires the installed native Ceph version to equal the version reported by
    the exact declared image.

## Prerequisites Unique To This Lab

This lab needs three pieces of credential material plus a staged RHEL ISO. None
goes in YAML — the secrets are stored, encrypted, in the bootwright context with
`bootwright secret set`, using the secret **names** the example's `Secret`
objects declare. The `secret set` commands themselves run after the context
exists, in [Validate And Set Up The Context](#validate-and-set-up-the-context)
below.

### Stage the RHEL DVD ISO

Download `rhel-9.7-x86_64-dvd.iso` from Red Hat, then stage it into the managed
media store. The `MachineImage` references it as
`local-media:rhel-9.7-x86_64-dvd.iso`, so the media name must match:

```bash
bootwright media add --name rhel-9.7-x86_64-dvd.iso --from-file /path/to/rhel-9.7-x86_64-dvd.iso
bootwright media list
```

The media store is root-local and shared by every context, which is why this
one step may run before the context exists.

### Get the Red Hat and IBM credentials

- **Red Hat subscription** (`rhel-org`, `rhel-activation-key`): a Red Hat account
  with RHEL entitlements (the no-cost Developer Subscription or a RHEL trial),
  then an **activation key** and your numeric **organization ID** from the Hybrid
  Cloud Console. Each node registers with `subscription-manager` right after its
  RHEL install; the storage stage then enables the RHEL repos.
- **IBM Storage Ceph entitlement** (`ibm-ceph-registry`): an IBM account with
  IBM Storage Ceph (trial/eval or entitled), then an **entitlement key** from the
  IBM Container Software Library. The registry login is username `cp` with that
  key as the password.

## Validate And Set Up The Context

The setup commands follow the sequence from
[Installation and Setup](installation.md). Validate the tree and import it into
a context first — every `secret` verb writes to the *current* context, so the
context must exist before any credential is loaded:

```bash
bootwright validate -f .
bootwright context init --name ceph-ibm-lab -f .
bootwright context current
```

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

# Generate ceph-cluster-ssh, bootwright-machine-key and bmc-credentials, and
# import the bastion-host-ssh file: source into the context.
bootwright secret generate
bootwright secret check
bootwright secret list
```

The remaining secrets are not "set": `ceph-cluster-ssh`,
`bootwright-machine-key`, and `bmc-credentials` are `generated`, and
`bastion-host-ssh` is the operator-owned key referenced as a `file:` source —
`secret generate` brings them all into the context.

### Trust, bastion prep, and the read-only checks

Record host trust, then prepare the bastion and run the read-only checks:

```bash
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
up the dnsmasq resolver, creates the three VMs with emulated Redfish BMCs,
installs RHEL 9.7 on each via the Anaconda kickstart, and — once the OS is in
place — registers every node with RHSM (the `registration.ceph-ibm` machines
task). **clusters** then, on every node, enables the RHEL and IBM Storage Ceph
repos, accepts the IBM license, logs in to `cp.icr.io`, installs cephadm,
bootstraps the cluster from `node-01`, adds `node-02` and the `node-03` tie-breaker
monitor, and creates the OSDs, pools, CephFS filesystem, and the RGW service with
its ingress VIP.
Re-running `apply --yes` is idempotent; for a focused storage rerun use
`bootwright apply --stage clusters --clusters ceph-ibm --yes`.

### If apply fails

Run `bootwright status`: on a failed run it reports the failed task, the log
path for it, and prints the exact scoped command to re-run. Fix the cause, then
re-run the printed command — completed work is recorded and skipped, so the run
resumes where it stopped; neither `destroy` nor `--mode rebuild` is the
recovery path for a partial apply. See
[Apply failed partway](../troubleshooting.md#apply-failed-partway).

## Verify

List the cluster and its access details:

```bash
bootwright cluster list
bootwright cluster info --name ceph-ibm
```

`cluster info` reports the seed node, the SSH and health-check commands, the
dashboard URL, and the dashboard credential (see below). Run the health check it
prints; `HEALTH_OK` from `ceph -s` confirms the cluster is reachable and healthy.
Expect 3 mons (`node-01`, `node-02`, `node-03`), 2 mgr, 6 OSDs, 1 CephFS, an RGW
service, and the mgmt-gateway plus ingress services.

The Ceph Dashboard is served HA through the native mgmt-gateway at
`http://dashboard.ceph-ibm.bootwright.test:8888` (its keepalived VIP is
`192.168.140.81`); the S3 endpoint is `http://rgw.ceph-ibm.bootwright.test` (RGW
ingress VIP `192.168.140.80`).

### The dashboard admin password

`cephadm bootstrap` generates a one-time random `admin` password. Bootwright
captures it **during the install** and stores it encrypted on the controller.
`cluster info` prints a `Show password` line naming the command that reveals it
(`bootwright cluster info --name ceph-ibm --secrets`); neither the encrypted
file path nor the bytes are printed by default, and the file on disk is an
encrypted envelope, so reading it directly does not work. Reveal it, then log in
as `admin`:

```bash
bootwright cluster info --name ceph-ibm --secrets
```

!!! note "Resolving the lab names from your workstation"
    The Ceph nodes resolve `*.bootwright.test` through the lab dnsmasq
    automatically, but your workstation does not, so the dashboard and RGW names
    will not resolve in your browser until you point the host at the lab dnsmasq
    (`192.168.140.1`). On a systemd-resolved + NetworkManager host, attach a
    routing-only domain to the lab bridge (`vbr-ceph-ibm`) so only
    `*.bootwright.test` goes to the lab:

    ```bash
    sudo nmcli connection modify vbr-ceph-ibm ipv4.dns 192.168.140.1
    sudo nmcli connection modify vbr-ceph-ibm ipv4.dns-search '~bootwright.test'
    sudo nmcli connection up vbr-ceph-ibm
    ```

    Otherwise add static `/etc/hosts` entries
    (`192.168.140.81 dashboard.ceph-ibm.bootwright.test` and
    `192.168.140.80 rgw.ceph-ibm.bootwright.test`) — simpler, but maintained by
    hand. "Resolving the lab names from your workstation" in
    `./my-ceph-lab/README.md` covers both in more detail, including a
    runtime-only `resolvectl` variant.

### Check it still matches

`bootwright diff` compares the desired state against the running cluster
(read-only, live by default — it discovers hosts, services, OSDs, and pools on
the seed node); `bootwright diff --recorded` compares offline against the last
recorded apply instead. Either exits `3` when something is out of sync, so CI
can gate on it. See
[Comparing against live cluster state](../advanced/operations.md#comparing-against-live-cluster-state).

## Tear It Down

Closing the lab out fully takes three steps: destroy the resources, confirm
nothing is left owned, then delete the context.

This lab's `Environment` sets `safety.destroyProtection: protected`, so every
teardown needs `--authorize protected`. Its OSD hosts are libvirt VMs whose disks
hold the OSD data, so every stage that deletes them also crosses the data-loss
gate — the gate follows the data, not the stage, so `--authorize data-loss`
belongs on both lines too.

Teardown inverts the apply order internally, so one command is enough:

```bash
bootwright destroy --authorize protected,data-loss --yes
```

Stage by stage, when you want the VMs left standing in between:

```bash
bootwright destroy --stage clusters --authorize protected,data-loss --yes
bootwright destroy --stage infra --authorize protected,data-loss --yes
```

See
[Operations, recovery & teardown](../advanced/operations.md#tearing-down-with-destroy)
for the ownership gates, scoping, and what each stage removes. To also remove
the captured installer inputs and per-run logs, add `--purge-history` to the
`destroy` — purged history is not recoverable; see
[Leaving no trace of a destroyed component](../advanced/operations.md#leaving-no-trace-of-a-destroyed-component).

`destroy` removes the resources but leaves the context and its encrypted
secret store behind. Confirm nothing is left owned, then delete the context:

```bash
bootwright status
bootwright context delete --name ceph-ibm-lab --purge
```

`context delete --purge` removes the context and with it the encrypted secret
store, which for this lab still holds the RHSM organization ID and activation
key, the IBM entitlement key, and the captured Ceph dashboard password.
`--purge` is mandatory, and the delete fails closed while the context still
owns resources — which is why it comes last. After the current context is
deleted, `bootwright context current` reports none — the next lab must
`context init` before any `secret` command.

## Next steps

- Read [Storage](../concepts/storage.md) for the full Ceph object model — pools,
  placement policies, filesystems, and gateways.
- See [Ceph topologies](../advanced/ceph-topologies.md) for multi-site stretch
  mode, OSD device selection, and production layouts.
- See [Managed-OS installs](../advanced/managed-os.md) for how bootwright installs
  the node operating system.
- See [Troubleshooting](../troubleshooting.md) when validation, SSH trust, the
  managed-OS install, or Ceph apply checks fail.
