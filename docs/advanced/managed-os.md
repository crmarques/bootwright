---
title: Managed OS installs
description: When Bootwright installs the node OS itself — MachineImage media, MachineInstallProfile customizations, staging media, and Anaconda over a proxy.
---

# Managed OS installs

Bootwright can install a machine's operating system before any cluster or storage
work runs — the path most commonly used for Ceph storage nodes, which need a
managed RHEL before cephadm. This page is the task how-to. The object model and
every field live on [Machines](../concepts/machines.md); the worked end-to-end
lab is `examples/ceph-ibm-libvirt-lab` (its `os/` subtree is the source for the
snippets below) and `examples/ceph-ibm-baremetal-redfish` for bare metal.

## When Bootwright installs the OS

`Machine.spec.os.provided` selects the mode:

- `os.provided: true` — the machine already runs a usable OS. Bootwright neither
  provisions a substrate nor installs an OS; it connects over `access.ssh` and
  does its work. `os.installProfileRef`, `os.install`, and `network.config` must
  all be empty.
- `os.provided: false` — the machine needs a substrate (`substrate.providerRef`).
  If `os.installProfileRef` is **also** set, Bootwright installs the OS itself.
  Otherwise the cluster's agent installer provisions it.

So a *managed OS install* is the `os.provided: false` + `os.installProfileRef`
combination: Bootwright drives Anaconda to lay down the OS, then takes the node
over SSH. A managed-install or OS-ready machine requires `access.ssh`. See the OS
mode rules on [Machines](../concepts/machines.md).

The install behavior splits across two reusable kinds:

- `MachineImage` — the bootable install media (which ISO, where packages come
  from, content trust).
- `MachineInstallProfile` — the Anaconda customizations (hostname, SSH, storage,
  packages, services, security).

A `Machine` ties them together: `os.installProfileRef` names the profile, and the
profile's `installer.anaconda.imageRef` names the image.

## MachineImage: install media

`MachineImage` describes the bootable media and where Anaconda fetches packages.

### Boot ISO vs DVD media

`spec.mediaType` is `boot` or `dvd`:

- A **DVD ISO** (~10 GB) bundles the installer and the BaseOS/AppStream
  repositories, so Anaconda installs offline with a Kickstart `cdrom` source and
  needs no `installSource`.
- A **boot ISO** (~1 GB) carries only the installer, so it **requires** an
  `installSource`: Bootwright renders a `url --url=` (or RHSM) install source plus
  `repo` entries instead of `cdrom`, and Anaconda fetches packages over the
  network during install.

`mediaType` auto-derives from the URL filename — `*boot.iso` becomes `boot`,
anything else `dvd` — but an authored value always wins, so a netinstall ISO not
named `*boot.iso` needs an explicit `mediaType: boot`. The DVD form from the lab
example:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-9-x86-64-dvd
spec:
  type: iso
  mediaType: dvd
  url: local-media:rhel-9.7-x86_64-dvd.iso
```

!!! tip "Disk footprint scales with media size × node count"
    Bootwright bakes each machine's Kickstart into its **own** install ISO
    (Redfish virtual-media boot cannot pass kernel arguments), so an N-node group
    costs about `N ×` the source media size in customized ISOs. The source ISO is
    staged once per `(cluster, image)`. This is the main reason to prefer a
    `mediaType: boot` source (~1 GB) over a full `dvd` (~10 GB) for groups.

### Install source: url vs redhatCDN

`installSource` (required for `mediaType: boot`) is a presence union; its `type`
is derived from the fields present when omitted.

For a **package mirror** (`type: url`), point `installSource.url` at a BaseOS
install tree and add the AppStream repository — a RHEL install needs both:

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

For the **Red Hat CDN** (`type: redhatCDN`), reference a `rhel` entitlement (an
RHSM organization plus activation key) declared in
`Environment.spec.entitlements`; Anaconda registers the node and installs from the
subscription CDN:

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

!!! note "Registering against a corporate Satellite"
    A `redhatCDN` install registers against the public Red Hat CDN unless the
    referenced entitlement's `rhsm` arm carries a `satellite` block, in which case
    the install registers and pulls content from that Red Hat Satellite instead.
    No `MachineImage` change is needed — see
    [Disconnected & proxied installs](disconnected-proxy.md).

### Checksum, trust, and headers

- `spec.checksum` — an optional `sha256:<hex>` content pin on the ISO.
- `spec.trustRefs[]` — `Environment` secrets holding CA bundles trusted when
  downloading the ISO.
- `spec.headersRefs[]` — `Environment` secrets holding extra HTTP headers sent
  when downloading the ISO.

`bootwright apply` preflight probes each `type: url` install tree's
`repodata/repomd.xml` before the machines phase: a server that answers without
yum metadata fails fast, while one the controller cannot reach only warns — the
install nodes, not the controller, are the authoritative fetcher.

## MachineInstallProfile: Anaconda customizations

`MachineInstallProfile` declares the OS identity and how Anaconda customizes the
install. The lab profile shows the common surface:

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
    type: anaconda
    anaconda:
      imageRef: rhel-9-x86-64-dvd
  customizations:
    ssh:
      authorizeMachineSSHKey: true
      passwordAuthentication: false
    storage:
      rootDevice:
        source: machineRootDeviceHints
      wipe: true
    packages:
      environment: minimal
      installWeakDeps: false
      excludeDocs: true
      languages:
        - en_US.UTF-8
      install:
        - podman
        - lvm2
        - chrony
        - firewalld
    services:
      enabled:
        - sshd
        - chronyd
        - firewalld
      disabled:
        - cockpit.socket
        - kdump
    security:
      selinux:
        mode: enforcing
      firewall:
        enabled: true
      fips:
        enabled: true
```

The customization arms, by area:

- **hostname** — `customizations.hostname.source`. The default sets each node's
  OS hostname to its FQDN; set `source: machineName` to keep the bare node name.
- **ssh** — `authorizeMachineSSHKey` authorizes the machine SSH key during
  install; `passwordAuthentication` enables or disables password SSH auth.
- **storage** — `rootDevice.source: machineRootDeviceHints` consumes the
  machine's root device hints (see below); `wipe` wipes the root device first.
- **packages** — `environment` (for example `minimal`), `install[]`, `languages[]`,
  `excludeDocs`, and the tri-state `installWeakDeps`.
- **services** — `enabled[]` and `disabled[]`.
- **security** — `selinux.mode` (`enforcing`/`permissive`/`disabled`),
  `firewall.enabled`, and `fips.enabled`.

!!! warning "Profile coupling rules"
    - A profile referenced by a managed-install machine must list `sshd` in
      `customizations.services.enabled`, or the referencing `Machine` fails
      validation.
    - `customizations.security.firewall.enabled: true` requires `firewalld` in
      **both** `customizations.packages.install` and
      `customizations.services.enabled`.
    - `customizations.security.fips.enabled: true` is RHEL-only (`os.family` must
      be `rhel`).

## Root device hints

Which disk Anaconda installs to is owned by the `Machine`, under
`spec.os.install.rootDeviceHints`. The profile's
`customizations.storage.rootDevice.source: machineRootDeviceHints` tells Anaconda
to honour them. Hints include `deviceName` (for example `/dev/sda`), `hctl`,
`model`, `vendor`, `serialNumber`, `minSizeGigabytes`, `wwn`, and `rotational` —
see [Machines](../concepts/machines.md). Set the hint on the machine, point the
profile at it, and the same profile drives nodes with different disk layouts.

## Staging media

A `local-media:<filename.iso>` URL resolves against the host-local media store at
`/var/lib/bootwright/media/`. Stage ISOs there with the `bootwright media`
subcommands before applying:

```bash
bootwright media add --name rhel-9.7-x86_64-boot.iso --from-file ./rhel-9.7-x86_64-boot.iso
bootwright media add --name rhel-9.7-x86_64-dvd.iso  --from-url http://mirror.example.test/rhel.iso --sha256 <hex>
bootwright media list
bootwright media remove --name rhel-9.7-x86_64-boot.iso --yes
```

`media add` takes exactly one of `--from-file` or `--from-url`, an optional
`--sha256` checksum, and `--force` to replace an existing entry. A `*boot.iso`
filename auto-derives `mediaType: boot` on any `MachineImage` that references it.
Besides `local-media:`, a `MachineImage.spec.url` may be a `file://` absolute
path or an `http(s)://` URL.

## Routing the Anaconda fetch through a proxy

When the install nodes reach the package source (a public mirror or the Red Hat
CDN) only through a forward proxy, set
`Environment.spec.proxyFor.machineOSInstall` to a declared **external** proxy.
Bootwright renders `--proxy=` onto the `rhsm`, `url`, and `repo` Kickstart
directives so Anaconda registers and fetches through it — useful for a
`type: redhatCDN` boot-ISO install on an estate with no internal mirror but a
corporate proxy. Each node brings up its network from its
[machine network config](../concepts/machines.md) before Anaconda fetches, so a
boot ISO needs no extra early-networking setup. For the full proxy-target model
and why `machineOSInstall` must be external, see
[Disconnected & proxied installs](disconnected-proxy.md).

## See also

- [Machines](../concepts/machines.md) — `Machine`, `MachineImage`, and
  `MachineInstallProfile` field reference.
- [Getting Started → Ceph](../getting-started/ceph.md) — the first managed-OS +
  Ceph apply path.
- [Disconnected & proxied installs](disconnected-proxy.md) — mirrors, Satellite
  redirect, and `proxyFor.machineOSInstall`.
