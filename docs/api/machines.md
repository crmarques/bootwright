---
title: Machines And OS API
description: Machine, MachineImage, and MachineInstallProfile fields.
---

# Machines And OS

`Machine` owns durable machine facts: substrate binding, hardware and BMC
inventory, the OS mode, the install network, named addresses, and SSH access.
The managed-OS kinds describe the install media (`MachineImage`) and the install
behavior (`MachineInstallProfile`) Bootwright uses when it installs an operating
system before storage or other machine-level work.

Every kind on this page uses the shared
[object envelope](index.md#object-envelope) (`apiVersion: bootwright.io/v1alpha1`,
`kind`, `metadata.name`). The tables below describe only `spec`. Each table
follows the uniform convention: **Required** distinguishes author-required fields
from normalize-defaulted ones, and **Default** states the value normalize injects
when the field is omitted.

!!! note "Reusable network templates live on the Infrastructure page"
    A `Machine` references a `NetworkConfig` by name through
    `spec.network.config.networkConfigRef`. The `NetworkConfig` kind itself is
    documented on the [Infrastructure](infrastructure.md#networkconfig) page.

## Machine

`Machine` represents raw hardware, a virtual machine, an OS-ready machine, or a
machine whose OS Bootwright should install. `spec.os.provided` selects the mode
and drives most cross-field rules on this kind.

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
| `spec.access.ssh` | When `os.provided: true` or `os.installProfileRef` is set | None | SSH address, user, key, and optional known-hosts material. |

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
    `os.installProfileRef`, `os.install`, and `network.config` must all be empty,
    and `access.ssh` is required. Setting any of those install fields alongside
    `os.provided: true` is a validation error.

When `os.provided: false`, the machine needs a substrate
(`substrate.providerRef`). It is OS-installed by Bootwright when
`os.installProfileRef` is also set, and otherwise installed by the cluster
agent installer.

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
| `hardware.management.bmc.disableCertificateVerification` | No | `false` | Lab-only TLS verification opt-out for the control-node-to-BMC leg. |

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
name from `spec.addresses[]`) and `ssh.keyRef` are required whenever the block
is present.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `addresses[].name` | Yes (per entry) | None | Local address name. |
| `addresses[].address` | Yes (per entry) | None | IP address or DNS name. |
| `access.ssh.addressRef` | Yes (when `access.ssh` is set) | None | Address name used for SSH; must resolve to `spec.addresses[]`. |
| `access.ssh.user` | No | Workflow-dependent | SSH user. |
| `access.ssh.keyRef` | Yes (when `access.ssh` is set) | None | Secret containing the private SSH key material. |
| `access.ssh.knownHostsRef` | No | Context-managed SSH trust | Explicit `known_hosts` secret. |

## MachineImage

`MachineImage` describes bootable media for managed OS installation. Normalize
materializes every derivable field (`mediaType`, `installSource.type`, and the
`repositories[0]` install-tree promotion), so `render effective` shows exactly
what rendering consumes.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes | None | Currently `iso`. |
| `spec.mediaType` | No | `boot` for a URL filename ending `boot.iso`, otherwise `dvd` | `dvd` or `boot`. An authored value always wins; netinstall ISOs not named `*boot.iso` need an explicit `boot`. |
| `spec.url` | Yes | None | `local-media:<filename.iso>`, a `file://` absolute path, `http://`, or `https://`. |
| `spec.installSource` | Required when `mediaType: boot` | None | Package source for media that carries no packages. |
| `spec.checksum` | No | None | Optional `sha256:<hex>` content pin. |
| `spec.trustRefs[]` | No | None | `Environment` secrets holding CA bundles trusted when downloading the ISO. |
| `spec.headersRefs[]` | No | None | `Environment` secrets holding extra HTTP headers sent when downloading the ISO. |

### Install source

`installSource` is a presence union: its `type` is derived from the fields
present when omitted (`entitlementRef` means `redhatCDN`; `url` or
`repositories` mean `url`).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `installSource.type` | No | Derived from present fields | `url` or `redhatCDN`. |
| `installSource.url` | One of `url` or `repositories` for `type: url` | None | Primary Anaconda install tree; must be `http(s)`. Must be empty for `type: redhatCDN`. |
| `installSource.repositories[].id` | Yes (per entry) | None | Additional Kickstart repository ID. |
| `installSource.repositories[].baseURL` | Yes (per entry) | None | Repository base URL; must be `http(s)`. |
| `installSource.entitlementRef` | Required for `type: redhatCDN` | None | `Environment.spec.entitlements[]` `rhel` entitlement. Must be empty for `type: url`. |

!!! note "Type-specific exclusivity"
    For `type: url`, `entitlementRef` must be empty and at least one of `url` or
    `repositories` is required. When `url` is omitted, normalize promotes
    `repositories[0].baseURL` to the primary install tree. For `type: redhatCDN`,
    `url` and `repositories` must be empty and `entitlementRef` is required.

### Installing from a boot ISO

A DVD ISO (~10 GB) bundles the installer and the BaseOS/AppStream package
repositories, so Anaconda installs offline with a Kickstart `cdrom` source and
needs no `installSource`. A boot ISO (~1 GB) carries only the installer, so it
**requires** an `installSource`: Bootwright renders a `url --url=` (or RHSM)
install source plus `repo` entries instead of `cdrom`, and Anaconda fetches
packages over the network during install. Stage either ISO the same way:

```bash
bootwright media add rhel-9.7-x86_64-boot.iso --from-file ./rhel-9.7-x86_64-boot.iso
```

A `*boot.iso` filename auto-derives `mediaType: boot`; a netinstall ISO named
otherwise needs an explicit `mediaType: boot`.

#### From a package mirror (`type: url`)

Point `installSource.url` at a BaseOS install tree and add the AppStream
repository — a RHEL install needs both:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-9-boot
spec:
  type: iso
  mediaType: boot
  url: local-media:rhel-9.7-x86_64-boot.iso
  installSource:
    type: url
    url: https://mirror.example.test/rhel/9/BaseOS/x86_64/os/
    repositories:
      - id: appstream
        baseURL: https://mirror.example.test/rhel/9/AppStream/x86_64/os/
```

#### From the Red Hat CDN (`type: redhatCDN`)

Reference a `rhel` entitlement (an RHSM organization plus activation key)
declared in [`Environment.spec.entitlements`](environment.md); Anaconda
registers the node and installs from the subscription CDN:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-9-boot-cdn
spec:
  type: iso
  mediaType: boot
  url: local-media:rhel-9.7-x86_64-boot.iso
  installSource:
    type: redhatCDN
    entitlementRef: rhel
```

`bootwright apply` preflight probes each `type: url` install tree's
`repodata/repomd.xml` before the machines phase: a server that answers without
yum metadata fails fast, while one the controller cannot reach only warns — the
install nodes, not the controller, are the authoritative fetcher. Each node
brings up its network from its [machine network config](#machine) (the rendered
Kickstart `network` directive) before Anaconda fetches packages, so a boot ISO
needs no extra early-networking setup. For a fully disconnected install, point
`installSource` at an internal mirror; Bootwright serves the ISO over its
artifact server but never the package tree.

## MachineInstallProfile

`MachineInstallProfile` declares how Bootwright installs and customizes an OS
through Anaconda.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.os.family` | Yes | None | OS family, for example `rhel`. |
| `spec.os.version` | Yes | None | OS version string. |
| `spec.os.architecture` | Yes | None | Architecture such as `x86_64`. |
| `spec.installer.type` | Yes | None | Currently `anaconda`. |
| `spec.installer.anaconda` | Required when `installer.type: anaconda` | None | Anaconda installer block. |
| `spec.installer.anaconda.imageRef` | Yes | None | Names a `MachineImage`. |
| `spec.installer.anaconda.repositories[]` | No | None | Additional install repositories; each requires `id` and an `http(s)` `baseURL`. |
| `spec.customizations.hostname.source` | No | None | Currently `machineName`. |
| `spec.customizations.ssh.authorizeMachineSSHKey` | No | `false` | Authorize the machine SSH key during install. |
| `spec.customizations.ssh.passwordAuthentication` | No | `false` | Enable or disable password SSH auth. |
| `spec.customizations.storage.rootDevice.source` | No | None | Currently `machineRootDeviceHints`. |
| `spec.customizations.storage.wipe` | No | `false` | Wipe the selected root device before install. |
| `spec.customizations.packages.environment` | No | None | Currently `minimal`. |
| `spec.customizations.packages.install[]` | No | None | Packages to install. |
| `spec.customizations.packages.excludeDocs` | No | `false` | Render Kickstart `--excludedocs`. |
| `spec.customizations.packages.installWeakDeps` | No | OS default | Tri-state weak dependency setting. |
| `spec.customizations.packages.languages[]` | No | None | Installed language set. |
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
