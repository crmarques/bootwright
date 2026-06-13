---
title: Machines And OS API
description: Machine, MachineImage, and MachineInstallProfile fields.
---

# Machines And OS

Machines own durable machine facts. Managed OS resources describe the media and
profile Bootwright uses when it installs an operating system before storage or
other machine-level work.

## Machine

`Machine` represents raw hardware, a virtual machine, an OS-ready machine, or a
machine whose OS Bootwright should install.

| Field | Required | Description |
| --- | --- | --- |
| `spec.capabilities[]` | No | Tags such as `openshift-node`, `ceph-node`, `ceph-admin`, `libvirt`, `load-balancer`, `proxy`, `name-resolution`, `ntp`, `registry`, or `artifact-server`. |
| `spec.substrate.providerRef` | Required when `os.provided: false` | Names the `InfraProvider` that supplies the machine substrate. |
| `spec.substrate.profileRef` | Provider-dependent | Names a provider `machineProfiles[]` entry for VM-like substrates. |
| `spec.hardware.nics[]` | Bare metal | Physical NIC inventory. |
| `spec.hardware.boot.nicRef` | Bare metal | Boot NIC name from `hardware.nics[]`. |
| `spec.hardware.management.bmc` | Bare metal | BMC access for Redfish virtual media. |
| `spec.os.provided` | Yes | `true` for OS-ready machines; `false` for machines Bootwright or the cluster installer provisions. |
| `spec.os.installProfileRef` | Managed OS | Names a `MachineInstallProfile` for Bootwright-managed OS install. |
| `spec.os.install.rootDeviceHints` | No | Root-device hints owned by the machine. |
| `spec.network.config` | No | Machine install network selection and overrides. |
| `spec.network.interfaceBinding[]` | No | Maps hardware NIC names to NMState interface names. |
| `spec.addresses[]` | No | Durable named addresses used by SSH, services, and endpoint resolution. |
| `spec.access.ssh` | Required for durable SSH targets | SSH address, user, key, and optional known-hosts material. |

### Hardware

| Field | Description |
| --- | --- |
| `hardware.nics[].name` | Local NIC name used by Bootwright references. |
| `hardware.nics[].macAddress` | Physical MAC address. |
| `hardware.boot.nicRef` | Name of the boot NIC. |
| `hardware.management.bmc.address` | Redfish BMC address, commonly `redfish-virtualmedia+https://...`. |
| `hardware.management.bmc.protocol` | Optional protocol override. |
| `hardware.management.bmc.credentialsRef` | Secret containing BMC credentials. |
| `hardware.management.bmc.disableCertificateVerification` | Lab-only TLS verification opt-out for BMC access. |

### OS

| Field | Description |
| --- | --- |
| `os.provided` | Required boolean. |
| `os.installProfileRef` | `MachineInstallProfile` name for managed OS install. |
| `os.install.rootDeviceHints.deviceName` | Device path such as `/dev/sda`. |
| `os.install.rootDeviceHints.hctl` | HCTL selector. |
| `os.install.rootDeviceHints.model` | Device model selector. |
| `os.install.rootDeviceHints.vendor` | Device vendor selector. |
| `os.install.rootDeviceHints.serialNumber` | Serial selector. |
| `os.install.rootDeviceHints.minSizeGigabytes` | Minimum device size. |
| `os.install.rootDeviceHints.wwn` | WWN selector. |
| `os.install.rootDeviceHints.rotational` | Rotational selector. |

### Network

| Field | Description |
| --- | --- |
| `network.config.networkConfigRef` | Names a reusable `NetworkConfig`. |
| `network.config.attachmentRef` | Names an `InfraProvider.spec.networkAttachments[]` entry. |
| `network.config.interfaceAddresses[].interface` | NMState interface receiving the static address. |
| `network.config.interfaceAddresses[].addressRef` | Name from `spec.addresses[]`. |
| `network.config.interfaceAddresses[].prefixLength` | Prefix length for the rendered address. |
| `network.config.interfaceAddresses[].family` | Optional address family; default is IPv4. |
| `network.config.overrides` | Additional NMState merged into the selected template. |
| `network.config.spec` | Inline one-off `NetworkConfig.spec`; mutually exclusive with `networkConfigRef`. |
| `network.interfaceBinding[].nicRef` | Name from `hardware.nics[]`. |
| `network.interfaceBinding[].interfaceName` | Effective NMState interface name. |

Set static install IPs in `spec.addresses[]` and reference them with
`interfaceAddresses[]`; do not duplicate the same IP into NMState overrides.

### Addresses And SSH

| Field | Description |
| --- | --- |
| `addresses[].name` | Local address name. |
| `addresses[].address` | IP address or DNS name. |
| `access.ssh.addressRef` | Address name used for SSH. |
| `access.ssh.user` | SSH user; renderer defaults depend on workflow. |
| `access.ssh.keyRef` | Secret containing private SSH key material. |
| `access.ssh.knownHostsRef` | Optional explicit known_hosts secret. |

## MachineImage

`MachineImage` describes bootable media for managed OS installation.

| Field | Required | Description |
| --- | --- | --- |
| `spec.type` | Yes | Currently `iso`. |
| `spec.mediaType` | No | `dvd` or `boot`; derived from the URL filename when omitted. |
| `spec.url` | Yes | `local-media:<filename.iso>`, `file://` absolute path, `http://`, or `https://`. |
| `spec.installSource` | Required for boot media | Package source for media that does not include packages. |
| `spec.checksum` | No | Optional `sha256:<hex>` content pin. |
| `spec.trustRefs[]` | No | CA bundle secrets trusted when downloading the ISO. |
| `spec.headersRefs[]` | No | Secrets containing extra HTTP headers for download. |

### Install Source

| Field | Description |
| --- | --- |
| `installSource.type` | `url` or `redhatCDN`; derived from present fields when omitted. |
| `installSource.url` | Primary Anaconda install tree for `type: url`. |
| `installSource.repositories[].id` | Additional Kickstart repository ID. |
| `installSource.repositories[].baseURL` | Repository base URL. |
| `installSource.entitlementRef` | Red Hat `rhel` entitlement for `type: redhatCDN`. |

When `type: url` omits `url`, the first repository `baseURL` is promoted to the
primary install tree during normalization.

## MachineInstallProfile

`MachineInstallProfile` declares how Bootwright installs and customizes an OS.

| Field | Required | Description |
| --- | --- | --- |
| `spec.os.family` | Yes | OS family, for example `rhel`. |
| `spec.os.version` | Yes | OS version string. |
| `spec.os.architecture` | Yes | Architecture such as `x86_64`. |
| `spec.installer.type` | Yes | Currently `anaconda`. |
| `spec.installer.anaconda.imageRef` | Yes for Anaconda | Names a `MachineImage`. |
| `spec.installer.anaconda.repositories[]` | No | Additional install repositories. |
| `spec.customizations.hostname.source` | No | Currently `machineName`. |
| `spec.customizations.ssh.authorizeMachineSSHKey` | No | Authorize the machine SSH key during install. |
| `spec.customizations.ssh.passwordAuthentication` | No | Enable or disable password SSH auth. |
| `spec.customizations.storage.rootDevice.source` | No | Currently `machineRootDeviceHints`. |
| `spec.customizations.storage.wipe` | No | Wipe selected root device before install. |
| `spec.customizations.packages.environment` | No | Currently `minimal`. |
| `spec.customizations.packages.install[]` | No | Packages to install. |
| `spec.customizations.packages.excludeDocs` | No | Render Kickstart `--excludedocs`. |
| `spec.customizations.packages.installWeakDeps` | No | Tri-state weak dependency setting. |
| `spec.customizations.packages.languages[]` | No | Installed language set. |
| `spec.customizations.services.enabled[]` | No | Services to enable. |
| `spec.customizations.services.disabled[]` | No | Services to disable. |
| `spec.customizations.security.selinux.mode` | No | `enforcing`, `permissive`, or `disabled`. |
| `spec.customizations.security.firewall.enabled` | No | Tri-state firewall control; explicit false disables. |
| `spec.customizations.security.fips.enabled` | No | `true` enables FIPS install configuration where supported. |
