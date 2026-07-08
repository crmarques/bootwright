---
title: Machines & operating systems
description: Machine vs node, OS modes, substrate binding, and the Machine, MachineImage, and MachineInstallProfile fields.
---

# Machines & operating systems

A `Machine` is a *desired-state* description of a host — raw hardware, a virtual
machine, an OS-ready box, or a machine whose OS Bootwright should install. It is
not a running node: it carries the durable facts (substrate binding, hardware and
BMC inventory, the OS mode, the install network, named addresses, and SSH
access) that let Bootwright provision the substrate, install an OS where asked,
and bind the host into a cluster. A cluster references machines by name and the
machine references its substrate by name; nodes are what those machines become
after install.

`spec.os.provided` selects the machine's **OS mode** and drives most cross-field
rules on this kind:

- **provided** (`os.provided: true`) — the machine already runs a usable OS.
  Bootwright neither provisions a substrate nor installs an OS; it reaches the
  machine over `access.ssh`. Omitting `access.ssh` declares it is the local
  bastion Bootwright runs on, and it is reached with a local connection.
- **managed** (`os.provided: false` plus `os.installProfileRef`) — Bootwright
  installs the OS through Anaconda before any cluster or storage work, using a
  [`MachineImage`](#machineimage) and a [`MachineInstallProfile`](#machineinstallprofile).
- **ready** (`os.provided: false`, no `os.installProfileRef`) — the cluster
  agent installer lays the OS down (RHCOS for OpenShift); Bootwright provisions
  the substrate and boots the agent ISO.

The two managed-OS kinds describe the install media (`MachineImage`) and the
install behavior (`MachineInstallProfile`). For the end-to-end managed-OS install
workflow see [Managed OS installs](../advanced/managed-os.md).

Every kind on this page uses the shared
[object envelope](index.md#object-envelope) (`apiVersion: bootwright.io/v1alpha1`,
`kind`, `metadata.name`) and the **Required** / **Default** column convention.
The tables below describe only `spec`.

!!! note "Reusable network templates live on the Infrastructure page"
    A `Machine` references a `NetworkConfig` by name through
    `spec.network.config.networkConfigRef`, and a provider attachment through
    `attachmentRef`. The `NetworkConfig`, `InfraProvider`, and `InfraComponent`
    kinds are documented on [Infrastructure](infrastructure.md).

## Machine

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.capabilities[]` | No | None | Roles a machine fulfills. See the canonical set below. |
| `spec.substrate.providerRef` | When `os.provided: false` | None | Names the `InfraProvider` that supplies the substrate. |
| `spec.substrate.profileRef` | When `os.provided: false` on `libvirt`, `vsphere`, or `kubevirt` | None | Names a provider `machineProfiles[]` entry. Must be empty for `baremetal`. |
| `spec.hardware.nics[]` | When `os.provided: false` on `baremetal` | None | Physical NIC inventory. |
| `spec.hardware.boot.nicRef` | When `os.provided: false` on `baremetal` | None | Boot NIC name from `hardware.nics[]`. |
| `spec.hardware.management.bmc` | When `os.provided: false` on `baremetal` | None | BMC access for Redfish virtual media. |
| `spec.os.provided` | Yes | None | `true` for OS-ready machines; `false` for machines Bootwright or the cluster installer provisions. |
| `spec.os.installProfileRef` | No | None | Names a `MachineInstallProfile` for Bootwright-managed OS install. Must be empty when `os.provided: true`. |
| `spec.os.install` | No | None | Machine-owned install hints. Must be empty when `os.provided: true`. |
| `spec.network.config` | No | None | Install network selection and overrides. Must be empty when `os.provided: true`. |
| `spec.network.interfaceBinding[]` | When `os.provided: false` on `baremetal` with a `NetworkConfig` | None | Maps hardware NIC names to NMState interface names. |
| `spec.addresses[]` | No | None | Durable named addresses used by SSH, services, and endpoint resolution. |
| `spec.access.ssh` | When `os.installProfileRef` is set | None | SSH address, user, key, and optional known-hosts material. Optional on a provided-OS machine: omit it to declare the local bastion (local connection). |

### Capabilities

`spec.capabilities[]` tags the roles a machine fulfills. Each entry must come
from the canonical set; an unknown, empty, or duplicate capability fails
validation. There are eleven capabilities:

`openshift-node`, `ceph-node`, `ceph-admin`, `libvirt`, `container-runtime`,
`load-balancer`, `proxy`, `name-resolution`, `ntp`, `registry`, and
`artifact-server`.

### OS mode

`spec.os.provided` is required and has no default. It selects the machine mode
and gates several other fields.

!!! warning "`os.provided: true` means OS-ready and substrate-free"
    When `os.provided: true` the machine already runs a usable OS and Bootwright
    neither provisions a substrate nor installs an OS. In that mode
    `os.installProfileRef`, `os.install`, and `network.config` must all be empty.
    `access.ssh` is optional: supply it to reach a remote host, or omit it to
    declare the local bastion Bootwright runs on (reached with a local
    connection). Setting any of those install fields alongside `os.provided:
    true` is a validation error.

When `os.provided: false`, the machine needs a substrate
(`substrate.providerRef`). It is OS-installed by Bootwright when
`os.installProfileRef` is also set (managed mode), and otherwise installed by the
cluster agent installer (ready mode).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `os.provided` | Yes | None | Boolean. `true` = OS-ready; `false` = needs a substrate. |
| `os.installProfileRef` | No | None | `MachineInstallProfile` name for managed OS install. Empty when `os.provided: true`. |
| `os.install.rootDeviceHints.deviceName` | No | None | Device path such as `/dev/sda`. |
| `os.install.rootDeviceHints.hctl` | No | None | HCTL selector. |
| `os.install.rootDeviceHints.model` | No | None | Device model selector. |
| `os.install.rootDeviceHints.vendor` | No | None | Device vendor selector. |
| `os.install.rootDeviceHints.serialNumber` | No | None | Serial selector. |
| `os.install.rootDeviceHints.minSizeGigabytes` | No | None | Minimum device size. |
| `os.install.rootDeviceHints.wwn` | No | None | WWN selector. |
| `os.install.rootDeviceHints.rotational` | No | None | Rotational selector. |

### Hardware

Hardware inventory is optional for VM-like substrates. For a `baremetal`
provider with `os.provided: false`, `nics[]` (each with a `macAddress`) and
`boot.nicRef` become required, and every NMState interface in the effective
`NetworkConfig` must be bound through `network.interfaceBinding[]`.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `hardware.nics[].name` | Yes (per entry) | None | Local NIC name used by Bootwright references. |
| `hardware.nics[].macAddress` | Required on `baremetal` `os.provided: false` machines | None | Physical MAC address. |
| `hardware.boot.nicRef` | Required on `baremetal` `os.provided: false` machines | None | Name of the boot NIC from `nics[]`. |
| `hardware.management.bmc.address` | Required when any BMC field is set | None | Redfish BMC address, commonly `redfish-virtualmedia+https://...`. |
| `hardware.management.bmc.protocol` | No | `redfish` | Only `redfish` is supported today; any other value is rejected. |
| `hardware.management.bmc.credentialsRef` | Required when any BMC field is set | None | Secret containing BMC credentials. |
| `hardware.management.bmc.tls.verify` | No | `true` | bootwright→BMC TLS leg: whether bootwright verifies the BMC's own certificate. Set `false` for a lab/self-signed BMC. Inherits `baremetal.defaults.bmc.tls`. |
| `hardware.management.bmc.virtualMedia.tls.verify` | No | `true` | BMC→artifact-server leg: whether the BMC verifies the artifact server certificate on the virtual-media fetch. `false` = skip (best-effort; some firmware ignores it). On xFusion/Huawei iBMC this toggles the SecurityService `HttpsTransferCertVerification` flag, which requires the BMC account's **Security Configuration** right. |
| `hardware.management.bmc.virtualMedia.tls.importServerCertificate` | No | `false` | Upload the artifact server certificate into the BMC trust store before the fetch so the BMC accepts a self-signed certificate. Uses the Redfish VirtualMedia Certificates collection, or the xFusion/Huawei iBMC `SecurityService.ImportRemoteHttpsServerRootCA` action; fails clearly if the BMC exposes neither. On xFusion/Huawei iBMC the action needs the BMC account's **Security Configuration** right; the fixed Operator and Common User roles lack it (use an Administrator-role account or a Custom Role granted that right), otherwise the import returns HTTP 403 `InsufficientPrivilege`. A privilege-limited account that cannot be changed must instead serve a BMC-trusted (CA-signed or trust-store-preloaded) artifact server certificate. |
| `hardware.management.bmc.virtualMedia.tls.removeServerCertificateAfterBoot` | No | `false` | Remove the imported certificate from the BMC after the agent ISO is mounted. Requires `importServerCertificate`. |

A per-`Machine` `hardware.management.bmc` block overrides the provider's
`baremetal.defaults.bmc` for that server (`tls` and `virtualMedia` inherit when
omitted; `credentialsRef` is always per-machine). A complete bare-metal `Machine`
inventory example sits on [Infrastructure](infrastructure.md#bare-metal).

### Network

`spec.network.config` selects the install network. Reference a reusable
`NetworkConfig` with `networkConfigRef`, or inline a one-off `NetworkConfig.spec`
with `spec`; the two are mutually exclusive. `overrides` and
`interfaceAddresses[]` layer on top of the selected template.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `network.config.networkConfigRef` | No | None | Names a reusable `NetworkConfig`. Mutually exclusive with `network.config.spec`. |
| `network.config.attachmentRef` | Required with `networkConfigRef` on a provider-backed machine | The `networkConfigRef` name, only when the provider declares exactly one attachment | Names an `InfraProvider.spec.networkAttachments[]` entry. |
| `network.config.interfaceAddresses[].interface` | Yes (per entry) | None | NMState interface receiving the static address. |
| `network.config.interfaceAddresses[].addressRef` | Yes (per entry) | None | Name from `spec.addresses[]`. |
| `network.config.interfaceAddresses[].prefixLength` | Yes (per entry) | None | Prefix length; must be 1-128, or 1-32 when the family is IPv4. |
| `network.config.interfaceAddresses[].family` | No | `ipv4` | `ipv4` or `ipv6`. |
| `network.config.overrides` | No | None | Additional NMState merged into the selected template. Only valid with `networkConfigRef`. |
| `network.config.spec` | No | None | Inline one-off `NetworkConfig.spec`. Mutually exclusive with `networkConfigRef`. |
| `network.interfaceBinding[].nicRef` | Yes (per entry) | None | Name from `hardware.nics[]`. |
| `network.interfaceBinding[].interfaceName` | Yes (per entry) | None | Effective NMState interface name. |

!!! note "Author each static install IP exactly once"
    Set static install IPs in `spec.addresses[]` and reference them with
    `interfaceAddresses[]`. Do not duplicate the same IP into NMState
    `overrides` — validation rejects a static address in `overrides` for an
    interface that `interfaceAddresses[]` already owns. `interfaceAddresses[]`
    itself is only valid alongside `networkConfigRef` or `spec`.

### Addresses and SSH

`access.ssh` carries durable SSH connection details. Both `ssh.addressRef` (a
name from `spec.addresses[]`) and `ssh.keyRef` are required whenever the block is
present. A provided-OS machine may omit the whole block to declare it is the
local bastion Bootwright runs on; it is then reached with a local connection and
needs no SSH address or key.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `addresses[].name` | Yes (per entry) | None | Local address name. |
| `addresses[].address` | Yes (per entry) | None | IP address or DNS name. |
| `access.ssh.addressRef` | Yes (when `access.ssh` is set) | None | Address name used for SSH; must resolve to `spec.addresses[]`. |
| `access.ssh.user` | No | Workflow-dependent | SSH user. |
| `access.ssh.keyRef` | Yes (when `access.ssh` is set) | None | Secret containing the private SSH key material. |
| `access.ssh.knownHostsRef` | No | Context-managed SSH trust | Explicit `known_hosts` secret. |

A complete `Machine` (libvirt, ready mode) referencing a provider profile, a
network config, and a static address:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: sno-libvirt-master-0
spec:
  capabilities:
    - openshift-node
  substrate:
    providerRef: lab-libvirt-provider
    profileRef: sno
  os:
    install:
      rootDeviceHints:
        deviceName: /dev/vda
    provided: false
  network:
    config:
      networkConfigRef: sno-bridge
      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24
  addresses:
    - name: ip
      address: 192.168.132.20
```

### Inspecting and connecting

`bootwright machine list` reports every declared `Machine` with its provisioning
state, OS, backing substrate, and the cluster (and node role) it belongs to. A
`Machine` is *provisioned* once Bootwright has recorded provisioning it in the
current context; a `Machine` with `os.provided: true` reports *external OS*
because Bootwright never provisions its substrate.

```console
$ bootwright machine list
$ bootwright machine list --clusters ceph-dc1        # only that cluster's nodes
$ bootwright machine list --silent                   # names only, one per line
$ bootwright machine list --output json
```

`bootwright machine rsh --name <machine>` opens an interactive SSH shell on a
`Machine` using the identity Bootwright already knows for it — the resolved
`access.ssh` address, user (default `root`), private key, and the context
host-key trust store recorded by `bootwright machine trust`. `machine exec` runs
a single command on the `Machine` instead of opening a shell:

```console
$ bootwright machine rsh --name ceph-dc1-0
$ bootwright machine exec --name ceph-dc1-0 -- systemctl status ceph.target
```

To reach a node cluster-first — by cluster and node rather than by Machine name —
use `bootwright cluster rsh --name <cluster> --node <node>` (and `cluster exec`
for a one-off command); the node selector accepts the Machine name, the node
hostname, or a `<role>-<ordinal>` such as `master-0`.

## MachineImage

`MachineImage` describes the install media for a managed OS install: one
bootable ISO (`bootMedia`) plus, for a boot ISO that carries no packages, where
Anaconda fetches them (`packageSource`). Omitting `packageSource` declares
`bootMedia` a full DVD that installs offline from its own payload.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.bootMedia` | Yes | None | The ISO the machine boots over BMC virtual media: `local-media:<filename.iso>`, a `file://` absolute path, `http://`, or `https://`. |
| `spec.packageSource` | No | None (⇒ full DVD) | Where Anaconda fetches packages for a boot ISO. Omit for a full DVD (installs via `cdrom`). Set exactly one arm — see below. |
| `spec.checksum` | No | None | Optional `sha256:<hex>` pin on `bootMedia`. |
| `spec.trustRefs[]` | No | None | `Environment` secrets holding CA bundles trusted when downloading `bootMedia`. |
| `spec.headersRefs[]` | No | None | `Environment` secrets holding extra HTTP headers sent when downloading `bootMedia`. |

### Package source

`packageSource` is a discriminated union — the arm you set is the source type,
so there is no `type` field. Set exactly one:

| Arm | Fields | Description |
| --- | --- | --- |
| `mirror` | `baseURL` (required, `http(s)`), `repositories[]` (`id` + `http(s)` `baseURL`) | Install from an HTTP(S) install tree you host. `baseURL` is the primary tree (BaseOS); `repositories` are additional (e.g. AppStream). |
| `redhatCDN` | `entitlementRef` (required) | Register against Red Hat's CDN over the named `redhat-rhel` `Entitlement`. |
| `hostedTree` | `fromMedia` (required, `local-media:`/`file://`) | Bootwright extracts the DVD named by `fromMedia` once and serves it from the cluster artifact server. `fromMedia` must be verifiable local media (staged via `bootwright media add`) and must differ from `spec.bootMedia`. |

!!! note "Registering against a corporate Satellite"
    A `redhatCDN` install registers against the public Red Hat CDN unless the
    referenced entitlement's `rhsm` arm carries a `satellite` block, in which
    case the install registers and pulls content from that Red Hat Satellite
    instead. No `MachineImage` change is needed — see
    [Environment › Corporate Satellite](environment.md#corporate-satellite).

### Boot media vs DVD

A DVD ISO (~10 GB) bundles the installer and the BaseOS/AppStream repositories,
so Anaconda installs offline with a Kickstart `cdrom` source — set only
`bootMedia` and omit `packageSource`. A boot ISO (~1 GB) carries only the
installer, so it needs a `packageSource`: Bootwright renders a `url --url=` (or
RHSM) install source plus `repo` entries instead of `cdrom`, and Anaconda
fetches packages over the network during install.

A boot ISO sourced from a package `mirror` points `baseURL` at a BaseOS install
tree and adds the AppStream repository — a RHEL install needs both:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-9-boot
spec:
  bootMedia: local-media:rhel-9.7-x86_64-boot.iso
  packageSource:
    mirror:
      baseURL: https://mirror.example.test/rhel/9/BaseOS/x86_64/os/
      repositories:
        - id: appstream
          baseURL: https://mirror.example.test/rhel/9/AppStream/x86_64/os/
```

A boot ISO from the Red Hat CDN (`redhatCDN`) references a `redhat-rhel`
[`Entitlement`](environment.md#entitlements) (an RHSM organization plus
activation key); Anaconda registers the node and installs from the subscription
CDN:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-9-boot-cdn
spec:
  bootMedia: local-media:rhel-9.7-x86_64-boot.iso
  packageSource:
    redhatCDN:
      entitlementRef: rhel
```

A boot ISO with a **self-hosted install tree** (`hostedTree`) is the air-gapped,
no-mirror case: Bootwright extracts the DVD named by `fromMedia` once into the
cluster artifact server's document root and the installing node fetches
GPG-signed packages from it, so the ~10&nbsp;GB payload lands on disk once per
`(cluster, image)` instead of inside every per-node ISO. Stage both the small
boot ISO (`bootMedia`) and the DVD (`fromMedia`) with `bootwright media add` —
the DVD is checksum-verified there — and bind the cluster's `machineBoot`
artifact endpoint to an **http** listener (Anaconda verifies TLS and would
reject a self-signed artifact certificate; the DVD's own `.treeinfo` advertises
BaseOS and AppStream, so no `repositories` are needed):

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-9-boot-tree
spec:
  bootMedia: local-media:rhel-9.7-x86_64-boot.iso
  packageSource:
    hostedTree:
      fromMedia: local-media:rhel-9.7-x86_64-dvd.iso
```

A full DVD needs no `packageSource` at all:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-9-dvd
spec:
  bootMedia: local-media:rhel-9.7-x86_64-dvd.iso
```

!!! note "Trust: as safe as a per-node ISO"
    A `hostedTree` matches the trust of a sealed per-node ISO. The DVD is
    sha256-verified (by `media add`), extracted faithfully (Red Hat's documented
    full copy, preserving `.treeinfo`), content-addressed and published
    atomically read-only, so the served tree provably equals the verified DVD.
    Packages stay Red&nbsp;Hat-signed end to end and `dnf` enforces `gpgcheck`
    during install, so a tampered tree cannot install a malicious package. The
    per-node boot ISO is still fetched by the BMC exactly as before (over HTTPS
    where the BMC requires it); only the package fetch moves to the node.

!!! tip "Disk footprint scales with media size × node count"
    Bootwright bakes each machine's Kickstart into its **own** install ISO
    (Redfish virtual-media boot cannot pass kernel arguments), so every node in a
    group keeps a customized ISO as large as the source media. The source ISO is
    staged once per `(cluster, image)` — read in place on controller-local
    installs, copied at most once on a remote provider host — but the per-machine
    output ISOs are unavoidable. An N-node group therefore costs about
    `N ×` media size of customized media, which is the main reason to prefer a
    small boot ISO with a `packageSource` (~1&nbsp;GB) over a full DVD
    (~10&nbsp;GB). To also keep the package payload off every node, a
    `hostedTree` package source serves the DVD's packages once from the artifact
    server (see above).

The boot-ISO reachability preflight, early networking, and proxy details are
covered in [Managed OS installs](../advanced/managed-os.md). When the install
nodes reach the package source only through a forward proxy, set
[`Environment.spec.proxyFor.machineOSInstall`](environment.md) to a declared
external proxy.

## MachineInstallProfile

`MachineInstallProfile` declares how Bootwright installs and customizes an OS
through Anaconda.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.os.family` | Yes | None | OS family, for example `rhel`. |
| `spec.os.version` | Yes | None | OS version string. |
| `spec.os.architecture` | Yes | None | Architecture such as `x86_64`. |
| `spec.installer.anaconda` | Yes | None | Anaconda installer block (its presence is the installer discriminator). |
| `spec.installer.anaconda.imageRef` | Yes | None | Names a `MachineImage`. |
| `spec.customizations.hostname.source` | No | None | Currently `machineName`. |
| `spec.customizations.localization.language` | No | `en_US.UTF-8` | System message locale. |
| `spec.customizations.localization.formats` | No | Follows `language` | Regional formatting locale (dates, numbers, currency). |
| `spec.customizations.localization.keyboard` | No | `us` | Console keyboard layout. |
| `spec.customizations.localization.timezone` | No | `UTC` | System timezone; the hardware clock stays UTC. |
| `spec.customizations.localization.additionalLocales[]` | No | None | Extra locales beyond `language`/`formats` to install. |
| `spec.customizations.ssh.authorizeMachineSSHKey` | No | `false` | Authorize the machine SSH key during install. |
| `spec.customizations.ssh.passwordAuthentication` | No | `false` | Enable or disable password SSH auth. |
| `spec.customizations.storage.rootDevice.source` | No | None | Currently `machineRootDeviceHints`. |
| `spec.customizations.storage.wipe` | No | `false` | Wipe the selected root device before install. |
| `spec.customizations.packages.environment` | No | None | Currently `minimal`. |
| `spec.customizations.packages.install[]` | No | None | Packages to install. |
| `spec.customizations.packages.excludeDocs` | No | `false` | Render Kickstart `--excludedocs`. |
| `spec.customizations.packages.installWeakDeps` | No | OS default | Tri-state weak dependency setting. |
| `spec.customizations.services.enabled[]` | No | None | Services to enable. |
| `spec.customizations.services.disabled[]` | No | None | Services to disable. |
| `spec.customizations.security.selinux.mode` | No | OS default | `enforcing`, `permissive`, or `disabled`. |
| `spec.customizations.security.firewall.enabled` | No | OS default | Tri-state firewall control; explicit `false` disables. |
| `spec.customizations.security.fips.enabled` | No | `false` | `true` enables FIPS install configuration. RHEL-only. |

!!! warning "Profile coupling rules"
    - A profile referenced by a managed-install machine (`os.installProfileRef`)
      must list `sshd` in `customizations.services.enabled`, or the referencing
      `Machine` fails validation.
    - `customizations.security.firewall.enabled: true` requires `firewalld` in
      **both** `customizations.packages.install` and
      `customizations.services.enabled`.
    - `customizations.security.fips.enabled: true` is supported only when
      `os.family` is `rhel` (compared case-insensitively).

See [The desired-state model](index.md) for the field-table and union
conventions, [Infrastructure](infrastructure.md) for providers and networks, and
[Managed OS installs](../advanced/managed-os.md) for the managed-OS install
how-to.
