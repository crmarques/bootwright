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
| `hardware.management.bmc.virtualMedia.tls.trust` | No | `disable-verification` | BMC→artifact-server leg: how the BMC comes to trust the artifact server certificate for the virtual-media fetch. `disable-verification` — bootwright turns certificate verification off on the BMC for the fetch (SecurityService `HttpsTransferCertVerification` and per-resource `VerifyCertificate` writes; on xFusion/Huawei iBMC these need the BMC account's **Security Configuration** right when the flag is enabled). `import-certificate` — bootwright uploads the artifact server certificate into the BMC trust store so verification can stay on; uses the Redfish VirtualMedia Certificates collection or the xFusion/Huawei iBMC `SecurityService.ImportRemoteHttpsServerRootCA` action (needs the same **Security Configuration** right; the fixed Operator and Common User roles lack it, so use an Administrator-role account or a Custom Role granted that right, otherwise HTTP 403 `InsufficientPrivilege`). `established` — trust already exists out of band (CA-signed certificate, root CA pre-loaded into the BMC trust store, or verification already disabled); bootwright performs **no** BMC security writes and only issues the canonical `InsertMedia`. `established` is the only mode a privilege-limited BMC account can use against a verification-enforcing BMC. |
| `hardware.management.bmc.virtualMedia.tls.restoreVerificationAfterBoot` | No | `true` | With `trust: disable-verification` only: re-enable the verification flags after the agent ISO is mounted. Set `false` to leave verification off. |
| `hardware.management.bmc.virtualMedia.tls.removeCertificateAfterBoot` | No | `false` | With `trust: import-certificate` only: remove the imported certificate from the BMC after the agent ISO is mounted. |

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

#### The `fqdn` address

When the `Environment` declares a domain, every `Machine`'s
`spec.addresses[]` implicitly contains

```yaml
- name: fqdn
  address: <metadata.name>.<machine domain>
```

The machine domain is the `Environment`'s machine zone: today the single
`baseDomain`, and under the planned domain model (ADR 0018) the dedicated
`domains.machines` key (which defaults to `domains.base`) — see
[Environment → Domain model](environment.md#domain-model).

A declared entry named `fqdn` overrides the default verbatim — it must be a
DNS subdomain (it may live in a zone outside the machine domain, e.g. a
corporate `srv4009.corp.example.com`) and must be unique across machines.
`metadata.name` itself stays a dot-free DNS label.

`fqdn` is the machine's canonical connection address: whenever Bootwright
reaches a machine over SSH (Ansible inventory, `machine rsh`/`exec`,
`cluster rsh`/`exec`, trust bootstrap), it connects to the `fqdn` name. The
entry named by `access.ssh.addressRef` keeps its meaning as the machine's
routable IP — it is what the `fqdn` DNS record must resolve to, and the
connection fallback. Two carve-outs connect by IP deliberately: a machine whose
network configuration references no name-resolution entry (no declared
resolver could answer), and the machine hosting the managed name-resolution
component its own network references (the resolver cannot serve its own
bootstrap). How the `fqdn` and node records are published and preflighted
is described in [Networking](../advanced/networking.md#name-resolution).

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

To provision or tear down individual machines rather than whole clusters, pass
`--machines <names>` to `apply` or `destroy`; it runs only the `fabric` and
`machines` phases for the named machines. See
[Selecting machines](index.md#selecting-machines).

`bootwright machine rsh --name <machine>` opens an interactive SSH shell on a
`Machine` using the identity Bootwright already knows for it — the machine's
[`fqdn` connection address](#the-dnsentry-address) (falling back to the
`access.ssh` IP for the carve-outs described there), user (default `root`),
private key, and the context host-key trust store recorded by
`bootwright machine trust`. `machine exec` runs
a single command on the `Machine` instead of opening a shell. An unknown server
key prompts for explicit acceptance on an interactive first connection; verify
it out of band first. Use `machine trust --machines <machine>` to pre-record it,
or `machine trust --replace <machine>` after deliberately verifying a changed
key:

```console
$ bootwright machine rsh --name ceph-dc1-0
$ bootwright machine exec --name ceph-dc1-0 -- systemctl status ceph.target
```

To reach a node cluster-first — by cluster and node rather than by Machine name —
use `bootwright cluster rsh --name <cluster> --node <node>` (and `cluster exec`
for a one-off command); the node selector accepts the node name (FQDN or
its short label) or a `<role>-<ordinal>` such as `master-0` — a Machine name is
rejected with a hint naming the node. Container-cluster access
uses `install.nodeSSH`, the `core` user, and the node's primary install IP, so
its backing Machine does not need `access.ssh`. Storage-cluster access keeps
using the Machine SSH identity. An unknown node key prompts for explicit
acceptance on an interactive first connection; a changed key fails closed.

## MachineImage

`MachineImage` describes the bootable media for a managed OS install. Where
Anaconda fetches install-time packages is declared by the
`MachineInstallProfile` that selects the image.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.bootMedia` | Yes | None | The ISO the machine boots over BMC virtual media: `local-media:<filename.iso>`, a `file://` absolute path, `http://`, or `https://`. |
| `spec.checksum` | No | None | Optional `sha256:<hex>` pin on `bootMedia`. |
| `spec.trustRefs[]` | No | None | `Environment` secrets holding CA bundles trusted when downloading `bootMedia`. |
| `spec.headersRefs[]` | No | None | `Environment` secrets holding extra HTTP headers sent when downloading `bootMedia`. |

### Anaconda package source

`packageSource` is a discriminated union — the arm you set is the source type,
so there is no `type` field. Set it under
`MachineInstallProfile.spec.installer.anaconda` when the referenced
`MachineImage` is a boot ISO; omit it for a full DVD.

| Arm | Fields | Description |
| --- | --- | --- |
| `mirror` | `baseURL` (required, `http(s)`), `repositories[]` (`id` + `http(s)` `baseURL`) | Install from an HTTP(S) install tree you host. `baseURL` is the primary tree (BaseOS); `repositories` are additional (e.g. AppStream). |
| `fromSubscription` | `entitlementRef` (required) | Register against Red Hat's CDN over the named `redhat-rhel` `Entitlement`. The entitlement must keep `rhsm.management: managed` (the default) — install-time registration is the package source and cannot be delegated; `mirror` and `hostedTree` are the delegation-compatible sources. |
| `hostedTree` | `fromMedia` (required, `local-media:`/`file://`), `artifactServerEndpoint` | Bootwright extracts the DVD named by `fromMedia` once and serves it from the selected managed artifact server. `fromMedia` must be verifiable local media (staged via `bootwright media add`) and must differ from the referenced image's `spec.bootMedia`; `artifactServerEndpoint.endpointRef` must select an HTTP endpoint. |

!!! note "Registering against a corporate Satellite"
    A `fromSubscription` install registers against the public Red Hat CDN unless the
    referenced entitlement's `rhsm` arm carries a `satellite` block, in which
    case the install registers and pulls content from that Red Hat Satellite
    instead. No `MachineImage` change is needed — see
    [Secrets & entitlements › Corporate Satellite](secrets.md#corporate-satellite).

### Boot media vs DVD

A DVD ISO (~10 GB) bundles the installer and the BaseOS/AppStream repositories,
so Anaconda installs offline with a Kickstart `cdrom` source — set only
`bootMedia` and omit `installer.anaconda.packageSource` on the profile. A boot
ISO (~1 GB) carries only the installer, so the profile needs a `packageSource`:
Bootwright renders a `url --url=` (or RHSM) install source plus `repo` entries
instead of `cdrom`, and Anaconda fetches packages over the network during
install.

A boot ISO sourced from a package `mirror` points `baseURL` at a BaseOS install
tree and adds the AppStream repository — a RHEL install needs both:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-9-ceph-node
spec:
  os:
    family: rhel
    version: "9.7"
    architecture: x86_64
  installer:
    anaconda:
      imageRef: rhel-9-boot
      packageSource:
        mirror:
          baseURL: https://mirror.example.test/rhel/9/BaseOS/x86_64/os/
          repositories:
            - id: appstream
              baseURL: https://mirror.example.test/rhel/9/AppStream/x86_64/os/
```

A boot ISO from the Red Hat CDN (`fromSubscription`) references a `redhat-rhel`
[`Entitlement`](secrets.md#entitlements) (an RHSM organization plus
activation key); Anaconda registers the node and installs from the subscription
CDN:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-9-ceph-node
spec:
  os:
    family: rhel
    version: "9.7"
    architecture: x86_64
  installer:
    anaconda:
      imageRef: rhel-9-boot
      packageSource:
        fromSubscription:
          entitlementRef: rhel
```

A boot ISO with a **self-hosted install tree** (`hostedTree`) is the air-gapped,
no-mirror case: Bootwright extracts the DVD named by `fromMedia` once into the
selected artifact server's document root and the installing node fetches
GPG-signed packages from it, so the ~10&nbsp;GB payload lands on disk once per
`(cluster, image)` instead of inside every per-node ISO. Stage both the small
boot ISO (`bootMedia`) and the DVD (`fromMedia`) with `bootwright media add` —
the DVD is checksum-verified there — and bind
`hostedTree.artifactServerEndpoint` to an **http** listener (Anaconda verifies
TLS and would reject a self-signed artifact certificate; the DVD's own
`.treeinfo` advertises BaseOS and AppStream, so no `repositories` are needed):

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-9-ceph-node
spec:
  os:
    family: rhel
    version: "9.7"
    architecture: x86_64
  installer:
    anaconda:
      imageRef: rhel-9-boot
      packageSource:
        hostedTree:
          fromMedia: local-media:rhel-9.7-x86_64-dvd.iso
          artifactServerEndpoint:
            endpointRef: tree
```

A full DVD image needs no profile `packageSource` at all:

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
    small boot ISO with a profile `packageSource` (~1&nbsp;GB) over a full DVD
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
| `spec.installer.anaconda.packageSource` | No | None (⇒ full DVD) | Where Anaconda fetches packages when `imageRef` names a boot ISO. Omit for a full DVD image. |
| `spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint` | No | None | Selects the managed artifact-server endpoint that serves this profile's managed-OS boot ISO to the BMC over Redfish virtual media. `serverRef` may default from `Environment.spec.defaults.artifactServerRef`; `endpointRef` must resolve to a **managed** artifact server. |
| `spec.subscription.entitlementRef` | No | None | Post-install RHSM registration of the installed node: names the `redhat-rhel` `Entitlement` (registered as `managed`) the node's OS registers against after install. Must resolve to a `redhat-rhel` entitlement, and **cannot** be combined with `installer.anaconda.packageSource.fromSubscription` (which already registers the node during install). |
| `spec.customizations.hostname.source` | No | None | Currently `machineName`: the OS hostname becomes the machine's `fqdn` name. Valid only for machines not bound to any cluster — a cluster-bound node's OS hostname must equal its node FQDN. |
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
